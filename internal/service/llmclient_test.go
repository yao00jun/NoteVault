package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseChunks 把若干增量写成 text/event-stream 响应并逐块 flush。
func sseChunks(w http.ResponseWriter, chunks []string, done bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	for _, chunk := range chunks {
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", chunk)
		if flusher != nil {
			flusher.Flush()
		}
	}
	if done {
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func TestLLMChatStream_OpenAIChatSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		if !req.Stream {
			t.Error("stream request must set stream=true")
		}
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" {
			t.Errorf("unexpected messages: %+v", req.Messages)
		}
		sseChunks(w, []string{"你好", "，", "世界"}, true)
	}))
	defer server.Close()

	var deltas []string
	answer, err := llmChatStream(context.Background(), server.Client(),
		"", server.URL, "test-model", "openai-chat", "sys", "user",
		func(delta string) { deltas = append(deltas, delta) })
	if err != nil {
		t.Fatal(err)
	}
	if answer != "你好，世界" {
		t.Fatalf("unexpected answer: %q", answer)
	}
	if strings.Join(deltas, "") != "你好，世界" || len(deltas) != 3 {
		t.Fatalf("unexpected deltas: %q", deltas)
	}
}

func TestLLMChatStream_ErrorBodyPassthrough(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer server.Close()

	_, err := llmChatStream(context.Background(), server.Client(),
		"bad", server.URL, "m", "openai-chat", "s", "u", nil)
	if err == nil || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("expected service error passthrough, got %v", err)
	}
}

func TestLLMChatStream_MidStreamErrorStops(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"部分\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"error\":{\"message\":\"额度不足\"}}\n\n")
	}))
	defer server.Close()

	var deltas []string
	_, err := llmChatStream(context.Background(), server.Client(),
		"", server.URL, "m", "openai-chat", "s", "u",
		func(delta string) { deltas = append(deltas, delta) })
	if err == nil || !strings.Contains(err.Error(), "额度不足") {
		t.Fatalf("expected mid-stream error, got %v", err)
	}
	if len(deltas) != 1 {
		t.Fatalf("deltas before error should be delivered, got %q", deltas)
	}
}

func TestLLMChatStream_ContextCancelInterrupts(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sseChunks(w, []string{"第一"}, false)
		// 挂住连接模拟后续增量迟迟不到；客户端取消后请求上下文断开，handler 随之退出
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var deltas []string
	_, err := llmChatStream(ctx, server.Client(),
		"", server.URL, "m", "openai-chat", "s", "u",
		func(delta string) {
			deltas = append(deltas, delta)
			cancel() // 收到首个增量后立即取消
		})
	if err == nil || !strings.Contains(err.Error(), "已取消") {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	if len(deltas) != 1 {
		t.Fatalf("first delta should be delivered before cancel, got %q", deltas)
	}
}

func TestLLMChatStream_NonOpenAIProtocolFallsBackToSingleDelta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/messages") {
			t.Errorf("anthropic fallback should hit /messages, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"content":[{"type":"text","text":"整段回答"}]}`)
	}))
	defer server.Close()

	var deltas []string
	answer, err := llmChatStream(context.Background(), server.Client(),
		"k", server.URL, "m", "anthropic-messages", "s", "u",
		func(delta string) { deltas = append(deltas, delta) })
	if err != nil {
		t.Fatal(err)
	}
	if answer != "整段回答" || len(deltas) != 1 || deltas[0] != "整段回答" {
		t.Fatalf("fallback should emit one full delta, answer=%q deltas=%q", answer, deltas)
	}
}

func TestLLMChatStream_EmptyStreamFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	_, err := llmChatStream(context.Background(), server.Client(),
		"", server.URL, "m", "openai-chat", "s", "u", nil)
	if err == nil || !strings.Contains(err.Error(), "未返回内容") {
		t.Fatalf("expected empty-stream error, got %v", err)
	}
}
