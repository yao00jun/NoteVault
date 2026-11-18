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
//
// Ollama 已从选项中移除（原生不支持 /api/rerank，上游 PR 未合并）。本地部署的
// 两条可行路线单独建模：llama.cpp 的 llama-server（--reranking）与 HuggingFace TEI，
// 二者的端点约定、请求体与错误提示都不一样，混在一个 provider 里会让自检提示
// 变成「404，请检查 baseURL」这种不可操作的话。
type RerankProvider string

const (
	// RerankProviderCohere Cohere / 硅基流动 / Jina 等兼容 /v1/rerank 端点（需 API Key）。
	RerankProviderCohere RerankProvider = "cohere"
	// RerankProviderLlamaCpp llama.cpp 的 llama-server（需以 --reranking 启动）。
	// 协议与 Cohere 同构（results[].relevance_score），走 /v1/rerank。
	RerankProviderLlamaCpp RerankProvider = "llamacpp"
	// RerankProviderTEI HuggingFace Text Embeddings Inference（TEI）的 /rerank。
	// 请求体是 query + texts，响应是裸数组 [{index,score}]，与 Cohere 不同，需单独构造与解析。
	RerankProviderTEI RerankProvider = "tei"
)

// normalizeRerankProvider 统一 provider 的大小写与空白，避免 "Cohere" 被判为不支持。
func normalizeRerankProvider(p RerankProvider) string {
	return strings.ToLower(strings.TrimSpace(string(p)))
}

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
	provider := normalizeRerankProvider(cfg.Provider)
	switch provider {
	case string(RerankProviderCohere), string(RerankProviderLlamaCpp), string(RerankProviderTEI):
	default:
		return "", fmt.Errorf("不支持的 Rerank provider: %q（支持 cohere / llamacpp / tei）", cfg.Provider)
	}
	// 与 LLM 端点不同，rerank 没有合理的公网默认地址：空 baseURL 直接报错，
	// 而不是静默落到 api.openai.com（那必然 404，且提示误导）。
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return "", fmt.Errorf("请填写 Rerank 接口地址（Base URL）")
	}
	// 三个 provider 的端点都是 <base>/rerank：
	//   cohere/llamacpp 的 base 通常已含 /v1（→ /v1/rerank）
	//   TEI 的 base 是根地址（→ /rerank）
	base := strings.TrimRight(normalizeBaseURL(cfg.BaseURL), "/")
	return base + "/rerank", nil
}

// rerankRequestBody 按协议构造请求体。
//
// 自检与真实重排共用这一处，避免「测试连接用的 body 和实际检索用的 body 不同」——
// 那会让 TEI 这类 schema 与 Cohere 不同的端点在自检时假通过。
func rerankRequestBody(provider string, cfg RerankConfig, query string, documents []string) ([]byte, error) {
	if provider == string(RerankProviderTEI) {
		// TEI：字段是 texts，不给 top_n（它返回全集，由上层截断）
		return json.Marshal(map[string]any{
			"query":    query,
			"texts":    documents,
			"truncate": true,
		})
	}
	// cohere / llamacpp：同构
	return json.Marshal(map[string]any{
		"model":     cfg.Model,
		"query":     query,
		"documents": documents,
		"top_n":     len(documents),
	})
}

// parseRerankResponse 按协议解析响应体。
//
// cohere / llamacpp：{"results":[{"index":0,"relevance_score":0.9}]}
// tei：              [{"index":0,"score":0.9}]
func parseRerankResponse(provider string, data []byte) ([]RerankResult, error) {
	if provider == string(RerankProviderTEI) {
		var items []struct {
			Index int     `json:"index"`
			Score float64 `json:"score"`
		}
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, fmt.Errorf("解析 TEI rerank 响应失败（期望裸数组 [{index,score}]）：%w", err)
		}
		out := make([]RerankResult, 0, len(items))
		for _, it := range items {
			out = append(out, RerankResult{Index: it.Index, Score: it.Score})
		}
		return out, nil
	}
	var rr rerankHTTPResponse
	if err := json.Unmarshal(data, &rr); err != nil {
		return nil, fmt.Errorf("解析 rerank 响应失败：%w", err)
	}
	if rr.Error != nil && rr.Error.Message != "" {
		return nil, fmt.Errorf("rerank 服务错误：%s", rr.Error.Message)
	}
	out := make([]RerankResult, 0, len(rr.Results))
	for _, r := range rr.Results {
		out = append(out, RerankResult{Index: r.Index, Score: r.RelevanceScore})
	}
	return out, nil
}

// rerankProviderLabel 给自检提示用的可读名称。
func rerankProviderLabel(provider string) string {
	switch provider {
	case string(RerankProviderLlamaCpp):
		return "llama-server"
	case string(RerankProviderTEI):
		return "TEI"
	default:
		return "Rerank"
	}
}

