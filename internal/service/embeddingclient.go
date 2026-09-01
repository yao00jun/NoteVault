package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Embedding 客户端（P1-3）
//
// 与 SummarizeService 的「调用时传配置」模式一致：Embedder 是无状态组件，
// 端点配置（baseURL/model/apiKey）在每次调用时传入，便于与 LLM 配置解耦
// （P1-6 #2：Chat / Embedding 端点分离）。
//
// 默认实现 OllamaEmbeddingClient 走 OpenAI 兼容的 /v1/embeddings，
// 复用 llmendpoint.go 的回环判定（本机端点免鉴权）。
// 未配置时由 NoopEmbedder 返回错误，触发检索降级为纯 BM25（红线 #1）。
// ---------------------------------------------------------------------------

// EmbeddingConfig 是 embedding 端点的连接配置（与 LLM 配置相互独立）。
type EmbeddingConfig struct {
	BaseURL string `json:"baseURL"`
	Model   string `json:"model"`
	APIKey  string `json:"apiKey"`
}

// Embedder 抽象一个 embedding 客户端。
// 批量接口：一次调用可嵌入多段文本，便于构建索引时批量请求、减少往返。
type Embedder interface {
	// Embed 将 texts 批量转换为向量。texts 为空时返回 nil, nil。
	Embed(ctx context.Context, cfg EmbeddingConfig, texts []string) ([][]float32, error)
}

// OllamaEmbeddingClient 通过 OpenAI 兼容的 /v1/embeddings 接口调用 embedding。
// 既支持本机 Ollama（http://localhost:11434/v1），也支持任何 OpenAI 兼容服务。
type OllamaEmbeddingClient struct {
	client *http.Client
}

// NewOllamaEmbeddingClient 创建默认 embedding 客户端。
func NewOllamaEmbeddingClient() *OllamaEmbeddingClient {
	return &OllamaEmbeddingClient{
		// 构建索引是批量嵌入，单批可能较大；但整体受调用方超时与用户耐心约束，
		// 不宜无限等待，60s 足够单批（数十段）完成。
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// ollamaEmbedRequest 对应 OpenAI 兼容 /v1/embeddings 请求体。
// input 字段同时支持 string 与 []string；这里统一用 []string 走批量。
type ollamaEmbedRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

// ollamaEmbedResponse 对应 /v1/embeddings 响应体。
type ollamaEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Embed 批量生成向量。
//
// 向量统一做 L2 归一化（长度=1），满足 chromem 余弦相似度对归一化的要求——
// 否则不同 embedding 模型的输出尺度不一，相似度排序会被尺度扭曲。
func (c *OllamaEmbeddingClient) Embed(ctx context.Context, cfg EmbeddingConfig, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("未配置 Embedding 模型")
	}
	base := normalizeBaseURL(cfg.BaseURL)
	key, err := requireCredential(cfg.BaseURL, cfg.APIKey)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(ollamaEmbedRequest{Input: texts, Model: cfg.Model})
	if err != nil {
		return nil, fmt.Errorf("构造 embedding 请求失败：%w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造 embedding 请求失败：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	applyAuth(req, key)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 embedding 服务失败（请确认 Embedding BaseURL 与模型已就绪）：%w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respData, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding 服务返回错误（%d）：%s", resp.StatusCode, string(respData))
	}

	var er ollamaEmbedResponse
	if err := json.Unmarshal(respData, &er); err != nil {
		return nil, fmt.Errorf("解析 embedding 响应失败：%w", err)
	}
	if er.Error != nil && er.Error.Message != "" {
		return nil, fmt.Errorf("embedding 服务错误：%s", er.Error.Message)
	}
	if len(er.Data) != len(texts) {
		return nil, fmt.Errorf("embedding 返回数量与输入不符（期望 %d，实际 %d）", len(texts), len(er.Data))
	}

	out := make([][]float32, len(er.Data))
	for i, d := range er.Data {
		out[i] = normalizeVector(d.Embedding)
	}
	return out, nil
}

// normalizeVector 做 L2 归一化，使向量欧氏长度=1。
func normalizeVector(v []float32) []float32 {
	if len(v) == 0 {
		return v
	}
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	scale := float32(1 / math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * scale
	}
	return out
}

// NoopEmbedder 未配置 embedding 端点时使用的占位实现。
// 任何调用都返回错误，由上层据此降级为纯 BM25。
type NoopEmbedder struct{}

// Embed 始终返回错误，确保缺配置时不会静默走向量路径。
func (NoopEmbedder) Embed(ctx context.Context, cfg EmbeddingConfig, texts []string) ([][]float32, error) {
	return nil, fmt.Errorf("未配置 Embedding 端点，向量检索不可用（已降级为纯 BM25）")
}
