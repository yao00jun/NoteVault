package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newQnATestWorkspace 创建包含若干 Markdown 文档的临时工作区
func newQnATestWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go-basics.md":  "# Go 基础\n\nGo 是一门静态类型的编译型语言，由 Google 开发。Go 支持协程 goroutine 和通道 channel 进行并发编程。",
		"deploy.md":     "# 部署指南\n\n部署流程：先执行 go build 构建二进制，再使用 systemd 托管进程，最后配置 nginx 反向代理。",
		"shopping.md":   "# 购物清单\n\n牛奶、鸡蛋、面包、咖啡豆。",
		"notes/term.md": "# 术语表\n\ngoroutine：Go 语言中的轻量级线程。channel：goroutine 之间通信的管道。",
	}
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestQnAService_Answer(t *testing.T) {
	ClearAllSearchIndexes()
	ws := newQnATestWorkspace(t)

	var gotBody chatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Go 支持 goroutine 并发 [1]。"}}]}`))
	}))
	defer server.Close()

	svc := NewQnAService()
	resp, err := svc.Answer("test-key", server.URL, "model-x", ws, "Go 语言如何做并发编程")
	if err != nil {
		t.Fatalf("Answer failed: %v", err)
	}
	if !strings.Contains(resp.Answer, "goroutine") {
		t.Errorf("unexpected answer: %s", resp.Answer)
	}
	if len(resp.Citations) == 0 {
		t.Fatalf("expected citations, got none")
	}
	// 检索应命中 Go 相关文档（go-basics.md / notes/term.md），而非购物清单
	paths := make(map[string]bool)
	for _, c := range resp.Citations {
		paths[c.Path] = true
	}
	if !paths["go-basics.md"] {
		t.Errorf("expected go-basics.md in citations, got %v", paths)
	}
	if paths["shopping.md"] {
		t.Errorf("shopping.md should not be cited: %v", paths)
	}
	// 请求中 system prompt 应包含检索到的文档内容
	if !strings.Contains(gotBody.Messages[0].Content, "goroutine") {
		t.Errorf("system prompt should contain retrieved context")
	}
	if gotBody.Messages[1].Content != "Go 语言如何做并发编程" {
		t.Errorf("user message should be the question, got %s", gotBody.Messages[1].Content)
	}
}

func TestQnAService_Answer_NoKey(t *testing.T) {
	svc := NewQnAService()
	_, err := svc.Answer("", "http://x", "m", "/tmp", "问题")
	if err == nil {
		t.Errorf("expected error for empty key")
	}
}

func TestQnAService_Answer_EmptyQuestion(t *testing.T) {
	svc := NewQnAService()
	_, err := svc.Answer("k", "http://x", "m", "/tmp", "   ")
	if err == nil {
		t.Errorf("expected error for empty question")
	}
}

func TestQnAService_Answer_NoWorkspace(t *testing.T) {
	svc := NewQnAService()
	_, err := svc.Answer("k", "http://x", "m", "", "问题")
	if err == nil {
		t.Errorf("expected error for empty workspace")
	}
}

func TestQnAService_Answer_NoMatch(t *testing.T) {
	ClearAllSearchIndexes()
	ws := newQnATestWorkspace(t)

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"x"}}]}`))
	}))
	defer server.Close()

	// 量子物理相关 token 不存在于任何文档，应直接返回提示且不调用 LLM
	svc := NewQnAService()
	resp, err := svc.Answer("k", server.URL, "m", ws, "quantum entanglement physics")
	if err != nil {
		t.Fatalf("Answer failed: %v", err)
	}
	if called {
		t.Errorf("LLM should not be called when no docs match")
	}
	if len(resp.Citations) != 0 {
		t.Errorf("expected no citations, got %d", len(resp.Citations))
	}
	if resp.Answer == "" {
		t.Errorf("expected non-empty fallback answer")
	}
}

func TestQnAService_Answer_APIError(t *testing.T) {
	ClearAllSearchIndexes()
	ws := newQnATestWorkspace(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer server.Close()

	svc := NewQnAService()
	_, err := svc.Answer("k", server.URL, "m", ws, "Go 并发")
	if err == nil || !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("expected API error, got %v", err)
	}
}

func TestQnAService_Retrieve_TitleBoost(t *testing.T) {
	ClearAllSearchIndexes()
	ws := newQnATestWorkspace(t)
	svc := NewQnAService()

	docs := svc.retrieve(ws, "部署 deploy 流程")
	if len(docs) == 0 {
		t.Fatalf("expected retrieved docs")
	}
	if docs[0].relPath != "deploy.md" {
		t.Errorf("expected deploy.md ranked first (title boost), got %s", docs[0].relPath)
	}
}