// rerank404Hint 针对各 provider 给出「怎么把它跑起来」的可操作提示。
// 404 是本地 rerank 最常见的失败形态：服务在跑，但没开 --reranking / 不是 reranker 服务，
// 泛泛的「请检查 baseURL」帮不上忙。
func rerank404Hint(provider, url string) string {
	switch provider {
	case string(RerankProviderLlamaCpp):
		return fmt.Sprintf("端点返回 404（探测 %s）：llama-server 需以 --reranking 启动并加载 reranker 模型，"+
			"例如 llama-server -m bge-reranker-v2-m3.gguf --reranking --port 8080，Base URL 填 http://127.0.0.1:8080/v1", url)
	case string(RerankProviderTEI):
		return fmt.Sprintf("端点返回 404（探测 %s）：TEI 需以 reranker 模型启动，"+
			"例如 docker run -p 8081:80 ghcr.io/huggingface/text-embeddings-inference:latest --model-id BAAI/bge-reranker-v2-m3，"+
			"Base URL 填 http://127.0.0.1:8081（不要带 /v1）", url)
	default:
		return fmt.Sprintf("端点返回 404，请检查 baseURL 是否指向 rerank 服务（当前探测 %s）", url)
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
	provider := normalizeRerankProvider(cfg.Provider)
	label := rerankProviderLabel(provider)

	credential, err := requireCredential(cfg.BaseURL, cfg.APIKey)
	if err != nil {
		res.Message = err.Error()
		return res
	}

	// 与真实重排共用 body 构造器：TEI 的 texts/query schema 在此同样生效，
	// 否则自检会对着一个「字段名不对」的 200 假通过。
	body, err := rerankRequestBody(provider, cfg, "connectivity check", []string{"a", "b"})
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
			res.Message = fmt.Sprintf("连接不上本机 rerank 服务（%s）：%v。请确认服务已启动，且已加载 reranker 模型（不是 chat 模型）", label, err)
		} else {
			res.Message = fmt.Sprintf("连接失败：%v", err)
		}
		return res
	}
	defer func() { _ = resp.Body.Close() }()
	respData, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch resp.StatusCode {
	case http.StatusOK:
		results, err := parseRerankResponse(provider, respData)
		if err != nil {
			res.Message = fmt.Sprintf("端点可达但响应不符合 %s 协议：%v", label, err)
			return res
		}
		res.OK = true
		// 明确报出「真的重排了」而不只是「HTTP 200」：本地服务也可能返回空结果，
		// 那说明模型没按 reranker 加载，检索侧会静默退回 RRF。
		if len(results) == 0 {
			res.OK = false
			res.Message = fmt.Sprintf("%s 端点可达但未返回任何重排结果：请确认服务以 reranking 模式启动并加载了 reranker 模型", label)
			return res
		}
		res.Message = fmt.Sprintf("%s 重排端点可用（返回 %d 条结果）", label, len(results))
		return res
	case http.StatusUnauthorized, http.StatusForbidden:
		res.Message = fmt.Sprintf("鉴权被拒绝（%d）：请检查 API Key", resp.StatusCode)
		return res
	case http.StatusNotFound:
		res.Message = rerank404Hint(provider, url)
		return res
	default:
		res.Message = fmt.Sprintf("端点返回 %d：%s", resp.StatusCode, strings.TrimSpace(string(respData)))
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

// NewReranker 创建默认重排客户端（分发 cohere / llamacpp / tei 三种协议）。
func NewReranker() Reranker {
	return &defaultReranker{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Rerank 按 provider 分发：cohere 与 llamacpp 同协议，tei 走独立 schema。
func (c *defaultReranker) Rerank(ctx context.Context, cfg RerankConfig, query string, documents []string) ([]RerankResult, error) {
	if len(documents) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(string(cfg.Provider)) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("未配置 Rerank（provider/model 为空），重排序不可用（已降级为纯 RRF 融合）")
	}
	provider := normalizeRerankProvider(cfg.Provider)
	switch provider {
	case string(RerankProviderCohere), string(RerankProviderLlamaCpp), string(RerankProviderTEI):
		return c.doRerank(ctx, provider, cfg, query, documents)
	default:
		return nil, fmt.Errorf("不支持的 Rerank provider: %q（支持 cohere / llamacpp / tei）", cfg.Provider)
	}
}

// doRerank 构造并执行重排请求，按 provider 解析响应。
func (c *defaultReranker) doRerank(ctx context.Context, provider string, cfg RerankConfig, query string, documents []string) ([]RerankResult, error) {
	url, err := rerankEndpointURL(cfg)
	if err != nil {
		return nil, err
	}

	body, err := rerankRequestBody(provider, cfg, query, documents)
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

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 rerank 服务失败（请确认 Rerank 端点与模型已就绪）：%w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respData, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rerank 服务返回错误（%d）：%s", resp.StatusCode, strings.TrimSpace(string(respData)))
	}

	results, err := parseRerankResponse(provider, respData)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		// 服务正常但返回空结果：视为「无可用重排」，让上层回落 RRF。
		return nil, fmt.Errorf("rerank 服务返回空结果")
	}
	return results, nil
}

// NoopReranker 未配置 rerank 端点时使用的占位实现。
// 任何调用都返回错误，由上层据此降级为纯 RRF 融合。
type NoopReranker struct{}

// Rerank 始终返回错误，确保缺配置时不会静默走重排路径。
func (NoopReranker) Rerank(ctx context.Context, cfg RerankConfig, query string, documents []string) ([]RerankResult, error) {
	return nil, fmt.Errorf("未配置 Rerank 端点，重排序不可用（已降级为纯 RRF 融合）")
}
