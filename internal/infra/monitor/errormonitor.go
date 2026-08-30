package monitor

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/notevault/notevault/internal/core"
	"github.com/notevault/notevault/internal/service"
)

// ErrorMonitorConfig 已上移至 core（见 core/models.go），本包通过 core.ErrorMonitorConfig 引用。
// ErrorReport 同理（core.ErrorReport）。
// 编译时断言：ErrorMonitor 实现 service.Reporter 端口接口。
var _ service.Reporter = (*ErrorMonitor)(nil)

// ErrorMonitor 错误监控服务
// 同时落地到本地日志文件（轮转）与可选的 Sentry 上报。
type ErrorMonitor struct {
	mu     sync.Mutex
	cfg    core.ErrorMonitorConfig
	logDir string
	// httpClient 上报用的 HTTP 客户端（可注入 mock）
	httpClient httpClientDoer
	// sentryParsed 缓存解析后的 Sentry 端点 URL（避免每次上报都解析）
	sentryParsed sentryEndpoint
}

// httpClientDoer 抽象 http.Client 的 Do 方法，便于测试注入
type httpClientDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// sentryEndpoint 解析后的 Sentry 端点信息
type sentryEndpoint struct {
	enabled   bool
	url       string
	publicKey string
	projectID string
}

// NewErrorMonitor 创建错误监控服务
// logDir 是本地错误日志所在目录；cfg 为初始配置。
func NewErrorMonitor(logDir string, cfg core.ErrorMonitorConfig) *ErrorMonitor {
	if cfg.AppVersion == "" {
		cfg.AppVersion = core.AppVersion
	}
	em := &ErrorMonitor{
		cfg:        cfg,
		logDir:     logDir,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	em.sentryParsed = parseSentryDSN(cfg.SentryDSN)
	return em
}

// setHTTPClient 注入 HTTP 客户端（仅测试用，故不导出）。
// 原先导出为 SetHTTPClient：它会被 Wails 反射绑定为前端 API，
// 但参数是本包未导出的 httpClientDoer 接口，生成的 TS 签名毫无意义——
// 纯测试辅助方法不应出现在前端绑定面上。
func (em *ErrorMonitor) setHTTPClient(c httpClientDoer) {
	em.mu.Lock()
	defer em.mu.Unlock()
	em.httpClient = c
}

// UpdateConfig 更新配置（运行时切换 DSN / 开关本地日志）
func (em *ErrorMonitor) UpdateConfig(cfg core.ErrorMonitorConfig) {
	em.mu.Lock()
	defer em.mu.Unlock()
	if cfg.AppVersion == "" {
		cfg.AppVersion = em.cfg.AppVersion
	}
	em.cfg = cfg
	em.sentryParsed = parseSentryDSN(cfg.SentryDSN)
}

// ReportError 接收前端上报的错误事件
func (em *ErrorMonitor) ReportError(report core.ErrorReport) error {
	if report.Message == "" {
		return core.NewError(core.ErrInvalidInput, "错误消息不能为空")
	}
	if report.Level == "" {
		report.Level = "error"
	}
	if report.Timestamp == 0 {
		report.Timestamp = time.Now().UnixMilli()
	}

	// 1) 本地日志（失败不影响上报）
	if em.cfg.EnableLocalLog {
		if err := em.writeLocal(report); err != nil {
			// 本地写入失败也继续尝试 Sentry
			fmt.Fprintf(os.Stderr, "[ErrorMonitor] local log write failed: %v\n", err)
		}
	}

	// 2) Sentry 上报（失败不返回错误，避免影响调用方）
	if em.sentryParsed.enabled {
		if err := em.sendToSentry(context.Background(), report); err != nil {
			fmt.Fprintf(os.Stderr, "[ErrorMonitor] sentry report failed: %v\n", err)
		}
	}
	return nil
}

// writeLocal 把错误事件追加到本地日志文件（按大小滚动：超过 1MB 重命名为 .1）
func (em *ErrorMonitor) writeLocal(report core.ErrorReport) error {
	if em.logDir == "" {
		return nil
	}
	if err := os.MkdirAll(em.logDir, 0750); err != nil {
		return err
	}
	logPath := filepath.Join(em.logDir, "errors.log")
	// 大小检查（≤1MB 时直接追加；超过则滚动到 .1 并重建）
	if info, err := os.Stat(logPath); err == nil && info.Size() > 1<<20 {
		_ = os.Rename(logPath, logPath+".1")
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}
	defer f.Close()
	ts := time.UnixMilli(report.Timestamp).Format("2006-01-02 15:04:05.000")
	line := fmt.Sprintf("[%s] [%s] %s", ts, strings.ToUpper(report.Level), report.Message)
	if report.Source != "" {
		line += " @ " + report.Source
	}
	if report.Stack != "" {
		// 堆栈压缩成单行（保留信息但避免日志膨胀）
		stack := strings.ReplaceAll(report.Stack, "\n", " | ")
		line += "\n  stack: " + stack
	}
	if len(report.Tags) > 0 {
		tagsJSON, _ := json.Marshal(report.Tags)
		line += "\n  tags: " + string(tagsJSON)
	}
	line += "\n"
	_, err = io.WriteString(f, line)
	return err
}

// sendToSentry 通过 Sentry envelope 协议上报事件
// 参考：https://develop.sentry.dev/sdk/envelopes/
func (em *ErrorMonitor) sendToSentry(ctx context.Context, report core.ErrorReport) error {
	em.mu.Lock()
	endpoint := em.sentryParsed
	appVer := em.cfg.AppVersion
	em.mu.Unlock()
	if !endpoint.enabled {
		return nil
	}

	// 构造 Sentry event
	event := map[string]any{
		"timestamp":   time.UnixMilli(report.Timestamp).Format(time.RFC3339Nano),
		"level":       report.Level,
		"message":     report.Message,
		"platform":    "javascript",
		"environment": "production",
		"release":     appVer,
		"tags":        report.Tags,
		"extra":       report.Extra,
		"user":        map[string]any{},
		"request": map[string]any{
			"headers": map[string]any{
				"User-Agent": report.UserAgent,
			},
		},
	}
	if report.Stack != "" {
		event["exception"] = map[string]any{
			"values": []map[string]any{
				{
					"type":  "Error",
					"value": report.Message,
					"stacktrace": map[string]any{
						"frames": parseJSStack(report.Stack),
					},
				},
			},
		}
	}

	// envelope：第一行 header，第二行 event item header，第三行 event JSON，每行换行分隔
	headerJSON, _ := json.Marshal(map[string]any{
		"event_id": randomEventID(),
		"sent_at":  time.Now().Format(time.RFC3339Nano),
		"sdk":      map[string]any{"name": "notevault-client", "version": appVer},
	})
	itemHeaderJSON, _ := json.Marshal(map[string]any{"type": "event"})
	eventJSON, _ := json.Marshal(event)

	body := bytes.NewBuffer(nil)
	body.Write(headerJSON)
	body.WriteByte('\n')
	body.Write(itemHeaderJSON)
	body.WriteByte('\n')
	body.Write(eventJSON)
	body.WriteByte('\n')

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-sentry-envelope")
	// Sentry envelope 要求 Auth 头部携带 public key
	req.Header.Set("X-Sentry-Auth", fmt.Sprintf("Sentry sentry_key=%s, sentry_version=7", endpoint.publicKey))
	resp, err := em.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("sentry returned status %d", resp.StatusCode)
	}
	return nil
}

