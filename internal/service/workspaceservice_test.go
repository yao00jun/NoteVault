package service

import (
	"os"
	"path/filepath"
	"testing"
)

// createTestService 创建一个使用临时目录的测试服务实例
func createTestService(t *testing.T) *WorkspaceService {
	t.Helper()
	tmpDir := t.TempDir()
	return &WorkspaceService{configDir: tmpDir}
}

func TestNewWorkspaceService(t *testing.T) {
	service := NewWorkspaceService()
	if service == nil {
		t.Fatal("NewWorkspaceService returned nil")
	}
	if service.configDir == "" {
		t.Error("configDir should not be empty")
	}
}

func TestCreateWorkspace(t *testing.T) {
	service := createTestService(t)
	tmpDir := t.TempDir()
	wsPath := filepath.Join(tmpDir, "test-vault")

	ws, err := service.CreateWorkspace("测试工作区", wsPath)
	if err != nil {
		t.Fatalf("CreateWorkspace failed: %v", err)
	}
	if ws == nil {
		t.Fatal("CreateWorkspace returned nil workspace")
	}
	if ws.Name != "测试工作区" {
		t.Errorf("expected name '测试工作区', got '%s'", ws.Name)
	}
	if ws.Path != wsPath {
		t.Errorf("expected path '%s', got '%s'", wsPath, ws.Path)
	}
	if ws.ID == "" {
		t.Error("workspace ID should not be empty")
	}

	// 验证文件夹已创建
	if _, err := os.Stat(wsPath); os.IsNotExist(err) {
		t.Error("workspace directory should be created")
	}
}

func TestListWorkspaces(t *testing.T) {
	service := createTestService(t)
	tmpDir := t.TempDir()

	// 初始应为空列表
	list, err := service.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces failed: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d items", len(list))
	}

	// 创建两个工作区
	_, err = service.CreateWorkspace("工作区1", filepath.Join(tmpDir, "ws1"))
	if err != nil {
		t.Fatalf("CreateWorkspace 1 failed: %v", err)
	}
	_, err = service.CreateWorkspace("工作区2", filepath.Join(tmpDir, "ws2"))
	if err != nil {
		t.Fatalf("CreateWorkspace 2 failed: %v", err)
	}

	// 验证列表有两个工作区
	list, err = service.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces failed: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 workspaces, got %d", len(list))
	}
}

func TestGetCurrentWorkspace(t *testing.T) {
	service := createTestService(t)
	tmpDir := t.TempDir()

	// 初始当前工作区应为 nil
	current, err := service.GetCurrentWorkspace()
	if err != nil {
		t.Fatalf("GetCurrentWorkspace failed: %v", err)
	}
	if current != nil {
		t.Error("expected nil current workspace initially")
	}

	// 创建工作区（CreateWorkspace 会自动设置为当前）
	ws, err := service.CreateWorkspace("测试", filepath.Join(tmpDir, "test"))
	if err != nil {
		t.Fatalf("CreateWorkspace failed: %v", err)
	}

	// 验证当前工作区
	current, err = service.GetCurrentWorkspace()
	if err != nil {
		t.Fatalf("GetCurrentWorkspace failed: %v", err)
	}
	if current == nil {
		t.Fatal("expected current workspace, got nil")
	}
	if current.ID != ws.ID {
		t.Errorf("expected current workspace ID '%s', got '%s'", ws.ID, current.ID)
	}
}

func TestSetCurrentWorkspace(t *testing.T) {
	service := createTestService(t)
	tmpDir := t.TempDir()

	ws1, _ := service.CreateWorkspace("工作区1", filepath.Join(tmpDir, "ws1"))
	ws2, _ := service.CreateWorkspace("工作区2", filepath.Join(tmpDir, "ws2"))

	// 当前应该是 ws2（最后创建的）
	current, _ := service.GetCurrentWorkspace()
	if current.ID != ws2.ID {
		t.Errorf("expected current to be ws2, got %s", current.Name)
	}

	// 切换到 ws1
	err := service.SetCurrentWorkspace(ws1.ID)
	if err != nil {
		t.Fatalf("SetCurrentWorkspace failed: %v", err)
	}

	current, _ = service.GetCurrentWorkspace()
	if current.ID != ws1.ID {
		t.Errorf("expected current to be ws1 after switch, got %s", current.Name)
	}
}

func TestDeleteWorkspace(t *testing.T) {
	service := createTestService(t)
	tmpDir := t.TempDir()

	ws1, _ := service.CreateWorkspace("工作区1", filepath.Join(tmpDir, "ws1"))
	ws2, _ := service.CreateWorkspace("工作区2", filepath.Join(tmpDir, "ws2"))

	// 删除 ws1
	err := service.DeleteWorkspace(ws1.ID)
	if err != nil {
		t.Fatalf("DeleteWorkspace failed: %v", err)
	}

	// 验证列表只剩 ws2
	list, _ := service.ListWorkspaces()
	if len(list) != 1 {
		t.Errorf("expected 1 workspace after delete, got %d", len(list))
	}
	if list[0].ID != ws2.ID {
		t.Errorf("expected remaining workspace to be ws2, got %s", list[0].Name)
	}

	// 验证删除当前工作区后当前为 nil
	err = service.DeleteWorkspace(ws2.ID)
	if err != nil {
		t.Fatalf("DeleteWorkspace failed: %v", err)
	}
	current, _ := service.GetCurrentWorkspace()
	if current != nil {
		t.Error("expected nil current workspace after deleting current")
	}
}

func TestGetWorkspaceByID(t *testing.T) {
	service := createTestService(t)
	tmpDir := t.TempDir()

	ws, _ := service.CreateWorkspace("测试", filepath.Join(tmpDir, "test"))

	// 测试存在的 ID
	found, err := service.GetWorkspaceByID(ws.ID)
	if err != nil {
		t.Fatalf("GetWorkspaceByID failed: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find workspace, got nil")
	}
	if found.Name != "测试" {
		t.Errorf("expected name '测试', got '%s'", found.Name)
	}

	// 测试不存在的 ID
	notFound, err := service.GetWorkspaceByID("nonexistent-id")
	if err != nil {
		t.Fatalf("GetWorkspaceByID failed: %v", err)
	}
	if notFound != nil {
		t.Error("expected nil for nonexistent ID")
	}
}

func TestWorkspacePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	service1 := &WorkspaceService{configDir: tmpDir}
	wsPath := filepath.Join(tmpDir, "vault")

	// 在第一个服务实例中创建工作区
	_, err := service1.CreateWorkspace("持久化测试", wsPath)
	if err != nil {
		t.Fatalf("CreateWorkspace failed: %v", err)
	}

	// 创建第二个服务实例（模拟应用重启）
	service2 := &WorkspaceService{configDir: tmpDir}

	// 验证数据已持久化
	list, err := service2.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 workspace after restart, got %d", len(list))
	}
	if list[0].Name != "持久化测试" {
		t.Errorf("expected name '持久化测试', got '%s'", list[0].Name)
	}
}
