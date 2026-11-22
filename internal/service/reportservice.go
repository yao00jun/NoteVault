package service

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/notevault/notevault/internal/core"
)

// ---------------------------------------------------------------------------
// ReportService（C 期）：知识周报自动生成
//
// 从工作区文件自动派生「本周写了什么」：近 7 天改动笔记 + 待办吞吐统计，
// 拼成周报骨架；用户配置了 AI 时再让 LLM 在骨架之上写一段回顾与建议。
// 产物是一篇普通 Markdown 笔记（Reports/<年>-W<周>.md），仍是纯文件、可 Git。
//
// AI 是可选项：未配置或调用失败都降级为纯统计版周报，绝不因 AI 不可用
// 而拒绝生成（与快照「保险不是主链路」同一设计立场）。
// ---------------------------------------------------------------------------

// WeeklyReportAIConfig 生成周报时的可选 AI 配置（与设置页 AI 总结同源）。
type WeeklyReportAIConfig struct {
	BaseURL  string `json:"baseURL"`
	Model    string `json:"model"`
	APIKey   string `json:"apiKey"`
	Protocol string `json:"protocol"`
}

// WeeklyReportResult 周报生成结果。
type WeeklyReportResult struct {
	Path   string `json:"path"`   // 生成的周报相对路径（正斜杠）
	AIUsed bool   `json:"aiUsed"` // AI 段落是否生成成功
	Notes  int    `json:"notes"`  // 本周改动笔记数
	Todos  int    `json:"todos"`  // 当前未完成待办数
	// Message 降级说明：AI 失败/未配置时非空，前端据此提示（空=完全成功）
	Message string `json:"message"`
}

// weeklyReportLimits 控制喂给 LLM 的上下文体积：最多 15 篇、每篇正文截 1200 字符。
const (
	weeklyReportMaxNotes     = 15
	weeklyReportNoteMaxRunes = 1200
)

// maxStoredReportResults 保留多少个周报任务的结果。与编译服务同一口径：
// 前端只在任务结束后取一次，留余量是防事件与取结果之间的时序抖动。
const maxStoredReportResults = 20

// ReportService 提供知识周报生成
type ReportService struct {
	fileService *FileService
	todos       *TodoService
	client      *http.Client

	// tasks 为 nil 时退化为同步生成（测试与未接装配场景），
	// 生产由容器注入，使周报走异步任务队列并可取消。
	tasks taskSubmitter

	// resultMu 保护 results：异步任务的结构化产出按任务 ID 暂存，
	// 前端在 task:finished 后按 ID 取回（事件只带进度，不带业务结果）。
	resultMu  sync.Mutex
	results   map[string]*WeeklyReportResult
	resultSeq []string
}

// NewReportService 构造周报服务（不接任务队列，StartWeeklyReport 返回空 ID）。
func NewReportService(fileService *FileService, todos *TodoService) *ReportService {
	return NewReportServiceWithTasks(fileService, todos, nil)
}

// NewReportServiceWithTasks 构造周报服务并注入异步任务队列。
// 注入走构造函数而不是 Setter，理由与 CompileService 相同：Setter 的形参会
// 让 Wails 绑定生成器把 TaskService 同时登记成服务与模型，撞出重复标识符。
func NewReportServiceWithTasks(fileService *FileService, todos *TodoService, tasks *TaskService) *ReportService {
	svc := &ReportService{
		fileService: fileService,
		todos:       todos,
		client:      &http.Client{Timeout: 120 * time.Second},
		results:     make(map[string]*WeeklyReportResult),
	}
	if tasks != nil {
		svc.tasks = tasks
	}
	return svc
}

// WeeklyReportTaskResult 取回某个异步周报任务的结构化结果。
// 任务未结束或 ID 未知时返回 nil——调用方应据此提示「仍在进行/结果已过期」。
func (s *ReportService) WeeklyReportTaskResult(taskID string) *WeeklyReportResult {
	s.resultMu.Lock()
	defer s.resultMu.Unlock()
	return s.results[taskID]
}

// saveTaskResult 暂存结果并按 FIFO 裁剪，避免长会话无限累积。
func (s *ReportService) saveTaskResult(taskID string, res *WeeklyReportResult) {
	if taskID == "" || res == nil {
		return
	}
	s.resultMu.Lock()
	defer s.resultMu.Unlock()
	s.results[taskID] = res
	s.resultSeq = append(s.resultSeq, taskID)
	for len(s.resultSeq) > maxStoredReportResults {
		oldest := s.resultSeq[0]
		s.resultSeq = s.resultSeq[1:]
		delete(s.results, oldest)
	}
}

