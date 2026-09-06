package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ---- 测试脚手架 ----

// fakeClipSummarizer 可编程的假 AI 摘要器。
type fakeClipSummarizer struct {
	calls    int
	lastText string
	reply    string
	err      error
}

func (f *fakeClipSummarizer) Summarize(apiKey, baseURL, model, protocol, content string) (string, error) {
	f.calls++
	f.lastText = content
	if f.err != nil {
		return "", f.err
	}
	return f.reply, nil
}

type clipTestEnv struct {
	svc    *ClipperService
	ws     *WorkspaceService
	wsPath string
	ai     *fakeClipSummarizer
}

// newClipTestEnv 起一个随机端口的 Clipper：临时工作区 + 临时 Token 文件。
func newClipTestEnv(t *testing.T) *clipTestEnv {
	t.Helper()
	tmp := t.TempDir()

	wsSvc := &WorkspaceService{configDir: filepath.Join(tmp, "config")}
	wsPath := filepath.Join(tmp, "vault")
	if _, err := wsSvc.CreateWorkspace("剪藏测试区", wsPath); err != nil {
		t.Fatalf("CreateWorkspace failed: %v", err)
	}

	ai := &fakeClipSummarizer{}
	svc := newTestClipperService(wsSvc, NewFileService(), filepath.Join(tmp, "config", "clipper-token.txt"))
	svc.ConfigureAI("key", "https://api.example.com/v1", "gpt-test", "openai-chat")
	svc.ai = ai // 注入假摘要器
	if err := svc.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop() })
	return &clipTestEnv{svc: svc, ws: wsSvc, wsPath: wsPath, ai: ai}
}

func (e *clipTestEnv) post(t *testing.T, token string, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/api/clip", e.svc.Status().Port)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Clip-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func clipBody() clipRequest {
	return clipRequest{
		Title:   "Go 并发模式",
		Content: "channel 是 Go 并发的核心原语……",
		URL:     "https://go.dev/blog/pipelines",
	}
}

// ---- 鉴权 ----

func TestClipper_RejectsMissingOrWrongToken(t *testing.T) {
	env := newClipTestEnv(t)
	token := env.svc.Status().Token
	if token == "" {
		t.Fatal("token should be generated on Start")
	}

	// 无 Token
	if resp := env.post(t, "", clipBody()); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: expected 401, got %d", resp.StatusCode)
	}
	// 错 Token
	if resp := env.post(t, "wrong-token", clipBody()); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: expected 401, got %d", resp.StatusCode)
	}
	// Token 不能旁路：Inbox 里没有任何剪藏文件落盘
	// （脚手架在建区时就会创建 Inbox 目录并放入使用引导.md，保护对象是"剪藏文件不被写入"）
	entries, err := os.ReadDir(filepath.Join(env.wsPath, "Inbox"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read Inbox failed: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "使用引导.md" { // 脚手架引导文件是建区时合法写入的
			t.Errorf("no clip file should land in Inbox when auth fails, got: %s", e.Name())
		}
	}
}

func TestClipper_BearerAuthAccepted(t *testing.T) {
	env := newClipTestEnv(t)
	token := env.svc.Status().Token

	url := fmt.Sprintf("http://127.0.0.1:%d/api/clip", env.svc.Status().Port)
	raw, _ := json.Marshal(clipBody())
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bearer auth: expected 200, got %d", resp.StatusCode)
	}
}

func TestClipper_MethodNotAllowed(t *testing.T) {
	env := newClipTestEnv(t)
	url := fmt.Sprintf("http://127.0.0.1:%d/api/clip", env.svc.Status().Port)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET: expected 405, got %d", resp.StatusCode)
	}
}

// ---- 落盘 ----

