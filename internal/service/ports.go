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

// Searcher 定义全文搜索接口（基于内存反向索引 + BM25 打分）
type Searcher interface {
	Search(workspacePath string, query string) ([]*SearchResult, error)
	// GetIndexStats 返回索引的覆盖情况（P0-5）。
	//
	// 存在意义：索引扫描有两道硬上限（maxScanFiles / maxSearchFileSize），
	// 超限部分会被跳过。不把它暴露出来，用户会以为「搜不到就是没有」，
	// 实际上只是那部分文件根本没进索引。
	GetIndexStats(workspacePath string) (*SearchIndexStats, error)
	// GetSearchSnippet 按需为单个结果生成片段（前端滚动到可视区时调用）。
	//
	// Search/HybridSearch 只对前 N 条即时生成 snippet（P0 基线表点明的 p95 优化点），
	// 其余留空；前端把留空结果滚入视区时再用本方法取片段，避免一次性读 200 篇正文。
	// 口径与即时片段一致（首个查询 token 定位、半径 50）。
	GetSearchSnippet(workspacePath, relPath, query string) (string, error)
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
	GetLinkCandidates(workspacePath, query string) ([]*LinkCandidate, error)
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
// 版本快照 / 时间机器
// ---------------------------------------------------------------------------

// SnapshotOperator 定义版本历史接口（内容寻址快照，存放于 .notevault/history/）。
//
// 注：自动留存钩子（captureBeforeWrite / captureBeforeDelete）故意不在端口里——
// 它们由 FileService 在写入链路内部调用，不是前端 API。若导出，Wails 会把它们
// 绑成前端方法，且本契约测试会正确报出漂移。
type SnapshotOperator interface {
	CreateManualSnapshot(workspacePath, relativePath string) (*Snapshot, error)
	ListSnapshots(workspacePath, relativePath string) ([]*Snapshot, error)
	ListSnapshotFiles(workspacePath string) ([]*SnapshotFileSummary, error)
	GetSnapshotContent(workspacePath, id string) (string, error)
	DiffWithCurrent(workspacePath, id string) (*SnapshotDiff, error)
	DiffSnapshots(workspacePath, fromID, toID string) (*SnapshotDiff, error)
	RestoreSnapshot(workspacePath, id string) (*SnapshotRestoreResult, error)
	DeleteSnapshot(workspacePath, id string) error
	ClearSnapshots(workspacePath, relativePath string) (int, error)
	PruneSnapshots(workspacePath string) (*SnapshotStats, error)
	GetSnapshotStats(workspacePath string) (*SnapshotStats, error)
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
	Summarize(apiKey, baseURL, model, protocol, content string) (string, error)
}

// LLMConfigurator 定义 LLM 端点配置与自检接口。
// 独立于 Summarizer / QnAProvider：端点配置服务于所有 AI 能力，
// 若挂在其中任一功能接口上，另一个就得反向依赖它。
type LLMConfigurator interface {
	Presets() []LLMEndpointPreset
	Probe(apiKey, baseURL, protocol string) *LLMProbeResult
	ProbeRerank(cfg RerankConfig) *RerankProbeResult
	ProbeEmbedding(apiKey, baseURL, model string) *EmbeddingProbeResult
}

// CredentialKeeper 定义系统级密钥存取接口（P2-5）。
// key 是白名单内的逻辑名（如 ai.apiKey），平台实现负责落到
// Windows 凭据管理器等系统存储。
type CredentialKeeper interface {
	SaveCredential(key, value string) error
	GetCredential(key string) (string, error)
	DeleteCredential(key string) error
}

// ---------------------------------------------------------------------------
// RAG 知识库问答
// ---------------------------------------------------------------------------

// QnAProvider 定义知识库问答接口（检索增强生成）
type QnAProvider interface {
	// Answer 提问并返回带引用的回答。
	// protocol 为 AI 生成协议（openai-chat / openai-responses / anthropic-messages / google-gemini / google-vertex）。
	// embBaseURL/embModel/embAPIKey 为语义检索的 embedding 端点配置（与生成端点独立）；
	// 三者为空时向量 leg 不启用，检索退化为纯 BM25，行为与 P0 结束态一致。
	// rerankCfg 为可选重排序端点配置（P1-3b）；未配置时融合退化为纯 BM25 + 向量 RRF，
	// 与 P1-3 结束态一致；重排服务不可用时静默回退，不阻断问答。
	Answer(apiKey, baseURL, model, protocol, embBaseURL, embModel, embAPIKey string, rerankCfg RerankConfig, workspacePath, question string) (*QnAResponse, error)
	// HybridSearch 文档级混合检索（BM25 + 向量 RRF 融合），供前端搜索增强。
	// rerankCfg 同上，可选。
	HybridSearch(workspacePath, query, embBaseURL, embModel, embAPIKey string, rerankCfg RerankConfig) ([]*SearchResult, error)
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
	// ImportMarkdownFolderAsync 异步导入（E-5），立即返回任务 ID。
	//
	// 大库导入会阻塞 UI 几十秒且无法中断，同步接口撑不住这个场景。
	// 结果不在这里返回：查 TaskService.GetTask 或订阅 task:finished 事件。
	ImportMarkdownFolderAsync(srcDir, workspacePath string, opts ImportOptions) (string, error)
}

// ---------------------------------------------------------------------------
// 异步任务（E-5）
// ---------------------------------------------------------------------------

// TaskOperator 定义异步任务的查询与取消接口。
//
// 刻意不包含"启动任务"：任务体是 Go 函数（TaskFunc），无法跨语言传递，
// 启动只能由包内其他 Service 调用非导出的 submit 发起。
// 前端能做的是查看进度、取消，以及在切换工作区 / 退出前批量收尾。
//
// 状态变化经 Wails 事件推送（task:started / task:progress / task:finished），
// 这里的查询接口是补充手段——事件可能因前端尚未订阅而丢失，
// 查询保证任何时刻都能拿到当前状态。
type TaskOperator interface {
	ListTasks() []*TaskInfo
	GetTask(taskID string) *TaskInfo
	// Cancel 发出取消信号。返回 true 仅代表信号已发出，
	// 任务体需要在自己的检查点响应 ctx.Done() 才会真正停下。
	Cancel(taskID string) bool
	CancelAll() int
	// Wait 阻塞至所有任务结束，供优雅退出使用。
	Wait()
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
var _ LLMConfigurator = (*LLMConfigService)(nil)
var _ CredentialKeeper = (*CredentialService)(nil)
var _ Searcher = (*SearchService)(nil)
var _ Tagger = (*TagService)(nil)
var _ Grapher = (*GraphService)(nil)
var _ TodoOperator = (*TodoService)(nil)
var _ ReminderOperator = (*ReminderService)(nil)
var _ ArchiveOperator = (*ArchiveService)(nil)
var _ TrashOperator = (*TrashService)(nil)
var _ SnapshotOperator = (*SnapshotService)(nil)
var _ WorkspaceOperator = (*WorkspaceService)(nil)
var _ Exporter = (*ExportService)(nil)
var _ Summarizer = (*SummarizeService)(nil)
var _ QnAProvider = (*QnAService)(nil)
var _ Importer = (*ImportService)(nil)
var _ TaskOperator = (*TaskService)(nil)
var _ Gitter = (*GitService)(nil)

// ---------------------------------------------------------------------------
// 模板系统（P2-2）
// ---------------------------------------------------------------------------

// Templater 定义模板列表与从模板创建笔记的接口。
// 模板是 Templates/ 目录下的普通 .md 文件（Obsidian 同名约定），
// {{title}}/{{date}}/{{time}}/{{datetime}} 为内置变量，其余 {{word}} 由用户填写。
type Templater interface {
	ListTemplates(workspacePath string) ([]*TemplateInfo, error)
	GetTemplateContent(workspacePath string, name string) (string, error)
	CreateFromTemplate(workspacePath string, templateName string, targetRelativePath string, variables map[string]string) (*FileNode, error)
}

var _ Templater = (*TemplateService)(nil)

// ---------------------------------------------------------------------------
// 知识编译（P1-5）
// ---------------------------------------------------------------------------

// CompileOperator 定义「知识编译流水线」接口：列出 Inbox 待编译笔记、
// 编译单篇、编译全部。Inbox/Compiled 目录约定由 CompileService 内部持有，
// 端口只暴露动作语义，不暴露目录名（保持后端可自由调整落盘布局）。
type CompileOperator interface {
	ListInbox(workspacePath string) ([]string, error)
	CompileNote(workspacePath, relativePath, apiKey, baseURL, model, protocol string) (*CompileResult, error)
	CompileAll(workspacePath, apiKey, baseURL, model, protocol string) (*CompileAllResult, error)
}

var _ CompileOperator = (*CompileService)(nil)

// ---------------------------------------------------------------------------
// Git 友好（P2-4）
// ---------------------------------------------------------------------------

// Gitter 定义工作区 Git 接入的最小接口。
// 战略边界：只做 init / .gitignore / 一键提交，**不做同步**——
// 冲突合并、远端管理交给用户自己的 Git 工具链。
type Gitter interface {
	Status(workspacePath string) (*GitStatus, error)
	InitRepo(workspacePath string) error
	EnsureGitignore(workspacePath string) (bool, error)
	CommitAll(workspacePath string, message string) (string, error)
}

// 注：以下两个端口的实现位于其他包，其编译时断言随实现下沉到各包内，
// 避免 service 包反向依赖它们：
//   - Reporter        → *ErrorMonitor（internal/infra/monitor/errormonitor.go）
//   - PluginOperator  → *PluginService（internal/plugin/pluginservice.go）
