package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Workspace 表示一个知识库工作区
type Workspace struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	CreatedAt    string `json:"createdAt"`
	LastOpenedAt string `json:"lastOpenedAt"`
}

// WorkspaceService 管理工作区的创建、打开、删除和持久化
type WorkspaceService struct {
	configDir string
}

// NewWorkspaceService 创建工作区服务实例
func NewWorkspaceService() *WorkspaceService {
	configDir, _ := os.UserConfigDir()
	dir := filepath.Join(configDir, "NoteVault")
	_ = os.MkdirAll(dir, 0750)
	return &WorkspaceService{configDir: dir}
}

// workspacesFilePath 返回工作区配置文件路径
func (s *WorkspaceService) workspacesFilePath() string {
	return filepath.Join(s.configDir, "workspaces.json")
}

// currentWorkspaceFilePath 返回当前工作区配置文件路径
func (s *WorkspaceService) currentWorkspaceFilePath() string {
	return filepath.Join(s.configDir, "current_workspace.json")
}

// ListWorkspaces 获取所有工作区列表
func (s *WorkspaceService) ListWorkspaces() ([]Workspace, error) {
	data, err := os.ReadFile(s.workspacesFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return []Workspace{}, nil
		}
		return nil, err
	}
	var workspaces []Workspace
	if err := json.Unmarshal(data, &workspaces); err != nil {
		return nil, err
	}
	return workspaces, nil
}

// saveWorkspaces 保存工作区列表到文件
func (s *WorkspaceService) saveWorkspaces(workspaces []Workspace) error {
	data, err := json.MarshalIndent(workspaces, "", "  ")
	if err != nil {
		return err
	}
	// 原子写：崩溃时不留半截 JSON（否则工作区列表丢失）
	return atomicWrite(s.workspacesFilePath(), data, 0644)
}

// CreateWorkspace 创建一个新工作区
// name: 工作区名称
// path: 工作区文件夹路径（如果文件夹不存在则创建）
// 如果路径已存在工作区，则更新名称和最后打开时间，返回已有工作区
func (s *WorkspaceService) CreateWorkspace(name string, path string) (*Workspace, error) {
	// 确保文件夹存在
	if err := os.MkdirAll(path, 0750); err != nil {
		return nil, err
	}

	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return nil, err
	}

	// 按路径去重：如果路径已存在，更新名称和最后打开时间
	for i := range workspaces {
		if workspaces[i].Path == path {
			workspaces[i].Name = name
			workspaces[i].LastOpenedAt = time.Now().Format(time.RFC3339)
			if err := s.saveWorkspaces(workspaces); err != nil {
				return nil, err
			}
			if err := s.SetCurrentWorkspace(workspaces[i].ID); err != nil {
				return nil, err
			}
			return &workspaces[i], nil
		}
	}

	// 生成唯一 ID（纳秒级时间戳，避免同一秒内冲突）
	id := fmt.Sprintf("ws_%d", time.Now().UnixNano())

	workspace := Workspace{
		ID:           id,
		Name:         name,
		Path:         path,
		CreatedAt:    time.Now().Format(time.RFC3339),
		LastOpenedAt: time.Now().Format(time.RFC3339),
	}

	workspaces = append(workspaces, workspace)
	if err := s.saveWorkspaces(workspaces); err != nil {
		return nil, err
	}

	// 设置为当前工作区
	if err := s.SetCurrentWorkspace(id); err != nil {
		return nil, err
	}

	return &workspace, nil
}

// GetCurrentWorkspace 获取当前打开的工作区
func (s *WorkspaceService) GetCurrentWorkspace() (*Workspace, error) {
	data, err := os.ReadFile(s.currentWorkspaceFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var currentID string
	if err := json.Unmarshal(data, &currentID); err != nil {
		return nil, err
	}

	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return nil, err
	}

	for _, ws := range workspaces {
		if ws.ID == currentID {
			return &ws, nil
		}
	}

	return nil, nil
}

// SetCurrentWorkspace 设置当前工作区
func (s *WorkspaceService) SetCurrentWorkspace(workspaceID string) error {
	// 更新最后打开时间
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return err
	}

	for i := range workspaces {
		if workspaces[i].ID == workspaceID {
			workspaces[i].LastOpenedAt = time.Now().Format(time.RFC3339)
			break
		}
	}

	if err := s.saveWorkspaces(workspaces); err != nil {
		return err
	}

	data, err := json.Marshal(workspaceID)
	if err != nil {
		return err
	}

	// 原子写：崩溃时不留半截 JSON（否则"当前工作区"指针丢失）
	return atomicWrite(s.currentWorkspaceFilePath(), data, 0644)
}

// DeleteWorkspace 从列表中删除工作区（不删除实际文件）
func (s *WorkspaceService) DeleteWorkspace(workspaceID string) error {
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return err
	}

	var filtered []Workspace
	for _, ws := range workspaces {
		if ws.ID != workspaceID {
			filtered = append(filtered, ws)
		}
	}

	if err := s.saveWorkspaces(filtered); err != nil {
		return err
	}

	// 如果删除的是当前工作区，清除当前工作区
	current, err := s.GetCurrentWorkspace()
	if err != nil {
		return err
	}
	if current != nil && current.ID == workspaceID {
		if err := os.Remove(s.currentWorkspaceFilePath()); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("清除当前工作区状态失败：%w", err)
		}
	}

	return nil
}

// GetWorkspaceByID 根据 ID 获取工作区
func (s *WorkspaceService) GetWorkspaceByID(workspaceID string) (*Workspace, error) {
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return nil, err
	}

	for _, ws := range workspaces {
		if ws.ID == workspaceID {
			return &ws, nil
		}
	}

	return nil, nil
}
