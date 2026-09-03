package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResolveLocalEndpoint(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		want    bool
	}{
		{"ollama 默认地址", "http://localhost:11434/v1", true},
		{"回环 IP", "http://127.0.0.1:1234/v1", true},
		{"IPv6 回环", "http://[::1]:8080/v1", true},
		{"大写 LOCALHOST", "http://LOCALHOST:11434/v1", true},
		{"缺协议头也能判定", "localhost:11434/v1", true},
		{"OpenAI 云端", "https://api.openai.com/v1", false},
		{"DeepSeek 云端", "https://api.deepseek.com/v1", false},
		{"空值按云端处理", "", false},
		// 私有网段不放行：向局域网内第三方服务发不带凭据的请求是另一个安全语义
		{"局域网地址不算本机", "http://192.168.1.10:11434/v1", false},
		{"内网 10 段不算本机", "http://10.0.0.5:11434/v1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveLocalEndpoint(c.baseURL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("resolveLocalEndpoint(%q) = %v, want %v", c.baseURL, got, c.want)
			}
		})
	}
}

// TestRequireCredential_LocalEndpointNeedsNoKey 是 P1-6 的核心回归测试。
// 旧实现无条件要求 apiKey 非空，导致本机 Ollama 填了地址也被直接拒绝——
// 「本地模型支持」不是缺预设，而是根本走不通。
func TestRequireCredential_LocalEndpointNeedsNoKey(t *testing.T) {
	key, err := requireCredential("http://localhost:11434/v1", "")
	if err != nil {
		t.Fatalf("本机端点应免鉴权，却报错：%v", err)
	}
	if key != "" {
		t.Errorf("本机端点无 Key 时应返回空串，got %q", key)
	}
}

func TestRequireCredential_CloudEndpointStillRequiresKey(t *testing.T) {
	_, err := requireCredential("https://api.openai.com/v1", "")
	if err == nil {
		t.Fatal("云端端点缺 Key 时必须报错")
	}
	// 错误提示要告诉用户本机端点可留空，否则用户不知道有这条路
	if !strings.Contains(err.Error(), "localhost") {
		t.Errorf("错误提示应指引本机端点用法，got %q", err.Error())
	}
}

func TestRequireCredential_ExplicitKeyAlwaysWins(t *testing.T) {
	// 本机端点也允许带 Key（有人给 Ollama 挂了反代加鉴权）
	key, err := requireCredential("http://localhost:11434/v1", "  sk-abc  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "sk-abc" {
		t.Errorf("expected trimmed key, got %q", key)
	}
}

// TestApplyAuth_SkipsEmptyKey 保证不给本地服务发空 Bearer 头。
// 部分本地服务见到空 Bearer 会直接 401，比不带头更糟。
func TestApplyAuth_SkipsEmptyKey(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://localhost:11434/v1/models", nil)
	applyAuth(req, "")
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("空 key 不应设置 Authorization 头，got %q", got)
	}
	applyAuth(req, "sk-x")
	if got := req.Header.Get("Authorization"); got != "Bearer sk-x" {
		t.Errorf("expected Bearer sk-x, got %q", got)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := map[string]string{
		"http://localhost:11434/v1/":      "http://localhost:11434/v1",
		"  https://api.deepseek.com/v1  ": "https://api.deepseek.com/v1",
		"":                                "https://api.openai.com/v1",
	}
	for in, want := range cases {
		if got := normalizeBaseURL(in); got != want {
			t.Errorf("normalizeBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLLMConfigService_Probe_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// 本机服务不该收到 Authorization 头
		if r.Header.Get("Authorization") != "" {
			t.Errorf("本机端点不应发送 Authorization 头")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen2.5:7b"},{"id":"llama3.2"}]}`))
	}))
	defer srv.Close()

	s := NewLLMConfigService()
	// httptest 监听 127.0.0.1，因此会被判定为本机端点
	res := s.Probe("", srv.URL+"/v1", "", "")
	if !res.OK {
		t.Fatalf("expected OK, got message=%q", res.Message)
	}
	if !res.IsLocal {
		t.Error("httptest 服务监听回环地址，应判定为本机端点")
	}
	if len(res.Models) != 2 || res.Models[0] != "llama3.2" {
		// Probe 内部排序，llama3.2 应排在 qwen 之前
		t.Errorf("expected sorted models [llama3.2 qwen2.5:7b], got %v", res.Models)
	}
}

func TestLLMConfigService_Probe_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	// 填了模型 → 走真实 chat 判定，401 必须失败并指向 Key
	res := NewLLMConfigService().Probe("sk-bad", srv.URL+"/v1", "", "gpt-4o-mini")
	if res.OK {
		t.Fatal("401 不应判定为成功")
	}
	if !strings.Contains(res.Message, "API Key") && !strings.Contains(res.Message, "失败") {
		t.Errorf("401 提示应指向 Key/失败，got %q", res.Message)
	}
}

