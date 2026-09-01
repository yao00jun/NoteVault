package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// wailsTaskEmitter 把任务事件推给前端（E-5）。
//
// 它存在的唯一理由是把 Wails 的全局状态挡在 service 包之外：
//   - service 包不该依赖 application，否则任务框架的单元测试必须起一个 Wails 应用；
//   - application.Get() 在 New() 之前返回 nil，直接取 .Event 会 panic。
//     任务事件只在运行时发出，那时应用已就绪，但兜底判空仍然必要——
//     桌面应用里一次 panic 就是整个进程崩掉，代价远高于漏推一帧进度。
type wailsTaskEmitter struct{}

// Emit 推送一个任务事件。应用尚未就绪时静默丢弃。
func (wailsTaskEmitter) Emit(eventName string, data any) {
	a := application.Get()
	if a == nil || a.Event == nil {
		return
	}
	a.Event.Emit(eventName, data)
}
