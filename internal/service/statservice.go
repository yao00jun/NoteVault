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

// DayActivity 是某一天的笔记改动量（报表热力图的最小单元）。
type DayActivity struct {
	Date   string `json:"date"`   // YYYY-MM-DD（本地时区）
	Edited int    `json:"edited"` // 当日有改动的 .md 笔记数
}

// WritingActivity 是报表中心的写作活跃数据。
type WritingActivity struct {
	Days          []DayActivity `json:"days"`          // 按天序列（含无改动的天，前端直接画热力图）
	ActiveDays    int           `json:"activeDays"`    // 窗口内有改动的天数
	WeekEdited    int           `json:"weekEdited"`    // 近 7 天有改动的笔记次数
	MonthEdited   int           `json:"monthEdited"`   // 近 30 天有改动的笔记次数
	LongestStreak int           `json:"longestStreak"` // 窗口内最长连续写作天数
}

// StatsService 提供今日工作台与报表中心聚合统计
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

// editRecent 是一次遍历中收集的文件改动记录。
type editRecent struct {
	rel string
	mod time.Time
}

// scanEditActivity 单次遍历工作区，返回「每天的改动笔记数」与文件改动记录。
// 隐藏目录口径与搜索索引一致；读不了的条目跳过不阻塞整体。
func scanEditActivity(workspacePath string) (map[string]int, []editRecent, error) {
	editCounts := map[string]int{}
	var recents []editRecent

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
		editCounts[mod.Format("2006-01-02")]++
		rel, rerr := filepath.Rel(workspacePath, path)
		if rerr != nil {
			return nil
		}
		recents = append(recents, editRecent{rel: filepath.ToSlash(rel), mod: mod})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return editCounts, recents, nil
}

// GetTodayStats 汇总今日工作台数据。
// 待办与提醒复用各自服务的既有接口；子项失败降级为零值不阻塞。
func (s *StatsService) GetTodayStats(workspacePath string) (*TodayStats, error) {
	now := time.Now()
	today := now.Format("2006-01-02")

	stats := &TodayStats{RecentFiles: []string{}}
	editCounts, recents, err := scanEditActivity(workspacePath)
	if err != nil {
		return nil, err
	}

	stats.EditedToday = editCounts[today]
	editDays := make(map[string]bool, len(editCounts))
	for day := range editCounts {
		editDays[day] = true
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

// GetWritingActivity 返回报表中心的写作活跃数据（GitHub 风格热力图 +
// 周/月汇总）。窗口为「含今天在内的最近 days 天」，days 越界收敛到 30..366。
func (s *StatsService) GetWritingActivity(workspacePath string, days int) (*WritingActivity, error) {
	if days < 30 {
		days = 30
	}
	if days > 366 {
		days = 366
	}

	editCounts, _, err := scanEditActivity(workspacePath)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	today := truncateDay(now)
	activity := &WritingActivity{Days: make([]DayActivity, 0, days)}

	// 从窗口首日走到今天，无改动的天也要补零，前端才能直接铺热力图
	first := today.AddDate(0, 0, -(days - 1))
	for d := first; !d.After(today); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		activity.Days = append(activity.Days, DayActivity{Date: key, Edited: editCounts[key]})
	}

	// 窗口内汇总：活跃天数、最长连续、近 7/30 天改动量
	longest, cur := 0, 0
	for i, day := range activity.Days {
		if day.Edited > 0 {
			activity.ActiveDays++
			cur++
			if cur > longest {
				longest = cur
			}
		} else {
			cur = 0
		}
		if i >= len(activity.Days)-7 {
			activity.WeekEdited += day.Edited
		}
		if i >= len(activity.Days)-30 {
			activity.MonthEdited += day.Edited
		}
	}
	activity.LongestStreak = longest

	return activity, nil
}

// GetOnThisDay 返回「那年今日」：往年同月同日有改动的笔记（按时间倒序，最多 5 篇）。
// 注意基于文件 modtime：笔记今年若被改过，modtime 已变，旧年份的那次改动
// 就不再出现在这里——这是纯文件存储下不维护创建时间索引的合理取舍。
func (s *StatsService) GetOnThisDay(workspacePath string) ([]string, error) {
	_, recents, err := scanEditActivity(workspacePath)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var hits []editRecent
	for _, r := range recents {
		if r.mod.Year() < now.Year() && r.mod.Month() == now.Month() && r.mod.Day() == now.Day() {
			hits = append(hits, r)
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].mod.After(hits[j].mod) })
	out := make([]string, 0, 5)
	for i, h := range hits {
		if i >= 5 {
			break
		}
		out = append(out, h.rel)
	}
	return out, nil
}

// truncateDay 去掉时分秒（本地时区），得到当天 0 点。
func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
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
