package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Rerank 客户端（P1-3b）
//
// Rerank（重排序）是 P1-3 混合检索的可选增强：先用 BM25 + 向量召回一个较宽的
// 候选块池（Top 20–50），再用一个 cross-encoder 同时看「query + 每个候选」，
// 给出比双塔向量更准的相关性分数，最后取 Top-N 进上下文。
//
// 与 EmbeddingClient 同模式：Reranker 是无状态组件，端点配置（provider/baseURL/
// model/apiKey）在每次调用时传入，便于与 LLM / Embedding 配置各自独立（P1-6 #2）。
//
// 默认实现 defaultReranker 按 provider 分发到兼容的 /rerank 端点：
//   - cohere：Cohere / 硅基流动等 /v1/rerank（需 API Key）
// 响应同构（results[].index + relevance_score），共用一套解析。
// 未配置时由 NoopReranker 返回错误，触发检索降级为纯 RRF（红线 #1）。
// ---------------------------------------------------------------------------

// RerankProvider 标识重排序服务的厂商/协议。
// Ollama 已从选项中移除（原生不支持 /api/rerank，上游 PR 未合并）。
type RerankProvider string

const (
	// RerankProviderCohere Cohere / 硅基流动等兼容 /v1/rerank 端点（需 API Key）。
	RerankProviderCohere RerankProvider = "cohere"
)

// RerankConfig 是 rerank 端点的连接配置（与 embedding / LLM 配置相互独立）。
type RerankConfig struct {
	Provider RerankProvider `json:"provider"`
	BaseURL  string         `json:"baseURL"`
	Model    string         `json:"model"`
	APIKey   string         `json:"apiKey"`
}

// RerankResult 是单条重排结果：documents 的下标 + 相关性分数（越大越相关）。
type RerankResult struct {
	Index int     `json:"index"`
	Score float64 `json:"score"`
}

// Reranker 抽象一个重排序客户端。
// 输入 query 与一批候选文档文本，输出按相关性降序排列的结果（含原下标，便于回填）。
type Reranker interface {
	Rerank(ctx context.Context, cfg RerankConfig, query string, documents []string) ([]RerankResult, error)
}

// RerankProbeResult 重排端点连通性自检结果（镜像 LLMProbeResult）。
// 供前端「设置 → 重排序」页的「测试连接」按钮使用，让 rerank 配置在保存前
// 就被明确校验——避免 P1-3b 的静默失效（Ollama 无 /api/rerank，404 被降级逻辑吞掉）。
type RerankProbeResult struct {
	OK        bool   `json:"ok"`
	Endpoint  string `json:"endpoint"`  // 实际探测的地址（应与真实重排请求一致）
	IsLocal   bool   `json:"isLocal"`   // 是否本机端点（免鉴权）
	LatencyMS int64  `json:"latencyMs"` // 往返耗时
	Message   string `json:"message"`   // 失败原因或可操作提示
}

// rerankEndpointURL 按 provider 拼出重排端点地址。
//
// 抽成单一实现是刻意的：连通性自检（LLMConfigService.ProbeRerank）必须探测
// **与实际重排请求完全一致**的地址，否则一旦两侧各自拼串，就会出现
// 「设置页自检显示可用、实际检索却 404」的漂移——而 404 会被降级逻辑静默吞掉，
// 用户完全无感（这正是 P1-3b 的静默失效）。
func rerankEndpointURL(cfg RerankConfig) (string, error) {
	base := normalizeBaseURL(cfg.BaseURL)
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	switch RerankProvider(strings.ToLower(strings.TrimSpace(string(cfg.Provider)))) {
	case RerankProviderCohere:
		return base + "rerank", nil
	default:
		return "", fmt.Errorf("不支持的 Rerank provider: %q（仅支持 cohere）", cfg.Provider)
	}
}

