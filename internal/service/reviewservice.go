package service

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/notevault/notevault/internal/core"
)

// ---------------------------------------------------------------------------
// ReviewService（v1.2：批量笔记总结 / 定期回顾）
//
// 两个能力，共用同一套批量总结引擎：
//   - GenerateReview：定期回顾笔记——近 N 天改动笔记逐篇 AI 摘要 + 旧笔记重温
//     清单，落成 Reviews/回顾-<日期>.md
//   - ScheduleWeeklyReview：用 ReminderService 预约下一次回顾提醒（一次性，
//     到点后由用户重新预约；无重复调度 schema，保持提醒模型简单）
//
// 降级立场与周报一致：AI 未配置/失败时仍生成纯结构回顾（标题清单 + 统计），
// 绝不因 AI 不可用而拒绝产出。
// ---------------------------------------------------------------------------

// reviewLimits 控制批量总结的体积：最多 10 篇逐篇摘要、重温清单最多 5 篇。
const (
	reviewMaxNotes = 10
	reviewMaxRecap = 5
	reviewOldDays  = 90 // 超过这么多天未改动视为"旧笔记"
)

// ReviewResult 回顾生成结果。
type ReviewResult struct {
	Path        string   `json:"path"`        // 回顾笔记相对路径（正斜杠）
	Summarized  int      `json:"summarized"`  // 成功生成 AI 摘要的笔记数
	Failed      int      `json:"failed"`      // 单篇摘要失败数（已降级为仅列标题）
	RecapTitles []string `json:"recapTitles"` // 旧笔记重温清单
	AIUsed      bool     `json:"aiUsed"`
	Message     string   `json:"message"` // 降级说明（空 = 完全成功）
}

// ReviewService 提供批量总结与定期回顾
type ReviewService struct {
	fileService *FileService
	summarize   *SummarizeService
	reminders   *ReminderService
}

// NewReviewService 构造回顾服务
func NewReviewService(fileService *FileService, summarize *SummarizeService, reminders *ReminderService) *ReviewService {
	return &ReviewService{fileService: fileService, summarize: summarize, reminders: reminders}
}

