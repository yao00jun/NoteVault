package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProbeRerank_Ollama_404 覆盖 P1-3b 的核心静默失效场景：
// Ollama 原生不提供 /api/rerank，自检必须**明确**识别为不可用，而不是 404 被降级逻辑吞掉。
func TestProbeRerank_Ollama_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	svc := NewLLMConfigService()
	res := svc.ProbeRerank(RerankConfig{
		Provider: RerankProviderOllama,
		BaseURL:  srv.URL,
		Model:    "bge-reranker-v2-m3",
	})
	if res.OK {
		t.Fatalf("期望 OK=false（Ollama 无 /api/rerank），实际 OK=true")
	}
	if !strings.Contains(res.Message, "Ollama") {
		t.Fatalf("期望提示 Ollama 不支持重排，实际: %s", res.Message)
	}
	t.Logf("Ollama 404 提示: %s", res.Message)
}

// TestProbeRerank_Cohere_404 验证非 Ollama provider 的 404 给出不同的可操作提示（指向 baseURL）。
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
		Provider: RerankProviderOllama,
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
