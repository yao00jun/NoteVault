package service

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/notevault/notevault/internal/infra/schema"
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
	// indexes 索引注册表（E-4）。工作区是索引的生命周期边界：
	// 切走时释放其索引，删除时一并释放。nil 时不做索引管理（零值实例可用）。
	indexes *SearchIndexRegistry

	// fileChangeSink 文件变更事件的出口（E-6）。nil 时不启监控（零值实例可用，
	// 测试与未接入 emitter 的场景都靠它短路）。
	fileChangeSink FileChangeSink
	// watcher 是当前正在运行的工作区文件监控器；切换工作区时停掉旧的、起新的。
	watcher   *WorkspaceWatcher
	watcherMu sync.Mutex
}

// NewWorkspaceService 创建工作区服务实例
func NewWorkspaceService() *WorkspaceService {
	configDir, _ := os.UserConfigDir()
	dir := filepath.Join(configDir, "NoteVault")
	_ = os.MkdirAll(dir, 0750)
	return &WorkspaceService{configDir: dir}
}

// NewWorkspaceServiceWithRegistry 创建工作区服务实例，并接管索引的释放。
//
// 索引的生命周期应跟随工作区，而工作区的切换与删除都发生在这里，
// 因此把注册表交给 WorkspaceService 持有是职责最自然的位置。
func NewWorkspaceServiceWithRegistry(indexes *SearchIndexRegistry) *WorkspaceService {
	s := NewWorkspaceService()
	s.indexes = indexes
	return s
}

// NewWorkspaceServiceWithRegistryAndSink 在注册表基础上再接入文件变更事件出口。
//
// sink 为 nil 时退化为不启监控——这保证了测试与未接入 emitter 的场景零副作用：
// 现有工作区测试（直接构造 *WorkspaceService）完全不受 E-6 影响。
func NewWorkspaceServiceWithRegistryAndSink(indexes *SearchIndexRegistry, sink FileChangeSink) *WorkspaceService {
	s := NewWorkspaceServiceWithRegistry(indexes)
	s.fileChangeSink = sink
	return s
}

// evictIndex 释放指定工作区的搜索索引。
//
// 失败只记日志：索引是派生缓存，摘不掉最坏是重建，不该让它阻断工作区操作。
func (s *WorkspaceService) evictIndex(workspacePath string) {
	if s.indexes == nil || strings.TrimSpace(workspacePath) == "" {
		return
	}
	if err := s.indexes.Evict(workspacePath); err != nil {
		log.Printf("[workspace] 释放搜索索引失败 path=%s err=%v", workspacePath, err)
	}
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
	// 统一版本信封（E-7）。工作区列表是最不能丢的一份元数据：
	// 读不出来等于所有知识库都要用户重新添加一遍，因此旧裸数组必须继续能读。
	workspaces, res, err := schema.UnmarshalAs[[]Workspace](data, schema.WorkspaceList)
	if err != nil {
		return nil, err
	}
	if res.Compat == schema.CompatNewer {
		log.Printf("[workspace] 列表 schemaVersion=%d 高于当前支持的 %d，按当前结构尽力解析",
			res.FileVersion, schema.WorkspaceList.Version)
	}
	if workspaces == nil {
		workspaces = []Workspace{}
	}
	return workspaces, nil
}

// saveWorkspaces 保存工作区列表到文件
func (s *WorkspaceService) saveWorkspaces(workspaces []Workspace) error {
	if workspaces == nil {
		workspaces = []Workspace{}
	}
	data, err := schema.MarshalAs(schema.WorkspaceList, workspaces)
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

	// 旧格式是裸 JSON 字符串（"ws_123"），信封解析会走 CompatLegacy 分支兼容读出。
	currentID, _, err := schema.UnmarshalAs[string](data, schema.CurrentWorkspace)
	if err != nil {
		return nil, err
	}

	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return nil, err
	}

	for _, ws := range workspaces {
		if ws.ID == currentID {
			// E-6：首次读到当前工作区时，顺带把文件监控接上（懒启动）。
			// 前端启动路径（App.vue）本来就会调 GetCurrentWorkspace，
			// 而它只读、不走 SetCurrentWorkspace——没有这一步，
			// "上次退出时的工作区"在整个会话里都没有监控。
			s.ensureFileWatcherFor(&ws)
			return &ws, nil
		}
	}

	return nil, nil
}

