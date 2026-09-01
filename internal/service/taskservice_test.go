package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// recordingEmitter 记录所有收到的事件，用于断言推送行为。
type recordingEmitter struct {
	mu     sync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	Name string
	Data *TaskInfo
}

func (r *recordingEmitter) Emit(name string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if info, ok := data.(*TaskInfo); ok {
		cp := *info
		r.events = append(r.events, recordedEvent{Name: name, Data: &cp})
		return
	}
	r.events = append(r.events, recordedEvent{Name: name})
}

func (r *recordingEmitter) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e.Name == name {
			n++
		}
	}
	return n
}

func (r *recordingEmitter) last(name string) *TaskInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i].Name == name {
			return r.events[i].Data
		}
	}
	return nil
}

func (r *recordingEmitter) all() []recordedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedEvent, len(r.events))
	copy(out, r.events)
	return out
}

// waitDone 等待任务进入终态，避免测试里的 sleep 竞态。
func waitDone(t *testing.T, h *TaskHandle) {
	t.Helper()
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("task did not finish within 5s")
	}
}

func TestTaskService_SubmitRunsAndSucceeds(t *testing.T) {
	em := &recordingEmitter{}
	svc := NewTaskService(em)

	h := svc.submit(context.Background(), "索引重建", func(ctx context.Context, rep TaskReporter) error {
		rep.Report(1, 2)
		rep.Report(2, 2)
		return nil
	})
	waitDone(t, h)

	if err := h.Err(); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if got := svc.GetTask(h.ID()); got == nil || got.Status != TaskSucceeded {
		t.Fatalf("expected status=succeeded, got %+v", got)
	}
	// 终态事件必须带完整进度，不能因为节流停在某个中间值
	finished := em.last(EventTaskFinished)
	if finished == nil {
		t.Fatal("no task:finished event emitted")
	}
	if finished.Percent != 100 {
		t.Errorf("finished event should carry percent=100, got %d", finished.Percent)
	}
}

func TestTaskService_FailureCarriesError(t *testing.T) {
	svc := NewTaskService(nil)
	boom := errors.New("boom")

	h := svc.submit(context.Background(), "导入", func(ctx context.Context, rep TaskReporter) error {
		return boom
	})
	waitDone(t, h)

	if !errors.Is(h.Err(), boom) {
		t.Fatalf("expected the original error, got %v", h.Err())
	}
	if got := svc.GetTask(h.ID()); got == nil || got.Status != TaskFailed || got.Error != "boom" {
		t.Fatalf("expected failed status with error text, got %+v", got)
	}
}

// TestTaskService_CancelStopsLongTask 取消是 E-5 的核心能力：
// 导入一个大库时用户应当能中途叫停，而不是只能等它跑完。
func TestTaskService_CancelStopsLongTask(t *testing.T) {
	svc := NewTaskService(nil)
	started := make(chan struct{})

	h := svc.submit(context.Background(), "长任务", func(ctx context.Context, rep TaskReporter) error {
		close(started)
		// 模拟长循环：每个检查点都看一眼 ctx
		for i := 0; i < 1000; i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Millisecond):
				rep.Report(i, 1000)
			}
		}
		return nil
	})

	<-started
	if !svc.Cancel(h.ID()) {
		t.Fatal("Cancel should return true for a running task")
	}
	waitDone(t, h)

	info := svc.GetTask(h.ID())
	if info.Status != TaskCancelled {
		t.Fatalf("expected status=cancelled, got %s (err=%s)", info.Status, info.Error)
	}
	// 取消后再次取消应返回 false——已经结束了，没什么可取消的
	if svc.Cancel(h.ID()) {
		t.Error("cancelling a finished task should return false")
	}
}

// TestTaskService_CancelQueuedTask 排队中的任务被取消后不应再执行。
// 这条锁住的是"取消信号要在取名额之前生效"，否则用户取消了却还是跑了一遍。
func TestTaskService_CancelQueuedTask(t *testing.T) {
	svc := NewTaskServiceWithOptions(nil, 1, 20) // 只允许 1 个并发

	firstStarted := make(chan struct{})
	release := make(chan struct{})
	h1 := svc.submit(context.Background(), "占坑", func(ctx context.Context, rep TaskReporter) error {
		close(firstStarted)
		<-release
		return nil
	})

	<-firstStarted // 第一个任务已开跑，占住唯一的名额

	secondRan := make(chan struct{})
	h2 := svc.submit(context.Background(), "排队", func(ctx context.Context, rep TaskReporter) error {
		close(secondRan)
		return nil
	})

	if !svc.Cancel(h2.ID()) {
		t.Fatal("queued task should be cancellable")
	}
	close(release)
	waitDone(t, h1)
	waitDone(t, h2)

	if svc.GetTask(h2.ID()).Status != TaskCancelled {
		t.Fatalf("queued task should end as cancelled, got %s", svc.GetTask(h2.ID()).Status)
	}
	select {
	case <-secondRan:
		t.Fatal("cancelled queued task must not execute")
	default:
	}
}

