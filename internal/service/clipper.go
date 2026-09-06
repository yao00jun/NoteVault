package service

// ClipperService —— 本地 Web Clipper 剪藏接口（蓝图专项 8）。
//
// 设计取舍：
//   - 极轻量：标准库 net/http，零第三方依赖、零 CGO；只监听 127.0.0.1，
//     外部机器不可达。
//   - 受 Token 保护：首次启动生成 32 字节随机 hex Token 落盘（仅当前用户可读），
//     请求必须带 `Authorization: Bearer <token>` 或 `X-Clip-Token: <token>`；
//     Token 不匹配一律 401，绝不在日志里打印 Token 本体。
//   - 纯 Markdown 落盘（架构红线 2）：剪藏内容写入当前工作区 Inbox/ 下，
//     frontmatter 记录来源 URL 与剪藏时间，正文即网页正文，不引入专有格式。
//   - 可撤销（架构红线 3）：走 FileService 保存链路，与编辑器保存共享
//     快照/历史管线（容器注入的是带 Snapshot 的实例）。
//   - 渐进降级（架构红线 4）：无当前工作区 → 503 明确报错但不崩；
//     端口被占 → Start 返回错误由调用方记日志，应用照常运行；
//     AI 摘要未配置或调用失败 → 静默省略摘要段，剪藏本体不受影响。
//
// 协议：POST /api/clip，JSON body {"title","content","url"}，
// 响应 {"ok":true,"path":"Inbox/xxx.md"} 或 {"error":"..."}。
import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DefaultClipperPort 本地剪藏端口（约定值，浏览器扩展按此配置）。
const DefaultClipperPort = 27123

// clipSummarizer 抽象 AI 摘要能力：SummarizeService 天然满足；
// 独立成接口是为了单测里注入假实现，不真的发 HTTP。
type clipSummarizer interface {
	Summarize(apiKey, baseURL, model, protocol, content string) (string, error)
}

// clipAIConfig 前端推送过来的对话模型配置（凭据不落盘，仅驻内存）。
type clipAIConfig struct {
	apiKey   string
	baseURL  string
	model    string
	protocol string
}

// ClipperService 本地剪藏 HTTP 服务。
type ClipperService struct {
	workspace *WorkspaceService
	files     *FileService
	ai        clipSummarizer

	tokenPath string // 为空用默认位置（%APPDATA%/NoteVault/clipper-token.txt）；测试注入临时文件
	port      int    // 0 = 仅用于测试的随机端口

	mu      sync.Mutex
	token   string
	server  *http.Server
	aiCfg   *clipAIConfig
}

// NewClipperService 构造剪藏服务。summarizer 可为 nil（无 AI 时纯落盘）。
func NewClipperService(ws *WorkspaceService, files *FileService, summarizer clipSummarizer) *ClipperService {
	return &ClipperService{
		workspace: ws,
		files:     files,
		ai:        summarizer,
		port:      DefaultClipperPort,
	}
}

// newTestClipperService 测试专用：随机端口 + 指定 Token 文件路径。
func newTestClipperService(ws *WorkspaceService, files *FileService, tokenPath string) *ClipperService {
	return &ClipperService{
		workspace: ws,
		files:     files,
		tokenPath: tokenPath,
		port:      0,
	}
}

// ConfigureAI 注入对话模型配置（由前端在 AI 设置就绪/变更时调用）。
// 四参全空视为关闭 AI 摘要。
func (s *ClipperService) ConfigureAI(apiKey, baseURL, model, protocol string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(apiKey) == "" && strings.TrimSpace(baseURL) == "" {
		s.aiCfg = nil
		return
	}
	s.aiCfg = &clipAIConfig{apiKey: apiKey, baseURL: baseURL, model: model, protocol: protocol}
}

// ClipperStatus 剪藏服务运行状态（暴露给设置页展示）。
type ClipperStatus struct {
	Running   bool   `json:"running"`
	Port      int    `json:"port"`
	Token     string `json:"token"`
	AIEnabled bool   `json:"aiEnabled"`
}

// Status 返回当前运行状态与 Token（Token 供用户复制进浏览器扩展）。
func (s *ClipperService) Status() ClipperStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := ClipperStatus{Port: s.port, Token: s.token}
	if s.server != nil {
		status.Running = true
	}
	status.AIEnabled = s.aiCfg != nil
	return status
}

// Start 加载/生成 Token 并在 127.0.0.1 上开始监听。非阻塞：Serve 在后台 goroutine。
// 端口被占用等监听失败原样返回错误，由调用方决定是否降级（应用不因此退出）。
func (s *ClipperService) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return nil // 幂等：已在运行
	}
	token, err := s.loadOrCreateToken()
	if err != nil {
		return fmt.Errorf("clipper: token 初始化失败: %w", err)
	}
	s.token = token

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", s.port))
	if err != nil {
		return fmt.Errorf("clipper: 监听失败: %w", err)
	}
	if s.port == 0 {
		// 测试模式：记录内核分配的实际端口，供请求构造 URL 用
		s.port = ln.Addr().(*net.TCPAddr).Port
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/clip", s.handleClip)
	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	srv := s.server
	go func() {
		// Serve 返回 ErrServerClosed 属正常停机；其余错误只记日志不崩应用
		if serveErr := srv.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			fmt.Printf("[clipper] serve 退出: %v\n", serveErr)
		}
	}()
	return nil
}

// Stop 优雅停机（应用退出时调用）。未启动时是 no-op。
func (s *ClipperService) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil {
		return nil
	}
	server := s.server
	s.server = nil
	return server.Close()
}

// clipRequest 浏览器扩展提交的剪藏载荷。
type clipRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	URL     string `json:"url"`
}