// SetCurrentWorkspace 设置当前工作区
func (s *WorkspaceService) SetCurrentWorkspace(workspaceID string) error {
	// E-4：切走旧工作区前先落盘并释放它的索引。
	// 顺序很重要——必须在新工作区生效「之前」读旧的，否则拿不到要释放的那一个。
	// 读旧工作区失败不阻断切换：索引是缓存，最坏是重建。
	if prev, err := s.GetCurrentWorkspace(); err == nil && prev != nil && prev.ID != workspaceID {
		s.evictIndex(prev.Path)
	}

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

	data, err := schema.MarshalAs(schema.CurrentWorkspace, workspaceID)
	if err != nil {
		return err
	}

	// 原子写：崩溃时不留半截 JSON（否则"当前工作区"指针丢失）
	if err := atomicWrite(s.currentWorkspaceFilePath(), data, 0644); err != nil {
		return err
	}

	// E-6：当前工作区变了，重启文件监控指向新路径。
	// 旧监控在 startWatcherFor 内部会被先停掉，所以这里不用显式 stop。
	if ws, gerr := s.GetWorkspaceByID(workspaceID); gerr == nil && ws != nil {
		s.startWatcherFor(ws.Path)
	}
	return nil
}

// startWatcherFor 为指定工作区路径启动文件监控（E-6）。
//
// 调用前会先停掉旧监控；sink 未注入时不启监控（零值实例 / 测试场景）。
// 监控创建失败只记日志不致命：最坏是事件晚到，不该因此阻断工作区切换。
func (s *WorkspaceService) startWatcherFor(workspacePath string) {
	s.stopWatcher()
	if s.fileChangeSink == nil {
		return
	}
	w, err := NewWorkspaceWatcher(workspacePath, s.fileChangeSink)
	if err != nil {
		log.Printf("[workspace] 启动文件监控失败 %s: %v", workspacePath, err)
		return
	}
	w.Start()
	s.watcherMu.Lock()
	s.watcher = w
	s.watcherMu.Unlock()
}

// ensureFileWatcherFor 确保指定工作区的文件监控在运行（E-6，懒启动入口）。
//
// 刻意是非导出方法：它是后端生命周期管理，不应暴露成前端 API
// （ports 契约测试会把多余的导出方法拦下来）。触发点有两类：
//  1. GetCurrentWorkspace 读到工作区（应用启动路径，懒启动）；
//  2. SetCurrentWorkspace 切换工作区（显式重启，直接走 startWatcherFor）。
//
// 已有监控在跑时不动——避免每次视图刷新GetCurrentWorkspace 都重建 watcher；
// sink 未注入时 startWatcherFor 内部会短路，零值实例（测试）零副作用。
func (s *WorkspaceService) ensureFileWatcherFor(ws *Workspace) {
	if ws == nil || ws.Path == "" {
		return
	}
	s.watcherMu.Lock()
	running := s.watcher != nil
	s.watcherMu.Unlock()
	if running {
		return
	}
	s.startWatcherFor(ws.Path)
}

// stopWatcher 停止当前文件监控（如果存在）。
func (s *WorkspaceService) stopWatcher() {
	s.watcherMu.Lock()
	w := s.watcher
	s.watcher = nil
	s.watcherMu.Unlock()
	if w != nil {
		w.Stop()
	}
}

// DeleteWorkspace 从列表中删除工作区（不删除实际文件）
func (s *WorkspaceService) DeleteWorkspace(workspaceID string) error {
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return err
	}

	var filtered []Workspace
	var removedPath string
	for _, ws := range workspaces {
		if ws.ID != workspaceID {
			filtered = append(filtered, ws)
		} else {
			removedPath = ws.Path // E-4：记下路径，从列表移除后释放其索引
		}
	}

	if err := s.saveWorkspaces(filtered); err != nil {
		return err
	}

	s.evictIndex(removedPath)

	// 如果删除的是当前工作区，清除当前工作区并停掉它的文件监控（E-6）。
	current, err := s.GetCurrentWorkspace()
	if err != nil {
		return err
	}
	if current != nil && current.ID == workspaceID {
		s.stopWatcher()
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