// TestLLMConfigService_Probe_RealModelChat 核心新语义：检测 = 用用户填的
// 模型真实发一次生成请求，而不是罗列端点清单。
func TestLLMConfigService_Probe_RealModelChat(t *testing.T) {
	var gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 只接受 chat completions；/models 不应被作为主判定
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			var req struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			gotModel = req.Model
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"正常"}}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	res := NewLLMConfigService().Probe("", srv.URL+"/v1", "openai-chat", "qwen2.5:7b")
	if !res.OK {
		t.Fatalf("填的模型可正常生成时应判定成功，message=%q", res.Message)
	}
	if gotModel != "qwen2.5:7b" {
		t.Fatalf("检测应真实请求用户填的模型，got %q", gotModel)
	}
	if res.Model != "qwen2.5:7b" {
		t.Fatalf("结果应回显被测模型，got %q", res.Model)
	}
}

// TestLLMConfigService_Probe_WrongModelFails 填了端点上不存在的模型 →
// 判定失败，并附上可用清单帮用户定位拼写问题。
func TestLLMConfigService_Probe_WrongModelFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"model 'nope' not found"}}`))
			return
		}
		// /models 正常返回清单
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"llama3.2"},{"id":"qwen2.5:7b"}]}`))
	}))
	defer srv.Close()

	res := NewLLMConfigService().Probe("", srv.URL+"/v1", "openai-chat", "nope")
	if res.OK {
		t.Fatal("模型不存在时不应判定成功")
	}
	if !strings.Contains(res.Message, "nope") {
		t.Errorf("失败信息应包含被测模型名，got %q", res.Message)
	}
	if len(res.Models) != 2 {
		t.Errorf("失败时应附端点可用清单辅助排查，got %v", res.Models)
	}
}

// TestLLMConfigService_Probe_ModelsNotImplemented 覆盖一个真实存在的情况：
// 端点支持 /chat/completions 但没实现 /models。这时不能判定为「连不上」，
// 否则用户会以为配置错了而去改本来正确的地址。
func TestLLMConfigService_Probe_ModelsNotImplemented(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	res := NewLLMConfigService().Probe("", srv.URL+"/v1", "", "")
	// 新语义：没填模型 + /models 也 404 → 没有任何东西被验证，不判成功
	if res.OK {
		t.Fatalf("未填模型且无法列举时不应判定成功，message=%q", res.Message)
	}
	// 但填了模型后走真实 chat → 不依赖 /models，应当成功
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv2.Close()
	res2 := NewLLMConfigService().Probe("", srv2.URL+"/v1", "openai-chat", "any-model")
	if !res2.OK {
		t.Fatalf("chat 可用时应判定成功（不依赖 /models），message=%q", res2.Message)
	}
}

