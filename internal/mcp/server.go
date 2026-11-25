// Package mcp 实现 NoteVault 的 Model Context Protocol (MCP) 服务端。
//
// 设计取舍：
//   - 零第三方依赖：手写 JSON-RPC 2.0 over stdio，不引入任何 MCP SDK，
//     与项目「只依赖 wails/v3 + golang.org/x/sys」的策略保持一致。
//   - 纯 CLI：本包与 cmd/notevault-mcp 只依赖 internal/service（不引 wails），
//     因此编译产物是干净的命令行进程，可被 Claude Code / Codex 等作为子进程拉起。
//   - 默认只读：14 个 tool 里 6 个只读；8 个写工具受 --enable-write 门控，
//     未开开关时调用会明确报错，绝不触碰任何文件（含 .notevault 缓存）。
package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/notevault/notevault/internal/core"
	"github.com/notevault/notevault/internal/service"
)

// 协议常量。ProtocolVersion 取截至实现时的最新稳定版；客户端在 initialize
// 时带来的版本若不为空则原样回显（主流客户端只会发受支持版本）。
const (
	ProtocolVersion = "2025-06-18"
	ServerName      = "notevault-mcp"
)

// ServerVersion 是 initialize 回给客户端的版本号。与客户端二进制同源：
// core.AppVersion 由构建时的 -ldflags -X 注入 tag，因此 MCP 二进制自报的
// 版本与安装包版本不会各说各话（此前这里硬编码 "1.0.0"，发 v3.1.0 时报 1.0.0）。
var ServerVersion = core.AppVersion

// Server 持有一个工作区的所有复用服务实例，并处理 MCP 消息。
type Server struct {
	workspacePath string
	enableWrite   bool

	fileSvc     *service.FileService
	snapshotSvc *service.SnapshotService
	searchSvc   *service.SearchService
	tagSvc      *service.TagService
	todoSvc     *service.TodoService
	graphSvc    *service.GraphService
}

