package core

import "time"

// ErrorMonitorConfig 错误监控配置
// 上移至 core：被 Reporter 端口的 UpdateConfig 方法引用（同 ErrorReport 的处理），
// 避免 service 反向依赖 monitor。
type ErrorMonitorConfig struct {
	// SentryDSN Sentry DSN，空表示不上报到 Sentry（仅本地日志）
	SentryDSN string `json:"sentryDsn"`
	// EnableLocalLog 是否写入本地日志文件（默认 true）
	EnableLocalLog bool `json:"enableLocalLog"`
	// AppVersion 应用版本号（用于上报事件）
	AppVersion string `json:"appVersion"`
}

// ErrorReport 前端上报的错误事件
// 上移至 core：被 Reporter 端口接口（service 层 ports.go）与 infra/monitor 共同引用，
// 避免 service 反向依赖 monitor。
type ErrorReport struct {
	// Message 错误主信息（前端 new Error().message）
	Message string `json:"message"`
	// Stack 错误堆栈（前端 new Error().stack）
	Stack string `json:"stack,omitempty"`
	// Source 错误来源（filename:line:col）
	Source string `json:"source,omitempty"`
	// Level 等级：info / warning / error / fatal（默认 error）
	Level string `json:"level,omitempty"`
	// Tags 额外标签（用于 Sentry 事件分组）
	Tags map[string]string `json:"tags,omitempty"`
	// Extra 额外上下文（不参与分组）
	Extra map[string]any `json:"extra,omitempty"`
	// UserAgent 用户代理字符串（前端 navigator.userAgent）
	UserAgent string `json:"userAgent,omitempty"`
	// Timestamp 触发时间（前端 Date.now() 毫秒，0 表示让后端补时间戳）
	Timestamp int64 `json:"timestamp,omitempty"`
}

// PluginTrust 插件请求的信任等级
type PluginTrust string

const (
	// TrustSandbox 默认等级：插件在 Web Worker 沙箱内运行，只能使用 manifest 声明的
	// 能力，且网络、本地存储、嵌套 Worker 均被禁用。绝大多数插件应停留在此等级。
	TrustSandbox PluginTrust = "sandbox"
	// TrustFull 完全信任：插件在主进程（WebView）上下文运行，可访问界面与完整的
	// notevault 运行时对象。
	//
	// 这不是插件自己声明了就能生效的等级——必须由用户逐插件显式授权，
	// 且授权与源码哈希绑定：插件一旦更新，授权自动失效、需要重新确认，
	// 防止"先以无害版本骗取授权、再通过更新投递恶意代码"。
	TrustFull PluginTrust = "full"
)

// PluginManifest 插件清单（从插件文件首部 front matter 解析）
type PluginManifest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Author      string   `json:"author,omitempty"`
	Description string   `json:"description,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	// Trust 请求的信任等级，缺省按 TrustSandbox 处理。
	// 声明为 full 的插件在获得用户显式授权前，仍按沙箱等级运行。
	Trust PluginTrust `json:"trust,omitempty"`
}

// PluginInfo 插件完整信息（清单 + 运行时状态 + 元数据）
// 上移至 core：被 PluginOperator 端口接口（service 层 ports.go）引用，
// 避免 service 反向依赖 plugin。
type PluginInfo struct {
	Manifest  PluginManifest `json:"manifest"`
	Enabled   bool           `json:"enabled"`
	FilePath  string         `json:"filePath"`
	Size      int64          `json:"size"`
	Hash      string         `json:"hash"` // sha256 摘要前 16 字符
	ModTime   time.Time      `json:"modTime"`
	Source    string         `json:"source"` // 完整源码（前端预览用）
	HasError  bool           `json:"hasError"`
	LoadError string         `json:"loadError,omitempty"`
	// TrustGranted 用户是否已授权该插件以 TrustFull 等级运行。
	// 仅当 Manifest.Trust == TrustFull 时有意义；授权与 Hash 绑定，
	// 源码变更后 Hash 对不上，此项会自动回落为 false。
	TrustGranted bool `json:"trustGranted"`
}