func TestClipper_WritesMarkdownToInbox(t *testing.T) {
	env := newClipTestEnv(t)
	token := env.svc.Status().Token

	resp := env.post(t, token, clipBody())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var out struct {
		OK   bool   `json:"ok"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !out.OK {
		t.Error("expected ok=true")
	}
	if !strings.HasPrefix(out.Path, "Inbox/") || !strings.HasSuffix(out.Path, ".md") {
		t.Fatalf("path should be Inbox/*.md, got %q", out.Path)
	}

	data, err := os.ReadFile(filepath.Join(env.wsPath, filepath.FromSlash(out.Path)))
	if err != nil {
		t.Fatalf("clip file missing: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"# Go 并发模式",
		"> 来源：https://go.dev/blog/pipelines",
		"channel 是 Go 并发的核心原语",
		"tags:", "clipping", // frontmatter
	} {
		if !strings.Contains(content, want) {
			t.Errorf("clip content should contain %q\n---\n%s", want, content)
		}
	}
}

func TestClipper_SanitizeTitleFilename(t *testing.T) {
	env := newClipTestEnv(t)
	token := env.svc.Status().Token

	resp := env.post(t, token, clipRequest{
		Title:   `设计/模式:*"笔记"?`,
		Content: "正文",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var out struct {
		Path string `json:"path"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	name := filepath.Base(out.Path)
	if strings.ContainsAny(name, `\/:*?"<>|`) {
		t.Errorf("filename should be sanitized, got %q", name)
	}
	if _, err := os.Stat(filepath.Join(env.wsPath, filepath.FromSlash(out.Path))); err != nil {
		t.Fatalf("sanitized clip file missing: %v", err)
	}
}

func TestClipper_DuplicateTitleDoesNotOverwrite(t *testing.T) {
	env := newClipTestEnv(t)
	token := env.svc.Status().Token

	first := env.post(t, token, clipBody())
	second := env.post(t, token, clipBody())
	if first.StatusCode != http.StatusOK || second.StatusCode != http.StatusOK {
		t.Fatalf("expected 200/200, got %d/%d", first.StatusCode, second.StatusCode)
	}
	var a, b struct {
		Path string `json:"path"`
	}
	_ = json.NewDecoder(first.Body).Decode(&a)
	_ = json.NewDecoder(second.Body).Decode(&b)
	if a.Path == b.Path {
		t.Errorf("second clip must not overwrite the first, both at %q", a.Path)
	}
}

// ---- 渐进降级 ----

func TestClipper_NoWorkspaceReturns503(t *testing.T) {
	tmp := t.TempDir()
	// 独立的空 workspace 服务：不创建任何工作区
	wsSvc := &WorkspaceService{configDir: filepath.Join(tmp, "config")}
	svc := newTestClipperService(wsSvc, NewFileService(), filepath.Join(tmp, "token.txt"))
	if err := svc.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop() })
	token := svc.Status().Token

	url := fmt.Sprintf("http://127.0.0.1:%d/api/clip", svc.Status().Port)
	raw, _ := json.Marshal(clipBody())
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	req.Header.Set("X-Clip-Token", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("no workspace: expected 503, got %d", resp.StatusCode)
	}
}

func TestClipper_EmptyContentRejected(t *testing.T) {
	env := newClipTestEnv(t)
	if resp := env.post(t, env.svc.Status().Token, clipRequest{Title: "空"}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty content: expected 400, got %d", resp.StatusCode)
	}
}

// ---- AI 摘要（可选段） ----

func TestClipper_AISummaryAppended(t *testing.T) {
	env := newClipTestEnv(t)
	env.ai.reply = "这段讲了 channel 与 goroutine 的协作模式。"
	token := env.svc.Status().Token

	resp := env.post(t, token, clipBody())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var out struct {
		Path string `json:"path"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if env.ai.calls != 1 {
		t.Fatalf("summarizer should be called once, got %d", env.ai.calls)
	}
	data, _ := os.ReadFile(filepath.Join(env.wsPath, filepath.FromSlash(out.Path)))
	if !strings.Contains(string(data), "## AI 摘要") || !strings.Contains(string(data), env.ai.reply) {
		t.Errorf("summary section missing:\n%s", data)
	}
}

func TestClipper_AIFailureIsSilent(t *testing.T) {
	env := newClipTestEnv(t)
	env.ai.err = fmt.Errorf("network down")
	token := env.svc.Status().Token

	resp := env.post(t, token, clipBody())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("AI failure must not fail the clip, got %d", resp.StatusCode)
	}
	var out struct {
		Path string `json:"path"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	data, _ := os.ReadFile(filepath.Join(env.wsPath, filepath.FromSlash(out.Path)))
	if strings.Contains(string(data), "## AI 摘要") {
		t.Errorf("failed summary should be omitted:\n%s", data)
	}
}

func TestClipper_AIDisabledWhenNoConfig(t *testing.T) {
	env := newClipTestEnv(t)
	env.svc.ConfigureAI("", "", "", "") // 关闭 AI
	token := env.svc.Status().Token

	resp := env.post(t, token, clipBody())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if env.ai.calls != 0 {
		t.Errorf("summarizer must not run when AI disabled, calls=%d", env.ai.calls)
	}
	if env.svc.Status().AIEnabled {
		t.Error("AIEnabled should be false after clearing config")
	}
}

// ---- Token 持久化 ----

func TestClipper_TokenPersistedAcrossRestarts(t *testing.T) {
	tmp := t.TempDir()
	tokenPath := filepath.Join(tmp, "clipper-token.txt")
	wsSvc := &WorkspaceService{configDir: filepath.Join(tmp, "config")}

	svc1 := newTestClipperService(wsSvc, NewFileService(), tokenPath)
	if err := svc1.Start(); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	token1 := svc1.Status().Token
	_ = svc1.Stop()

	// Token 文件权限应为仅当前用户可读（Windows 不映射 POSIX 位，跳过）
	if info, err := os.Stat(tokenPath); err != nil {
		t.Fatalf("token file missing: %v", err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Errorf("token file should not be group/world readable, mode=%v", info.Mode().Perm())
	}

	svc2 := newTestClipperService(wsSvc, NewFileService(), tokenPath)
	if err := svc2.Start(); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	t.Cleanup(func() { _ = svc2.Stop() })
	if svc2.Status().Token != token1 {
		t.Error("token should be reused across restarts, not regenerated")
	}
}

// ---- 编译期契约：SummarizeService 满足摘要接口 ----

var _ clipSummarizer = (*SummarizeService)(nil)
