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

// EmbeddingProbeResult 嵌入端点连通性自检结果（镜像 RerankProbeResult / LLMProbeResult）。
// 供前端「设置 → 语义检索」页的「测试连接」按钮使用，让 embedding 配置在保存前
// 就被明确校验——避免「以为配好、实际建向量索引才 404」的等待浪费，与「不静默」红线对齐。
type EmbeddingProbeResult struct {
	OK        bool   `json:"ok"`
	Endpoint  string `json:"endpoint"`  // 实际探测的地址（应与真实嵌入请求一致）
	IsLocal   bool   `json:"isLocal"`   // 是否本机端点（免鉴权）
	LatencyMS int64  `json:"latencyMs"` // 往返耗时
	Message   string `json:"message"`   // 失败原因或可操作提示
}

// ProbeEmbedding 对 embedding 端点做连通性自检。
//
// 关键约束：必须探测与实际嵌入请求**完全一致**的地址与协议（normalizeBaseURL + /embeddings），
// 否则会出现「设置页显示可用、实际建索引却 404」的漂移。这里明确识别 401/403/404 等失败，
// 而不是让调用方在批量建索引时才报模糊错误。
func (s *LLMConfigService) ProbeEmbedding(apiKey, baseURL, model string) *EmbeddingProbeResult {
	if strings.TrimSpace(model) == "" {
		return &EmbeddingProbeResult{OK: false, Message: "未配置 Embedding 模型（model 为空），无法验证"}
	}
	base := normalizeBaseURL(baseURL)
	endpoint := base + "/embeddings"
	isLocal, _ := resolveLocalEndpoint(baseURL)
	res := &EmbeddingProbeResult{Endpoint: endpoint, IsLocal: isLocal}

	credential, err := requireCredential(baseURL, apiKey)
	if err != nil {
		res.Message = err.Error()
		return res
	}

	body, err := json.Marshal(ollamaEmbedRequest{Input: []string{"connectivity check"}, Model: model})
	if err != nil {
		res.Message = fmt.Sprintf("构造请求失败：%v", err)
		return res
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
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
			res.Message = fmt.Sprintf("连接不上本机 embedding 服务：%v（请确认服务已启动且已 pull 对应模型）", err)
		} else {
			res.Message = fmt.Sprintf("连接失败：%v", err)
		}
		return res
	}
	defer func() { _ = resp.Body.Close() }()
	respData, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch resp.StatusCode {
	case http.StatusOK:
		var er ollamaEmbedResponse
		if err := json.Unmarshal(respData, &er); err != nil {
			res.Message = fmt.Sprintf("端点可达但响应解析失败：%v", err)
			return res
		}
		if er.Error != nil && er.Error.Message != "" {
			res.Message = fmt.Sprintf("服务错误：%s", er.Error.Message)
			return res
		}
		if len(er.Data) == 0 || len(er.Data[0].Embedding) == 0 {
			res.Message = "端点返回空向量，请检查模型是否支持 embedding"
			return res
		}
		res.OK = true
		res.Message = fmt.Sprintf("Embedding 端点可用（向量维度 %d）", len(er.Data[0].Embedding))
		return res
	case http.StatusUnauthorized, http.StatusForbidden:
		res.Message = fmt.Sprintf("鉴权被拒绝（%d）：请检查 API Key", resp.StatusCode)
		return res
	default:
		res.Message = fmt.Sprintf("端点返回 %d：%s", resp.StatusCode, string(respData))
		return res
	}
}
