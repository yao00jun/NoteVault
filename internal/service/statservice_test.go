package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func mustWriteStats(t *testing.T, path, content string, modTime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

func TestStats_GetTodayStats(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// 今天改动的两篇 + 昨天一篇 + 前天一篇 → 连续 3 天，今日编辑 2
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0750); err != nil {
		t.Fatal(err)
	}
	mustWriteStats(t, filepath.Join(dir, "a.md"), "- [ ] 普通待办\n", now.Add(-1*time.Hour))
	mustWriteStats(t, filepath.Join(dir, "sub", "b.md"), "- [x] 已完成\n- [ ] !! 高优先级\n", now.Add(-2*time.Hour))
	yesterday := now.AddDate(0, 0, -1)
	mustWriteStats(t, filepath.Join(dir, "c.md"), "# old\n", yesterday.Add(-2*time.Hour))
	twoDaysAgo := now.AddDate(0, 0, -2)
	mustWriteStats(t, filepath.Join(dir, "d.md"), "# older\n", twoDaysAgo.Add(-2*time.Hour))
	// 366 天前的改动不应计入连续记录
	mustWriteStats(t, filepath.Join(dir, "e.md"), "# ancient\n", now.AddDate(0, 0, -400))

	s := NewStatsService(NewTodoService(), NewReminderService())
	stats, err := s.GetTodayStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.EditedToday != 2 {
		t.Fatalf("expected EditedToday=2, got %d", stats.EditedToday)
	}
	if stats.StreakDays != 3 {
		t.Fatalf("expected StreakDays=3, got %d", stats.StreakDays)
	}
	if stats.PendingTodos != 2 {
		t.Fatalf("expected PendingTodos=2, got %d", stats.PendingTodos)
	}
	if stats.HighPriorityTodos != 1 {
		t.Fatalf("expected HighPriorityTodos=1, got %d", stats.HighPriorityTodos)
	}
	if len(stats.RecentFiles) == 0 || stats.RecentFiles[0] != "a.md" {
		t.Fatalf("recent files should lead with a.md, got %v", stats.RecentFiles)
	}
	if len(stats.RecentFiles) > 5 {
		t.Fatalf("recent files should cap at 5, got %d", len(stats.RecentFiles))
	}
}

func TestStats_SkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	mustWriteStats(t, filepath.Join(dir, "real.md"), "# ok\n", now)
	if err := os.MkdirAll(filepath.Join(dir, ".trash", "nested"), 0750); err != nil {
		t.Fatal(err)
	}
	mustWriteStats(t, filepath.Join(dir, ".trash", "nested", "deleted.md"), "# deleted\n", now)
	if err := os.MkdirAll(filepath.Join(dir, ".archive"), 0750); err != nil {
		t.Fatal(err)
	}
	mustWriteStats(t, filepath.Join(dir, ".archive", "archived.md"), "# archived\n", now)

	s := NewStatsService(nil, nil)
	stats, err := s.GetTodayStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.EditedToday != 1 {
		t.Fatalf("hidden dirs should be skipped, expected EditedToday=1, got %d", stats.EditedToday)
	}
}

func TestStats_StreakBreaksOnGap(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	// 今天有写，但昨天断档 → 连续只有 1 天
	mustWriteStats(t, filepath.Join(dir, "a.md"), "# a\n", now)
	mustWriteStats(t, filepath.Join(dir, "b.md"), "# b\n", now.AddDate(0, 0, -2))

	s := NewStatsService(nil, nil)
	stats, err := s.GetTodayStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.StreakDays != 1 {
		t.Fatalf("expected StreakDays=1 (gap at yesterday), got %d", stats.StreakDays)
	}
}

func TestStats_StreakSurvivesQuietMorning(t *testing.T) {
	dir := t.TempDir()
	// 今天还没写，昨天写了 → 连续记录从昨天起算，不清零
	yesterday := time.Now().AddDate(0, 0, -1)
	mustWriteStats(t, filepath.Join(dir, "a.md"), "# a\n", yesterday)

	s := NewStatsService(nil, nil)
	stats, err := s.GetTodayStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.StreakDays < 1 {
		t.Fatalf("streak should count from yesterday on a quiet morning, got %d", stats.StreakDays)
	}
}

