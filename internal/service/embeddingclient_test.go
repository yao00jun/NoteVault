package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProbeEmbedding_OK 验证端点正常返回向量时自检通过，且能读出向量维度。
func TestProbeEmbedding_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("期望打到 /embeddings，实际 %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer srv.Close()

	svc := NewLLMConfigService()
	res := svc.ProbeEmbedding("", srv.URL, "bge-m3")
	if !res.OK {
		t.Fatalf("期望 OK=true，实际 OK=false: %s", res.Message)
	}
	if !strings.Contains(res.Message, "3") {
		t.Fatalf("期望消息含向量维度 3，实际: %s", res.Message)
	}
}

// TestProbeEmbedding_MissingModel 验证 model 缺失时不被静默放过。
func TestProbeEmbedding_MissingModel(t *testing.T) {
	svc := NewLLMConfigService()
	res := svc.ProbeEmbedding("", "http://localhost:11434/v1", "")
	if res.OK {
		t.Fatalf("期望未配置模型时 OK=false")
	}
	if !strings.Contains(res.Message, "未配置") {
		t.Fatalf("期望提示未配置，实际: %s", res.Message)
	}
}

// TestProbeEmbedding_Unauthorized 验证 Key 失效（401/403）被明确识别。
func TestProbeEmbedding_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	svc := NewLLMConfigService()
	res := svc.ProbeEmbedding("bad-key", srv.URL, "bge-m3")
	if res.OK {
		t.Fatalf("期望 OK=false，实际 OK=true")
	}
	if !strings.Contains(res.Message, "401") {
		t.Fatalf("期望提示 401，实际: %s", res.Message)
	}
}

// TestProbeEmbedding_EmptyVector 验证端点虽 200 但返回空向量时给出明确提示。
func TestProbeEmbedding_EmptyVector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	svc := NewLLMConfigService()
	res := svc.ProbeEmbedding("", srv.URL, "bge-m3")
	if res.OK {
		t.Fatalf("期望空向量时 OK=false，实际 OK=true")
	}
	if !strings.Contains(res.Message, "空向量") {
		t.Fatalf("期望提示空向量，实际: %s", res.Message)
	}
}