// NewServer 构造一个绑定到 workspacePath 的 MCP 服务端。
// enableWrite=false 时 create_note 一律拒绝。
func NewServer(workspacePath string, enableWrite bool) *Server {
	fs := service.NewFileService()
	return &Server{
		workspacePath: workspacePath,
		enableWrite:   enableWrite,
		fileSvc:       fs,
		snapshotSvc:   service.NewSnapshotService(),
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
			"name":        "append_note",
			"description": "Append content to an existing note (or create it if missing). Optionally insert under a given heading. DISABLED unless --enable-write. A manual snapshot of the original file is taken BEFORE any write, and the snapshot id is returned for undo.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []any{"path", "content"},
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Relative path of the target note."},
					"content": map[string]any{"type": "string", "description": "Markdown content to append."},
					"heading": map[string]any{"type": "string", "description": "Optional heading text; content is appended right under this heading instead of at end of file."},
				},
			},
		},
		{
			"name":        "patch_note",
			"description": "Replace an exact target chunk inside a note with replacement content. DISABLED unless --enable-write. A manual snapshot of the original file is taken BEFORE any write, and the snapshot id is returned for undo.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []any{"path", "target_chunk", "replacement_chunk"},
				"properties": map[string]any{
					"path":              map[string]any{"type": "string", "description": "Relative path of the target note."},
					"target_chunk":      map[string]any{"type": "string", "description": "Exact text to replace (must match uniquely)."},
					"replacement_chunk": map[string]any{"type": "string", "description": "Replacement text."},
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
		{
			"name":        "append_worklog",
			"description": "Append one entry to today's agent work log at Daily/Agent/<YYYY-MM-DD>.md (the file renders on the app's Today page as agent activity). DISABLED unless --enable-write.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []any{"content"},
				"properties": map[string]any{
					"content": map[string]any{"type": "string", "description": "Single-line summary of what was done, e.g. 'fixed the flaky sidebar test'."},
					"agent":   map[string]any{"type": "string", "description": "Optional agent name shown in brackets, e.g. 'claude-code'."},
				},
			},
		},
		{
			"name":        "record_decision",
			"description": "Append a dated decision line under the '## 资料与决策' heading of Projects/<project>/project.md (the heading is created at the end of the file if missing). DISABLED unless --enable-write.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []any{"project", "content"},
				"properties": map[string]any{
					"project": map[string]any{"type": "string", "description": "Project folder name under Projects/, e.g. 'notevault'."},
					"content": map[string]any{"type": "string", "description": "Single-line decision summary."},
				},
			},
		},
		{
			"name":        "update_project_status",
			"description": "Update the status, phase and optionally next_step frontmatter fields of Projects/<project>/project.md. When the project folder exists but project.md is missing, a minimal archive file is created. DISABLED unless --enable-write.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []any{"project", "status"},
				"properties": map[string]any{
					"project":   map[string]any{"type": "string", "description": "Project folder name under Projects/."},
					"status":    map[string]any{"type": "string", "description": "New status value, e.g. '进行中' or '已完成'."},
					"phase":     map[string]any{"type": "string", "description": "Optional phase value, e.g. '调研', '规划', '执行', '发布' or '复盘'."},
					"next_step": map[string]any{"type": "string", "description": "Optional next action, stored as the nextStep frontmatter field."},
				},
			},
		},
		{
			"name":        "get_project_context",
			"description": "Read the full working context of one project in a single call: goals, phase, core question, background, next step, open tasks, deliverables, recent decisions and retrospective path. Use this before planning the next step instead of reading files one by one.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []any{"project"},
				"properties": map[string]any{
					"project": map[string]any{"type": "string", "description": "Project name or folder name under Projects/."},
				},
			},
		},
		{
			"name":        "register_deliverable",
			"description": "Create or update a deliverable under Projects/<project>/Deliverables/ with type, stage and version metadata, so the project page can group it by draft/review/final. DISABLED unless --enable-write.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []any{"project", "title"},
				"properties": map[string]any{
					"project": map[string]any{"type": "string", "description": "Project folder name under Projects/."},
					"title":   map[string]any{"type": "string", "description": "Deliverable title; used as the file name when creating a new one."},
					"type":    map[string]any{"type": "string", "description": "Deliverable type: article | table | ppt | report | image | demo | other. Defaults to article."},
					"stage":   map[string]any{"type": "string", "description": "Delivery stage: draft | review | final. Defaults to draft."},
					"version": map[string]any{"type": "string", "description": "Optional version label, e.g. v2."},
					"content": map[string]any{"type": "string", "description": "Optional Markdown body written after the title when the deliverable file is created."},
				},
			},
		},
		{
			"name":        "append_retro",
			"description": "Append one dated line to Projects/<project>/复盘.md under '## 复盘记录', creating the retrospective with a standard skeleton when missing. DISABLED unless --enable-write.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []any{"project", "content"},
				"properties": map[string]any{
					"project": map[string]any{"type": "string", "description": "Project folder name under Projects/."},
					"content": map[string]any{"type": "string", "description": "Single-line retrospective note (result, lesson or next improvement)."},
				},
			},
		},
	}
}

type toolHandler func(args map[string]any) (string, error)

func (s *Server) toolHandlers() map[string]toolHandler {
	return map[string]toolHandler{
		"list_notes":            s.listNotes,
		"read_note":             s.readNote,
		"search_notes":          s.searchNotes,
		"get_tags":              s.getTags,
		"get_backlinks":         s.getBacklinks,
		"create_note":           s.createNote,
		"append_note":           s.appendNote,
		"patch_note":            s.patchNote,
		"append_worklog":        s.appendWorklog,
		"record_decision":       s.recordDecision,
		"update_project_status": s.updateProjectStatus,
		"get_project_context":   s.getProjectContext,
		"register_deliverable":  s.registerDeliverable,
		"append_retro":          s.appendRetro,
	}
}

