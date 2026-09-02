// Package mcp 实现 NoteVault 的 Model Context Protocol (MCP) 服务端。
//
// 设计取舍：
//   - 零第三方依赖：手写 JSON-RPC 2.0 over stdio，不引入任何 MCP SDK，
//     与项目「只依赖 wails/v3 + golang.org/x/sys」的策略保持一致。
//   - 纯 CLI：本包与 cmd/notevault-mcp 只依赖 internal/service（不引 wails），
//     因此编译产物是干净的命令行进程，可被 Claude Code / Codex 等作为子进程拉起。
//   - 默认只读：6 个 tool 里 5 个只读；唯独 create_note 受 --enable-write 门控，
//     未开开关时调用会明确报错，绝不触碰任何文件（含 .notevault 缓存）。
package mcp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/notevault/notevault/internal/service"
)

// 协议常量。ProtocolVersion 取截至实现时的最新稳定版；客户端在 initialize
// 时带来的版本若不为空则原样回显（主流客户端只会发受支持版本）。
const (
	ProtocolVersion = "2025-06-18"
	ServerName      = "notevault-mcp"
	ServerVersion   = "1.0.0"
)

// Server 持有一个工作区的所有复用服务实例，并处理 MCP 消息。
type Server struct {
	workspacePath string
	enableWrite   bool

	fileSvc   *service.FileService
	searchSvc *service.SearchService
	tagSvc    *service.TagService
	todoSvc   *service.TodoService
	graphSvc  *service.GraphService
}

// NewServer 构造一个绑定到 workspacePath 的 MCP 服务端。
// enableWrite=false 时 create_note 一律拒绝。
func NewServer(workspacePath string, enableWrite bool) *Server {
	fs := service.NewFileService()
	return &Server{
		workspacePath: workspacePath,
		enableWrite:   enableWrite,
		fileSvc:       fs,
		searchSvc:     service.NewSearchServiceForFullSnippets(fs),
		tagSvc:        service.NewTagService(),
		todoSvc:       service.NewTodoService(),
		graphSvc:      service.NewGraphService(),
	}
}

// ---- JSON-RPC 2.0 报文结构 ----

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Process 解析并分发一条 JSON-RPC 报文。
// 返回值：响应字节（可能为空）、是否有响应（notification 无响应）、是否应继续。
// 任何内部错误都已吞掉并转为合法的 JSON-RPC 错误响应，不会让进程崩溃。
func (s *Server) Process(raw []byte) ([]byte, bool) {
	var req jsonRPCRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		// 连 id 都拿不到时按 null id 回错误。
		return mustJSON(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: -32700, Message: "Parse error: " + err.Error()},
		}), true
	}

	// 通知（无 id）：消费但不回复（如 notifications/initialized）。
	if len(req.ID) == 0 {
		s.handleNotification(req.Method, req.Params)
		return nil, false
	}

	var result any
	var rpcErr *rpcError
	switch req.Method {
	case "initialize":
		result = s.initialize(req.Params)
	case "tools/list":
		result = s.toolsList()
	case "tools/call":
		result, rpcErr = s.toolsCall(req.Params)
	case "ping":
		result = map[string]any{}
	case "resources/list", "prompts/list":
		result = map[string]any{"items": []any{}}
	default:
		rpcErr = &rpcError{Code: -32601, Message: "Method not found: " + req.Method}
	}

	resp := jsonRPCResponse{JSONRPC: "2.0", ID: req.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	return mustJSON(resp), true
}

func (s *Server) handleNotification(method string, _ json.RawMessage) {
	// 目前没有需要处理的通知；保留钩子以便将来扩展（如进度通知）。
	_ = method
}

// ---- 协议方法实现 ----

func (s *Server) initialize(params json.RawMessage) map[string]any {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &p)
	pv := p.ProtocolVersion
	if pv == "" {
		pv = ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": pv,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    ServerName,
			"version": ServerVersion,
		},
	}
}

func (s *Server) toolsList() map[string]any {
	return map[string]any{"tools": s.toolDefs()}
}

