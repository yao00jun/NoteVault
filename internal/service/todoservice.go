package service

import (
	"sort"
	"sync"
	"time"
)

// TodoItem 表示一个待办事项
type TodoItem struct {
	ID          string              `json:"id"`
	FilePath    string              `json:"filePath"`
	FileName    string              `json:"fileName"`
	Content     string              `json:"content"`
	LineIndex   int                 `json:"lineIndex"`
	Completed   bool                `json:"completed"`
	Priority    string              `json:"priority"` // high, medium, low
	SourceLine  string              `json:"sourceLine"`
	Title       string              `json:"title"`
	Type        string              `json:"type"`
	Project     string              `json:"project"`
	ProjectPath string              `json:"projectPath"`
	Due         string              `json:"due"`
	Date        string              `json:"date"`
	CompletedAt string              `json:"completedAt"`
	Status      string              `json:"status"`
	Blocker     string              `json:"blocker"`
	Progress    []WorkbenchProgress `json:"progress"`
}

// TodoService 提供待办事项管理功能
type TodoService struct {
	workbenchMu sync.RWMutex
}

// NewTodoService 创建待办服务实例
func NewTodoService() *TodoService {
	return &TodoService{}
}

// GetAllTodos 获取工作区中所有待办事项
func (s *TodoService) GetAllTodos(workspacePath string) ([]*TodoItem, error) {
	s.workbenchMu.RLock()
	defer s.workbenchMu.RUnlock()
	files, _, err := readWorkbenchFiles(workspacePath)
	if err != nil {
		return nil, err
	}
	todos := make([]*TodoItem, 0)
	for _, file := range files {
		todos = append(todos, parseWorkbenchTasks(file)...)
	}

	// 排序：未完成在前，高优先级在前（sort.Slice 稳定且 O(n log n)）
	priorityOrder := map[string]int{"high": 0, "medium": 1, "low": 2}
	sort.SliceStable(todos, func(i, j int) bool {
		if todos[i].Completed != todos[j].Completed {
			return !todos[i].Completed
		}
		return priorityOrder[todos[i].Priority] < priorityOrder[todos[j].Priority]
	})

	return todos, nil
}

// ToggleTodo 切换待办事项的完成状态
func (s *TodoService) ToggleTodo(workspacePath string, filePath string, lineIndex int) error {
	s.workbenchMu.Lock()
	defer s.workbenchMu.Unlock()
	change, err := readWorkbenchChange(workspacePath, filePath)
	if err != nil {
		return err
	}
	file := newWorkbenchFile(change.relative, change.before, "")
	for _, task := range parseWorkbenchTasks(file) {
		if task.LineIndex == lineIndex {
			return s.updateWorkbenchTask(workspacePath, filePath, lineIndex, task.SourceLine, "toggle", "", time.Now())
		}
	}
	// Historical callers treat a missing/non-task line as an idempotent no-op.
	return nil
}

// GetTodoStats 获取待办事项统计
func (s *TodoService) GetTodoStats(workspacePath string) (map[string]int, error) {
	todos, err := s.GetAllTodos(workspacePath)
	if err != nil {
		return nil, err
	}

	stats := map[string]int{
		"total":     len(todos),
		"completed": 0,
		"pending":   0,
		"high":      0,
		"medium":    0,
		"low":       0,
	}

	for _, todo := range todos {
		if todo.Completed {
			stats["completed"]++
		} else {
			stats["pending"]++
		}
		stats[todo.Priority]++
	}

	return stats, nil
}
