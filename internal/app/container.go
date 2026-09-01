package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/notevault/notevault/internal/core"
	"github.com/notevault/notevault/internal/infra/monitor"
	"github.com/notevault/notevault/internal/platform"
	"github.com/notevault/notevault/internal/plugin"
	"github.com/notevault/notevault/internal/service"
)

// ContainerConfig 是装配容器的全部外部输入。
//
// 刻意不在这里读环境变量：进程级的东西（APPDATA、Sentry DSN、E2E 开关）
// 一律由 main 解析后传进来，容器自身保持纯函数式，测试里可以随便造。
type ContainerConfig struct {
	// ErrorLogDir 错误日志落盘目录，空串时 ErrorMonitor 自行回退
	ErrorLogDir string
	// SentryDSN 为空表示只写本地日志，不上报
	SentryDSN string
	// AppVersion 空串时由 monitor 回退到 core.AppVersion
	AppVersion string
	// PluginsDir 空串时 PluginService 用默认位置（%APPDATA%/NoteVault/plugins）
	PluginsDir string
	// EnableLocalErrorLog 关掉后连本地日志也不写，仅用于测试
	EnableLocalErrorLog bool
}

// DefaultContainerConfig 返回一份只启用本地错误日志的最小配置。
func DefaultContainerConfig() ContainerConfig {
	return ContainerConfig{
		AppVersion:          core.AppVersion,
		EnableLocalErrorLog: true,
	}
}

// Container 持有所有后端服务实例。
//
// 存在的意义有两条，都不是"为了好看"：
//  1. 装配顺序是有隐含约束的（Snapshot 必须先于 File、File 必须先于 Search），
//     散在 main 里靠注释维持迟早会被改坏，收进来才能用测试锁住。
//  2. 服务之间将来还会长出更多交叉依赖，有了具名字段才能在不改 main 的前提下接线。
type Container struct {
	App          *AppService
	Workspace    *service.WorkspaceService
	Snapshot     *service.SnapshotService
	File         *service.FileService
	Search       *service.SearchService
	Tag          *service.TagService
	Todo         *service.TodoService
	Base         *service.BaseService
	Reminder     *service.ReminderService
	Archive      *service.ArchiveService
	Trash        *service.TrashService
	Graph        *service.GraphService
	Export       *service.ExportService
	Summarize    *service.SummarizeService
	QnA          *service.QnAService
	LLMConfig    *service.LLMConfigService
	Credentials  *service.CredentialService
	Import       *service.ImportService
	Compile      *service.CompileService
	Tasks        *service.TaskService
	Git          *service.GitService
	Templates    *service.TemplateService
	ErrorMonitor *monitor.ErrorMonitor
	Plugin       *plugin.PluginService

	// Indexes 是容器级索引注册表（E-4），不注册给 Wails。
	// 它不是服务，而是被 Workspace / Search / QnA 三方共享的基础设施：
	// Workspace 管释放，Search 与 QnA 共用同一份索引。
	Indexes *service.SearchIndexRegistry
}

