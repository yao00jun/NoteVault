// Package fsutil 提供跨层共用的文件写入原语。
//
// 单独成包的原因：原子写原先是 internal/service 的私有实现（fsutil.go），
// internal/plugin 取不到，只能退回 os.WriteFile 覆盖写信任状态，
// 留了一条「半截写入 → 授权被判为无效 → 用户需重新确认」的路径。
// 提到 infra 层后两边共用同一份实现。
package fsutil

import (
	"os"
	"path/filepath"
)

// AtomicWrite 原子写文件：先写同目录临时文件，成功后再 rename 覆盖目标。
//
// 直接用 os.WriteFile 覆盖写索引 JSON（回收站 / 提醒 / 工作区 / 插件状态）有一个
// 数据完整性风险：进程在写入中途崩溃会留下半截文件，下次 json.Unmarshal 失败导致
// 索引丢失（典型表现：文件已移入回收站，重启后却查不到）。temp+rename 保证目标文件
// 要么是完整的旧内容，要么是完整的新内容；崩溃最多残留一个 .tmp 文件。
//
// 临时文件必须与目标同目录：跨卷 rename 在 Windows 上会失败。
// os.Rename 在 Windows 上使用 MoveFileEx(REPLACE_EXISTING)，可覆盖已存在文件。
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		// rename 失败时清掉临时文件，避免下次调用把它当成残留脏数据
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
