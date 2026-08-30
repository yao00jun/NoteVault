package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSummarizeService_Summarize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"- 要点一\n- 要点二"}}]}`))
	}))
	defer server.Close()

	svc := NewSummarizeService()
	got, err := svc.Summarize("test-key", server.URL, "model-x", "这是一段需要总结的笔记内容")
	if err != nil {
		t.Fatalf("Summarize failed: %v", err)
	}
	if !strings.Contains(got, "要点一") {
		t.Errorf("unexpected summary: %s", got)
	}
}

func TestSummarizeService_Summarize_NoKey(t *testing.T) {
	svc := NewSummarizeService()
	_, err := svc.Summarize("", "http://x", "m", "content")
	if err == nil {
		t.Errorf("expected error for empty key")
	}
}

func TestSummarizeService_Summarize_EmptyContent(t *testing.T) {
	svc := NewSummarizeService()
	_, err := svc.Summarize("k", "http://x", "m", "   ")
	if err == nil {
		t.Errorf("expected error for empty content")
	}
}

func TestSummarizeService_Summarize_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer server.Close()
	svc := NewSummarizeService()
	_, err := svc.Summarize("k", server.URL, "m", "c")
	if err == nil || !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("expected API error, got %v", err)
	}
}