// ProbeRerank 对 rerank 端点做连通性自检。
//
// 关键约束：必须探测与实际重排请求**完全一致**的地址与协议（见 rerankEndpointURL 的注释）。
// 否则会出现「设置页显示可用、实际 404 被静默降级」的漂移——这正是 P1-3b 的静默失效根因：
// Ollama 原生不提供 /api/rerank（上游 PR 从未合并），此前该 404 会被降级逻辑静默吞掉，
// 用户只觉得「开了 rerank 没区别」。这里把它明确识别为「不可用」并给出可操作提示，
// 与项目「不静默」红线对齐。
func (s *LLMConfigService) ProbeRerank(cfg RerankConfig) *RerankProbeResult {
	if strings.TrimSpace(string(cfg.Provider)) == "" || strings.TrimSpace(cfg.Model) == "" {
		return &RerankProbeResult{OK: false, Message: "未配置 Rerank（provider / model 为空），重排序不可用"}
	}

	url, err := rerankEndpointURL(cfg)
	if err != nil {
		return &RerankProbeResult{OK: false, Message: err.Error()}
	}
	isLocal, _ := resolveLocalEndpoint(cfg.BaseURL)
	res := &RerankProbeResult{Endpoint: url, IsLocal: isLocal}

	credential, err := requireCredential(cfg.BaseURL, cfg.APIKey)
	if err != nil {
		res.Message = err.Error()
		return res
	}

	body, err := json.Marshal(map[string]any{
		"model":     cfg.Model,
		"query":     "connectivity check",
		"documents": []string{"a", "b"},
	})
	if err != nil {
		res.Message = fmt.Sprintf("构造请求失败：%v", err)
		return res
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		res.Message = fmt.Sprintf("构造请求失败：%v", err)
		return res
	}
	req.Header.Set("Content-Type", "application/json")
	applyAuth(req, credential)

	start := time.Now()
	resp, err := s.client.Do(req)
	res.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		if isLocal {
			res.Message = fmt.Sprintf("连接不上本机 rerank 服务：%v（请确认服务已启动且已 pull 对应模型）", err)
		} else {
			res.Message = fmt.Sprintf("连接失败：%v", err)
		}
		return res
	}
	defer func() { _ = resp.Body.Close() }()
	respData, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch resp.StatusCode {
	case http.StatusOK:
		var rr rerankHTTPResponse
		if err := json.Unmarshal(respData, &rr); err != nil {
			res.Message = fmt.Sprintf("端点可达但响应解析失败：%v", err)
			return res
		}
		if rr.Error != nil && rr.Error.Message != "" {
			res.Message = fmt.Sprintf("服务错误：%s", rr.Error.Message)
			return res
		}
		res.OK = true
		res.Message = "Rerank 端点可用"
		return res
	case http.StatusUnauthorized, http.StatusForbidden:
		res.Message = fmt.Sprintf("鉴权被拒绝（%d）：请检查 API Key", resp.StatusCode)
		return res
	case http.StatusNotFound:
		res.Message = fmt.Sprintf("端点返回 404，请检查 baseURL 是否指向 rerank 服务（当前探测 %s）", url)
		return res
	default:
		res.Message = fmt.Sprintf("端点返回 %d：%s", resp.StatusCode, string(respData))
		return res
	}
}

// rerankHTTPResponse 是 Ollama / Cohere 共用的 /rerank 响应体。
// Ollama 与 Cohere 的返回字段名一致（results[].index / relevance_score），统一解析。
type rerankHTTPResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// defaultReranker 是内置的重排分发器，按 cfg.Provider 选择厂商端点。
// 由容器注入 QnAService；未配置（provider/model 为空）时 Rerank 返回错误，
// 由上层据此降级为纯 RRF 融合。
type defaultReranker struct {
	client *http.Client
}

// NewReranker 创建默认重排客户端（仅分发 cohere 兼容端点）。
func NewReranker() Reranker {
	return &defaultReranker{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Rerank 按 provider 分发到具体厂商实现。
func (c *defaultReranker) Rerank(ctx context.Context, cfg RerankConfig, query string, documents []string) ([]RerankResult, error) {
	if len(documents) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(string(cfg.Provider)) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("未配置 Rerank（provider/model 为空），重排序不可用（已降级为纯 RRF 融合）")
	}
	switch RerankProvider(strings.ToLower(strings.TrimSpace(string(cfg.Provider)))) {
	case RerankProviderCohere:
		return c.rerankCohere(ctx, cfg, query, documents)
	default:
		return nil, fmt.Errorf("不支持的 Rerank provider: %q（仅支持 cohere）", cfg.Provider)
	}
}

// rerankCohere 调用 Cohere 兼容的 /v1/rerank（需 API Key，走 Bearer）。
func (c *defaultReranker) rerankCohere(ctx context.Context, cfg RerankConfig, query string, documents []string) ([]RerankResult, error) {
	url, err := rerankEndpointURL(cfg)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(map[string]any{
		"model":     cfg.Model,
		"query":     query,
		"documents": documents,
		"top_n":     len(documents),
	})
	if err != nil {
		return nil, fmt.Errorf("构造 rerank 请求失败：%w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造 rerank 请求失败：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	credential, err := requireCredential(cfg.BaseURL, cfg.APIKey)
	if err != nil {
		return nil, err
	}
	applyAuth(req, credential)

	return c.doRerank(req)
}

// doRerank 执行请求并解析 Ollama / Cohere 共用的响应体。
func (c *defaultReranker) doRerank(req *http.Request) ([]RerankResult, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 rerank 服务失败（请确认 Rerank 端点与模型已就绪）：%w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respData, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rerank 服务返回错误（%d）：%s", resp.StatusCode, string(respData))
	}

	var rr rerankHTTPResponse
	if err := json.Unmarshal(respData, &rr); err != nil {
		return nil, fmt.Errorf("解析 rerank 响应失败：%w", err)
	}
	if rr.Error != nil && rr.Error.Message != "" {
		return nil, fmt.Errorf("rerank 服务错误：%s", rr.Error.Message)
	}
	if len(rr.Results) == 0 {
		// 服务正常但返回空结果：视为「无可用重排」，让上层回落 RRF。
		return nil, fmt.Errorf("rerank 服务返回空结果")
	}

	out := make([]RerankResult, 0, len(rr.Results))
	for _, r := range rr.Results {
		out = append(out, RerankResult{Index: r.Index, Score: r.RelevanceScore})
	}
	return out, nil
}

// NoopReranker 未配置 rerank 端点时使用的占位实现。
// 任何调用都返回错误，由上层据此降级为纯 RRF 融合。
type NoopReranker struct{}

// Rerank 始终返回错误，确保缺配置时不会静默走重排路径。
func (NoopReranker) Rerank(ctx context.Context, cfg RerankConfig, query string, documents []string) ([]RerankResult, error) {
	return nil, fmt.Errorf("未配置 Rerank 端点，重排序不可用（已降级为纯 RRF 融合）")
}
