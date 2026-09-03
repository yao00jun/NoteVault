package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// 本文件解决 P1-6 的核心问题：本地模型端点（Ollama / LM Studio / llama.cpp）
// 不需要 API Key，但旧实现无条件硬校验 `apiKey == ""` 就直接报错，
// 导致用户把 BaseURL 填成 http://localhost:11434/v1 也用不了——
// 「本地模型支持」不是缺预设，而是根本走不通。
//
// 判定策略：只有「主机名解析到回环地址」才免鉴权。
// 不放宽到整个私有网段（192.168.x / 10.x），因为向局域网内的第三方服务
// 发送不带凭据的请求是另一个安全语义，不该由一个宽松的默认值决定。

// localResolveTimeout 主机名解析的硬超时。
//
// 为什么必须有：resolveLocalEndpoint 会被 Summarize / Answer 在请求前同步调用。
// 用 net.LookupIP 裸查一个不存在的主机名（用户把 BaseURL 填成 "http://x" 这类
// 笔误），系统解析器会走完整的重试链，实测阻塞 7 秒以上 —— 界面直接冻住。
// 判定「是否本机」本质是个本地决策，不值得为它等待任何可感知的时间。
const localResolveTimeout = 300 * time.Millisecond

// localResolveTTL 判定结果的缓存有效期。
// 生成链路每次调用都要判定一次，同一端点反复解析纯属浪费；
// 但也不能永久缓存——用户可能中途改了 hosts 或启停了本地服务。
const localResolveTTL = 5 * time.Minute

type localVerdict struct {
	isLocal bool
	at      time.Time
}

// localHostCache host → localVerdict
var localHostCache sync.Map

// lookupHostIsLoopback 解析主机名并判断是否全部指向回环地址。
// 带超时与缓存；解析失败、超时或无结果一律返回 false（宁可多要一个 Key，不可少要）。
func lookupHostIsLoopback(host string) bool {
	key := strings.ToLower(host)
	if cached, ok := localHostCache.Load(key); ok {
		if v, valid := cached.(localVerdict); valid && time.Since(v.at) < localResolveTTL {
			return v.isLocal
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), localResolveTimeout)
	defer cancel()

	isLocal := false
	if addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host); err == nil && len(addrs) > 0 {
		isLocal = true
		for _, a := range addrs {
			if !a.IP.IsLoopback() {
				isLocal = false
				break
			}
		}
	}
	localHostCache.Store(key, localVerdict{isLocal: isLocal, at: time.Now()})
	return isLocal
}

// resolveLocalEndpoint 判断 baseURL 是否指向本机回环地址。
// 返回 (isLocal, error)：baseURL 无法解析时 error 非空。
func resolveLocalEndpoint(baseURL string) (bool, error) {
	raw := strings.TrimSpace(baseURL)
	if raw == "" {
		// 空 BaseURL 会被上层补成官方云端地址，因此视为非本机
		return false, nil
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false, fmt.Errorf("BaseURL 格式不正确：%w", err)
	}
	host := u.Hostname()
	if host == "" {
		return false, fmt.Errorf("BaseURL 缺少主机名")
	}
	// 以下两类无需查 DNS：
	//   - localhost：约定即回环，且不一定真在 hosts 里
	//   - *.localhost：RFC 6761 保留给回环，解析器必须如此处理
	if strings.EqualFold(host, "localhost") ||
		strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true, nil
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback(), nil
	}
	// 主机名形式（如 ollama.lan）：解析后要求全部地址都是回环，
	// 任一地址指向外部就按非本机处理。
	return lookupHostIsLoopback(host), nil
}

// requireCredential 校验凭据。本机端点允许 apiKey 为空，云端端点必须提供。
// 返回实际应写入 Authorization 头的 key（本机且为空时返回空串，调用方据此跳过该头）。
func requireCredential(baseURL, apiKey string) (string, error) {
	key := strings.TrimSpace(apiKey)
	if key != "" {
		return key, nil
	}
	isLocal, err := resolveLocalEndpoint(baseURL)
	if err != nil {
		return "", err
	}
	if isLocal {
		// Ollama / LM Studio 等本机服务不校验凭据，放行
		return "", nil
	}
	return "", fmt.Errorf("请先在「设置 → AI」中填写 API Key（本机端点如 http://localhost:11434/v1 可留空）")
}

