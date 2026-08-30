package security

import (
	"strings"
	"testing"
)

// directiveValue 从策略串里取出某条指令的值，便于断言
func directiveValue(t *testing.T, policy, name string) string {
	t.Helper()
	for _, part := range strings.Split(policy, ";") {
		part = strings.TrimSpace(part)
		if value, ok := strings.CutPrefix(part, name+" "); ok {
			return value
		}
	}
	t.Fatalf("directive %q not found in policy: %s", name, policy)
	return ""
}

func TestContentSecurityPolicy_DefaultDeniesAllOutbound(t *testing.T) {
	t.Setenv(AllowedHostsEnv, "")

	policy := ContentSecurityPolicy()

	if got := directiveValue(t, policy, "connect-src"); got != "'self'" {
		t.Errorf("connect-src = %q, want %q", got, "'self'")
	}
	// 默认策略里不应出现任何远程 origin（http/https 或裸域名）
	if strings.Contains(policy, "http") {
		t.Errorf("default policy must not allow any remote origin, got: %s", policy)
	}
}

func TestContentSecurityPolicy_AllowsUnsafeEvalForMainThreadPlugins(t *testing.T) {
	t.Setenv(AllowedHostsEnv, "")

	policy := ContentSecurityPolicy()

	// mainThreadTransport 用 new Function('notevault', src) 执行 full-trust 插件源码。
	// 这条入口是受控的（只有用户显式授权的 full-trust 插件才走主进程），
	// 但收紧它会让主进程模式直接挂在 "unsafe-eval is not allowed" 上——
	// 收紧之前务必三思，详见 csp.go 的兼容性注释。
	if got := directiveValue(t, policy, "script-src"); !strings.Contains(got, "'unsafe-eval'") {
		t.Errorf("script-src = %q, must allow 'unsafe-eval' for mainThreadTransport", got)
	}
}

func TestContentSecurityPolicy_AllowsBlobForPluginWorkers(t *testing.T) {
	t.Setenv(AllowedHostsEnv, "")

	policy := ContentSecurityPolicy()

	// 插件 Worker 由 createObjectURL + new Worker 创建，缺了 blob: 插件系统直接瘫痪
	if got := directiveValue(t, policy, "worker-src"); !strings.Contains(got, "blob:") {
		t.Errorf("worker-src = %q, must allow blob:", got)
	}
	if got := directiveValue(t, policy, "script-src"); !strings.Contains(got, "blob:") {
		t.Errorf("script-src = %q, must allow blob:", got)
	}
}

func TestContentSecurityPolicy_KeepsWailsRuntimeWorking(t *testing.T) {
	t.Setenv(AllowedHostsEnv, "")

	policy := ContentSecurityPolicy()

	// wails runtime 用 fetch(window.location.origin + "/wails/runtime") 调后端，
	// 是同源请求。这条一旦收紧，整个应用的 Go 调用全断。
	if got := directiveValue(t, policy, "connect-src"); !strings.Contains(got, "'self'") {
		t.Errorf("connect-src = %q, must keep 'self' for the wails runtime", got)
	}
}

func TestContentSecurityPolicy_AllowlistOnlyWidensRequestChannels(t *testing.T) {
	t.Setenv(AllowedHostsEnv, "api.example.com, cdn.example.com")

	policy := ContentSecurityPolicy()

	for _, name := range []string{"connect-src", "img-src"} {
		got := directiveValue(t, policy, name)
		if !strings.Contains(got, "api.example.com") || !strings.Contains(got, "cdn.example.com") {
			t.Errorf("%s = %q, should carry the allowlist", name, got)
		}
	}
	// 白名单绝不能扩散到执行类指令，否则等于允许加载远程脚本
	if got := directiveValue(t, policy, "script-src"); strings.Contains(got, "example.com") {
		t.Errorf("script-src = %q, must not be widened by the allowlist", got)
	}
	if got := directiveValue(t, policy, "default-src"); strings.Contains(got, "example.com") {
		t.Errorf("default-src = %q, must not be widened by the allowlist", got)
	}
}

func TestAllowedHosts_ParsesAndTrims(t *testing.T) {
	t.Setenv(AllowedHostsEnv, "  a.example.com , , b.example.com  ")
	got := AllowedHosts()
	want := []string{"a.example.com", "b.example.com"}
	if len(got) != len(want) {
		t.Fatalf("AllowedHosts() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AllowedHosts()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	t.Setenv(AllowedHostsEnv, "   ")
	if got := AllowedHosts(); got != nil {
		t.Errorf("blank value should yield nil, got %v", got)
	}
}
