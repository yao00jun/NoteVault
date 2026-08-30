package core

// 应用元信息常量：单一事实来源。
// 此前 "0.1.0" 与 "NoteVault" 在 main.go / appservice.go / errormonitor.go
// 各自硬编码，升级时容易漂移。
const (
	AppName    = "NoteVault"
	AppVersion = "0.1.0"
)