// applyAuth 按需设置 Authorization 头。key 为空时不设置——
// 部分本地服务对空 Bearer 头会直接 401，比不带头更糟。
func applyAuth(req *http.Request, key string) {
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
}

// normalizeBaseURL 规整 BaseURL：去尾斜杠，空值补默认云端地址。
func normalizeBaseURL(baseURL string) string {
	b := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if b == "" {
		return "https://api.openai.com/v1"
	}
	return b
}

// LLMEndpointPreset 描述一个可一键填入的端点预设。
type LLMEndpointPreset struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	BaseURL     string `json:"baseURL"`
	Model       string `json:"model"`
	RequiresKey bool   `json:"requiresKey"`
	Hint        string `json:"hint"`
}

// LLMProbeResult 连通性自检结果。
type LLMProbeResult struct {
	OK        bool     `json:"ok"`
	Endpoint  string   `json:"endpoint"`  // 实际探测的地址
	IsLocal   bool     `json:"isLocal"`   // 是否判定为本机端点（免鉴权）
	Models    []string `json:"models"`    // 端点声明可用的模型（拉取失败则为空）
	LatencyMS int64    `json:"latencyMs"` // 往返耗时
	Message   string   `json:"message"`   // 失败原因或补充说明
}

// LLMConfigService 提供端点预设与连通性自检。
// 独立成一个服务而不是挂在 SummarizeService 上：
// 自检要同时服务于「AI 总结」「知识问答」以及将来的 embedding 配置，
// 挂在任一功能服务上都会让依赖方向变歪。
type LLMConfigService struct {
	client *http.Client
}

// NewLLMConfigService 创建端点配置服务。
func NewLLMConfigService() *LLMConfigService {
	return &LLMConfigService{
		// 自检不该让用户等太久——填错地址时要快速失败
		client: &http.Client{Timeout: 8 * time.Second},
	}
}

// Presets 返回内置端点预设列表。
func (s *LLMConfigService) Presets() []LLMEndpointPreset {
	return []LLMEndpointPreset{
		{
			ID:          "ollama",
			Label:       "Ollama（本机）",
			BaseURL:     "http://localhost:11434/v1",
			Model:       "qwen2.5:7b",
			RequiresKey: false,
			Hint:        "需先运行 ollama serve，并 ollama pull 对应模型",
		},
		{
			ID:          "lmstudio",
			Label:       "LM Studio（本机）",
			BaseURL:     "http://localhost:1234/v1",
			Model:       "local-model",
			RequiresKey: false,
			Hint:        "在 LM Studio 的 Local Server 页开启服务",
		},
		{
			ID:          "openai",
			Label:       "OpenAI",
			BaseURL:     "https://api.openai.com/v1",
			Model:       "gpt-4o-mini",
			RequiresKey: true,
		},
		{
			ID:          "deepseek",
			Label:       "DeepSeek",
			BaseURL:     "https://api.deepseek.com/v1",
			Model:       "deepseek-chat",
			RequiresKey: true,
		},
	}
}

