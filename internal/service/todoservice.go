package service

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// TodoItem 表示一个待办事项
type TodoItem struct {
	ID        string `json:"id"`
	FilePath  string `json:"filePath"`
	FileName  string `json:"fileName"`
	Content   string `json:"content"`
	LineIndex int    `json:"lineIndex"`
	Completed bool   `json:"completed"`
	Priority  string `json:"priority"` // high, medium, low
}

// TodoService 提供待办事项管理功能
type TodoService struct {
	todoRegex *regexp.Regexp
}

// NewTodoService 创建待办服务实例
func NewTodoService() *TodoService {
	return &TodoService{
		// 匹配 - [ ] 或 - [x] 格式的待办事项
		todoRegex: regexp.MustCompile(`^\s*[-*]\s+\[([ xX])\]\s+(.*)$`),
	}
}

// GetAllTodos 获取工作区中所有待办事项
func (s *TodoService) GetAllTodos(workspacePath string) ([]*TodoItem, error) {
	// 获取所有 Markdown 文件
	var allFiles []string
	err := filepath.Walk(workspacePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".md" || ext == ".markdown" {
			allFiles = append(allFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 并发提取待办事项（有界 worker 池；旧实现每文件一个 goroutine）
	var todos []*TodoItem
	var mu sync.Mutex

	forEachFileBounded(allFiles, func(fp string) {
		content, err := os.ReadFile(fp)
		if err != nil {
			return
		}
		lines := strings.Split(string(content), "\n")
		relPath, _ := filepath.Rel(workspacePath, fp)
		relPath = filepath.ToSlash(relPath)
		fileName := filepath.Base(fp)

		for i, line := range lines {
			matches := s.todoRegex.FindStringSubmatch(line)
			if matches == nil {
				continue
			}
			completed := matches[1] == "x" || matches[1] == "X"
			todoContent := strings.TrimSpace(matches[2])

			// 检测优先级（! 高优先级，!! 更高）
			priority := "medium"
			if strings.HasPrefix(todoContent, "!!") {
				priority = "high"
				todoContent = strings.TrimPrefix(todoContent, "!!")
			} else if strings.HasPrefix(todoContent, "!") {
				priority = "high"
				todoContent = strings.TrimPrefix(todoContent, "!")
			}
			todoContent = strings.TrimSpace(todoContent)

			id := relPath + ":" + string(rune(i))

			mu.Lock()
			todos = append(todos, &TodoItem{
				ID:        id,
				FilePath:  relPath,
				FileName:  fileName,
				Content:   todoContent,
				LineIndex: i,
				Completed: completed,
				Priority:  priority,
			})
			mu.Unlock()
		}
	})

	// 排序：未完成在前，高优先级在前
	for i := 0; i < len(todos); i++ {
		for j := i + 1; j < len(todos); j++ {
			// 未完成在前
			if todos[i].Completed && !todos[j].Completed {
				todos[i], todos[j] = todos[j], todos[i]
				continue
			}
			if todos[i].Completed == todos[j].Completed {
				// 高优先级在前
				priorityOrder := map[string]int{"high": 0, "medium": 1, "low": 2}
				if priorityOrder[todos[i].Priority] > priorityOrder[todos[j].Priority] {
					todos[i], todos[j] = todos[j], todos[i]
				}
			}
		}
	}

	return todos, nil
}

// ToggleTodo 切换待办事项的完成状态
func (s *TodoService) ToggleTodo(workspacePath string, filePath string, lineIndex int) error {
	// 防路径穿越：解析后的路径必须仍位于工作区内
	fullPath := filepath.Join(workspacePath, filePath)
	rel, err := filepath.Rel(workspacePath, fullPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("非法文件路径：%s", filePath)
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	if lineIndex < 0 || lineIndex >= len(lines) {
		return nil
	}

	line := lines[lineIndex]
	matches := s.todoRegex.FindStringSubmatch(line)
	if matches == nil {
		return nil
	}

	// 切换完成状态
	if matches[1] == " " {
		line = strings.Replace(line, "[ ]", "[x]", 1)
	} else {
		line = strings.Replace(line, "[x]", "[ ]", 1)
		line = strings.Replace(line, "[X]", "[ ]", 1)
	}
	lines[lineIndex] = line

	// #nosec G703 -- fullPath 已在上方通过 filepath.Rel 校验确保位于工作区内
	// 原子写：这里写回的是用户的笔记正文，崩溃时不能留下半截文件
	return atomicWrite(fullPath, []byte(strings.Join(lines, "\n")), 0644)
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