func (s *ClipperService) handleClip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeClipError(w, http.StatusMethodNotAllowed, "只支持 POST")
		return
	}
	if !s.checkToken(r) {
		writeClipError(w, http.StatusUnauthorized, "Token 缺失或不匹配")
		return
	}
	var req clipRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 5<<20)) // 5MB 上限，防恶意大包
	if err := dec.Decode(&req); err != nil {
		writeClipError(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeClipError(w, http.StatusBadRequest, "content 不能为空")
		return
	}

	ws, err := s.workspace.GetCurrentWorkspace()
	if err != nil || ws == nil || strings.TrimSpace(ws.Path) == "" {
		// 渐进降级：无工作区时明确告知，而不是写散文件
		writeClipError(w, http.StatusServiceUnavailable, "当前没有打开的工作区，请先在 NoteVault 中打开一个工作区")
		return
	}

	rel := s.buildClipPath(req.Title)
	md := buildClipMarkdown(req)
	if err := s.files.SaveFile(ws.Path, rel, md); err != nil {
		writeClipError(w, http.StatusInternalServerError, "写入剪藏失败: "+err.Error())
		return
	}

	// 可选 AI 摘要：未配置/失败都静默跳过，不影响剪藏本体
	s.appendAISummary(ws.Path, rel, req.Content)

	writeClipJSON(w, http.StatusOK, map[string]any{"ok": true, "path": rel})
}

// checkToken 校验 Bearer / X-Clip-Token；未初始化 Token 时一律拒绝。
func (s *ClipperService) checkToken(r *http.Request) bool {
	s.mu.Lock()
	expected := s.token
	s.mu.Unlock()
	if expected == "" {
		return false
	}
	got := r.Header.Get("X-Clip-Token")
	if got == "" {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			got = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	// 常量时间比较，避免侧信道猜测 Token
	return subtleEqual(got, expected)
}

// appendAISummary 尽力而为地补一段 AI 摘要；任何失败都静默（红线 4）。
func (s *ClipperService) appendAISummary(wsPath, rel, content string) {
	s.mu.Lock()
	cfg := s.aiCfg
	s.mu.Unlock()
	if cfg == nil || s.ai == nil || len(content) > 20000 {
		return
	}
	summary, err := s.ai.Summarize(cfg.apiKey, cfg.baseURL, cfg.model, cfg.protocol, content)
	if err != nil || strings.TrimSpace(summary) == "" {
		return
	}
	existing, err := s.files.ReadFile(wsPath, rel)
	if err != nil {
		return
	}
	updated := existing + "\n## AI 摘要\n\n" + strings.TrimSpace(summary) + "\n"
	_ = s.files.SaveFile(wsPath, rel, updated)
}

// buildClipPath 生成 Inbox/ 下的相对路径；同名冲突追加序号而不是覆盖。
func (s *ClipperService) buildClipPath(title string) string {
	base := sanitizeClipFilename(title)
	if base == "" {
		base = "clip-" + time.Now().Format("20060102-150405")
	}
	rel := filepath.Join("Inbox", base+".md")
	for i := 2; i < 100; i++ {
		ws, err := s.workspace.GetCurrentWorkspace()
		if err != nil || ws == nil {
			break
		}
		if _, err := os.Stat(filepath.Join(ws.Path, filepath.FromSlash(rel))); err != nil {
			break // 不存在，可用
		}
		rel = filepath.Join("Inbox", fmt.Sprintf("%s-%d.md", base, i))
	}
	return filepath.ToSlash(rel)
}

// sanitizeClipFilename 清洗标题为安全的文件名主干（Windows 保留字符/控制字符全剥）。
func sanitizeClipFilename(title string) string {
	name := strings.TrimSpace(title)
	name = strings.TrimSuffix(name, ".md")
	name = strings.TrimSuffix(name, ".markdown")
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20:
			continue
		case strings.ContainsRune(`\/:*?"<>|`, r):
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	// Windows 保留设备名
	if regexp.MustCompile(`^(?i)(con|prn|aux|nul|com[1-9]|lpt[1-9])$`).MatchString(out) {
		out = "-" + out
	}
	if len(out) > 80 {
		out = strings.TrimSpace(out[:80])
	}
	return strings.Trim(out, "-.")
}

// buildClipMarkdown 组装剪藏 Markdown：frontmatter + 来源引用 + 正文。
func buildClipMarkdown(req clipRequest) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("tags:\n  - clipping\n")
	if u := strings.TrimSpace(req.URL); u != "" {
		fmt.Fprintf(&b, "source: %s\n", u)
	}
	fmt.Fprintf(&b, "clipped: %s\n", time.Now().Format(time.RFC3339))
	b.WriteString("---\n\n")

	title := strings.TrimSpace(req.Title)
	if title == "" {
		if u := strings.TrimSpace(req.URL); u != "" {
			title = u
		} else {
			title = "未命名剪藏"
		}
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	if u := strings.TrimSpace(req.URL); u != "" {
		fmt.Fprintf(&b, "> 来源：%s\n\n", u)
	}
	b.WriteString(strings.TrimRight(req.Content, "\n"))
	b.WriteString("\n")
	return b.String()
}

// loadOrCreateToken 读取或生成 Token 文件（0600，仅当前用户可读）。
func (s *ClipperService) loadOrCreateToken() (string, error) {
	path := s.tokenPath
	if path == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(base, "NoteVault", "clipper-token.txt")
	}
	if data, err := os.ReadFile(path); err == nil {
		if token := strings.TrimSpace(string(data)); token != "" {
			return token, nil
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

func subtleEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func writeClipJSON(w http.ResponseWriter, code int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeClipError(w http.ResponseWriter, code int, msg string) {
	writeClipJSON(w, code, map[string]any{"error": msg})
}