// modelsResponse OpenAI 兼容的 /models 响应
type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// Probe 对端点做连通性自检。
// 用协议对应的模型列表端点（通常是 GET /models）而不是发一次真实的 chat 请求：
// 不消耗 token、不产生费用，且能顺带把可用模型列表带回来给前端做下拉。
// 若端点不支持列举，只标记为「可达」而非失败。
func (s *LLMConfigService) Probe(apiKey, baseURL, protocol string) *LLMProbeResult {
	p := LLMProtocol(strings.ToLower(strings.TrimSpace(protocol)))
	if p == "" {
		p = LLMProtocolOpenAIChat
	}

	isLocal, _ := resolveLocalEndpoint(baseURL)
	res := &LLMProbeResult{IsLocal: isLocal}

	req, endpoint, err := newProbeRequest(apiKey, baseURL, p)
	if err != nil {
		res.Message = err.Error()
		return res
	}
	res.Endpoint = endpoint

	start := time.Now()
	resp, err := s.client.Do(req)
	res.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		if isLocal {
			res.Message = fmt.Sprintf("连接不上本机服务：%v\n请确认服务已启动（Ollama：ollama serve；LM Studio：Local Server 页）", err)
		} else {
			res.Message = fmt.Sprintf("连接失败：%v", err)
		}
		return res
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		res.Message = fmt.Sprintf("鉴权被拒绝（%d）：请检查 API Key", resp.StatusCode)
		return res
	}
	if resp.StatusCode != http.StatusOK {
		// 模型列举端点未实现但 chat 可用的端点是存在的，因此不判定为彻底失败。
		res.OK = true
		res.Message = fmt.Sprintf("端点可达，但返回 %d（该端点可能未实现模型列举，不影响正常使用）", resp.StatusCode)
		return res
	}

	res.OK = true
	switch p {
	case LLMProtocolOpenAIChat, LLMProtocolOpenAIResponses:
		var mr modelsResponse
		if err := json.Unmarshal(body, &mr); err == nil {
			for _, m := range mr.Data {
				if m.ID != "" {
					res.Models = append(res.Models, m.ID)
				}
			}
			sort.Strings(res.Models)
		}
	case LLMProtocolAnthropicMessages:
		var ar anthropicModelsResponse
		if err := json.Unmarshal(body, &ar); err == nil {
			for _, m := range ar.Data {
				if m.ID != "" {
					res.Models = append(res.Models, m.ID)
				}
			}
			sort.Strings(res.Models)
		}
	case LLMProtocolGoogleGemini, LLMProtocolGoogleVertex:
		var gr geminiModelsResponse
		if err := json.Unmarshal(body, &gr); err == nil {
			for _, m := range gr.Models {
				if m.Name != "" {
					res.Models = append(res.Models, m.Name)
				}
			}
			sort.Strings(res.Models)
		}
	}
	if len(res.Models) == 0 {
		res.Message = "端点可达，但未返回模型列表"
	}
	return res
}

// newProbeRequest 按协议构造自检请求与探测地址。
func newProbeRequest(apiKey, baseURL string, p LLMProtocol) (*http.Request, string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		switch p {
		case LLMProtocolAnthropicMessages:
			baseURL = "https://api.anthropic.com/v1"
		case LLMProtocolGoogleGemini:
			baseURL = "https://generativelanguage.googleapis.com/v1beta"
		case LLMProtocolGoogleVertex:
			return nil, "", fmt.Errorf("Vertex AI 必须填写 Base URL")
		default:
			baseURL = "https://api.openai.com/v1"
		}
	}

	switch p {
	case LLMProtocolAnthropicMessages:
		endpoint := baseURL + "/models"
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("anthropic-version", "2023-06-01")
		if key := strings.TrimSpace(apiKey); key != "" {
			req.Header.Set("x-api-key", key)
		}
		return req, endpoint, nil
	case LLMProtocolGoogleGemini:
		// Key 走请求头（同 llmclient.go 口径），避免 URL 进错误信息泄漏 Key
		endpoint := baseURL + "/models"
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, "", err
		}
		if key := strings.TrimSpace(apiKey); key != "" {
			req.Header.Set("x-goog-api-key", key)
		}
		return req, endpoint, nil
	case LLMProtocolGoogleVertex:
		endpoint := baseURL + "/models"
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, "", err
		}
		if key := strings.TrimSpace(apiKey); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		return req, endpoint, nil
	default:
		endpoint := baseURL + "/models"
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, "", err
		}
		if key := strings.TrimSpace(apiKey); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		return req, endpoint, nil
	}
}

// anthropicModelsResponse Anthropic /v1/models 响应（与 OpenAI 同构）。
type anthropicModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// geminiModelsResponse Gemini /v1beta/models 响应。
type geminiModelsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}
