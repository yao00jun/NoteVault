package service

import (
	"os"
	"path/filepath"
)

// atomicWrite 原子写文件：先写同目录临时文件，成功后再 rename 覆盖目标。
//
// 直接用 os.WriteFile 覆盖写索引 JSON（回收站 / 提醒 / 工作区）有一个数据完整性风险：
// 进程在写入中途崩溃会留下半截文件，下次 json.Unmarshal 失败导致索引丢失
// （典型表现：文件已移入回收站，重启后却查不到）。temp+rename 保证目标文件
// 要么是完整的旧内容，要么是完整的新内容；崩溃最多残留一个 .tmp 文件。
// os.Rename 在 Windows 上使用 MoveFileEx(REPLACE_EXISTING)，可覆盖已存在文件。
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
