//go:build !windows

package service

import "os/exec"

// hideSubprocessWindow 在非 Windows 平台上无事可做：macOS / Linux 的
// 子进程不会额外弹出控制台窗口。
func hideSubprocessWindow(cmd *exec.Cmd) {}