// GenerateReview 生成定期回顾笔记。
// days 为回看窗口（30..366 之外收敛），近窗口内改动的笔记逐篇 AI 摘要
// （单篇失败不影响其它篇），另附"旧笔记重温"清单（>90 天未改动、最久优先）。
// 产物覆盖写 Reviews/回顾-<日期>.md（重复生成是正常操作）。
func (s *ReviewService) GenerateReview(workspacePath string, ai WeeklyReportAIConfig, days int) (*ReviewResult, error) {
	if s.fileService == nil {
		return nil, fmt.Errorf("缺少文件服务，无法写入回顾")
	}
	if days < 7 {
		days = 7
	}
	if days > 366 {
		days = 366
	}
	now := time.Now()
	recapCutoff := now.AddDate(0, 0, -reviewOldDays)
	windowStart := now.AddDate(0, 0, -days)

	_, recents, err := scanEditActivity(workspacePath)
	if err != nil {
		return nil, err
	}

	var inWindow, old []editRecent
	for _, r := range recents {
		if r.mod.After(windowStart) {
			inWindow = append(inWindow, r)
		} else if r.mod.Before(recapCutoff) {
			old = append(old, r)
		}
	}
	// 近期笔记按新→旧，旧笔记按久→近（最久未动的最值得重温）
	sort.Slice(inWindow, func(i, j int) bool { return inWindow[i].mod.After(inWindow[j].mod) })
	sort.Slice(old, func(i, j int) bool { return old[i].mod.Before(old[j].mod) })
	if len(inWindow) > reviewMaxNotes {
		inWindow = inWindow[:reviewMaxNotes]
	}
	if len(old) > reviewMaxRecap {
		old = old[:reviewMaxRecap]
	}

	result := &ReviewResult{RecapTitles: []string{}}
	aiReady := strings.TrimSpace(ai.BaseURL) != "" && strings.TrimSpace(ai.Model) != ""

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# 定期回顾 %s\n\n", now.Format("2006-01-02")))
	sb.WriteString(fmt.Sprintf("> 覆盖近 %d 天改动 %d 篇 · 旧笔记重温 %d 篇\n\n", days, len(inWindow), len(old)))

	sb.WriteString("## 本期笔记摘要\n\n")
	if len(inWindow) == 0 {
		sb.WriteString("（窗口期内没有笔记改动）\n\n")
	}
	for _, r := range inWindow {
		title := strings.TrimSuffix(filepath.Base(r.rel), filepath.Ext(r.rel))
		sb.WriteString(fmt.Sprintf("### [[%s|%s]]\n\n", r.rel, title))
		if !aiReady || s.summarize == nil {
			continue
		}
		content, rerr := s.fileService.ReadFile(workspacePath, r.rel)
		if rerr != nil || strings.TrimSpace(content) == "" {
			continue
		}
		summary, serr := s.summarize.Summarize(ai.APIKey, ai.BaseURL, ai.Model, ai.Protocol, content)
		if serr != nil {
			result.Failed++
			sb.WriteString(fmt.Sprintf("> 摘要生成失败：%v\n\n", serr))
			continue
		}
		result.Summarized++
		sb.WriteString(strings.TrimSpace(summary) + "\n\n")
	}

	sb.WriteString("## 旧笔记重温\n\n")
	if len(old) == 0 {
		sb.WriteString("（没有超过 90 天未动过的笔记，或者还没有足够早的笔记）\n\n")
	}
	for _, r := range old {
		title := strings.TrimSuffix(filepath.Base(r.rel), filepath.Ext(r.rel))
		result.RecapTitles = append(result.RecapTitles, r.rel)
		sb.WriteString(fmt.Sprintf("- [[%s|%s]]（上次改动 %s）\n", r.rel, title, r.mod.Format("2006-01-02")))
	}
	sb.WriteString("\n---\n\n重温方法：读一遍，把仍然认同的划掉「已回顾」，有新想法的直接补进笔记；确定过时的移入归档。\n")

	result.AIUsed = aiReady && result.Summarized > 0
	if !aiReady {
		result.Message = "未配置 AI，已生成纯结构回顾（仅标题清单）；配置后重新生成可获得逐篇摘要"
	} else if result.Failed > 0 && result.Summarized == 0 {
		result.Message = "AI 摘要全部失败，已降级为标题清单"
	}

	relPath := "Reviews/回顾-" + now.Format("2006-01-02") + ".md"
	result.Path = relPath
	content := sb.String()
	if _, cerr := s.fileService.CreateFile(workspacePath, relPath, content); cerr != nil {
		if serr := s.fileService.SaveFile(workspacePath, relPath, content); serr != nil {
			return nil, core.WrapError(core.ErrPermission, "写入回顾失败: "+relPath, serr)
		}
	}
	return result, nil
}

// ScheduleWeeklyReview 预约下一次回顾提醒：下一个周一 09:00（本地时区）。
// 今天是周一且已过 09:00 则预约下周一。提醒是一次性的——模型刻意不做
// 重复调度（Reminder schema 保持简单），到点重新点一次即可。
func (s *ReviewService) ScheduleWeeklyReview(workspacePath string) (*Reminder, error) {
	if s.reminders == nil {
		return nil, fmt.Errorf("提醒服务不可用")
	}
	now := time.Now()
	next := now
	for {
		next = next.AddDate(0, 0, 1)
		if next.Weekday() == time.Monday {
			break
		}
	}
	at := time.Date(next.Year(), next.Month(), next.Day(), 9, 0, 0, 0, next.Location())
	return s.reminders.AddReminder(workspacePath, "", "每周知识回顾：打开 写作报表 → 生成本期回顾", at.Format(time.RFC3339))
}
