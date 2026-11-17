//go:build windows

package service

import (
	"os/exec"
	"syscall"
)

// createNoWindow 对应 Windows 的 CREATE_NO_WINDOW 进程创建标志。
// 标准库 syscall 未导出该常量，这里按 Win32 API 定义显式声明。
const createNoWindow = 0x08000000

// hideSubprocessWindow 抑制子进程的控制台窗口。
//
// NoteVault 以 -H windowsgui 编译，进程自身没有控制台。此时启动 git.exe
// 这类控制台程序，系统会为子进程新建一个控制台窗口，在界面上表现为
// 「一闪而过的黑框」。CREATE_NO_WINDOW 让子进程完全不分配控制台，
// HideWindow 再兜底隐藏窗口，两者同时设置可覆盖不同启动路径。
func hideSubprocessWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
