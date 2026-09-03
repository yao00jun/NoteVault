package service

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
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

// ReportService 提供知识周报生成
type ReportService struct {
	fileService *FileService
	todos       *TodoService
	client      *http.Client
}

// NewReportService 构造周报服务
func NewReportService(fileService *FileService, todos *TodoService) *ReportService {
	return &ReportService{
		fileService: fileService,
		todos:       todos,
		client:      &http.Client{Timeout: 120 * time.Second},
	}
}

// GenerateWeeklyReport 生成本周（ISO 周）知识周报并写入 Reports/ 目录。
// 周报已存在时覆盖重写（重复点击生成是正常操作，不是冲突）。
func (s *ReportService) GenerateWeeklyReport(workspacePath string, ai WeeklyReportAIConfig) (*WeeklyReportResult, error) {
	if s.fileService == nil {
		return nil, fmt.Errorf("缺少文件服务，无法写入周报")
	}
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
	sb.WriteString(fmt.Sprintf("# 知识周报 %d-W%02d\n\n", year, week))
	sb.WriteString(fmt.Sprintf("> 生成于 %s · 覆盖 %s 至 %s\n\n",
		now.Format("2006-01-02 15:04"),
		weekStart.Format("2006-01-02"), now.Format("2006-01-02")))

	sb.WriteString("## 本周数据\n\n")
	sb.WriteString(fmt.Sprintf("- 改动笔记：%d 篇\n", len(changed)))
	sb.WriteString(fmt.Sprintf("- 待办：未完成 %d（高优先级 %d）· 已完成累计 %d\n\n", pending, high, completed))

	sb.WriteString("## 本周改动的笔记\n\n")
	if len(changed) == 0 {
		sb.WriteString("（本周还没有笔记改动）\n\n")
	}
	noteSnippets := make([]string, 0, weeklyReportMaxNotes)
	for i, r := range changed {
		if i >= weeklyReportMaxNotes {
			break
		}
		sb.WriteString(fmt.Sprintf("- [[%s|%s]]\n", r.rel, filepath.Base(r.rel)))
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
		answer, aerr := llmChatComplete(context.Background(), s.client,
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
		sb.WriteString(fmt.Sprintf("- %s：%d 篇\n", day.Format("01-02"), editCounts[day.Format("2006-01-02")]))
	}

	content := sb.String()
	// 已存在则覆盖（SaveFile 走快照链路，旧版本可从时间机器找回）
	if _, err := s.fileService.CreateFile(workspacePath, relPath, content); err != nil {
		if serr := s.fileService.SaveFile(workspacePath, relPath, content); serr != nil {
			return nil, core.WrapError(core.ErrPermission, "写入周报失败: "+relPath, serr)
		}
	}
	return result, nil
}

// truncateRunes 按字符数截断（不切断 rune），超出部分以省略号结尾。
func truncateRunes(s string, max int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max]) + "…"
}
