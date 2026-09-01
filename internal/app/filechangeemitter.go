package app

import (
	"github.com/notevault/notevault/internal/service"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// EventWorkspaceFileChanged 是后端文件监控推送给前端的事件名。
//
// 前端用 Events.On(EventWorkspaceFileChanged, ...) 订阅，把后端实时监测到的
// 文件增删改转成 WorkspaceEvent 喂给插件——这正是 E-6 要替换掉的
// "前端轮询快照比对" 机制。
//
// payload 是 *service.FileChangeEvent（Type: create/modify/delete, Path: 相对路径）。
const EventWorkspaceFileChanged = "workspace:file-changed"

// wailsFileChangeEmitter 把后端文件变更事件推到 Wails 事件总线。
//
// 用 application.Get() 拿全局 App：它在 application.New() 之后才有值，
// 而 watcher 是在运行时（SetCurrentWorkspace）才启动的，所以这里一定非 nil。
// 仍做 nil 防御——桌面应用里 panic 的代价远高于一次空判断。
type wailsFileChangeEmitter struct{}

func (wailsFileChangeEmitter) OnFileChange(event service.FileChangeEvent) {
	app := application.Get()
	if app == nil {
		return
	}
	app.Event.Emit(EventWorkspaceFileChanged, &event)
}