// GenerateWeeklyReport 生成本周（ISO 周）知识周报并写入 Reports/ 目录。
// 周报已存在时覆盖重写（重复点击生成是正常操作，不是冲突）。
// 保留同步语义给测试、MCP 与未注入任务队列的装配；前端走 StartWeeklyReport。
func (s *ReportService) GenerateWeeklyReport(workspacePath string, ai WeeklyReportAIConfig) (*WeeklyReportResult, error) {
	return s.generateWeeklyReportCtx(context.Background(), TaskReporter{}, workspacePath, ai)
}

// StartWeeklyReport 把「生成本周知识周报」提交到异步任务队列，返回任务 ID。
//
// 与同步 GenerateWeeklyReport 的关系：同一套生成逻辑，区别只在
//   - AI 回顾是唯一的长操作，进行中可通过 TaskService.Cancel(taskID) 取消，
//     在途 LLM 请求随 ctx 一起中断；取消时纯统计骨架仍会落盘；
//   - 结果按任务 ID 暂存，前端在 task:finished 后用 WeeklyReportTaskResult 取回。
//
// 未注入 TaskService 时返回空字符串，调用方应退回同步 GenerateWeeklyReport。
func (s *ReportService) StartWeeklyReport(workspacePath string, ai WeeklyReportAIConfig) string {
	if s.tasks == nil {
		return ""
	}
	handle := s.tasks.submit(context.Background(), "生成知识周报", func(ctx context.Context, rep TaskReporter) error {
		res, err := s.generateWeeklyReportCtx(ctx, rep, workspacePath, ai)
		if res == nil {
			// 失败也要给前端一个可展示的结果：错误原因放进 Message
			res = &WeeklyReportResult{Message: err.Error()}
		}
		s.saveTaskResult(rep.TaskID(), res)
		if err != nil {
			// 取消必须回传 ctx 错误，TaskService 才会把终态记为 cancelled
			//（而不是 failed），前端据此区分「用户取消」与「真的出错」。
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		rep.Report(1, 1)
		// 成功路径也回传 ctx.Err()：AI 请求被取消时生成逻辑会优雅降级、
		// 返回 (纯统计结果, nil)，但任务终态应记为 cancelled 而不是 succeeded。
		return ctx.Err()
	})
	return handle.ID()
}

// generateWeeklyReportCtx 是 GenerateWeeklyReport 的 ctx 版：取消时中断在途的
// LLM 请求。rep 可为零值（同步路径不回报进度）。
func (s *ReportService) generateWeeklyReportCtx(ctx context.Context, rep TaskReporter, workspacePath string, ai WeeklyReportAIConfig) (*WeeklyReportResult, error) {
	if s.fileService == nil {
		return nil, fmt.Errorf("缺少文件服务，无法写入周报")
	}
	rep.Message("正在统计本周数据")
	now := time.Now()
	year, week := now.ISOWeek()
	relPath := fmt.Sprintf("Reports/%d-W%02d.md", year, week)
	// 数据窗口取本周一 0 点起，与 ISO 周文件名对齐：
	// 若用滚动 7 天，周一上午生成的"周报"装的是上一周的数据却写进本周文件
	weekday := (int(now.Weekday()) + 6) % 7 // 周一=0 … 周日=6
	weekStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -weekday)

	// 近 7 天改动笔记（按改动时间倒序）
	editCounts, recents, err := scanEditActivity(workspacePath)
	if err != nil {
		return nil, err
	}
	var changed []editRecent
	for _, r := range recents {
		if !r.mod.Before(weekStart) {
			changed = append(changed, r)
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].mod.After(changed[j].mod) })

	// 待办吞吐（解析口径与待办面板一致）
	var pending, high, completed int
	if s.todos != nil {
		if todos, terr := s.todos.GetAllTodos(workspacePath); terr == nil {
			for _, td := range todos {
				if td.Completed {
					completed++
				} else {
					pending++
					if td.Priority == "high" {
						high++
					}
				}
			}
		}
	}

	result := &WeeklyReportResult{
		Path:   relPath,
		Notes:  len(changed),
		Todos:  pending,
		AIUsed: false,
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# 知识周报 %d-W%02d\n\n", year, week)
	fmt.Fprintf(&sb, "> 生成于 %s · 覆盖 %s 至 %s\n\n",
		now.Format("2006-01-02 15:04"),
		weekStart.Format("2006-01-02"), now.Format("2006-01-02"))

	sb.WriteString("## 本周数据\n\n")
	fmt.Fprintf(&sb, "- 改动笔记：%d 篇\n", len(changed))
	fmt.Fprintf(&sb, "- 待办：未完成 %d（高优先级 %d）· 已完成累计 %d\n\n", pending, high, completed)

	sb.WriteString("## 本周改动的笔记\n\n")
	if len(changed) == 0 {
		sb.WriteString("（本周还没有笔记改动）\n\n")
	}
	noteSnippets := make([]string, 0, weeklyReportMaxNotes)
	for i, r := range changed {
		if i >= weeklyReportMaxNotes {
			break
		}
		fmt.Fprintf(&sb, "- [[%s|%s]]\n", r.rel, filepath.Base(r.rel))
		if i < weeklyReportMaxNotes {
			if content, rerr := s.fileService.ReadFile(workspacePath, r.rel); rerr == nil {
				noteSnippets = append(noteSnippets, fmt.Sprintf("【%s】\n%s", r.rel, truncateRunes(content, weeklyReportNoteMaxRunes)))
			}
		}
	}
	sb.WriteString("\n")

	// AI 段：配置齐全才尝试，失败降级并在 Message 里说明
	prompt := strings.Join(noteSnippets, "\n\n---\n\n")
	if strings.TrimSpace(ai.BaseURL) != "" && strings.TrimSpace(ai.Model) != "" {
		systemPrompt := "你是个人知识库的周报助手。根据用户本周改动的笔记原文，写一段中文周报回顾：\n" +
			"1. 概括本周的知识工作主题（2-3 句）；\n" +
			"2. 指出笔记之间可能的主题关联或值得展开的方向（1-2 条）；\n" +
			"3. 只依据给出的笔记内容，不要编造；使用 Markdown，不超过 300 字。\n" +
			"待办情况：未完成 " + fmt.Sprintf("%d", pending) + " 条（高优先级 " + fmt.Sprintf("%d", high) + "）。"
		rep.Message("正在生成 AI 回顾")
		answer, aerr := llmChatComplete(ctx, s.client,
			ai.APIKey, ai.BaseURL, ai.Model, ai.Protocol, systemPrompt, prompt)
		if aerr != nil {
			result.Message = "AI 回顾生成失败，已降级为纯统计周报：" + aerr.Error()
		} else {
			result.AIUsed = true
			sb.WriteString("## AI 回顾\n\n" + strings.TrimSpace(answer) + "\n\n")
		}
	} else {
		result.Message = "未配置 AI 端点，已生成纯统计周报（可在设置页配置后重新生成）"
	}

	// 底部固定统计块：即使 AI 段失败也有完整数据落盘
	sb.WriteString("## 附：本周逐日改动\n\n")
	for d := weekStart; !d.After(now); d = d.AddDate(0, 0, 1) {
		day := d
		fmt.Fprintf(&sb, "- %s：%d 篇\n", day.Format("01-02"), editCounts[day.Format("2006-01-02")])
	}

	content := sb.String()
	// 已存在则覆盖（SaveFile 走快照链路，旧版本可从时间机器找回）
	rep.Message("正在写入周报")
	if _, err := s.fileService.CreateFile(workspacePath, relPath, content); err != nil {
		if serr := s.fileService.SaveFile(workspacePath, relPath, content); serr != nil {
			return nil, core.WrapError(core.ErrPermission, "写入周报失败: "+relPath, serr)
		}
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// 今日 AI 简报（打开即今天）：把工作台快照（到期/阻塞任务、到期复习、今日
// Agent 动态）喂给 LLM，输出 3-5 条"重点/风险/建议"。
// 与周报"必出统计版"不同，简报的价值就在 AI 综合本身——AI 未配置或失败时
// 返回错误由前端提示（工作台的确定性指标卡不受影响，符合"AI 可降级不阻断
// 主链路"）。简报不落盘：它是当天的即时视图数据，由前端按 工作区+日期 缓存。
// ---------------------------------------------------------------------------

// DailyBriefingResult 今日简报结果。
type DailyBriefingResult struct {
	Date     string `json:"date"`
	Briefing string `json:"briefing"` // Markdown 要点（"- " 开头的 3-5 行）
	AIUsed   bool   `json:"aiUsed"`
}

// dailyBriefingLimits 控制喂给 LLM 的工作台数据体积。
const (
	dailyBriefingMaxTasks   = 12
	dailyBriefingMaxEvents  = 8
	dailyBriefingTaskRunes  = 120
	dailyBriefingEventRunes = 700
)

// GenerateDailyBriefing 生成今日 AI 简报。
func (s *ReportService) GenerateDailyBriefing(workspacePath, date string, ai WeeklyReportAIConfig) (*DailyBriefingResult, error) {
	if s.todos == nil {
		return nil, fmt.Errorf("缺少工作台服务，无法生成简报")
	}
	baseURL := strings.TrimSpace(ai.BaseURL)
	model := strings.TrimSpace(ai.Model)
	if baseURL == "" || model == "" {
		return nil, fmt.Errorf("未配置 AI 端点，可在设置页配置后生成简报")
	}
	snapshot, err := s.todos.GetWorkbench(workspacePath, date)
	if err != nil {
		return nil, err
	}

	// 任务：阻塞优先，其后按到期升序；未到期靠后。
	var open, blocked, due []*WorkbenchTask
	for _, task := range snapshot.Tasks {
		if task.Completed {
			continue
		}
		open = append(open, task)
		if strings.TrimSpace(task.Blocker) != "" {
			blocked = append(blocked, task)
		} else if task.Due != "" && task.Due <= date {
			due = append(due, task)
		}
	}
	var dueCards int
	for _, card := range snapshot.Cards {
		if card.Due != "" && card.Due <= date {
			dueCards++
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "今日工作台数据（%s）：\n\n## 任务\n未完成 %d 条；今日到期/已逾期 %d 条；阻塞 %d 条\n",
		date, len(open), len(due), len(blocked))
	listed := make(map[string]bool, dailyBriefingMaxTasks)
	appendTask := func(label string, list []*WorkbenchTask) {
		for _, task := range list {
			if len(listed) >= dailyBriefingMaxTasks || listed[task.ID] {
				continue
			}
			listed[task.ID] = true
			line := fmt.Sprintf("- [%s] %s（项目：%s，到期：%s", task.Type, truncateRunes(task.Title, dailyBriefingTaskRunes), task.Project, task.Due)
			if strings.TrimSpace(task.Blocker) != "" {
				line += "，阻塞：" + truncateRunes(task.Blocker, 80)
			}
			sb.WriteString(line + "）\n")
		}
	}
	appendTask("阻塞", blocked)
	appendTask("到期", due)

	fmt.Fprintf(&sb, "\n## 复习\n到期题卡 %d 张\n", dueCards)
	if len(snapshot.AgentEvents) > 0 {
		sb.WriteString("\n## 今日 Agent 动态\n")
		for i, event := range snapshot.AgentEvents {
			if i >= dailyBriefingMaxEvents {
				break
			}
			line := fmt.Sprintf("- %s [%s] %s", event.Time, event.Agent, strings.TrimSpace(event.Content))
			sb.WriteString(truncateRunes(line, dailyBriefingEventRunes) + "\n")
		}
	}

	systemPrompt := "你是开发者个人工作台的每日简报助手。根据用户给出的工作台数据（任务、到期复习、Agent 动态），用中文输出今日简报：\n" +
		"1. 直接输出 3-5 条要点，每条一行以 \"- \" 开头，不要标题；\n" +
		"2. 覆盖今日应优先处理的任务、风险或阻塞、值得趁热做的复习、Agent 已完成的工作；\n" +
		"3. 只依据给出的数据，不编造；每条不超过 60 字。"
	answer, aerr := llmChatComplete(context.Background(), s.client,
		ai.APIKey, baseURL, model, ai.Protocol, systemPrompt, sb.String())
	if aerr != nil {
		return nil, fmt.Errorf("AI 简报生成失败：%w", aerr)
	}
	briefing := strings.TrimSpace(answer)
	if briefing == "" {
		return nil, fmt.Errorf("AI 未返回简报内容，请稍后重试")
	}
	return &DailyBriefingResult{Date: date, Briefing: briefing, AIUsed: true}, nil
}

// truncateRunes 按字符数截断（不切断 rune），超出部分以省略号结尾。
func truncateRunes(s string, max int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max]) + "…"
}
