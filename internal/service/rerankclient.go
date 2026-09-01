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
// 默认实现 defaultReranker 按 provider 分发到不同厂商的 /rerank 端点：
//   - ollama：本机 Ollama 的 /api/rerank（bge-reranker-v2-m3 自托管，localhost 免鉴权）
//   - cohere：Cohere 的 /v1/rerank（需 API Key）
// 两者响应同构（results[].index + relevance_score），共用一套解析。
// 未配置时由 NoopReranker 返回错误，触发检索降级为纯 RRF（红线 #1）。
// ---------------------------------------------------------------------------

// RerankProvider 标识重排序服务的厂商/协议。
type RerankProvider string

const (
	// RerankProviderOllama 本机 Ollama /api/rerank 端点（默认，免鉴权）。
	RerankProviderOllama RerankProvider = "ollama"
	// RerankProviderCohere Cohere /v1/rerank 端点（需 API Key）。
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

// rerankHTTPResponse 是 Ollama / Cohere 共用的 /rerank 响应体。
// Ollama 与 Cohere 的返回字段名一致（results[].index / relevance_score），统一解析。
type rerankHTTPResponse struct {
	Results []struct {
		Index           int     `json:"index"`
		RelevanceScore  float64 `json:"relevance_score"`
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

// NewReranker 创建默认重排客户端（分发 ollama / cohere）。
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
	case RerankProviderOllama:
		return c.rerankOllama(ctx, cfg, query, documents)
	case RerankProviderCohere:
		return c.rerankCohere(ctx, cfg, query, documents)
	default:
		return nil, fmt.Errorf("不支持的 Rerank provider: %q（仅支持 ollama / cohere）", cfg.Provider)
	}
}

// rerankOllama 调用本机 Ollama 的 /api/rerank。
// 本机端点（localhost / 127.0.0.1）免鉴权，requireCredential 会放行。
func (c *defaultReranker) rerankOllama(ctx context.Context, cfg RerankConfig, query string, documents []string) ([]RerankResult, error) {
	base := normalizeBaseURL(cfg.BaseURL)
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	url := base + "api/rerank"

	body, err := json.Marshal(map[string]any{
		"model":     cfg.Model,
		"query":     query,
		"documents": documents,
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

// rerankCohere 调用 Cohere 的 /v1/rerank（需 API Key，走 Bearer）。
func (c *defaultReranker) rerankCohere(ctx context.Context, cfg RerankConfig, query string, documents []string) ([]RerankResult, error) {
	base := normalizeBaseURL(cfg.BaseURL)
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	url := base + "rerank"

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
