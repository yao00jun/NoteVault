package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// 异步任务框架（E-5）
// ---------------------------------------------------------------------------
//
// 背景：导入、AI 总结、索引重建目前都是同步阻塞调用。
// QnA 的 HTTP 客户端超时是 120 秒，意味着前端点下去之后，
// 整个 Wails 调用会一直挂着——用户看不到进度、也取消不了，
// 只能等它自己结束或者关掉应用。
//
// 框架提供三件事：
//  1. 可取消——每个任务拿到自己的 context，Cancel 时取消它；
//  2. 带进度——任务体通过 TaskReporter 回报进度，框架节流后推送给前端；
//  3. 有事件——任务开始 / 进度 / 结束都推 Wails 事件，前端可以订阅做进度条。
//
// 刻意不做的事：
//  - 不做任务持久化。任务是进程内的短时概念，重启后没有意义，
//    落盘反而要在崩溃时处理"僵尸任务"。
//  - 不做重试 / 调度。这里解决的是"当前这一次长操作的用户体验"，
//    不是后台作业系统。

// 任务事件名。前端用 EventsOn 订阅。
const (
	EventTaskStarted  = "task:started"
	EventTaskProgress = "task:progress"
	EventTaskFinished = "task:finished"
)

// TaskEmitter 把任务状态变化推给前端。
//
// 抽象成接口而不是直接调 application.Event.Emit，是因为后者依赖全局的
// globalApplication——单元测试里它是 nil，一调用就 panic。
// 把推送收窄成一个接口，任务框架才能在纯 Go 环境下完整测试。
type TaskEmitter interface {
	Emit(eventName string, data any)
}

// nopTaskEmitter 丢弃所有事件。用于未注入 emitter 的零值实例与测试。
type nopTaskEmitter struct{}

func (nopTaskEmitter) Emit(string, any) {}

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"   // 已提交，等待并发名额
	TaskRunning   TaskStatus = "running"   // 正在执行
	TaskSucceeded TaskStatus = "succeeded" // 正常完成
	TaskFailed    TaskStatus = "failed"    // 返回错误
	TaskCancelled TaskStatus = "cancelled" // 被用户取消
)

// TaskInfo 是任务的对外快照（会被 Wails 序列化成前端 TS 类型）。
type TaskInfo struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Status    TaskStatus `json:"status"`
	Done      int        `json:"done"`
	Total     int        `json:"total"`
	Percent   int        `json:"percent"`
	Message   string     `json:"message"`
	Error     string     `json:"error,omitempty"`
	StartedAt string     `json:"startedAt"`
	EndedAt   string     `json:"endedAt,omitempty"`
}

// progressEmitInterval 进度事件的最小推送间隔。
//
// 任务体可能每个文件都报一次进度；全量导入一个大库是几千次调用。
// 不节流的话，事件推送本身会成为瓶颈（每次都要序列化 + 跨进程 IPC）。
// 终态（完成 / 失败 / 取消）不受此限制，一定会推送完整结果。
const progressEmitInterval = 100 * time.Millisecond

// taskEntry 是任务的内部表示。
type taskEntry struct {
	info TaskInfo

	mu         sync.Mutex
	cancel     context.CancelFunc
	finished   bool
	lastEmitAt time.Time
	lastPct    int

	// done 在任务进入终态时关闭，供 TaskHandle.Done() 等待。
	done chan struct{}
	// err 只在 done 关闭后读取，无需额外同步。
	err error
}

// percentLocked 计算百分比。total<=0 时无法计算，返回 -1 表示"不确定"。
func percentLocked(done, total int) int {
	if total <= 0 {
		return -1
	}
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	return done * 100 / total
}

// TaskReporter 是任务函数回报进度的唯一出口。
//
// 它是值类型、可安全拷贝：所有可变状态都在 taskEntry 里由互斥锁保护。
type TaskReporter struct {
	entry *taskEntry
	svc   *TaskService
}

// Report 回报已完成数量与总量。total<=0 表示总量未知，此时只更新 Message。
func (r TaskReporter) Report(done, total int) {
	r.ReportMessage(done, total, "")
}

