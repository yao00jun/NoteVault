package core

// 应用元信息：单一事实来源。
// 此前 "0.1.0" 与 "NoteVault" 在 main.go / appservice.go / errormonitor.go
// 各自硬编码，升级时容易漂移。
const AppName = "NoteVault"

// DefaultAppVersion 仓库内的默认版本号（开发态、go test、未注入时的回退值）。
const DefaultAppVersion = "0.1.0"

// AppVersion 应用版本号，运行期可被链接器覆盖：
//
//	go build -ldflags "-X github.com/notevault/notevault/internal/core.AppVersion=v0.2.0"
//
// 发布构建（`wails3 task release VERSION=vX.Y.Z` 与 Release 流水线）都走这条
// 注入路径，因此 tag、产物文件名、二进制自报版本三者不会各说各话。
var AppVersion = DefaultAppVersion