// parseSentryDSN 解析 Sentry DSN 字符串
// 形如：https://<publickey>@<host>/<projectid>
// 或带 path：https://<publickey>@<host>/<path>/<projectid>
func parseSentryDSN(dsn string) sentryEndpoint {
	if dsn == "" {
		return sentryEndpoint{}
	}
	// 简化校验：必须以 https:// 或 http:// 开头，含 @
	if !strings.HasPrefix(dsn, "http://") && !strings.HasPrefix(dsn, "https://") {
		return sentryEndpoint{}
	}
	if !strings.Contains(dsn, "@") {
		return sentryEndpoint{}
	}
	// https://<key>@host/path/id
	schemeRest := strings.SplitN(dsn, "://", 2)
	if len(schemeRest) != 2 {
		return sentryEndpoint{}
	}
	scheme := schemeRest[0]
	rest := schemeRest[1]
	atIdx := strings.Index(rest, "@")
	if atIdx < 0 {
		return sentryEndpoint{}
	}
	publicKey := rest[:atIdx]
	hostPath := rest[atIdx+1:]
	// 最后一段作为 project id
	slashIdx := strings.LastIndex(hostPath, "/")
	if slashIdx < 0 {
		return sentryEndpoint{}
	}
	projectID := hostPath[slashIdx+1:]
	host := hostPath[:slashIdx]
	if publicKey == "" || projectID == "" || host == "" {
		return sentryEndpoint{}
	}
	// envelope 端点 URL
	url := fmt.Sprintf("%s://%s/api/%s/envelope/", scheme, host, projectID)
	return sentryEndpoint{
		enabled:   true,
		url:       url,
		publicKey: publicKey,
		projectID: projectID,
	}
}

