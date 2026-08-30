package main

import (
	"embed"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/notevault/notevault/internal/app"
	"github.com/notevault/notevault/internal/core"
	"github.com/notevault/notevault/internal/infra/monitor"
	"github.com/notevault/notevault/internal/platform"
	"github.com/notevault/notevault/internal/plugin"
	"github.com/notevault/notevault/internal/security"
	"github.com/notevault/notevault/internal/service"
)

//go:embed all:frontend/dist
var assets embed.FS

// debugPort returns the WebView2 remote debugging port.
// It is enabled by NOTEVAULT_DEBUG_PORT (empty/invalid = disabled) for desktop E2E only;
// production defaults to disabled.
func debugPort() string {
	raw := os.Getenv("NOTEVAULT_DEBUG_PORT")
	if raw == "" {
		return ""
	}
	if p, err := strconv.Atoi(raw); err != nil || p <= 0 || p > 65535 {
		return ""
	}
	return "--remote-debugging-port=" + raw
}

// debugBrowserArgs returns WebView2 browser arguments required by E2E tests.
// Remote debugging is disabled unless NOTEVAULT_DEBUG_PORT is explicitly set.
// The sandbox switch is required only for the E2E build: WebView2 152 currently
// crashes its renderer when a CDP client attaches to a sandboxed target.
func debugBrowserArgs() []string {
	port := debugPort()
	if port == "" {
		return nil
	}
	return []string{port, "--no-sandbox"}
}

func singleInstanceID() string {
	suffix := os.Getenv("NOTEVAULT_E2E_ID")
	if suffix == "" {
		return "com.notevault.app"
	}
	return "com.notevault.app." + suffix
}

// errorLogDir 返回错误日志目录：优先 %APPDATA%/NoteVault，回退到系统 TempDir
func errorLogDir() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "NoteVault")
}

// webviewUserDataPath 返回 WebView2 的稳定数据目录。
// E2E 测试使用独立目录，避免多个测试实例共享同一个浏览器锁。
func webviewUserDataPath() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.Getenv("APPDATA")
	}
	if base == "" {
		base = os.TempDir()
	}

	path := filepath.Join(base, "NoteVault", "WebView2")
	if suffix := os.Getenv("NOTEVAULT_E2E_ID"); suffix != "" {
		path = filepath.Join(path, "e2e-"+suffix)
	}
	if err := os.MkdirAll(path, 0750); err != nil {
		log.Printf("[WebView2] unable to create user data directory %q: %v", path, err)
		return ""
	}
	return path
}

func main() {
	processRelease, processAcquired := platform.AcquireProcessLock(singleInstanceID())
	if !processAcquired {
		return
	}
	defer processRelease()

	fileService := service.NewFileService()

	// 错误监控：本地日志 + 可选 Sentry（无 DSN 时仅本地）
	errorMonitor := monitor.NewErrorMonitor(errorLogDir(), core.ErrorMonitorConfig{
		SentryDSN:      os.Getenv("NOTEVAULT_SENTRY_DSN"),
		EnableLocalLog: true,
		AppVersion:     core.AppVersion,
	})

	// 主窗口引用：单实例锁回调里聚焦已有窗口
	var mainWindow *application.WebviewWindow

	opts := application.Options{
		Name:        core.AppName,
		Description: "本地优先的个人知识库管理工具",
		Windows: application.WindowsOptions{
			WebviewUserDataPath: webviewUserDataPath(),
		},
		// 单实例锁：多开时第二个实例直接退出并聚焦已有窗口，
		// 避免多个实例共享 WebView2 user data folder 导致渲染进程冲突、UI 卡死。
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: singleInstanceID(),
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				if mainWindow != nil {
					mainWindow.Show()
					mainWindow.Focus()
				}
			},
		},
		Services: []application.Service{
			application.NewService(&app.AppService{}),
			application.NewService(service.NewWorkspaceService()),
			application.NewService(fileService),
			application.NewService(service.NewSearchService(fileService)),
			application.NewService(service.NewTagService()),
			application.NewService(service.NewTodoService()),
			application.NewService(service.NewReminderService()),
			application.NewService(service.NewArchiveService()),
			application.NewService(service.NewTrashService()),
			application.NewService(service.NewGraphService()),
			application.NewService(service.NewExportService()),
			application.NewService(service.NewSummarizeService()),
			application.NewService(service.NewQnAService()),
			application.NewService(service.NewImportService()),
			application.NewService(errorMonitor),
			application.NewService(plugin.NewPluginService("")),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
			// 注入 CSP：即使插件能执行任意 JS，也掐断它把笔记数据带出去的通道。
			// 此中间件位于 Wails 内部中间件之前，能覆盖所有静态资源响应。
			// 策略在启动时计算一次，避免每个请求重建字符串。
			Middleware: func(next http.Handler) http.Handler {
				policy := security.ContentSecurityPolicy()
				if hosts := security.AllowedHosts(); len(hosts) > 0 {
					log.Printf("[CSP] outbound allowlist enabled: %s", strings.Join(hosts, ", "))
				}
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Security-Policy", policy)
					next.ServeHTTP(w, r)
				})
			},
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	}
	if args := debugBrowserArgs(); len(args) > 0 {
		opts.Windows.AdditionalBrowserArgs = args
		log.Printf("[E2E] WebView2 remote debugging enabled: %s", args[0])
	}

	wailsApp := application.New(opts)

	// 主窗口
	mainWindow = wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            core.AppName,
		Width:            1440,
		Height:           900,
		MinWidth:         1000,
		MinHeight:        700,
		BackgroundColour: application.NewRGB(30, 31, 36),
		URL:              "/",
		// 关掉 Windows 原生标题栏，让前端 TitleBar.vue 画唯一的窗口控制按钮，
		// 否则原生标题栏的 × 会与自定义顶栏的 × 叠成两套。
		Frameless: true,
		// 让 WebView2 的 -webkit-app-region: drag 生效，
		// 这样自定义顶栏仍能拖动窗口（frameless 窗口默认拖不动）。
		Windows: application.WindowsWindow{
			NonClientRegionSupport: true,
		},
	})

	_ = mainWindow

	err := wailsApp.Run()
	if err != nil {
		log.Fatal(err)
	}
}