// TestTaskService_ProgressIsThrottled 进度必须节流。
// 不节流的话，导入 5000 个文件会推 5000 次事件，IPC 开销反而成为瓶颈。
func TestTaskService_ProgressIsThrottled(t *testing.T) {
	em := &recordingEmitter{}
	svc := NewTaskService(em)

	// 模拟真实的长任务：300 次汇报，每次 5ms —— 总共约 1.5 秒。
	// 若任务在几十毫秒内就跑完，本来也没几帧可推，测不出节流效果。
	h := svc.submit(context.Background(), "刷进度", func(ctx context.Context, rep TaskReporter) error {
		for i := 0; i < 300; i++ {
			rep.Report(i, 300)
			time.Sleep(5 * time.Millisecond)
		}
		return nil
	})
	waitDone(t, h)

	// 300 次 Report 跨越约 1.5 秒，按 100ms 节流上限约 15 帧。
	// 断言「远少于 300」而不是精确值：CI 机器快慢不同，精确断言必然 flaky。
	if n := em.count(EventTaskProgress); n > 60 {
		t.Errorf("progress events were not throttled: %d events for 300 reports", n)
	} else if n < 2 {
		t.Errorf("progress events were over-throttled: only %d events", n)
	}
}

// TestTaskService_FastTaskStillReportsCompletion 极快的任务也要有正确的终态进度。
// 若沿用最后一次节流的中间值，前端会停在"完成但 50%"。
func TestTaskService_FastTaskStillReportsCompletion(t *testing.T) {
	em := &recordingEmitter{}
	svc := NewTaskService(em)

	h := svc.submit(context.Background(), "快任务", func(ctx context.Context, rep TaskReporter) error {
		rep.Report(1, 100) // 只报了一次 1%
		return nil
	})
	waitDone(t, h)

	finished := em.last(EventTaskFinished)
	if finished == nil {
		t.Fatal("no finished event")
	}
	if finished.Percent != 100 {
		t.Errorf("successful task must report 100%%, got %d", finished.Percent)
	}
}

// TestTaskService_RespectsConcurrencyLimit 并发上限要真的生效，
// 否则"后台任务"会把前台操作一起拖慢。
func TestTaskService_RespectsConcurrencyLimit(t *testing.T) {
	const limit = 3
	svc := NewTaskServiceWithOptions(nil, limit, 100)

	var cur, peak int32
	var mu sync.Mutex

	for i := 0; i < 12; i++ {
		svc.submit(context.Background(), "并发", func(ctx context.Context, rep TaskReporter) error {
			mu.Lock()
			cur++
			if cur > peak {
				peak = cur
			}
			mu.Unlock()

			time.Sleep(20 * time.Millisecond)

			mu.Lock()
			cur--
			mu.Unlock()
			return nil
		})
	}
	svc.Wait()

	mu.Lock()
	got := peak
	mu.Unlock()
	if got > limit {
		t.Errorf("concurrency limit breached: peak=%d, limit=%d", got, limit)
	}
}

// TestTaskService_PrunesFinishedHistory 已完成任务要限量保留：
// 不清理会让长跑进程无限累积任务记录。
func TestTaskService_PrunesFinishedHistory(t *testing.T) {
	svc := NewTaskServiceWithOptions(nil, 4, 5)

	for i := 0; i < 20; i++ {
		h := svc.submit(context.Background(), "短任务", func(ctx context.Context, rep TaskReporter) error {
			return nil
		})
		waitDone(t, h)
	}
	// prune 在 finish 里同步执行，Wait 确保所有任务都已收尾
	svc.Wait()

	if n := len(svc.ListTasks()); n > 5 {
		t.Errorf("finished history should be pruned to <=5, got %d", n)
	}
}

