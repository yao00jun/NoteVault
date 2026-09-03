package service

import (
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