// toolsCall 返回 (result, rpcError)。协议层错误（未知 tool）走 rpcError；
// tool 自身的执行错误则封装成 isError=true 的 result，符合 MCP 惯例。
func (s *Server) toolsCall(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: -32602, Message: "Invalid params: " + err.Error()}
	}
	if p.Arguments == nil {
		p.Arguments = map[string]any{}
	}

	handler, ok := s.toolHandlers()[p.Name]
	if !ok {
		return nil, &rpcError{Code: -32602, Message: "Unknown tool: " + p.Name}
	}

	text, toolErr := handler(p.Arguments)
	isError := toolErr != nil
	if isError {
		text = "Error: " + toolErr.Error()
	}
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
		"isError": isError,
	}, nil
}

// ---- tool 注册表 ----

func (s *Server) toolDefs() []map[string]any {
	return []map[string]any{
		{
			"name":        "list_notes",
			"description": "List all notes in the vault. Returns one line per note: <relative-path> <TAB> <title>. Optional 'folder' filters by path prefix; optional 'limit' caps the count.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"folder": map[string]any{"type": "string", "description": "Only list notes whose relative path starts with this prefix (e.g. 'notes/')."},
					"limit":  map[string]any{"type": "integer", "description": "Maximum number of notes to return (0 = no limit)."},
				},
			},
		},
		{
			"name":        "read_note",
			"description": "Read the full Markdown content of a note by its relative path (e.g. 'notes/foo.md').",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []any{"path"},
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Relative path of the note within the vault."},
				},
			},
		},
		{
			"name":        "search_notes",
			"description": "Full-text search across all notes using NoteVault's BM25 index. Returns ranked results with title, path, match count and a snippet.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []any{"query"},
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search keywords."},
					"limit": map[string]any{"type": "integer", "description": "Maximum number of results (default 20)."},
				},
			},
		},
		{
			"name":        "get_tags",
			"description": "List all tags used in the vault with their occurrence counts, e.g. '#ai (12)'.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "get_backlinks",
			"description": "Given a note (by relative path or title), return all notes that link to it via [[wikilinks]].",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":  map[string]any{"type": "string", "description": "Relative path of the target note."},
					"title": map[string]any{"type": "string", "description": "Title of the target note (used if path is omitted)."},
				},
			},
		},
		{
			"name":        "create_note",
			"description": "Create a new Markdown note. DISABLED unless the server was started with --enable-write. When enabled, the new file is created atomically (fails if it already exists).",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []any{"path"},
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Relative path of the new note, e.g. 'notes/idea.md'."},
					"content": map[string]any{"type": "string", "description": "Initial Markdown content. Defaults to a title heading derived from the filename."},
				},
			},
		},
	}
}

type toolHandler func(args map[string]any) (string, error)

func (s *Server) toolHandlers() map[string]toolHandler {
	return map[string]toolHandler{
		"list_notes":    s.listNotes,
		"read_note":     s.readNote,
		"search_notes":  s.searchNotes,
		"get_tags":      s.getTags,
		"get_backlinks": s.getBacklinks,
		"create_note":   s.createNote,
	}
}

// ---- tool 实现（复用 internal/service） ----

