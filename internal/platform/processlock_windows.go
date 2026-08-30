//go:build windows && !server

package platform

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// AcquireProcessLock 保证同一安装的主进程只有一个。
// Wails 的单实例回调负责唤醒已有窗口，这个锁负责在 Wails 初始化窗口前
// 也阻止第二个进程继续创建 WebView2。
func AcquireProcessLock(uniqueID string) (release func(), acquired bool) {
	if uniqueID == "" {
		uniqueID = "com.notevault.app"
	}
	name := windows.StringToUTF16Ptr(fmt.Sprintf("Local\\NoteVault.Process.%s", uniqueID))
	handle, err := windows.CreateMutex(nil, false, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		return func() {}, false
	}
	if err != nil || handle == 0 {
		return func() {}, false
	}

	return func() {
		_ = windows.CloseHandle(handle)
	}, true
}
