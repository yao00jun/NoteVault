package service

import (
	"os"

	"github.com/notevault/notevault/internal/infra/fsutil"
)

// atomicWrite 是 infra/fsutil.AtomicWrite 的包内别名。
//
// 实现已上移到 internal/infra/fsutil，以便 internal/plugin 共用同一份原子写
// （插件信任状态原先只能用 os.WriteFile 覆盖写）。这里保留薄封装，
// 避免 service 包内 17 处调用点全部改名。
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	return fsutil.AtomicWrite(path, data, perm)
}
