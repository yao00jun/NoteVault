package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTodo_GetAllTodos(t *testing.T) {
	dir := t.TempDir()
	// 含未完成、已完成、高优先级、普通优先级
	mustWrite(t, filepath.Join(dir, "a.md"), "# A\n- [ ] 普通任务\n- [x] 已完成任务\n- [ ] !高优先级任务\n")
	mustWrite(t, filepath.Join(dir, "b.md"), "# B\n* [ ] 星号格式\n- [X] 大写X已完成\n")

	s := NewTodoService()
	todos, err := s.GetAllTodos(dir)
	if err != nil {
		t.Fatalf("GetAllTodos failed: %v", err)
	}
	if len(todos) != 5 {
		t.Fatalf("expected 5 todos, got %d", len(todos))
	}

	// 验证排序：未完成在前
	completedCount := 0
	for _, todo := range todos {
		if todo.Completed {
			completedCount++
		}
	}
	if completedCount != 2 {
		t.Fatalf("expected 2 completed, got %d", completedCount)
	}

	// 验证高优先级
	highCount := 0
	for _, todo := range todos {
		if todo.Priority == "high" {
			highCount++
		}
	}
	if highCount != 1 {
		t.Fatalf("expected 1 high priority, got %d", highCount)
	}
}

func TestTodo_EmptyWorkspace(t *testing.T) {
	dir := t.TempDir()
	s := NewTodoService()
	todos, err := s.GetAllTodos(dir)
	if err != nil {
		t.Fatalf("GetAllTodos on empty dir failed: %v", err)
	}
	if len(todos) != 0 {
		t.Fatalf("expected 0 todos, got %d", len(todos))
	}
}

func TestTodo_ToggleTodo(t *testing.T) {
	dir := t.TempDir()
	content := "# Task\n- [ ] 未完成任务\n一些其他文字\n- [x] 已完成任务\n"
	mustWrite(t, filepath.Join(dir, "task.md"), content)

	s := NewTodoService()
	// 切换第 1 行（未完成 -> 已完成）
	if err := s.ToggleTodo(dir, "task.md", 1); err != nil {
		t.Fatalf("ToggleTodo failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "task.md"))
	str := string(data)
	if !contains(str, "[x] 未完成任务") {
		t.Fatalf("expected [x] after toggle, got: %s", str)
	}

	// 切换回去
	if err := s.ToggleTodo(dir, "task.md", 1); err != nil {
		t.Fatalf("ToggleTodo back failed: %v", err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "task.md"))
	str = string(data)
	if !contains(str, "[ ] 未完成任务") {
		t.Fatalf("expected [ ] after toggle back, got: %s", str)
	}
}

func TestTodo_ToggleInvalidLine(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# A\n普通文字\n")

	s := NewTodoService()
	// 非待办行，应安全返回 nil
	if err := s.ToggleTodo(dir, "a.md", 1); err != nil {
		t.Fatalf("ToggleTodo on non-todo line should not error: %v", err)
	}
	// 越界行
	if err := s.ToggleTodo(dir, "a.md", 99); err != nil {
		t.Fatalf("ToggleTodo on out-of-range line should not error: %v", err)
	}
}

func TestTodo_ToggleTodoPathTraversal(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "outside.md")
	mustWrite(t, outside, "# Outside\n- [ ] 不应被修改\n")

	s := NewTodoService()
	// ../ 穿越到工作区外，应拒绝
	if err := s.ToggleTodo(dir, "../"+filepath.Base(outside), 1); err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}

	// 验证工作区外文件未被修改
	data, _ := os.ReadFile(outside)
	if !contains(string(data), "[ ] 不应被修改") {
		t.Fatalf("outside file was modified: %s", string(data))
	}
}

func TestTodo_GetTodoStats(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# A\n- [ ] task1\n- [x] task2\n- [ ] !urgent\n")

	s := NewTodoService()
	stats, err := s.GetTodoStats(dir)
	if err != nil {
		t.Fatalf("GetTodoStats failed: %v", err)
	}
	if stats["total"] != 3 {
		t.Fatalf("expected total=3, got %d", stats["total"])
	}
	if stats["completed"] != 1 {
		t.Fatalf("expected completed=1, got %d", stats["completed"])
	}
	if stats["pending"] != 2 {
		t.Fatalf("expected pending=2, got %d", stats["pending"])
	}
	if stats["high"] != 1 {
		t.Fatalf("expected high=1, got %d", stats["high"])
	}
}

// contains 是一个简单的字符串包含检查
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