func TestStats_DueRemindersDatetimeLocal(t *testing.T) {
	// RemindersView 的 <input type="datetime-local"> 存的是无时区格式，
	// 早期实现只认 RFC3339 → 今日工作台"到期提醒"恒为 0。回归护栏。
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".notevault"), 0750); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	pastLocal := now.Add(-1 * time.Hour).Format("2006-01-02T15:04")
	content := `[{"id":"r1","filePath":"a.md","fileName":"a","content":"过期","remindAt":"` + pastLocal + `","createdAt":"","completed":false}]`
	if err := os.WriteFile(filepath.Join(dir, ".notevault", "reminders.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	s := NewStatsService(nil, NewReminderService())
	stats, err := s.GetTodayStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.DueReminders != 1 {
		t.Fatalf("datetime-local reminder should count as due, got %d", stats.DueReminders)
	}
}

func TestStats_DueReminders(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".notevault"), 0750); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	past := now.Add(-1 * time.Hour).Format(time.RFC3339)
	future := now.Add(1 * time.Hour).Format(time.RFC3339)
	content := `[
		{"id":"r1","filePath":"a.md","fileName":"a","content":"过期未完成","remindAt":"` + past + `","createdAt":"` + past + `","completed":false},
		{"id":"r2","filePath":"a.md","fileName":"a","content":"未来提醒","remindAt":"` + future + `","createdAt":"` + past + `","completed":false},
		{"id":"r3","filePath":"a.md","fileName":"a","content":"过期已完成","remindAt":"` + past + `","createdAt":"` + past + `","completed":true}
	]`
	if err := os.WriteFile(filepath.Join(dir, ".notevault", "reminders.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	s := NewStatsService(nil, NewReminderService())
	stats, err := s.GetTodayStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.DueReminders != 1 {
		t.Fatalf("expected DueReminders=1, got %d", stats.DueReminders)
	}
}

func TestStats_GetWritingActivity(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// 今天 2 篇、昨天 1 篇、3 天前 1 篇 → 周改动 4、最长连续 2
	mustWriteStats(t, filepath.Join(dir, "a.md"), "# a\n", now.Add(-1*time.Hour))
	mustWriteStats(t, filepath.Join(dir, "b.md"), "# b\n", now.Add(-2*time.Hour))
	mustWriteStats(t, filepath.Join(dir, "c.md"), "# c\n", now.AddDate(0, 0, -1))
	mustWriteStats(t, filepath.Join(dir, "d.md"), "# d\n", now.AddDate(0, 0, -3))

	s := NewStatsService(nil, nil)
	activity, err := s.GetWritingActivity(dir, 91)
	if err != nil {
		t.Fatal(err)
	}
	if len(activity.Days) != 91 {
		t.Fatalf("expected 91 day cells, got %d", len(activity.Days))
	}
	if activity.Days[len(activity.Days)-1].Date != now.Format("2006-01-02") {
		t.Fatalf("last cell should be today, got %s", activity.Days[len(activity.Days)-1].Date)
	}
	if activity.Days[len(activity.Days)-1].Edited != 2 {
		t.Fatalf("today cell should count 2 edits, got %d", activity.Days[len(activity.Days)-1].Edited)
	}
	if activity.WeekEdited != 4 {
		t.Fatalf("expected WeekEdited=4, got %d", activity.WeekEdited)
	}
	if activity.MonthEdited != 4 {
		t.Fatalf("expected MonthEdited=4, got %d", activity.MonthEdited)
	}
	if activity.ActiveDays != 3 {
		t.Fatalf("expected ActiveDays=3, got %d", activity.ActiveDays)
	}
	if activity.LongestStreak != 2 {
		t.Fatalf("expected LongestStreak=2, got %d", activity.LongestStreak)
	}
	// 全部为零的窗口不产生连续
	empty, err := s.GetWritingActivity(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if empty.LongestStreak != 0 || empty.ActiveDays != 0 {
		t.Fatalf("empty workspace should have zero activity, got %+v", empty)
	}
}