// requireWrite 是所有写工具的统一门控：--enable-write 未开启时明确拒绝。
func (s *Server) requireWrite(action string) error {
	if !s.enableWrite {
		return fmt.Errorf("write operations are disabled; restart the server with --enable-write to %s", action)
	}
	return nil
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
	if err := s.requireWrite("create notes"); err != nil {
		return "", err
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

// snapshotBeforeWrite 写操作前置留底（蓝图专项 3 / 红线 3）：
// 原文件存在时打手动快照，返回 snapshot id 供回滚；文件不存在（新建）跳过。
func (s *Server) snapshotBeforeWrite(relPath string) (string, error) {
	if s.snapshotSvc == nil {
		return "", nil
	}
	// 先探测文件是否存在：不存在（新建场景）无需留底，
	// 也不应把 NOT_FOUND 当错误抛给调用方
	if _, err := s.fileSvc.ReadFile(s.workspacePath, relPath); err != nil {
		return "", nil
	}
	snap, err := s.snapshotSvc.CreateManualSnapshot(s.workspacePath, relPath)
	if err != nil {
		return "", err
	}
	if snap == nil {
		return "", nil
	}
	return snap.ID, nil
}

// appendNote 追加内容：可选 heading 定位，文件不存在自动创建（写前快照）
func (s *Server) appendNote(args map[string]any) (string, error) {
	if err := s.requireWrite("append notes"); err != nil {
		return "", err
	}
	p := getString(args, "path", "")
	content := getString(args, "content", "")
	heading := getString(args, "heading", "")
	if p == "" || content == "" {
		return "", fmt.Errorf("missing required arguments: path, content")
	}

	snapshotID, err := s.snapshotBeforeWrite(p)
	if err != nil {
		return "", fmt.Errorf("pre-write snapshot failed: %w", err)
	}

	full, err := s.fileSvc.ReadFile(s.workspacePath, p)
	if err != nil {
		// 文件不存在 → 自动创建（首快照无可留底）
		if _, cerr := s.fileSvc.CreateFile(s.workspacePath, p, content); cerr != nil {
			return "", cerr
		}
		return fmt.Sprintf("Created %s (new file, no prior snapshot)", p), nil
	}

	var updated string
	if heading != "" {
		updated, err = appendUnderHeading(full, heading, content)
		if err != nil {
			return "", err
		}
	} else {
		updated = strings.TrimRight(full, "\n") + "\n\n" + strings.TrimRight(content, "\n") + "\n"
	}
	if err := s.fileSvc.SaveFile(s.workspacePath, p, updated); err != nil {
		return "", err
	}
	if snapshotID != "" {
		return fmt.Sprintf("Appended to %s (snapshot: %s)", p, snapshotID), nil
	}
	return fmt.Sprintf("Appended to %s", p), nil
}

// appendUnderHeading 在指定标题（任意级别）之下、下一个标题之前插入内容
func appendUnderHeading(doc, heading, content string) (string, error) {
	lines := strings.Split(doc, "\n")
	var headingLine = -1
	var insertAt = -1
	for i, line := range lines {
		if insertAt < 0 && strings.HasPrefix(strings.TrimSpace(line), "#") {
			if strings.Contains(strings.TrimLeft(strings.TrimSpace(line), "#"), heading) {
				headingLine = i
				insertAt = i + 1
				continue
			}
		}
		if headingLine >= 0 {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				insertAt = i
				break
			}
		}
	}
	if headingLine < 0 {
		return "", fmt.Errorf("heading %q not found in %s", heading, "note")
	}
	block := strings.Split(strings.TrimRight(content, "\n"), "\n")
	out := make([]string, 0, len(lines)+len(block)+1)
	out = append(out, lines[:insertAt]...)
	out = append(out, block...)
	out = append(out, "")
	out = append(out, lines[insertAt:]...)
	return strings.Join(out, "\n"), nil
}

// patchNote 精准替换：target_chunk 必须唯一匹配，避免误伤
func (s *Server) patchNote(args map[string]any) (string, error) {
	if err := s.requireWrite("patch notes"); err != nil {
		return "", err
	}
	p := getString(args, "path", "")
	target := getString(args, "target_chunk", "")
	replacement := getString(args, "replacement_chunk", "")
	if p == "" || target == "" {
		return "", fmt.Errorf("missing required arguments: path, target_chunk")
	}

	snapshotID, err := s.snapshotBeforeWrite(p)
	if err != nil {
		return "", fmt.Errorf("pre-write snapshot failed: %w", err)
	}

	full, err := s.fileSvc.ReadFile(s.workspacePath, p)
	if err != nil {
		return "", err
	}
	count := strings.Count(full, target)
	if count == 0 {
		return "", fmt.Errorf("target chunk not found in %s", p)
	}
	if count > 1 {
		return "", fmt.Errorf("target chunk matches %d times in %s; provide a longer, unique chunk", count, p)
	}
	updated := strings.Replace(full, target, replacement, 1)
	if err := s.fileSvc.SaveFile(s.workspacePath, p, updated); err != nil {
		return "", err
	}
	if snapshotID != "" {
		return fmt.Sprintf("Patched %s (snapshot: %s)", p, snapshotID), nil
	}
	return fmt.Sprintf("Patched %s", p), nil
}

// appendWorklog 追加一条 Agent 工作日志：落 Daily/Agent/<当日>.md，
// 行格式 `- 15:04 [agent] 内容`（agent 省略则无方括号段）。
// 与人工日报（Daily/Reports/…-日报.md）完全隔离，互不覆盖。
func (s *Server) appendWorklog(args map[string]any) (string, error) {
	if err := s.requireWrite("append the agent work log"); err != nil {
		return "", err
	}
	content, err := singleLine(getString(args, "content", ""))
	if err != nil {
		return "", err
	}
	agent := strings.TrimSpace(getString(args, "agent", ""))
	if strings.ContainsAny(agent, "\r\n\x00") {
		return "", fmt.Errorf("agent must be a single line")
	}

	now := time.Now()
	date := now.Format("2006-01-02")
	rel := "Daily/Agent/" + date + ".md"
	line := "- " + now.Format("15:04")
	if agent != "" {
		line += " [" + agent + "]"
	}
	line += " " + content

	snapshotID, err := s.snapshotBeforeWrite(rel)
	if err != nil {
		return "", fmt.Errorf("pre-write snapshot failed: %w", err)
	}
	full, err := s.fileSvc.ReadFile(s.workspacePath, rel)
	if err != nil {
		// 当日日志尚不存在：建文件并写入标题 + 首条
		created := fmt.Sprintf("# Agent 工作日志 %s\n\n%s\n", date, line)
		if _, cerr := s.fileSvc.CreateFile(s.workspacePath, rel, created); cerr != nil {
			return "", cerr
		}
		return fmt.Sprintf("Created %s (new file, no prior snapshot)", rel), nil
	}
	updated := strings.TrimRight(full, "\n") + "\n" + line + "\n"
	if err := s.fileSvc.SaveFile(s.workspacePath, rel, updated); err != nil {
		return "", err
	}
	if snapshotID != "" {
		return fmt.Sprintf("Appended to %s (snapshot: %s)", rel, snapshotID), nil
	}
	return fmt.Sprintf("Appended to %s", rel), nil
}

// recordDecision 把决策记录追加到 Projects/<project>/project.md 的
// 「## 资料与决策」章节下（标题缺失时在文末补建），行前缀日期。
func (s *Server) recordDecision(args map[string]any) (string, error) {
	if err := s.requireWrite("record decisions"); err != nil {
		return "", err
	}
	content, err := singleLine(getString(args, "content", ""))
	if err != nil {
		return "", err
	}
	rel, err := projectNoteRel(getString(args, "project", ""))
	if err != nil {
		return "", err
	}

	snapshotID, err := s.snapshotBeforeWrite(rel)
	if err != nil {
		return "", fmt.Errorf("pre-write snapshot failed: %w", err)
	}
	full, err := s.fileSvc.ReadFile(s.workspacePath, rel)
	if err != nil {
		return "", fmt.Errorf("project not found: %s (project.md does not exist)", rel)
	}
	line := "- " + time.Now().Format("2006-01-02") + " · " + content
	updated, err := appendUnderSectionEnd(full, "资料与决策", line)
	if err != nil {
		return "", err
	}
	if err := s.fileSvc.SaveFile(s.workspacePath, rel, updated); err != nil {
		return "", err
	}
	if snapshotID != "" {
		return fmt.Sprintf("Recorded decision in %s (snapshot: %s)", rel, snapshotID), nil
	}
	return fmt.Sprintf("Recorded decision in %s", rel), nil
}

// updateProjectStatus 补丁式更新 project.md frontmatter 的 status / nextStep；
// 项目目录存在但 project.md 缺失时创建最小档案，目录不存在时报错。
func (s *Server) updateProjectStatus(args map[string]any) (string, error) {
	if err := s.requireWrite("update project status"); err != nil {
		return "", err
	}
	project := getString(args, "project", "")
	rel, err := projectNoteRel(project)
	if err != nil {
		return "", err
	}
	status, err := singleLine(getString(args, "status", ""))
	if err != nil {
		return "", err
	}
	nextStep := strings.TrimSpace(getString(args, "next_step", ""))
	if nextStep != "" {
		if nextStep, err = singleLine(nextStep); err != nil {
			return "", err
		}
	}
	phase := strings.TrimSpace(getString(args, "phase", ""))
	if phase != "" {
		if phase, err = singleLine(phase); err != nil {
			return "", err
		}
	}

	snapshotID, err := s.snapshotBeforeWrite(rel)
	if err != nil {
		return "", fmt.Errorf("pre-write snapshot failed: %w", err)
	}
	full, err := s.fileSvc.ReadFile(s.workspacePath, rel)
	if err != nil {
		// 区分「目录存在但档案缺失」与「项目不存在」
		dir := filepath.Join(s.workspacePath, "Projects", project)
		if info, serr := os.Stat(dir); serr != nil || !info.IsDir() {
			return "", fmt.Errorf("project not found: Projects/%s", project)
		}
		created := "---\nname: " + project + "\nstatus: " + status + "\n"
		if phase != "" {
			created += "phase: " + phase + "\n"
		}
		if nextStep != "" {
			created += "nextStep: " + nextStep + "\n"
		}
		created += "---\n\n"
		if _, cerr := s.fileSvc.CreateFile(s.workspacePath, rel, created); cerr != nil {
			return "", cerr
		}
		return fmt.Sprintf("Created %s (new file, no prior snapshot): status=%s", rel, status), nil
	}
	updated, err := setFrontmatterField(full, "status", []string{"status", "状态"}, status)
	if err != nil {
		return "", err
	}
	if nextStep != "" {
		updated, err = setFrontmatterField(updated, "nextStep", []string{"nextStep", "next_step", "下一步", "下一步行动"}, nextStep)
		if err != nil {
			return "", err
		}
	}
	if phase != "" {
		updated, err = setFrontmatterField(updated, "phase", []string{"phase", "阶段", "当前阶段"}, phase)
		if err != nil {
			return "", err
		}
	}
	if err := s.fileSvc.SaveFile(s.workspacePath, rel, updated); err != nil {
		return "", err
	}
	result := fmt.Sprintf("Updated %s: status=%s", rel, status)
	if phase != "" {
		result += fmt.Sprintf(", phase=%s", phase)
	}
	if nextStep != "" {
		result += fmt.Sprintf(", nextStep=%s", nextStep)
	}
	if snapshotID != "" {
		result += fmt.Sprintf(" (snapshot: %s)", snapshotID)
	}
	return result, nil
}

// getProjectContext 一次性返回项目的完整工作现场：上下文、未完成任务、交付物、
// 最近决策与复盘路径。AI 规划下一步时应优先调用它，避免逐个读文件丢上下文。
func (s *Server) getProjectContext(args map[string]any) (string, error) {
	project := strings.TrimSpace(getString(args, "project", ""))
	if project == "" {
		return "", fmt.Errorf("missing required argument: project")
	}
	snapshot, err := s.todoSvc.GetWorkbench(s.workspacePath, time.Now().Format("2006-01-02"))
	if err != nil {
		return "", err
	}
	var found *service.WorkbenchProject
	for i := range snapshot.Projects {
		candidate := &snapshot.Projects[i]
		folderName := strings.TrimPrefix(candidate.Folder, "Projects/")
		if strings.EqualFold(candidate.Name, project) ||
			strings.EqualFold(folderName, project) ||
			strings.EqualFold(candidate.Folder, "Projects/"+project) ||
			strings.EqualFold(candidate.Path, project) {
			found = candidate
			break
		}
	}
	if found == nil {
		return "", fmt.Errorf("project not found: %s", project)
	}
	openTasks := make([]map[string]any, 0)
	for _, task := range snapshot.Tasks {
		if task.Completed {
			continue
		}
		if task.Project != found.Name && !strings.HasPrefix(task.FilePath, found.Folder+"/") {
			continue
		}
		openTasks = append(openTasks, map[string]any{
			"title": task.Title, "type": task.Type, "priority": task.Priority,
			"due": task.Due, "status": task.Status, "blocker": task.Blocker,
		})
	}
	deliverables := found.Deliverables
	if deliverables == nil {
		deliverables = []service.WorkbenchDeliverable{}
	}
	decisions := []string{}
	if doc, readErr := s.fileSvc.ReadFile(s.workspacePath, found.Path); readErr == nil {
		decisions = projectDecisionLines(doc, 8)
	}
	payload := map[string]any{
		"project": map[string]any{
			"name": found.Name, "path": found.Path, "status": found.Status,
			"phase": found.Phase, "coreQuestion": found.CoreQuestion, "background": found.Background,
			"nextStep": found.NextStep, "techStack": found.TechStack,
			"retrospective": found.Retrospective,
		},
		"openTasks":       openTasks,
		"deliverables":    deliverables,
		"recentDecisions": decisions,
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// registerDeliverable 在 Projects/<project>/Deliverables/ 下登记交付物：
// 文件已存在时只更新类型/阶段/版本，不存在时按骨架创建。
func (s *Server) registerDeliverable(args map[string]any) (string, error) {
	if err := s.requireWrite("register deliverables"); err != nil {
		return "", err
	}
	project := strings.TrimSpace(getString(args, "project", ""))
	if _, err := projectNoteRel(project); err != nil {
		return "", err
	}
	title, err := singleLine(getString(args, "title", ""))
	if err != nil {
		return "", fmt.Errorf("title: %w", err)
	}
	kind := strings.ToLower(strings.TrimSpace(getString(args, "type", "article")))
	if kind == "" {
		kind = "article"
	}
	stage := strings.ToLower(strings.TrimSpace(getString(args, "stage", "draft")))
	if stage == "" {
		stage = "draft"
	}
	version := strings.TrimSpace(getString(args, "version", ""))
	if version != "" {
		if version, err = singleLine(version); err != nil {
			return "", err
		}
	}
	body := strings.TrimSpace(getString(args, "content", ""))

	rel := "Projects/" + project + "/Deliverables/" + safeDeliverableName(title) + ".md"
	snapshotID, err := s.snapshotBeforeWrite(rel)
	if err != nil {
		return "", fmt.Errorf("pre-write snapshot failed: %w", err)
	}
	if full, readErr := s.fileSvc.ReadFile(s.workspacePath, rel); readErr == nil {
		updated, setErr := setFrontmatterField(full, "deliverableType", []string{"deliverableType", "deliverable_type", "类型", "交付物类型"}, kind)
		if setErr != nil {
			return "", setErr
		}
		if updated, setErr = setFrontmatterField(updated, "stage", []string{"stage", "阶段", "交付阶段"}, stage); setErr != nil {
			return "", setErr
		}
		if version != "" {
			if updated, setErr = setFrontmatterField(updated, "version", []string{"version", "版本"}, version); setErr != nil {
				return "", setErr
			}
		}
		if err := s.fileSvc.SaveFile(s.workspacePath, rel, updated); err != nil {
			return "", err
		}
		return fmt.Sprintf("Updated deliverable %s: type=%s stage=%s (snapshot: %s)", rel, kind, stage, snapshotID), nil
	}

	created := "---\ndeliverableType: " + kind + "\nstage: " + stage + "\n"
	if version != "" {
		created += "version: " + version + "\n"
	}
	created += "tags: [交付物]\n---\n\n# " + title + "\n\n"
	if body != "" {
		created += body + "\n"
	}
	if _, err := s.fileSvc.CreateFile(s.workspacePath, rel, created); err != nil {
		return "", err
	}
	return fmt.Sprintf("Created deliverable %s: type=%s stage=%s", rel, kind, stage), nil
}

// appendRetro 把一条复盘记录追加到 Projects/<project>/复盘.md，
// 文件缺失时先按标准骨架创建。
func (s *Server) appendRetro(args map[string]any) (string, error) {
	if err := s.requireWrite("append retrospective"); err != nil {
		return "", err
	}
	project := strings.TrimSpace(getString(args, "project", ""))
	if _, err := projectNoteRel(project); err != nil {
		return "", err
	}
	content, err := singleLine(getString(args, "content", ""))
	if err != nil {
		return "", err
	}
	rel := "Projects/" + project + "/复盘.md"
	snapshotID, err := s.snapshotBeforeWrite(rel)
	if err != nil {
		return "", fmt.Errorf("pre-write snapshot failed: %w", err)
	}
	full, readErr := s.fileSvc.ReadFile(s.workspacePath, rel)
	if readErr != nil {
		skeleton := "---\ntags: [复盘]\n---\n\n# " + project + " · 复盘\n\n## 复盘记录\n"
		if _, createErr := s.fileSvc.CreateFile(s.workspacePath, rel, skeleton); createErr != nil {
			return "", fmt.Errorf("project not found or retrospective cannot be created: %w", createErr)
		}
		full = skeleton
	}
	line := "- " + time.Now().Format("2006-01-02") + " · " + content
	updated, err := appendUnderSectionEnd(full, "复盘记录", line)
	if err != nil {
		return "", err
	}
	if err := s.fileSvc.SaveFile(s.workspacePath, rel, updated); err != nil {
		return "", err
	}
	return fmt.Sprintf("Appended retrospective to %s (snapshot: %s)", rel, snapshotID), nil
}

// ---- 内部辅助 ----

// singleLine 校验单行内容：拒绝换行/空字节与空串（写工作日志、决策、状态字段都用单行）。
func singleLine(value string) (string, error) {
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("content must be a single line; call the tool once per line")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("content must not be empty")
	}
	return value, nil
}

// projectNoteRel 校验项目名为单段路径，返回其 project.md 的相对路径。
func projectNoteRel(project string) (string, error) {
	project = strings.TrimSpace(strings.Trim(project, "/"))
	if project == "" || project == "." || project == ".." ||
		strings.ContainsAny(project, "/\\:") || strings.ContainsAny(project, "\r\n\x00") {
		return "", fmt.Errorf("invalid project name %q; expected a single folder name under Projects/", project)
	}
	return "Projects/" + project + "/project.md", nil
}

// appendUnderSectionEnd 把一行追加到指定章节内容的末尾（下一个标题之前、
// 跳过章节尾部空行），保持章节内按时间正序；标题不存在时在文末补建「## <heading>」。
func appendUnderSectionEnd(doc, heading, line string) (string, error) {
	lines := strings.Split(doc, "\n")
	headingIdx := -1
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "#") && strings.Contains(strings.TrimLeft(trimmed, "#"), heading) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		trimmedDoc := strings.TrimRight(doc, "\n")
		if trimmedDoc == "" {
			return "# " + heading + "\n\n" + line + "\n", nil
		}
		return trimmedDoc + "\n\n## " + heading + "\n\n" + line + "\n", nil
	}
	end := len(lines)
	for i := headingIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			end = i
			break
		}
	}
	insertAt := end
	for insertAt > headingIdx+1 && strings.TrimSpace(lines[insertAt-1]) == "" {
		insertAt--
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:insertAt]...)
	out = append(out, line)
	out = append(out, lines[insertAt:]...)
	return strings.Join(out, "\n"), nil
}

