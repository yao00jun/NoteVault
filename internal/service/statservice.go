package service

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TodayStats 今日工作台统计。
// 所有数字都从工作区文件与既有索引派生，用户不需要为仪表盘维护任何数据。
type TodayStats struct {
	EditedToday       int      `json:"editedToday"`       // 今天有改动的 .md 笔记数
	StreakDays        int      `json:"streakDays"`        // 连续有笔记改动的天数（今天没写则从昨天回溯）
	PendingTodos      int      `json:"pendingTodos"`      // 未完成待办总数
	HighPriorityTodos int      `json:"highPriorityTodos"` // 未完成中带 !! 的高优先级数量
	DueReminders      int      `json:"dueReminders"`      // 已到期未完成的提醒数
	RecentFiles       []string `json:"recentFiles"`       // 最近改动的笔记相对路径（正斜杠，最多 5 篇）
}

// StatsService 提供今日工作台聚合统计
type StatsService struct {
	todos     *TodoService
	reminders *ReminderService
}

// NewStatsService 构造统计服务：复用 Todo / Reminder 的既有读取逻辑，
// 不另起一套待办/提醒解析口径。
func NewStatsService(todos *TodoService, reminders *ReminderService) *StatsService {
	return &StatsService{todos: todos, reminders: reminders}
}

// statsSkipDirs 聚合统计时跳过的目录：隐藏/系统目录不产生"今日编辑"
var statsSkipDirs = map[string]bool{
	".trash":       true,
	".archive":     true,
	".notevault":   true,
	".git":         true,
	"node_modules": true,
	"assets":       true,
}

// GetTodayStats 汇总今日工作台数据。
// 单次遍历工作区取 modtime（沿用搜索索引的隐藏目录口径），待办与提醒
// 复用各自服务的既有接口；遍历失败返回错误，子项失败降级为零值不阻塞。
func (s *StatsService) GetTodayStats(workspacePath string) (*TodayStats, error) {
	now := time.Now()
	today := now.Format("2006-01-02")

	stats := &TodayStats{RecentFiles: []string{}}
	// editDays 记录"哪些天有笔记改动"（近一年窗口内足够算连续记录）
	editDays := map[string]bool{}
	type recent struct {
		rel string
		mod time.Time
	}
	var recents []recent

	err := filepath.Walk(workspacePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 读不了的条目不阻塞整体统计
		}
		if info.IsDir() {
			name := info.Name()
			if path != workspacePath && (strings.HasPrefix(name, ".") || statsSkipDirs[name]) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".markdown" {
			return nil
		}
		mod := info.ModTime()
		day := mod.Format("2006-01-02")
		if mod.After(now.AddDate(0, 0, -366)) {
			editDays[day] = true
		}
		if day == today {
			stats.EditedToday++
		}
		rel, rerr := filepath.Rel(workspacePath, path)
		if rerr != nil {
			return nil
		}
		recents = append(recents, recent{rel: filepath.ToSlash(rel), mod: mod})
		return nil
	})
	if err != nil {
		return nil, err
	}

	stats.StreakDays = computeStreak(editDays, now)

	sort.Slice(recents, func(i, j int) bool {
		return recents[i].mod.After(recents[j].mod)
	})
	for i, r := range recents {
		if i >= 5 {
			break
		}
		stats.RecentFiles = append(stats.RecentFiles, r.rel)
	}

	// 待办：只统计未完成与高优先级（解析逻辑与待办面板同一份）
	if s.todos != nil {
		if todos, terr := s.todos.GetAllTodos(workspacePath); terr == nil {
			for _, td := range todos {
				if td.Completed {
					continue
				}
				stats.PendingTodos++
				if td.Priority == "high" {
					stats.HighPriorityTodos++
				}
			}
		}
	}

	// 提醒：已到期且未完成
	if s.reminders != nil {
		if rems, rerr := s.reminders.GetAllReminders(workspacePath); rerr == nil {
			for _, rm := range rems {
				if rm.Completed {
					continue
				}
				at, perr := time.Parse(time.RFC3339, rm.RemindAt)
				if perr == nil && !at.After(now) {
					stats.DueReminders++
				}
			}
		}
	}

	return stats, nil
}

// computeStreak 连续写作天数：从今天起往回数有改动的天；今天还没写则
// 从昨天起算（连续记录不该在你睡醒还没打开应用的早晨清零）。
func computeStreak(editDays map[string]bool, now time.Time) int {
	day := now
	if !editDays[day.Format("2006-01-02")] {
		day = day.AddDate(0, 0, -1)
	}
	streak := 0
	for i := 0; i < 366; i++ {
		if !editDays[day.Format("2006-01-02")] {
			break
		}
		streak++
		day = day.AddDate(0, 0, -1)
	}
	return streak
}