func (s *Server) listNotes(args map[string]any) (string, error) {
	graph, err := s.graphSvc.GetGraph(s.workspacePath)
	if err != nil {
		return "", err
	}
	folder := getString(args, "folder", "")
	limit := getInt(args, "limit", 0)

	var lines []string
	for _, n := range graph.Nodes {
		if !n.Resolved {
			continue // 跳过 [[未解析]] 的虚拟节点
		}
		if folder != "" && !strings.HasPrefix(n.Path, folder) {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s\t%s", n.Path, n.Title))
	}
	if limit > 0 && len(lines) > limit {
		lines = lines[:limit]
	}
	if len(lines) == 0 {
		return "(no notes found)", nil
	}
	return strings.Join(lines, "\n"), nil
}

func (s *Server) readNote(args map[string]any) (string, error) {
	p := getString(args, "path", "")
	if p == "" {
		return "", fmt.Errorf("missing required argument: path")
	}
	return s.fileSvc.ReadFile(s.workspacePath, p)
}

func (s *Server) searchNotes(args map[string]any) (string, error) {
	q := getString(args, "query", "")
	if q == "" {
		return "", fmt.Errorf("missing required argument: query")
	}
	limit := getInt(args, "limit", 20)
	results, err := s.searchSvc.Search(s.workspacePath, q)
	if err != nil {
		return "", err
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	if len(results) == 0 {
		return fmt.Sprintf("No results for %q", q), nil
	}
	var lines []string
	for _, r := range results {
		lines = append(lines, fmt.Sprintf("- %s (%s) — matches: %d\n  %s", r.Title, r.Path, r.MatchCount, r.Snippet))
	}
	return strings.Join(lines, "\n"), nil
}

func (s *Server) getTags(args map[string]any) (string, error) {
	tags, err := s.tagSvc.GetAllTags(s.workspacePath)
	if err != nil {
		return "", err
	}
	if len(tags) == 0 {
		return "(no tags)", nil
	}
	var lines []string
	for _, t := range tags {
		lines = append(lines, fmt.Sprintf("#%s (%d)", t.Name, t.Count))
	}
	return strings.Join(lines, "\n"), nil
}

func (s *Server) getBacklinks(args map[string]any) (string, error) {
	ref := getString(args, "path", "")
	if ref == "" {
		ref = getString(args, "title", "")
	}
	if ref == "" {
		return "", fmt.Errorf("missing required argument: path or title")
	}
	graph, err := s.graphSvc.GetGraph(s.workspacePath)
	if err != nil {
		return "", err
	}
	targetID, err := s.resolveNode(graph, ref)
	if err != nil {
		return "", err
	}
	var lines []string
	seen := map[string]bool{}
	for _, e := range graph.Edges {
		if e.Target != targetID {
			continue
		}
		if seen[e.Source] {
			continue
		}
		n := nodeByID(graph, e.Source)
		if n == nil || !n.Resolved {
			continue
		}
		seen[e.Source] = true
		lines = append(lines, fmt.Sprintf("%s\t%s", n.Path, n.Title))
	}
	if len(lines) == 0 {
		return fmt.Sprintf("No backlinks found for %q", ref), nil
	}
	return strings.Join(lines, "\n"), nil
}

func (s *Server) createNote(args map[string]any) (string, error) {
	if !s.enableWrite {
		return "", fmt.Errorf("write operations are disabled; restart the server with --enable-write to create notes")
	}
	p := getString(args, "path", "")
	if p == "" {
		return "", fmt.Errorf("missing required argument: path")
	}
	content := getString(args, "content", "")
	if content == "" {
		base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		content = "# " + base + "\n\n"
	}
	node, err := s.fileSvc.CreateFile(s.workspacePath, p, content)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Created %s", node.Path), nil
}

// ---- 内部辅助 ----

// resolveNode 把用户输入解析成图谱节点 ID（即相对路径）。
// 匹配优先级：精确 path > 精确 title（大小写不敏感）> 文件名 base。
func (s *Server) resolveNode(graph *service.GraphData, ref string) (string, error) {
	refLower := strings.ToLower(strings.TrimSpace(ref))
	refBase := strings.ToLower(strings.TrimSuffix(filepath.Base(ref), filepath.Ext(ref)))

	for _, n := range graph.Nodes {
		if n.Path == ref {
			return n.ID, nil
		}
	}
	for _, n := range graph.Nodes {
		if strings.ToLower(n.Title) == refLower {
			return n.ID, nil
		}
	}
	for _, n := range graph.Nodes {
		nBase := strings.ToLower(strings.TrimSuffix(filepath.Base(n.Path), filepath.Ext(n.Path)))
		if nBase == refBase {
			return n.ID, nil
		}
	}
	return "", fmt.Errorf("note not found: %q", ref)
}

func nodeByID(graph *service.GraphData, id string) *service.GraphNode {
	for _, n := range graph.Nodes {
		if n.ID == id {
			return n
		}
	}
	return nil
}

func getString(args map[string]any, key, def string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

func getInt(args map[string]any, key string, def int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case string:
			if i, err := strconv.Atoi(n); err == nil {
				return i
			}
		}
	}
	return def
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		// 仅当结构体本身无法序列化时才走到这，理论上不会发生。
		return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error"}}`)
	}
	return b
}
