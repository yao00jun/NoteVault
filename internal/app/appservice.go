package app

import (
	"os"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/notevault/notevault/internal/core"
)

// AppService 是 NoteVault 的核心应用服务
// 后续 Phase 会拆分为多个独立 Service（FileService、SearchService 等）
type AppService struct {
	// App 在服务初始化后注入（Wails 的 OnBackendReady 钩子）
	App *application.App
}

// GetVersion 返回应用版本号
func (s *AppService) GetVersion() string {
	return core.AppVersion
}

// GetAppName 返回应用名称
func (s *AppService) GetAppName() string {
	return core.AppName
}

// OpenFolderDialog 弹出系统原生对话框选择文件夹，返回绝对路径（用户取消返回空串）
func (s *AppService) OpenFolderDialog() (string, error) {
	dialog := s.app().Dialog.OpenFile()
	dialog.SetTitle("选择文件夹").CanChooseFiles(false).CanChooseDirectories(true)
	path, err := dialog.PromptForSingleSelection()
	// 用户取消是正常交互，不向上层报错（Wails 内部 cfd.ErrorCancelled 在 internal 包，无法 errors.Is）
	if err != nil && isUserCancelled(err) {
		return "", nil
	}
	return path, err
}

// OpenFileDialog 弹出系统原生对话框选择单个文件，可指定过滤器（如 "*.zip"），返回绝对路径
func (s *AppService) OpenFileDialog(filter string) (string, error) {
	dialog := s.app().Dialog.OpenFile()
	dialog.SetTitle("选择文件").CanChooseFiles(true).CanChooseDirectories(false)
	if filter != "" {
		dialog.AddFilter(filter+" 文件", filter)
	}
	path, err := dialog.PromptForSingleSelection()
	if err != nil && isUserCancelled(err) {
		return "", nil
	}
	return path, err
}

// isUserCancelled 判断错误是否为用户主动取消（不同平台错误文案略有差异，按字符串匹配）
func isUserCancelled(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "cancelled by user") ||
		strings.Contains(msg, "canceled by user") ||
		strings.Contains(msg, "cancelled")
}

// ForceQuit 立即终止进程，跳过任何 graceful shutdown。
// 用于 Wails app.Quit() 卡死时的兜底（已知 beta 版本 Quit 在某些场景下会阻塞）。
func (s *AppService) ForceQuit() {
	os.Exit(0)
}

// app 返回当前应用实例（通过全局 globalApplication 获取，service 创建后即稳定）
func (s *AppService) app() *application.App {
	if s.App != nil {
		return s.App
	}
	return application.Get()
}