// ReportMessage 回报进度，同时更新一条给用户看的说明。
func (r TaskReporter) ReportMessage(done, total int, message string) {
	if r.entry == nil {
		return
	}
	now := time.Now()

	r.entry.mu.Lock()
	r.entry.info.Done = done
	r.entry.info.Total = total
	r.entry.info.Percent = percentLocked(done, total)
	if message != "" {
		r.entry.info.Message = message
	}
	pct := r.entry.info.Percent

	// 节流：百分比没变或距上次推送太近就跳过。
	// 终态事件一定会推，所以这里丢帧不会导致前端停在 99%。
	shouldEmit := pct != r.entry.lastPct && now.Sub(r.entry.lastEmitAt) >= progressEmitInterval
	if shouldEmit {
		r.entry.lastPct = pct
		r.entry.lastEmitAt = now
	}
	snapshot := r.entry.info
	r.entry.mu.Unlock()

	if shouldEmit && r.svc != nil {
		r.svc.emit(EventTaskProgress, &snapshot)
	}
}

// Message 只更新说明文字，不动进度数字。
func (r TaskReporter) Message(message string) {
	if r.entry == nil {
		return
	}
	r.entry.mu.Lock()
	r.entry.info.Message = message
	snapshot := r.entry.info
	r.entry.mu.Unlock()
	if r.svc != nil {
		r.svc.emit(EventTaskProgress, &snapshot)
	}
}

// TaskFunc 是任务体的签名。
//
// ctx 在用户取消时会被取消——任务体应当在合理的检查点响应它。
// 框架不会强杀 goroutine（Go 做不到），所以长期循环的任务必须自己看 ctx.Done()。
type TaskFunc func(ctx context.Context, rep TaskReporter) error

// TaskService 管理进程内的异步任务。
type TaskService struct {
	mu    sync.RWMutex
	tasks map[string]*taskEntry

	emitter TaskEmitter
	// sem 是并发名额信号量，容量 = maxConcurrent。
	// 限制并发的理由：AI 调用与全量索引重建都是重活，放任并发会把 IO 打满，
	// 让"看起来在后台跑"变成"整个应用卡住"。
	//
	// 用 channel 而不是计数 + 忙等待，是因为 channel 的"获取/归还"是一对
	// 天然配对的操作，配合 defer 归还不会漏；计数方案要自己在每条退出路径
	// 上记得减回去，与取消路径交织时容易出错。
	sem chan struct{}
	// maxHistory 保留多少个已结束任务的记录。
	// 保留少量历史是为了让前端在事件到达前也能查到结果（事件可能丢失），
	// 但不设上限会让长时间运行的进程无限累积。
	maxHistory int

	seq atomic.Uint64
	wg  sync.WaitGroup
}

// NewTaskService 创建任务服务。emitter 为 nil 时丢弃所有事件（零值可用、测试友好）。
func NewTaskService(emitter TaskEmitter) *TaskService {
	return NewTaskServiceWithOptions(emitter, 4, 20)
}

