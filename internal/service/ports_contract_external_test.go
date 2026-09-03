// 外部测试包：承接 infra/monitor 与 plugin 的端口契约断言。
//
// 这两个包都实现 service 包定义的端口接口（Reporter / PluginOperator）。
// 断言原本写在各自的生产代码里（var _ service.Reporter = ...），导致
// infra/plugin 生产代码反向 import service，破坏「端口定义方不感知实现方」
// 的分层约束。挪到这里后依赖方向恢复为：测试 → service + 实现包，生产代码零依赖。
package service_test

import (
	"testing"

	"github.com/notevault/notevault/internal/core"
	"github.com/notevault/notevault/internal/infra/monitor"
	"github.com/notevault/notevault/internal/plugin"
	"github.com/notevault/notevault/internal/service"
)

// 编译期断言：实现方满足端口接口，漂移时在 go build ./... 即刻报错。
var (
	_ service.Reporter       = (*monitor.ErrorMonitor)(nil)
	_ service.PluginOperator = (*plugin.PluginService)(nil)
)

// TestPortImplementations 向测试运行器显式登记契约（纯编译断言的载体）。
func TestPortImplementations(t *testing.T) {
	var reporter service.Reporter = monitor.NewErrorMonitor(t.TempDir(), core.ErrorMonitorConfig{EnableLocalLog: false})
	var operator service.PluginOperator = plugin.NewPluginService(t.TempDir())
	_ = reporter
	_ = operator
}