func TestLLMConfigService_Probe_ConnectionRefused(t *testing.T) {
	// 关掉的端口：连接必然失败
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	res := NewLLMConfigService().Probe("", url+"/v1", "", "")
	if res.OK {
		t.Fatal("连接被拒不应判定为成功")
	}
	// 本机端点失败时要给出启动服务的指引，而不是干巴巴一句「连接失败」
	if !strings.Contains(res.Message, "ollama serve") {
		t.Errorf("本机端点连接失败应给出启动指引，got %q", res.Message)
	}
}

func TestLLMConfigService_Presets(t *testing.T) {
	presets := NewLLMConfigService().Presets()
	if len(presets) < 4 {
		t.Fatalf("expected at least 4 presets, got %d", len(presets))
	}
	var sawLocalFree, sawCloudPaid bool
	for _, p := range presets {
		if p.BaseURL == "" || p.ID == "" || p.Label == "" {
			t.Errorf("preset %+v 字段不完整", p)
		}
		if !p.RequiresKey {
			sawLocalFree = true
			if p.Hint == "" {
				t.Errorf("本机预设 %s 应带启动提示", p.ID)
			}
		} else {
			sawCloudPaid = true
		}
	}
	if !sawLocalFree || !sawCloudPaid {
		t.Error("预设应同时包含本机免鉴权与云端需鉴权两类")
	}
}

// 回归测试：判定「是否本机」绝不能阻塞可感知的时间。
//
// 背景（实测过的真实缺陷）：早期用 net.LookupIP 裸查主机名，用户把 BaseURL
// 填成 "http://x" 这类笔误时，系统解析器走完整重试链阻塞 7 秒以上，
// 而这段判定位于 Summarize / Answer 的同步路径上 —— 界面直接冻住。
func TestResolveLocalEndpoint_UnresolvableHostFailsFast(t *testing.T) {
	// 用随机且必然不存在的名字，避免命中 DNS 缓存或运营商的通配解析
	host := "notevault-nonexistent-" + time.Now().Format("20060102150405.000000") + ".invalid"

	start := time.Now()
	isLocal, err := resolveLocalEndpoint("http://" + host + ":11434/v1")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("无法解析的主机名不应返回错误（应降级为非本机）：%v", err)
	}
	if isLocal {
		t.Fatal("解析失败时必须判定为非本机（宁可多要一个 Key，不可少要）")
	}
	// 硬超时 300ms，留足余量；一旦回归到裸 LookupIP 会是数秒级
	if elapsed > 2*time.Second {
		t.Fatalf("主机名解析阻塞过久：%v（应受 localResolveTimeout=%v 约束）", elapsed, localResolveTimeout)
	}
}

// RFC 6761：*.localhost 保留给回环，不该为它发起 DNS 查询
func TestResolveLocalEndpoint_LocalhostSuffixNeedsNoDNS(t *testing.T) {
	for _, host := range []string{"ollama.localhost", "My-Box.LOCALHOST"} {
		start := time.Now()
		isLocal, err := resolveLocalEndpoint("http://" + host + ":11434/v1")
		if err != nil {
			t.Fatalf("%s 不应报错: %v", host, err)
		}
		if !isLocal {
			t.Errorf("%s 应判定为本机端点", host)
		}
		if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
			t.Errorf("%s 走了 DNS 查询（耗时 %v），应直接短路", host, elapsed)
		}
	}
}

// 缓存命中后不应再产生任何解析开销
func TestLookupHostIsLoopback_CachesVerdict(t *testing.T) {
	host := "notevault-cache-probe-" + time.Now().Format("150405.000000") + ".invalid"

	first := lookupHostIsLoopback(host)
	start := time.Now()
	second := lookupHostIsLoopback(host)
	elapsed := time.Since(start)

	if first != second {
		t.Fatalf("同一主机名两次判定应一致：%v vs %v", first, second)
	}
	if elapsed > 20*time.Millisecond {
		t.Fatalf("第二次判定应命中缓存，实际耗时 %v", elapsed)
	}
	if _, ok := localHostCache.Load(strings.ToLower(host)); !ok {
		t.Fatal("判定结果应写入缓存")
	}
}