// NewTaskServiceWithOptions 创建任务服务并指定并发上限与历史保留条数。
// 非正值回落到默认值，保证调用方传 0 也能正常工作。
func NewTaskServiceWithOptions(emitter TaskEmitter, maxConcurrent, maxHistory int) *TaskService {
	if emitter == nil {
		emitter = nopTaskEmitter{}
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	if maxHistory <= 0 {
		maxHistory = 20
	}
	return &TaskService{
		tasks:      make(map[string]*taskEntry),
		emitter:    emitter,
		sem:        make(chan struct{}, maxConcurrent),
		maxHistory: maxHistory,
	}
}

// emit 推送事件。emitter 永远非 nil（构造时兜底），这里仍防御一次是为了
// 让零值 TaskService{} 也不 panic —— 桌面应用里 panic 的代价远高于一次空判断。
func (s *TaskService) emit(name string, data any) {
	if s == nil || s.emitter == nil {
		return
	}
	s.emitter.Emit(name, data)
}

// nextID 生成任务 ID。用递增序号而不是 UUID：ID 只用于进程内识别，
// 但递增序号能让日志与前端列表按提交顺序稳定排列，排查问题时有用。
func (s *TaskService) nextID() string {
	return fmt.Sprintf("task-%d", s.seq.Add(1))
}

// ---------------------------------------------------------------------------
// 包内提交接口（非导出：任务体是 Go 函数，不能也不会暴露给前端）
// ---------------------------------------------------------------------------

// submit 提交一个任务并立即返回句柄。
//
// 非导出的原因：TaskFunc 是函数类型，导出会被 Wails 反射成前端 API，
// 生成的 TS 签名既无意义又脆弱。任务只能由包内其他 Service 发起。
func (s *TaskService) submit(ctx context.Context, name string, fn TaskFunc) *TaskHandle {
	if ctx == nil {
		ctx = context.Background()
	}
	taskCtx, cancel := context.WithCancel(ctx)

	entry := &taskEntry{
		info: TaskInfo{
			ID:        s.nextID(),
			Name:      name,
			Status:    TaskPending,
			Percent:   -1,
			StartedAt: time.Now().Format(time.RFC3339),
		},
		cancel: cancel,
		done:   make(chan struct{}),
	}

	s.mu.Lock()
	s.tasks[entry.info.ID] = entry
	s.mu.Unlock()

	s.emit(EventTaskStarted, entry.snapshot())

	s.wg.Add(1)
	go s.run(taskCtx, entry, fn)

	return &TaskHandle{id: entry.info.ID, entry: entry}
}

// run 执行任务体并处理终态。
func (s *TaskService) run(ctx context.Context, entry *taskEntry, fn TaskFunc) {
	defer s.wg.Done()

	// 取并发名额；排队期间也响应取消——
	// 用户取消了排队中的任务，它就不该再占名额执行。
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }() // 归还名额：唯一出口，不会漏
	case <-ctx.Done():
		s.finish(entry, TaskCancelled, ctx.Err())
		return
	}

	// 排队期间可能已被取消（拿到名额之前 ctx 就关了），再确认一次，
	// 避免"用户取消了但任务还是跑了一遍"。
	if ctx.Err() != nil {
		s.finish(entry, TaskCancelled, ctx.Err())
		return
	}

	s.transition(entry, TaskRunning)

	rep := TaskReporter{entry: entry, svc: s}
	err := fn(ctx, rep)

	switch {
	case err == nil:
		s.finish(entry, TaskSucceeded, nil)
	case ctx.Err() != nil:
		// 任务是因为取消而结束的：无论它返回什么错，都归为 cancelled，
		// 否则用户点了取消却看到"导入失败"，会以为操作真的出了问题。
		s.finish(entry, TaskCancelled, err)
	default:
		s.finish(entry, TaskFailed, err)
	}
}

// transition 把任务切到 running（仅内部状态，不推事件——started 已推过）。
func (s *TaskService) transition(entry *taskEntry, status TaskStatus) {
	entry.mu.Lock()
	entry.info.Status = status
	entry.mu.Unlock()
}

// finish 写终态、推事件、关闭 done 通道，并按需清理历史。
func (s *TaskService) finish(entry *taskEntry, status TaskStatus, err error) {
	entry.mu.Lock()
	if entry.finished {
		entry.mu.Unlock()
		return
	}
	entry.finished = true
	entry.info.Status = status
	entry.info.EndedAt = time.Now().Format(time.RFC3339)
	// 成功终态一律钉到 100%：任务确实做完了，
	// 若沿用最后一次节流的中间值（比如 50%），前端会出现"完成但进度条一半"。
	// 失败 / 取消则保留真实进度——用户需要知道停在哪一步。
	if status == TaskSucceeded {
		entry.info.Percent = 100
	}
	if err != nil {
		entry.info.Error = err.Error()
	}
	entry.err = err
	snapshot := entry.info
	entry.mu.Unlock()

	close(entry.done)
	s.emit(EventTaskFinished, &snapshot)
	s.pruneHistory()
}

// pruneHistory 清理过多的已结束任务。
//
// 只删已结束的，运行中的任务永远保留——删掉它们会让前端查不到正在跑的任务，
// 用户就失去了取消的入口。
func (s *TaskService) pruneHistory() {
	s.mu.Lock()
	defer s.mu.Unlock()

	var finished []*taskEntry
	for _, e := range s.tasks {
		e.mu.Lock()
		isFinished := e.finished
		e.mu.Unlock()
		if isFinished {
			finished = append(finished, e)
		}
	}
	if len(finished) <= s.maxHistory {
		return
	}

	// 按开始时间排序，删最老的。
	sort.Slice(finished, func(i, j int) bool {
		return finished[i].info.StartedAt < finished[j].info.StartedAt
	})
	for _, e := range finished[:len(finished)-s.maxHistory] {
		delete(s.tasks, e.info.ID)
	}
}

