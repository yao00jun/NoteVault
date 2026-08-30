package service

import (
	"github.com/notevault/notevault/internal/core"
)

// 本文件定义所有 Service 的公开接口（端口）。
// Go 鸭子类型：各 *Service 已隐式实现这些接口，无需显式声明 implements。
// 接口的用途：1) 文档化公开契约；2) 便于测试注入 mock；3) 未来替换实现。
// 编译时断言放在文件末尾，确保签名不漂移。

// ---------------------------------------------------------------------------
// 文件操作
// ---------------------------------------------------------------------------

// FileOperator 定义文件 CRUD + 文件树 + 图片保存接口
type FileOperator interface {
	GetFileTree(workspacePath string) ([]*FileNode, error)
	ReadFile(workspacePath string, relativePath string) (string, error)
	CreateFile(workspacePath string, relativePath string, content string) (*FileNode, error)
	SaveFile(workspacePath string, relativePath string, content string) error
	DeleteFile(workspacePath string, relativePath string) error
	RenameFile(workspacePath string, oldRelativePath string, newName string) (*FileNode, error)
	CreateFolder(workspacePath string, relativePath string) (*FileNode, error)
	SaveImage(workspacePath string, fileName string, data []byte) (string, error)
}

// ---------------------------------------------------------------------------
// 搜索
// ---------------------------------------------------------------------------

// Searcher 定义全文搜索接口（基于内存反向索引）
type Searcher interface {
	Search(workspacePath string, query string) ([]*SearchResult, error)
}

// ---------------------------------------------------------------------------
// 标签
// ---------------------------------------------------------------------------

// Tagger 定义标签管理接口（带 TTL 缓存）
type Tagger interface {
	GetAllTags(workspacePath string) ([]*TagInfo, error)
	GetFilesByTag(workspacePath string, tag string) ([]*TagFileInfo, error)
	InvalidateCache(workspacePath string)
}

// ---------------------------------------------------------------------------
// 知识图谱
// ---------------------------------------------------------------------------

// Grapher 定义知识图谱构建接口
type Grapher interface {
	GetGraph(workspacePath string) (*GraphData, error)
}

// ---------------------------------------------------------------------------
// 待办事项
// ---------------------------------------------------------------------------

// TodoOperator 定义待办事项管理接口
type TodoOperator interface {
	GetAllTodos(workspacePath string) ([]*TodoItem, error)
	ToggleTodo(workspacePath string, filePath string, lineIndex int) error
	GetTodoStats(workspacePath string) (map[string]int, error)
}

// ---------------------------------------------------------------------------
// 提醒
// ---------------------------------------------------------------------------

// ReminderOperator 定义提醒管理接口
type ReminderOperator interface {
	GetAllReminders(workspacePath string) ([]*Reminder, error)
	AddReminder(workspacePath string, filePath string, content string, remindAt string) (*Reminder, error)
	DeleteReminder(workspacePath string, id string) error
	ToggleReminder(workspacePath string, id string) (*Reminder, error)
	GetUpcomingReminders(workspacePath string) ([]*Reminder, error)
}

// ---------------------------------------------------------------------------
// 归档
// ---------------------------------------------------------------------------

// ArchiveOperator 定义归档管理接口
type ArchiveOperator interface {
	ArchiveFile(workspacePath string, relativePath string) (*ArchivedFile, error)
	UnarchiveFile(workspacePath string, relativePath string) error
	GetArchivedFiles(workspacePath string) ([]*ArchivedFile, error)
	IsArchived(workspacePath string, relativePath string) bool
}

// ---------------------------------------------------------------------------
// 回收站
// ---------------------------------------------------------------------------

// TrashOperator 定义回收站管理接口
type TrashOperator interface {
	MoveToTrash(workspacePath string, relativePath string) (*TrashedFile, error)
	RestoreFromTrash(workspacePath string, id string) error
	PermanentlyDelete(workspacePath string, id string) error
	GetTrashedFiles(workspacePath string) ([]*TrashedFile, error)
	EmptyTrash(workspacePath string) error
	GetTrashStats(workspacePath string) (map[string]int64, error)
}

// ---------------------------------------------------------------------------
// 工作区
// ---------------------------------------------------------------------------

// WorkspaceOperator 定义工作区管理接口
type WorkspaceOperator interface {
	ListWorkspaces() ([]Workspace, error)
	CreateWorkspace(name string, path string) (*Workspace, error)
	GetCurrentWorkspace() (*Workspace, error)
	SetCurrentWorkspace(workspaceID string) error
	DeleteWorkspace(workspaceID string) error
	GetWorkspaceByID(workspaceID string) (*Workspace, error)
}