// NewContainer 按依赖顺序构造全部服务。
//
// 顺序上的硬约束（改动前先读这段）：
//   - Snapshot 先于 File：版本快照挂在 File 的写入链路上，覆盖保存 / 硬删除前留存旧版本
//   - File 先于 Search：SearchService 显式依赖 FileService 读取文件内容
//
// 其余服务彼此无依赖，字段顺序按功能相关性排，不代表构造顺序有要求。
func NewContainer(cfg ContainerConfig) *Container {
	// 版本快照必须先于 fileService 构造，才能注入到写入链路
	snapshotService := service.NewSnapshotService()
	fileService := service.NewFileServiceWithHistory(snapshotService)

	errorMonitor := monitor.NewErrorMonitor(cfg.ErrorLogDir, core.ErrorMonitorConfig{
		SentryDSN:      cfg.SentryDSN,
		EnableLocalLog: cfg.EnableLocalErrorLog,
		AppVersion:     cfg.AppVersion,
	})

	// E-4：索引注册表是容器级单例。
	// Search 与 QnA 必须共用同一份索引（否则内存翻倍、增量更新互不可见），
	// Workspace 持有它是为了在切换 / 删除工作区时释放对应索引。
	indexRegistry := service.NewSearchIndexRegistry()

	// E-5：任务框架必须先于 Import 构造——导入要在它上面提交异步任务。
	taskService := service.NewTaskService(wailsTaskEmitter{})

	return &Container{
		App:          &AppService{},
		Workspace:    service.NewWorkspaceServiceWithRegistryAndSink(indexRegistry, wailsFileChangeEmitter{}),
		Snapshot:     snapshotService,
		File:         fileService,
		Search:       service.NewSearchServiceWithRegistry(fileService, indexRegistry),
		Tag:          service.NewTagService(),
		Todo:         service.NewTodoService(),
		Base:         service.NewBaseService(),
		Reminder:     service.NewReminderService(),
		Archive:      service.NewArchiveService(),
		Trash:        service.NewTrashService(),
		Graph:        service.NewGraphService(),
		Export:       service.NewExportService(),
		Summarize:    service.NewSummarizeService(),
		QnA:          service.NewQnAServiceWithRegistry(fileService, indexRegistry, service.NewOllamaEmbeddingClient(), service.NewReranker()),
		LLMConfig:    service.NewLLMConfigService(),
		Credentials:  service.NewCredentialService(platform.NewCredentialStore("NoteVault")),
		Import:       service.NewImportServiceWithTasks(taskService),
		Tasks:        taskService,
		Git:          service.NewGitService(),
		Templates:    service.NewTemplateService(fileService),
		Compile:      service.NewCompileService(fileService, snapshotService, service.NewSummarizeCompileAI(service.NewSummarizeService()), "Inbox", "Compiled"),
		ErrorMonitor: errorMonitor,
		Plugin:       plugin.NewPluginService(cfg.PluginsDir),
		Indexes:      indexRegistry,
	}
}

// FlushIndexes 把所有工作区的搜索索引摘要落盘。
//
// 应在应用退出前调用：索引平时刻意节流写盘（避免每次搜索都序列化整份摘要），
// 不显式 flush 的话，最后一次搜索之后的变更会丢，下次启动得重新扫一遍全库。
// 失败只返回第一个错误——落盘失败不该让退出流程卡住，也不该掩盖后续工作区的落盘。
func (c *Container) FlushIndexes() error {
	if c.Indexes == nil {
		return nil
	}
	return c.Indexes.FlushAll()
}

// WailsServices 返回注册给 Wails 的服务列表。
//
// 这个列表决定了前端 bindings 的生成范围：漏一个，前端就调不到；
// 因此 container_test.go 会断言它与 Container 的字段数一致，防止新增服务时忘记注册。
func (c *Container) WailsServices() []application.Service {
	return []application.Service{
		application.NewService(c.App),
		application.NewService(c.Workspace),
		application.NewService(c.File),
		application.NewService(c.Search),
		application.NewService(c.Tag),
		application.NewService(c.Todo),
		application.NewService(c.Base),
		application.NewService(c.Reminder),
		application.NewService(c.Archive),
		application.NewService(c.Trash),
		application.NewService(c.Snapshot),
		application.NewService(c.Graph),
		application.NewService(c.Export),
		application.NewService(c.Summarize),
		application.NewService(c.QnA),
		application.NewService(c.LLMConfig),
		application.NewService(c.Credentials),
		application.NewService(c.Import),
		application.NewService(c.Compile),
		application.NewService(c.Tasks),
		application.NewService(c.Git),
		application.NewService(c.Templates),
		application.NewService(c.ErrorMonitor),
		application.NewService(c.Plugin),
	}
}
