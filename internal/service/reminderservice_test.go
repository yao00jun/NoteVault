package service

import (
	"path/filepath"
	"testing"
	"time"
)

func TestReminder_AddAndGet(t *testing.T) {
	dir := t.TempDir()
	s := NewReminderService()

	rem, err := s.AddReminder(dir, "note.md", "提醒内容", time.Now().Add(2*time.Hour).Format(time.RFC3339))
	if err != nil {
		t.Fatalf("AddReminder failed: %v", err)
	}
	if rem == nil {
		t.Fatal("returned nil Reminder")
	}
	if rem.Content != "提醒内容" {
		t.Fatalf("expected content '提醒内容', got '%s'", rem.Content)
	}
	if rem.Completed {
		t.Fatal("new reminder should not be completed")
	}
	if rem.ID == "" {
		t.Fatal("ID should not be empty")
	}

	// 验证能取回
	all, err := s.GetAllReminders(dir)
	if err != nil {
		t.Fatalf("GetAllReminders failed: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 reminder, got %d", len(all))
	}
}

func TestReminder_Delete(t *testing.T) {
	dir := t.TempDir()
	s := NewReminderService()

	rem, _ := s.AddReminder(dir, "note.md", "to delete", time.Now().Add(1*time.Hour).Format(time.RFC3339))

	if err := s.DeleteReminder(dir, rem.ID); err != nil {
		t.Fatalf("DeleteReminder failed: %v", err)
	}
	all, _ := s.GetAllReminders(dir)
	if len(all) != 0 {
		t.Fatalf("expected 0 after delete, got %d", len(all))
	}
}

func TestReminder_DeleteNonExist(t *testing.T) {
	dir := t.TempDir()
	s := NewReminderService()
	// 删除不存在的 ID 不应报错
	if err := s.DeleteReminder(dir, "nonexistent"); err != nil {
		t.Fatalf("DeleteReminder on non-existent should not error: %v", err)
	}
}

func TestReminder_Toggle(t *testing.T) {
	dir := t.TempDir()
	s := NewReminderService()

	rem, _ := s.AddReminder(dir, "note.md", "toggle me", time.Now().Add(1*time.Hour).Format(time.RFC3339))

	toggled, err := s.ToggleReminder(dir, rem.ID)
	if err != nil {
		t.Fatalf("ToggleReminder failed: %v", err)
	}
	if toggled == nil {
		t.Fatal("returned nil")
	}
	if !toggled.Completed {
		t.Fatal("should be completed after toggle")
	}

	// 再切换回来
	toggled2, _ := s.ToggleReminder(dir, rem.ID)
	if toggled2.Completed {
		t.Fatal("should be uncompleted after second toggle")
	}
}

func TestReminder_ToggleNonExist(t *testing.T) {
	dir := t.TempDir()
	s := NewReminderService()
	result, err := s.ToggleReminder(dir, "nonexistent")
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if result != nil {
		t.Fatal("should return nil for non-existent")
	}
}

func TestReminder_GetUpcoming(t *testing.T) {
	dir := t.TempDir()
	s := NewReminderService()

	now := time.Now()
	// 未来 2 小时 — 应出现
	s.AddReminder(dir, "a.md", "upcoming", now.Add(2*time.Hour).Format(time.RFC3339))
	// 过去 — 不应出现
	s.AddReminder(dir, "b.md", "past", now.Add(-1*time.Hour).Format(time.RFC3339))
	// 未来 48 小时 — 超出 24h 窗口，不应出现
	s.AddReminder(dir, "c.md", "far", now.Add(48*time.Hour).Format(time.RFC3339))
	// 已完成 — 不应出现
	rem, _ := s.AddReminder(dir, "d.md", "done", now.Add(2*time.Hour).Format(time.RFC3339))
	s.ToggleReminder(dir, rem.ID)

	upcoming, err := s.GetUpcomingReminders(dir)
	if err != nil {
		t.Fatalf("GetUpcomingReminders failed: %v", err)
	}
	if len(upcoming) != 1 {
		t.Fatalf("expected 1 upcoming, got %d", len(upcoming))
	}
	if upcoming[0].Content != "upcoming" {
		t.Fatalf("expected 'upcoming', got '%s'", upcoming[0].Content)
	}
}

func TestReminder_EmptyWorkspace(t *testing.T) {
	dir := t.TempDir()
	s := NewReminderService()
	all, err := s.GetAllReminders(dir)
	if err != nil {
		t.Fatalf("GetAllReminders on empty workspace failed: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected 0 reminders, got %d", len(all))
	}
}

func TestReminder_Persistence(t *testing.T) {
	dir := t.TempDir()
	s1 := NewReminderService()
	rem, _ := s1.AddReminder(dir, filepath.Join("notes", "a.md"), "persist test", time.Now().Add(1*time.Hour).Format(time.RFC3339))

	// 新实例应能读到旧数据
	s2 := NewReminderService()
	all, _ := s2.GetAllReminders(dir)
	if len(all) != 1 {
		t.Fatalf("expected 1 reminder after reload, got %d", len(all))
	}
	if all[0].ID != rem.ID {
		t.Fatalf("ID mismatch: %s vs %s", all[0].ID, rem.ID)
	}
}