// ---------------------------------------------------------------------------
// 导出
// ---------------------------------------------------------------------------

// Exporter 定义笔记导出接口
type Exporter interface {
	ExportWorkspaceMarkdown(workspacePath, destPath string) error
	ExportNoteMarkdown(workspacePath, relPath, destPath string) error
	SaveText(destPath, content string) error
}

// ---------------------------------------------------------------------------
// AI 摘要
// ---------------------------------------------------------------------------

// Summarizer 定义 AI 摘要接口
type Summarizer interface {
	Summarize(apiKey, baseURL, model, content string) (string, error)
}

// ---------------------------------------------------------------------------
// RAG 知识库问答
// ---------------------------------------------------------------------------

// QnAProvider 定义知识库问答接口（检索增强生成）
type QnAProvider interface {
	Answer(apiKey, baseURL, model, workspacePath, question string) (*QnAResponse, error)
}

// ---------------------------------------------------------------------------
// 数据导入
// ---------------------------------------------------------------------------

// Importer 定义笔记数据导入接口（Markdown 文件夹 / zip）
// 注：实现暂无独立的 Obsidian vault 方法——Obsidian 本质是 Markdown 文件夹，
// 直接用 ImportMarkdownFolder 导入其仓库目录即可。新增导入格式时须同步此注释。
type Importer interface {
	ImportMarkdownFolder(srcDir, workspacePath string, opts ImportOptions) (*ImportResult, error)
	ImportZip(zipPath, workspacePath string, opts ImportOptions) (*ImportResult, error)
}

// ---------------------------------------------------------------------------
// 错误监控
// ---------------------------------------------------------------------------

// Reporter 定义错误上报接口（本地日志 + 可选 Sentry）
// 注：UpdateConfig 是前端真实调用的 API（设置页运行时切换 DSN / 本地日志开关），
// 必须纳入端口，否则绑定方法集契约测试会正确报出漂移。
type Reporter interface {
	ReportError(report core.ErrorReport) error
	UpdateConfig(cfg core.ErrorMonitorConfig)
}

// ---------------------------------------------------------------------------
// 插件系统
// ---------------------------------------------------------------------------

// PluginOperator 定义插件管理接口
// （发现 / 启用 / 禁用 / 源码读取 / 信任等级授权）
type PluginOperator interface {
	// ListPlugins 扫描插件目录并返回完整插件清单。
	// 原签名带 rescan bool 参数，但实现恒重扫（无缓存），该参数是死参数；
	// 已移除，避免调用方误以为存在缓存语义。
	ListPlugins() ([]core.PluginInfo, error)
	GetPlugin(id string) (*core.PluginInfo, error)
	EnablePlugin(id string) error
	DisablePlugin(id string) error
	// GrantTrust 授权插件以 full 信任等级在主进程运行。
	// 前置条件是该插件 manifest 声明了 trust=full；授权与源码哈希绑定，
	// 插件更新后哈希对不上，授权自动失效，需用户重新确认。
	GrantTrust(id string) error
	// RevokeTrust 撤销 full 信任授权，插件立即回落到沙箱等级运行。
	RevokeTrust(id string) error
	// LoadPluginData / SavePluginData 插件私有数据持久化（#29）。
	// 插件数据存在应用数据目录而非工作区——它属于插件配置，不是用户的笔记。
	// 数据不存在时 LoadPluginData 返回空串而非错误。
	LoadPluginData(id string) (string, error)
	SavePluginData(id string, data string) error
	GetPluginsDir() string
}

// ---------------------------------------------------------------------------
// 编译时断言：确保各 *Service 实现对应接口
// ---------------------------------------------------------------------------

var _ FileOperator = (*FileService)(nil)
var _ Searcher = (*SearchService)(nil)
var _ Tagger = (*TagService)(nil)
var _ Grapher = (*GraphService)(nil)
var _ TodoOperator = (*TodoService)(nil)
var _ ReminderOperator = (*ReminderService)(nil)
var _ ArchiveOperator = (*ArchiveService)(nil)
var _ TrashOperator = (*TrashService)(nil)
var _ WorkspaceOperator = (*WorkspaceService)(nil)
var _ Exporter = (*ExportService)(nil)
var _ Summarizer = (*SummarizeService)(nil)
var _ QnAProvider = (*QnAService)(nil)
var _ Importer = (*ImportService)(nil)

// 注：以下两个端口的实现位于其他包，其编译时断言随实现下沉到各包内，
// 避免 service 包反向依赖它们：
//   - Reporter        → *ErrorMonitor（internal/infra/monitor/errormonitor.go）
//   - PluginOperator  → *PluginService（internal/plugin/pluginservice.go）
