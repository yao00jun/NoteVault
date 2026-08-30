package monitor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/notevault/notevault/internal/core"
)

func newTestMonitor(t *testing.T, dsn string, httpC httpClientDoer) *ErrorMonitor {
	t.Helper()
	dir := t.TempDir()
	m := NewErrorMonitor(dir, core.ErrorMonitorConfig{
		SentryDSN:      dsn,
		EnableLocalLog: true,
		AppVersion:     "test-1.0",
	})
	if httpC != nil {
		m.setHTTPClient(httpC)
	}
	return m
}

func TestErrorMonitor_LocalLogWrite(t *testing.T) {
	m := newTestMonitor(t, "", nil)
	if err := m.ReportError(core.ErrorReport{
		Message:   "test error",
		Source:    "app.js:1:2",
		Level:     "error",
		Timestamp: 1700000000000,
	}); err != nil {
		t.Fatalf("ReportError failed: %v", err)
	}
	logPath := filepath.Join(m.logDir, "errors.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("log file not written: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "[ERROR]") || !strings.Contains(s, "test error") || !strings.Contains(s, "app.js:1:2") {
		t.Errorf("log content unexpected: %q", s)
	}
}

func TestErrorMonitor_LocalLogStack(t *testing.T) {
	m := newTestMonitor(t, "", nil)
	stack := "Error: boom\n    at foo (file.js:10:5)\n    at bar (file.js:20:3)"
	if err := m.ReportError(core.ErrorReport{
		Message:   "boom",
		Stack:     stack,
		Timestamp: 1700000000000,
	}); err != nil {
		t.Fatalf("ReportError failed: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(m.logDir, "errors.log"))
	s := string(data)
	if !strings.Contains(s, "stack:") || !strings.Contains(s, "at foo (file.js:10:5)") {
		t.Errorf("stack not preserved: %q", s)
	}
}

func TestErrorMonitor_EmptyMessageRejected(t *testing.T) {
	m := newTestMonitor(t, "", nil)
	if err := m.ReportError(core.ErrorReport{}); !core.IsCode(err, core.ErrInvalidInput) {
		t.Errorf("expected core.ErrInvalidInput, got %v", err)
	}
}

func TestErrorMonitor_LogRotation(t *testing.T) {
	m := newTestMonitor(t, "", nil)
	// 强制把日志文件大小压到 >1MB 触发滚动
	big := strings.Repeat("x", 1<<20+100)
	if err := os.WriteFile(filepath.Join(m.logDir, "errors.log"), []byte(big), 0640); err != nil {
		t.Fatal(err)
	}
	if err := m.ReportError(core.ErrorReport{Message: "trigger rotation", Timestamp: 1700000000000}); err != nil {
		t.Fatalf("ReportError failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.logDir, "errors.log.1")); err != nil {
		t.Errorf("rotated backup not created: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(m.logDir, "errors.log"))
	if !strings.Contains(string(data), "trigger rotation") {
		t.Errorf("new log file should contain latest message, got %q", data)
	}
}

func TestErrorMonitor_SentryDisabledWhenNoDSN(t *testing.T) {
	m := newTestMonitor(t, "", nil)
	// 没注入 HTTP client，正常流程下也根本不会触发上报
	if m.sentryParsed.enabled {
		t.Error("Sentry should be disabled when DSN empty")
	}
	// 应该不发请求且无错误
	if err := m.ReportError(core.ErrorReport{Message: "no sentry", Timestamp: 1}); err != nil {
		t.Errorf("ReportError should succeed: %v", err)
	}
}

func TestErrorMonitor_SentryEnabledSendsEnvelope(t *testing.T) {
	var (
		gotPath string
		gotAuth string
		gotBody string
		gotCT   string
		called  bool
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("X-Sentry-Auth")
		gotCT = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"abc"}`))
	}))
	defer srv.Close()

	// 构造指向 httptest server 的 DSN：http://publickey@127.0.0.1:port/1
	dsn := strings.Replace(srv.URL, "http://", "http://publickey@", 1) + "/1"
	m := newTestMonitor(t, dsn, srv.Client())
	if !m.sentryParsed.enabled {
		t.Fatal("Sentry should be enabled with valid DSN")
	}
	if err := m.ReportError(core.ErrorReport{
		Message:   "boom",
		Level:     "error",
		Stack:     "Error: boom\n    at foo (a.js:1:2)",
		UserAgent: "test-agent",
		Timestamp: 1700000000000,
	}); err != nil {
		t.Fatalf("ReportError failed: %v", err)
	}
	if !called {
		t.Fatal("expected HTTP call to Sentry endpoint")
	}
	if !strings.HasSuffix(gotPath, "/api/1/envelope/") {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if !strings.Contains(gotAuth, "sentry_key=publickey") {
		t.Errorf("auth header missing key: %s", gotAuth)
	}
	if gotCT != "application/x-sentry-envelope" {
		t.Errorf("unexpected content type: %s", gotCT)
	}
	if !strings.Contains(gotBody, "boom") || !strings.Contains(gotBody, "test-agent") {
		t.Errorf("envelope body missing expected fields: %q", gotBody)
	}
	if !strings.Contains(gotBody, "test-1.0") {
		t.Errorf("release version missing in envelope: %q", gotBody)
	}
}

func TestErrorMonitor_SentryFailedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	dsn := strings.Replace(srv.URL, "http://", "http://k@", 1) + "/1"
	m := newTestMonitor(t, dsn, srv.Client())
	// 上报失败不应让 ReportError 失败（仅打印 stderr）
	if err := m.ReportError(core.ErrorReport{Message: "fail", Timestamp: 1}); err != nil {
		t.Errorf("ReportError should not fail on Sentry error: %v", err)
	}
	// 但本地日志应该写了
	if _, err := os.Stat(filepath.Join(m.logDir, "errors.log")); err != nil {
		t.Errorf("local log should still be written: %v", err)
	}
}

func TestErrorMonitor_UpdateConfig(t *testing.T) {
	m := newTestMonitor(t, "", nil)
	if m.sentryParsed.enabled {
		t.Error("initially disabled")
	}
	m.UpdateConfig(core.ErrorMonitorConfig{
		SentryDSN:      "https://abc@host.example/42",
		EnableLocalLog: true,
		AppVersion:     "2.0",
	})
	if !m.sentryParsed.enabled {
		t.Error("should enable after UpdateConfig")
	}
	if m.cfg.AppVersion != "2.0" {
		t.Errorf("version not updated: %s", m.cfg.AppVersion)
	}
	if m.sentryParsed.projectID != "42" {
		t.Errorf("project id not parsed: %s", m.sentryParsed.projectID)
	}
	if m.sentryParsed.publicKey != "abc" {
		t.Errorf("public key not parsed: %s", m.sentryParsed.publicKey)
	}
}

func TestParseSentryDSN_Invalid(t *testing.T) {
	cases := []string{
		"",
		"not-a-url",
		"http://host/no-at-sign",
		"ftp://k@host/1",  // wrong scheme
		"http://@host/1",  // no key
		"http://k@host",   // no slash/project
		"https://k@host/", // trailing slash, no project id
	}
	for _, c := range cases {
		if parseSentryDSN(c).enabled {
			t.Errorf("expected disabled for %q", c)
		}
	}
}

func TestParseSentryDSN_Valid(t *testing.T) {
	cases := []struct {
		dsn      string
		wantHost string
		wantKey  string
		wantProj string
	}{
		{"https://abc@sentry.io/123", "sentry.io", "abc", "123"},
		{"http://key@localhost:8000/myproj", "localhost:8000", "key", "myproj"},
		{"https://secret@host.example/path/to/456", "host.example/path/to", "secret", "456"},
	}
	for _, c := range cases {
		got := parseSentryDSN(c.dsn)
		if !got.enabled {
			t.Errorf("expected enabled for %q", c.dsn)
			continue
		}
		if got.publicKey != c.wantKey || got.projectID != c.wantProj {
			t.Errorf("dsn=%q got key=%q proj=%q, want key=%q proj=%q",
				c.dsn, got.publicKey, got.projectID, c.wantKey, c.wantProj)
		}
		if !strings.Contains(got.url, c.wantHost) || !strings.Contains(got.url, "/envelope/") {
			t.Errorf("dsn=%q url=%q missing host or envelope path", c.dsn, got.url)
		}
	}
}

func TestParseJSStack(t *testing.T) {
	// 末尾的 "at baz" 无 location，应被跳过
	stack := "Error: boom\n    at foo (file.js:10:5)\n    at Object.bar (file2.js:20:3)\n    at baz"
	frames := parseJSStack(stack)
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames (no-location line skipped), got %d", len(frames))
	}
	if frames[0]["function"] != "foo" || frames[0]["lineno"] != 10 || frames[0]["colno"] != 5 {
		t.Errorf("frame[0] wrong: %+v", frames[0])
	}
	if frames[1]["function"] != "Object.bar" {
		t.Errorf("frame[1] function wrong: %+v", frames[1])
	}
	// 纯 location 行（无函数名）
	frames2 := parseJSStack("    at only.js:1:1")
	if len(frames2) != 1 || frames2[0]["filename"] != "only.js" || frames2[0]["lineno"] != 1 {
		t.Errorf("location-only frame wrong: %+v", frames2)
	}
}

func TestAtoiSafe(t *testing.T) {
	if atoiSafe("10") != 10 {
		t.Error("atoiSafe 10")
	}
	if atoiSafe("abc") != 0 {
		t.Error("atoiSafe non-digit should be 0")
	}
	if atoiSafe("") != 0 {
		t.Error("atoiSafe empty should be 0")
	}
}

func TestRandomEventID(t *testing.T) {
	id := randomEventID()
	if len(id) != 32 {
		t.Errorf("event id length = %d, want 32", len(id))
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("event id has non-hex char: %q", id)
			break
		}
	}
	id2 := randomEventID()
	if id == id2 {
		t.Error("two consecutive IDs are identical (randomness broken)")
	}
}

// TestReadRandImpl_NotFallbackOnWindows 守住一个真实缺陷：
// readRandImpl 原先读 /dev/urandom，Windows 上恒失败并静默退化到
// fillFallbackID（时间戳 + 计数器，非随机），使 Sentry 事件 ID 可预测。
// 这里直接断言底层随机源在本平台可用。
func TestReadRandImpl_NotFallbackOnWindows(t *testing.T) {
	b1 := make([]byte, 16)
	b2 := make([]byte, 16)

	n, err := readRandImpl(b1)
	if err != nil {
		t.Fatalf("readRandImpl failed on this platform (event IDs would fall back to timestamps): %v", err)
	}
	if n != len(b1) {
		t.Fatalf("readRandImpl read %d bytes, want %d", n, len(b1))
	}
	if _, err := readRandImpl(b2); err != nil {
		t.Fatalf("second readRandImpl failed: %v", err)
	}
	if bytes.Equal(b1, b2) {
		t.Fatal("two consecutive random reads returned identical bytes (not random)")
	}
}

func TestErrorMonitor_DisableLocalLog(t *testing.T) {
	m := newTestMonitor(t, "", nil)
	m.UpdateConfig(core.ErrorMonitorConfig{EnableLocalLog: false, AppVersion: "1"})
	if err := m.ReportError(core.ErrorReport{Message: "no log", Timestamp: 1}); err != nil {
		t.Fatalf("ReportError failed: %v", err)
	}
	// 不写本地日志时不应有 errors.log 文件
	if _, err := os.Stat(filepath.Join(m.logDir, "errors.log")); err == nil {
		t.Error("local log file should not exist when EnableLocalLog=false")
	}
}

// 兼容 context 使用，避免编译时报未使用导入
var _ = context.Background
