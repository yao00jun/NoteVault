//go:build !windows || server

package platform

import "testing"

// TestAcquireProcessLock_NoopOnNonWindows 覆盖非 Windows（以及 server 构建标签）下的
// no-op 锁分支。该分支此前从未被任何测试执行过：唯一的进程锁测试带
// `windows && !server` 标签，因此 Linux/macOS 与 server 构建下零覆盖。
func TestAcquireProcessLock_NoopOnNonWindows(t *testing.T) {
	release, acquired := AcquireProcessLock("com.notevault.app.test")
	if !acquired {
		t.Fatal("non-windows/server build should always acquire (no-op lock)")
	}
	if release == nil {
		t.Fatal("release func should not be nil")
	}
	// release 必须是无副作用的空操作，调用不应 panic
	release()
}