// TestTaskService_ListTasksOrderedBySubmission 列表要按提交顺序排。
// ID 是递增序号，用字典序排会让 task-10 跑到 task-9 前面。
func TestTaskService_ListTasksOrderedBySubmission(t *testing.T) {
	svc := NewTaskServiceWithOptions(nil, 1, 100) // 串行，保证完成顺序 = 提交顺序

	var ids []string
	for i := 0; i < 12; i++ {
		h := svc.submit(context.Background(), "顺序", func(ctx context.Context, rep TaskReporter) error {
			return nil
		})
		ids = append(ids, h.ID())
		waitDone(t, h)
	}

	list := svc.ListTasks()
	if len(list) != len(ids) {
		t.Fatalf("expected %d tasks, got %d", len(ids), len(list))
	}
	for i, info := range list {
		if info.ID != ids[i] {
			t.Fatalf("task %d: got %s, want %s (list must follow submission order)", i, info.ID, ids[i])
		}
	}
}

// TestTaskService_CancelAll 批量取消用于"切换工作区 / 退出应用"，
// 返回实际发出信号的数量便于前端提示。
func TestTaskService_CancelAll(t *testing.T) {
	svc := NewTaskServiceWithOptions(nil, 8, 100)

	block := make(chan struct{})
	for i := 0; i < 4; i++ {
		svc.submit(context.Background(), "可取消", func(ctx context.Context, rep TaskReporter) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-block:
				return nil
			}
		})
	}
	// 已完成的任务不该被计入取消数
	h := svc.submit(context.Background(), "已完成", func(ctx context.Context, rep TaskReporter) error {
		return nil
	})
	waitDone(t, h)

	// 等 4 个任务都进入 running（否则可能还在 pending，语义上也能取消，但不稳）
	time.Sleep(50 * time.Millisecond)

	if n := svc.CancelAll(); n != 4 {
		t.Errorf("CancelAll should report 4 cancellations, got %d", n)
	}
	close(block)
	svc.Wait()

	for _, info := range svc.ListTasks() {
		if info.Name == "可取消" && info.Status != TaskCancelled {
			t.Errorf("task %s should be cancelled, got %s", info.ID, info.Status)
		}
	}
}

// TestTaskService_ZeroValueDoesNotPanic 零值实例不该 panic。
// 桌面应用里 panic 会直接崩掉整个进程，兜底比"快速失败"更合适。
func TestTaskService_ZeroValueDoesNotPanic(t *testing.T) {
	var svc TaskService
	// 返回空切片而非 nil：JSON 里 [] 比 null 好用，前端不必额外判空。
	if got := svc.ListTasks(); len(got) != 0 {
		t.Errorf("zero-value ListTasks should be empty, got %v", got)
	}
	if got := svc.GetTask("nope"); got != nil {
		t.Errorf("zero-value GetTask should return nil, got %v", got)
	}
	if svc.Cancel("nope") {
		t.Error("zero-value Cancel should return false")
	}
	if n := svc.CancelAll(); n != 0 {
		t.Errorf("zero-value CancelAll should return 0, got %d", n)
	}
	svc.Wait() // 不应阻塞或 panic
}

// TestTaskHandle_ZeroValueSafe 句柄同样要能安全面对零值：
// 提交失败的路径上很容易拿到 nil 句柄。
func TestTaskHandle_ZeroValueSafe(t *testing.T) {
	var h *TaskHandle
	if h.ID() != "" {
		t.Error("nil handle ID should be empty")
	}
	if h.Err() != nil {
		t.Error("nil handle Err should be nil")
	}
	if h.Done() != nil {
		t.Error("nil handle Done should be nil")
	}
}

// TestTaskService_EmitterReceivesLifecycle 三类事件的顺序与内容要正确：
// started → progress(可选) → finished。
func TestTaskService_EmitterReceivesLifecycle(t *testing.T) {
	em := &recordingEmitter{}
	svc := NewTaskService(em)

	h := svc.submit(context.Background(), "生命周期", func(ctx context.Context, rep TaskReporter) error {
		rep.Message("开始处理")
		return nil
	})
	waitDone(t, h)

	events := em.all()
	if len(events) == 0 {
		t.Fatal("no events emitted")
	}
	if events[0].Name != EventTaskStarted {
		t.Errorf("first event should be task:started, got %s", events[0].Name)
	}
	last := events[len(events)-1]
	if last.Name != EventTaskFinished {
		t.Errorf("last event should be task:finished, got %s", last.Name)
	}
	if last.Data == nil || last.Data.Name != "生命周期" {
		t.Errorf("finished event should carry the task info, got %+v", last.Data)
	}
}