// ---------------------------------------------------------------------------
// 导出接口（Wails 前端 API）
// ---------------------------------------------------------------------------

// ListTasks 返回全部任务快照，按提交顺序排列。
func (s *TaskService) ListTasks() []*TaskInfo {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	entries := make([]*taskEntry, 0, len(s.tasks))
	for _, e := range s.tasks {
		entries = append(entries, e)
	}
	s.mu.RUnlock()

	out := make([]*TaskInfo, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.snapshot())
	}
	sort.Slice(out, func(i, j int) bool {
		// ID 是递增序号，按数值排序而不是字典序（"task-10" < "task-9" 是错的）
		return taskSeq(out[i].ID) < taskSeq(out[j].ID)
	})
	return out
}

// GetTask 查询单个任务；不存在返回 nil。
func (s *TaskService) GetTask(taskID string) *TaskInfo {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	entry, ok := s.tasks[taskID]
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	return entry.snapshot()
}

// Cancel 取消一个任务。
//
// 返回 true 只表示"取消信号已发出"，不代表任务已经停下——
// 任务体需要在自己的检查点响应 ctx.Done()。这是 Go 取消语义的固有约束。
// 对已结束的任务调用返回 false（没什么可取消的）。
func (s *TaskService) Cancel(taskID string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	entry, ok := s.tasks[taskID]
	s.mu.RUnlock()
	if !ok {
		return false
	}

	entry.mu.Lock()
	if entry.finished {
		entry.mu.Unlock()
		return false
	}
	cancel := entry.cancel
	entry.mu.Unlock()

	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// CancelAll 取消所有未结束的任务，返回实际发出取消信号的数量。
//
// 用于"关闭工作区 / 退出应用"这类场景：没必要让后台任务继续跑，
// 它们操作的目标马上就不存在了。
func (s *TaskService) CancelAll() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	entries := make([]*taskEntry, 0, len(s.tasks))
	for _, e := range s.tasks {
		entries = append(entries, e)
	}
	s.mu.RUnlock()

	n := 0
	for _, e := range entries {
		e.mu.Lock()
		finished := e.finished
		cancel := e.cancel
		e.mu.Unlock()
		if finished || cancel == nil {
			continue
		}
		cancel()
		n++
	}
	return n
}

// Wait 等待所有任务结束。主要用于测试与优雅退出。
func (s *TaskService) Wait() {
	if s == nil {
		return
	}
	s.wg.Wait()
}

// ---------------------------------------------------------------------------
// TaskHandle
// ---------------------------------------------------------------------------

// TaskHandle 是提交方持有的任务句柄。
//
// 有了它，调用方可以选择同步等待（测试、或"必须拿到结果才能继续"的流程），
// 而不必依赖事件。事件是给前端用的，句柄是给 Go 调用方用的。
type TaskHandle struct {
	id    string
	entry *taskEntry
}

// ID 返回任务 ID，可用于 Cancel / GetTask。
func (h *TaskHandle) ID() string {
	if h == nil {
		return ""
	}
	return h.id
}

// Done 返回一个在任务进入终态时关闭的通道。
func (h *TaskHandle) Done() <-chan struct{} {
	if h == nil || h.entry == nil {
		return nil
	}
	return h.entry.done
}

// Err 返回任务的最终错误。在 Done() 关闭前调用结果不确定。
func (h *TaskHandle) Err() error {
	if h == nil || h.entry == nil {
		return nil
	}
	select {
	case <-h.entry.done:
		return h.entry.err
	default:
		return nil
	}
}

// taskSeq 从任务 ID 里取出数字序号（"task-12" → 12）。
//
// ID 是递增序号，但按字符串排会把 "task-10" 排在 "task-9" 前面，
// 前端任务列表的顺序就乱了，所以必须按数值排。
// 解析失败返回 0（排到最前）——ID 格式由本包生成，走到这里说明有人手改过。
func taskSeq(id string) uint64 {
	i := strings.LastIndexByte(id, '-')
	if i < 0 || i == len(id)-1 {
		return 0
	}
	n, err := strconv.ParseUint(id[i+1:], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// snapshot 拷贝一份任务快照（持锁）。
func (e *taskEntry) snapshot() *TaskInfo {
	e.mu.Lock()
	defer e.mu.Unlock()
	cp := e.info
	return &cp
}