// setFrontmatterField 在 YAML frontmatter 中更新（或插入）一个键值：
// 键匹配大小写不敏感并兼容别名；文件没有 frontmatter 时在文件头创建一个。
func setFrontmatterField(doc, canonical string, aliases []string, value string) (string, error) {
	lines := strings.Split(doc, "\n")
	if strings.TrimSpace(lines[0]) != "---" {
		block := []string{"---", canonical + ": " + value, "---", ""}
		return strings.Join(append(block, lines...), "\n"), nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", fmt.Errorf("malformed frontmatter in project.md (missing closing ---)")
	}
	for i := 1; i < end; i++ {
		trimmed := strings.TrimSpace(lines[i])
		colon := strings.Index(trimmed, ":")
		if colon <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(trimmed[:colon]))
		for _, alias := range aliases {
			if key == strings.ToLower(alias) {
				lines[i] = trimmed[:colon] + ": " + value
				return strings.Join(lines, "\n"), nil
			}
		}
	}
	// 字段缺失：插入到 frontmatter 末尾（闭合 --- 之前）
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:end]...)
	out = append(out, canonical+": "+value)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n"), nil
}

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

// safeDeliverableName 把交付物标题转成安全文件名：去掉路径分隔符与
// Windows 非法字符，限制长度，避免越界写入或创建失败。
func safeDeliverableName(title string) string {
	replacer := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", "*", "-", "?", "-",
		"\"", "", "<", "", ">", "", "|", "-",
	)
	name := strings.Trim(replacer.Replace(strings.TrimSpace(title)), ". ")
	runes := []rune(name)
	if len(runes) > 80 {
		name = string(runes[:80])
	}
	if name == "" {
		name = "deliverable"
	}
	return name
}

// projectDecisionLines 抽取 project.md 中「## 资料与决策」章节的项目符号行，
// limit>0 时只保留最近的若干条。
func projectDecisionLines(doc string, limit int) []string {
	lines := strings.Split(doc, "\n")
	inSection := false
	out := make([]string, 0)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			inSection = strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")) == "资料与决策"
			continue
		}
		if inSection && strings.HasPrefix(trimmed, "- ") {
			out = append(out, strings.TrimPrefix(trimmed, "- "))
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}