// parseJSStack 把 JS stack 字符串解析为 Sentry frame 数组
// JS stack 形如：
//
//	Error: msg\n    at foo (file.js:10:5)\n    at bar (file.js:20:3)
//
// 仅保留带 location 的帧（含至少一个冒号的），跳过 "at fn" 形式的无位置行。
func parseJSStack(stack string) []map[string]any {
	frames := []map[string]any{}
	for _, line := range strings.Split(stack, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "at ") {
			continue
		}
		body := strings.TrimPrefix(line, "at ")
		// 形如 "foo (file.js:10:5)" 或 "file.js:10:5"
		fn := ""
		loc := body
		if idx := strings.LastIndex(body, "("); idx >= 0 && strings.HasSuffix(body, ")") {
			fn = strings.TrimSpace(body[:idx])
			loc = strings.TrimSuffix(body[idx+1:], ")")
		}
		// 跳过无 location（无冒号）的 at 行
		if !strings.Contains(loc, ":") {
			continue
		}
		parts := strings.Split(loc, ":")
		frame := map[string]any{
			"filename": loc,
		}
		if fn != "" {
			frame["function"] = fn
		}
		if len(parts) >= 3 {
			frame["filename"] = strings.Join(parts[:len(parts)-2], ":")
			frame["lineno"] = atoiSafe(parts[len(parts)-2])
			frame["colno"] = atoiSafe(parts[len(parts)-1])
		}
		frames = append(frames, frame)
	}
	return frames
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// randomEventID 生成 32 字符的 hex 事件 ID（Sentry 要求 16 字节 UUID hex）
// 优先用 /dev/urandom；失败时退化为时间戳 + 计数器混合（保证调用间唯一性）
func randomEventID() string {
	var b [16]byte
	if _, err := readRand(b[:]); err != nil {
		// 退化方案：用纳秒时间戳 + 全局计数器填充，保证多次调用不重复
		fillFallbackID(b[:])
	}
	const hex = "0123456789abcdef"
	out := make([]byte, 32)
	for i, v := range b {
		out[i*2] = hex[v>>4]
		out[i*2+1] = hex[v&0x0f]
	}
	return string(out)
}

// fallbackIDCounter 退化解的递增计数器
var fallbackIDCounter uint64

// fillFallbackID 用时间戳 + 计数器混合填充 16 字节
// 前 8 字节：纳秒时间戳（小端序）；后 8 字节：计数器 + 时间戳高位异或
func fillFallbackID(b []byte) {
	now := uint64(time.Now().UnixNano())
	cnt := incFallbackCounter()
	for i := 0; i < 8; i++ {
		b[i] = byte(now >> (i * 8))
	}
	mix := cnt ^ now
	for i := 0; i < 8; i++ {
		b[8+i] = byte(mix >> (i * 8))
	}
}

// incFallbackCounter 原子递增计数器（无 sync/atomic 时用 mutex 模拟）
var fallbackMu sync.Mutex

func incFallbackCounter() uint64 {
	fallbackMu.Lock()
	defer fallbackMu.Unlock()
	fallbackIDCounter++
	return fallbackIDCounter
}

// readRand 包装 rand.Read 便于测试
var readRand = func(b []byte) (int, error) {
	return readRandImpl(b)
}

// readRandImpl 读取密码学安全随机数。
// 原先读 /dev/urandom：Windows 上该文件不存在，会恒失败并退化到
// fillFallbackID（时间戳 + 计数器），使 Sentry 事件 ID 变成可预测的值。
// crypto/rand 在所有平台都可用（Windows 走 BCryptGenRandom）。
func readRandImpl(b []byte) (int, error) {
	return crand.Read(b)
}

// helperError 仅用于测试，提供稳定的 errors.Is 链
var _ = errors.Is
