package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProbeRerank_Cohere_404 验证 404 给出指向 baseURL 的可操作提示。
func TestProbeRerank_Cohere_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	svc := NewLLMConfigService()
	res := svc.ProbeRerank(RerankConfig{
		Provider: RerankProviderCohere,
		BaseURL:  srv.URL,
		Model:    "rerank-english-v3.0",
	})
	if res.OK {
		t.Fatalf("期望 OK=false，实际 OK=true")
	}
	if !strings.Contains(res.Message, "404") {
		t.Fatalf("期望提示 404，实际: %s", res.Message)
	}
}

// TestProbeRerank_OK 验证端点正常时自检通过。
func TestProbeRerank_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.3}]}`))
	}))
	defer srv.Close()

	svc := NewLLMConfigService()
	res := svc.ProbeRerank(RerankConfig{
		Provider: RerankProviderCohere,
		BaseURL:  srv.URL,
		Model:    "bge-reranker-v2-m3",
	})
	if !res.OK {
		t.Fatalf("期望 OK=true，实际 OK=false: %s", res.Message)
	}
}

// TestProbeRerank_MissingConfig 验证 provider/model 缺失时不被静默放过。
func TestProbeRerank_MissingConfig(t *testing.T) {
	svc := NewLLMConfigService()
	res := svc.ProbeRerank(RerankConfig{Provider: "", Model: ""})
	if res.OK {
		t.Fatalf("期望未配置时 OK=false")
	}
	if !strings.Contains(res.Message, "未配置") {
		t.Fatalf("期望提示未配置，实际: %s", res.Message)
	}
}

// TestProbeRerank_TEI_UsesTextsSchema 验证 TEI 走 query + texts 的独立协议：
// 若自检仍按 Cohere 的 documents 发，TEI 会 422/400，自检结果就没有意义。
func TestProbeRerank_TEI_UsesTextsSchema(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("请求体不是合法 JSON：%v", err)
		}
		if _, ok := body["texts"]; !ok {
			t.Errorf("TEI 请求应带 texts 字段，实际：%v", body)
		}
		if _, ok := body["documents"]; ok {
			t.Errorf("TEI 请求不应带 documents 字段（那是 Cohere 的 schema）：%v", body)
		}
		_, _ = w.Write([]byte(`[{"index":1,"score":0.91},{"index":0,"score":0.12}]`))
	}))
	defer srv.Close()

	svc := NewLLMConfigService()
	res := svc.ProbeRerank(RerankConfig{
		Provider: RerankProviderTEI,
		BaseURL:  srv.URL,
		Model:    "BAAI/bge-reranker-v2-m3",
	})
	if !res.OK {
		t.Fatalf("期望 TEI 自检通过，实际失败：%s", res.Message)
	}
	if gotPath != "/rerank" {
		t.Fatalf("TEI 端点应为 /rerank，实际 %q", gotPath)
	}
	if !strings.Contains(res.Message, "TEI") || !strings.Contains(res.Message, "2 条") {
		t.Fatalf("成功提示应报出 provider 与结果条数，实际：%s", res.Message)
	}
}

// TestProbeRerank_TEI_EmptyResults 验证「200 但零结果」不被当作可用——
// 那通常意味着服务加载的是 chat 模型，强行可用只会让检索静默退回 RRF。
func TestProbeRerank_TEI_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	svc := NewLLMConfigService()
	res := svc.ProbeRerank(RerankConfig{
		Provider: RerankProviderTEI,
		BaseURL:  srv.URL,
		Model:    "BAAI/bge-reranker-v2-m3",
	})
	if res.OK {
		t.Fatalf("零结果不应判定为可用")
	}
	if !strings.Contains(res.Message, "reranker 模型") {
		t.Fatalf("应提示确认加载的是 reranker 模型，实际：%s", res.Message)
	}
}

// TestProbeRerank_LlamaCpp_404Hint 验证 llama.cpp 的 404 给出启动参数级提示，
// 而不是泛泛的「检查 baseURL」。
func TestProbeRerank_LlamaCpp_404Hint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	svc := NewLLMConfigService()
	res := svc.ProbeRerank(RerankConfig{
		Provider: RerankProviderLlamaCpp,
		BaseURL:  srv.URL + "/v1",
		Model:    "bge-reranker-v2-m3",
	})
	if res.OK {
		t.Fatalf("404 应为不可用")
	}
	for _, want := range []string{"--reranking", "llama-server"} {
		if !strings.Contains(res.Message, want) {
			t.Fatalf("提示应包含 %q，实际：%s", want, res.Message)
		}
	}
	if res.Endpoint != srv.URL+"/v1/rerank" {
		t.Fatalf("探测地址应为 <base>/rerank，实际 %q", res.Endpoint)
	}
}

// TestReranker_TEI_ParsesScoreArray 验证真实重排路径与自检共用同一套 schema：
// TEI 返回裸数组（index/score），需要正确回填下标供上层重排候选。
func TestReranker_TEI_ParsesScoreArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string   `json:"query"`
			Texts []string `json:"texts"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Query != "缓存" || len(body.Texts) != 2 {
			t.Errorf("请求内容不符：%+v", body)
		}
		_, _ = w.Write([]byte(`[{"index":1,"score":0.8},{"index":0,"score":0.2}]`))
	}))
	defer srv.Close()

	res, err := NewReranker().Rerank(context.Background(), RerankConfig{
		Provider: RerankProviderTEI,
		BaseURL:  srv.URL,
		Model:    "bge-reranker-v2-m3",
	}, "缓存", []string{"文档一", "文档二"})
	if err != nil {
		t.Fatalf("TEI 重排失败：%v", err)
	}
	if len(res) != 2 || res[0].Index != 1 || res[0].Score != 0.8 {
		t.Fatalf("TEI 结果解析错误：%+v", res)
	}
}

// TestRerankEndpointURL_EmptyBaseURL 验证空地址不被静默补成 api.openai.com。
func TestRerankEndpointURL_EmptyBaseURL(t *testing.T) {
	_, err := rerankEndpointURL(RerankConfig{Provider: RerankProviderCohere, Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "Base URL") {
		t.Fatalf("期望提示填写 Base URL，实际：%v", err)
	}
}
