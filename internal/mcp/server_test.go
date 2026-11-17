package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestVault 建一个临时 vault：alpha 链接到 Beta，两者都带 ai 标签。
func newTestVault(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes")
	if err := os.MkdirAll(notes, 0750); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"notes/alpha.md": "---\ntitle: Alpha\ntags: [project, ai]\n---\n# Alpha\n\nLinks to [[Beta]].\n\n- [ ] do thing\n",
		"notes/beta.md":  "---\ntitle: Beta\ntags: [ai]\n---\n# Beta\n\nKeyword zebra somewhere special.\n",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// rpcRaw 发一条带 id 的请求，返回原始响应字节与是否应有响应。
func rpcRaw(s *Server, method string, params any) ([]byte, bool) {
	raw, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	return s.Process(raw)
}

// rpc 发请求并解析成 map。
func rpc(s *Server, method string, params any) map[string]any {
	b, has := rpcRaw(s, method, params)
	if !has {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

// toolText 调用 tools/call 并取出返回文本。
func toolText(t *testing.T, s *Server, name string, args map[string]any) string {
	t.Helper()
	resp := rpc(s, "tools/call", map[string]any{"name": name, "arguments": args})
	if resp == nil {
		t.Fatalf("tool %s: no response", name)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tool %s: missing result (resp=%v)", name, resp)
	}
	content, ok := res["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("tool %s: empty content", name)
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("tool %s: bad content item", name)
	}
	text, _ := first["text"].(string)
	return text
}

func TestInitialize(t *testing.T) {
	s := NewServer(newTestVault(t), false)
	resp := rpc(s, "initialize", map[string]any{"protocolVersion": "2025-06-18"})

	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize: missing result: %v", resp)
	}
	if res["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v, want 2025-06-18", res["protocolVersion"])
	}
	info, ok := res["serverInfo"].(map[string]any)
	if !ok || info["name"] != ServerName {
		t.Errorf("serverInfo.name = %v, want %s", info["name"], ServerName)
	}
	caps, ok := res["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities missing")
	}
	if _, ok := caps["tools"]; !ok {
		t.Errorf("capabilities.tools missing")
	}
}

func TestToolsListHasFourteen(t *testing.T) {
	s := NewServer(newTestVault(t), false)
	resp := rpc(s, "tools/list", nil)
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list: missing result: %v", resp)
	}
	tools, ok := res["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list: tools not array")
	}
	if len(tools) != 14 {
		t.Errorf("tool count = %d, want 14", len(tools))
	}
	want := map[string]bool{
		"list_notes": true, "read_note": true, "search_notes": true,
		"get_tags": true, "get_backlinks": true, "create_note": true,
		"append_note": true, "patch_note": true,
		"append_worklog": true, "record_decision": true, "update_project_status": true,
		"get_project_context": true, "register_deliverable": true, "append_retro": true,
	}
	for _, td := range tools {
		m, ok := td.(map[string]any)
		if !ok {
			t.Fatalf("tool def not object: %v", td)
		}
		name, _ := m["name"].(string)
		if !want[name] {
			t.Errorf("unexpected tool in list: %s", name)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Errorf("missing tools: %v", want)
	}
}

func TestListNotes(t *testing.T) {
	s := NewServer(newTestVault(t), false)
	text := toolText(t, s, "list_notes", map[string]any{})
	if !strings.Contains(text, "notes/alpha.md") || !strings.Contains(text, "notes/beta.md") {
		t.Errorf("list_notes missing entries:\n%s", text)
	}
}

func TestReadNote(t *testing.T) {
	s := NewServer(newTestVault(t), false)
	text := toolText(t, s, "read_note", map[string]any{"path": "notes/alpha.md"})
	if !strings.Contains(text, "Links to [[Beta]]") {
		t.Errorf("read_note content unexpected:\n%s", text)
	}
	// 缺参数应走 isError。
	resp := rpc(s, "tools/call", map[string]any{"name": "read_note", "arguments": map[string]any{}})
	res := resp["result"].(map[string]any)
	if res["isError"] != true {
		t.Errorf("read_note without path should be isError=true")
	}
}

func TestSearchNotes(t *testing.T) {
	s := NewServer(newTestVault(t), false)
	text := toolText(t, s, "search_notes", map[string]any{"query": "zebra"})
	if !strings.Contains(text, "notes/beta.md") {
		t.Errorf("search_notes(zebra) should find beta.md:\n%s", text)
	}
}

func TestGetTags(t *testing.T) {
	s := NewServer(newTestVault(t), false)
	text := toolText(t, s, "get_tags", map[string]any{})
	if !strings.Contains(text, "#ai (2)") {
		t.Errorf("get_tags should report #ai (2):\n%s", text)
	}
}

func TestGetBacklinks(t *testing.T) {
	s := NewServer(newTestVault(t), false)
	// 用 title 解析目标。
	text := toolText(t, s, "get_backlinks", map[string]any{"title": "Beta"})
	if !strings.Contains(text, "notes/alpha.md") {
		t.Errorf("get_backlinks(Beta) should include alpha.md:\n%s", text)
	}
	// 用 path 解析目标。
	text = toolText(t, s, "get_backlinks", map[string]any{"path": "notes/beta.md"})
	if !strings.Contains(text, "notes/alpha.md") {
		t.Errorf("get_backlinks(notes/beta.md) should include alpha.md:\n%s", text)
	}
}

func TestCreateNoteDisabledByDefault(t *testing.T) {
	s := NewServer(newTestVault(t), false)
	resp := rpc(s, "tools/call", map[string]any{
		"name":      "create_note",
		"arguments": map[string]any{"path": "notes/new.md", "content": "hi"},
	})
	res := resp["result"].(map[string]any)
	if res["isError"] != true {
		t.Fatalf("create_note must be blocked when --enable-write is off")
	}
	text, _ := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "disabled") {
		t.Errorf("blocked message should mention disabled: %s", text)
	}
}

func TestCreateNoteEnabled(t *testing.T) {
	dir := newTestVault(t)
	s := NewServer(dir, true)
	text := toolText(t, s, "create_note", map[string]any{"path": "notes/new.md", "content": "# New\n"})
	if !strings.Contains(text, "Created notes/new.md") {
		t.Errorf("create_note enabled should report created: %s", text)
	}
	// 落盘验证。
	data, err := os.ReadFile(filepath.Join(dir, "notes/new.md"))
	if err != nil {
		t.Fatalf("created file not on disk: %v", err)
	}
	if !strings.Contains(string(data), "# New") {
		t.Errorf("created file content wrong: %s", data)
	}
}

func TestUnknownToolReturnsRPCError(t *testing.T) {
	s := NewServer(newTestVault(t), false)
	resp := rpc(s, "tools/call", map[string]any{"name": "no_such_tool", "arguments": map[string]any{}})
	if _, ok := resp["error"]; !ok {
		t.Errorf("unknown tool should return JSON-RPC error: %v", resp)
	}
}

func TestNotificationNoResponse(t *testing.T) {
	s := NewServer(newTestVault(t), false)
	raw, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	_, has := s.Process(raw)
	if has {
		t.Errorf("notification should not produce a response")
	}
}

// ---------------------------------------------------------------------------
// 蓝图专项 3：append_note / patch_note（写前快照留底）
// ---------------------------------------------------------------------------

func TestAppendNote_BasicAndSnapshot(t *testing.T) {
	ws := newTestVault(t)
	s := NewServer(ws, true)
	resp := rpc(s, "tools/call", map[string]any{
		"name": "append_note",
		"arguments": map[string]any{
			"path":    "notes/alpha.md",
			"content": "追加的一行",
		},
	})
	if _, has := resp["error"]; has {
		t.Fatalf("append_note failed: %v", resp["error"])
	}
	res := resp["result"].(map[string]any)
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "snapshot:") {
		t.Errorf("响应应携带快照 id 供回滚: %s", text)
	}
	data, err := os.ReadFile(filepath.Join(ws, "notes", "alpha.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "追加的一行") {
		t.Errorf("内容未追加: %s", data)
	}
}

func TestAppendNote_UnderHeading(t *testing.T) {
	ws := newTestVault(t)
	s := NewServer(ws, true)
	resp := rpc(s, "tools/call", map[string]any{
		"name": "append_note",
		"arguments": map[string]any{
			"path":    "notes/alpha.md",
			"content": "标题下的新段落",
			"heading": "Alpha",
		},
	})
	if _, has := resp["error"]; has {
		t.Fatalf("append_note failed: %v", resp["error"])
	}
	data, err := os.ReadFile(filepath.Join(ws, "notes", "alpha.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	// 应插在「Alpha」标题之下、下一个标题之前
	if !strings.Contains(text, "# Alpha\n标题下的新段落") {
		t.Errorf("应插在指定标题之下:\n%s", text)
	}
}

func TestAppendNote_NewFileCreated(t *testing.T) {
	ws := newTestVault(t)
	s := NewServer(ws, true)
	resp := rpc(s, "tools/call", map[string]any{
		"name": "append_note",
		"arguments": map[string]any{
			"path":    "notes/新笔记.md",
			"content": "首段内容",
		},
	})
	if _, has := resp["error"]; has {
		t.Fatalf("append_note failed: %v", resp["error"])
	}
	res := resp["result"].(map[string]any)
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Created") {
		t.Errorf("新文件应返回 Created: %s", text)
	}
}

func TestAppendNote_WriteDisabled(t *testing.T) {
	s := NewServer(newTestVault(t), false)
	resp := rpc(s, "tools/call", map[string]any{
		"name":      "append_note",
		"arguments": map[string]any{"path": "a.md", "content": "x"},
	})
	// 工具错误走 MCP isError 约定（result.isError + 文本），不是 JSON-RPC error
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("应返回 result: %v", resp)
	}
	if isError, _ := res["isError"].(bool); !isError {
		t.Errorf("写禁用时 isError 应为 true: %v", res)
	}
}

func TestPatchNote_UniqueMatchReplaces(t *testing.T) {
	ws := newTestVault(t)
	s := NewServer(ws, true)
	resp := rpc(s, "tools/call", map[string]any{
		"name": "patch_note",
		"arguments": map[string]any{
			"path":              "notes/beta.md",
			"target_chunk":      "Keyword zebra somewhere special.",
			"replacement_chunk": "Keyword zebra patched.",
		},
	})
	if _, has := resp["error"]; has {
		t.Fatalf("patch_note failed: %v", resp["error"])
	}
	data, _ := os.ReadFile(filepath.Join(ws, "notes", "beta.md"))
	if !strings.Contains(string(data), "Keyword zebra patched.") {
		t.Errorf("替换未生效: %s", data)
	}
}

func TestPatchNote_AmbiguousTargetRejected(t *testing.T) {
	ws := newTestVault(t)
	s := NewServer(ws, true)
	// 先注入一个重复片段
	rpc(s, "tools/call", map[string]any{
		"name": "append_note",
		"arguments": map[string]any{
			"path":    "notes/beta.md",
			"content": "重复片段",
		},
	})
	// 追加两次制造两处相同片段
	for i := 0; i < 2; i++ {
		rpc(s, "tools/call", map[string]any{
			"name": "append_note",
			"arguments": map[string]any{
				"path":    "notes/beta.md",
				"content": "重复片段",
			},
		})
	}
	resp := rpc(s, "tools/call", map[string]any{
		"name": "patch_note",
		"arguments": map[string]any{
			"path":              "notes/beta.md",
			"target_chunk":      "重复片段",
			"replacement_chunk": "x",
		},
	})
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("应返回 result: %v", resp)
	}
	if isError, _ := res["isError"].(bool); !isError {
		t.Errorf("多目标匹配应拒绝: %v", res)
	}
}

func TestPatchNote_TargetNotFound(t *testing.T) {
	s := NewServer(newTestVault(t), true)
	resp := rpc(s, "tools/call", map[string]any{
		"name": "patch_note",
		"arguments": map[string]any{
			"path":              "notes/beta.md",
			"target_chunk":      "不存在的片段",
			"replacement_chunk": "x",
		},
	})
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("应返回 result: %v", resp)
	}
	if isError, _ := res["isError"].(bool); !isError {
		t.Errorf("目标不存在应 isError: %v", res)
	}
}

// ---------------------------------------------------------------------------
// P1：结构化写入工具（append_worklog / record_decision / update_project_status）
// ---------------------------------------------------------------------------

// addProject 在 vault 里放一个项目档案，返回 project.md 原内容。
func addProject(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "Projects", name), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Projects", name, "project.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestAppendWorklog_DisabledByDefault(t *testing.T) {
	s := NewServer(newTestVault(t), false)
	resp := rpc(s, "tools/call", map[string]any{
		"name":      "append_worklog",
		"arguments": map[string]any{"content": "x"},
	})
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("应返回 result: %v", resp)
	}
	if isError, _ := res["isError"].(bool); !isError {
		t.Errorf("写禁用时 isError 应为 true: %v", res)
	}
}

func TestAppendWorklog_CreatesDailyFile(t *testing.T) {
	ws := newTestVault(t)
	s := NewServer(ws, true)
	today := time.Now().Format("2006-01-02")
	rel := filepath.Join("Daily", "Agent", today+".md")

	text := toolText(t, s, "append_worklog", map[string]any{"content": "修复导航测试", "agent": "claude-code"})
	if !strings.Contains(text, "Created") {
		t.Errorf("首次写入应返回 Created: %s", text)
	}
	data, err := os.ReadFile(filepath.Join(ws, rel))
	if err != nil {
		t.Fatalf("当日日志未落盘: %v", err)
	}
	if !strings.Contains(string(data), "# Agent 工作日志 "+today) ||
		!strings.Contains(string(data), "[claude-code] 修复导航测试") {
		t.Errorf("日志内容/标题不对:\n%s", data)
	}

	// 第二次追加：同一文件继续追加行，且带快照 id
	text = toolText(t, s, "append_worklog", map[string]any{"content": "重构 store"})
	if !strings.Contains(text, "Appended") || !strings.Contains(text, "snapshot:") {
		t.Errorf("再次追加应返回 Appended+快照: %s", text)
	}
	data, _ = os.ReadFile(filepath.Join(ws, rel))
	if !strings.Contains(string(data), "重构 store") || strings.Contains(string(data), "[claude-code] 重构 store") {
		t.Errorf("第二次追加不应带 agent 名且应包含内容:\n%s", data)
	}
}

func TestAppendWorklog_RejectsMultiline(t *testing.T) {
	s := NewServer(newTestVault(t), true)
	resp := rpc(s, "tools/call", map[string]any{
		"name":      "append_worklog",
		"arguments": map[string]any{"content": "两行\n内容"},
	})
	res := resp["result"].(map[string]any)
	if isError, _ := res["isError"].(bool); !isError {
		t.Errorf("多行内容应拒绝: %v", res)
	}
}

func TestRecordDecision_AppendsUnderHeading(t *testing.T) {
	ws := newTestVault(t)
	addProject(t, ws, "demo", "---\nname: demo\nstatus: 进行中\n---\n# demo\n\n## 目标\n\n做点事\n\n## 资料与决策\n\n- 2026-01-01 · 旧决策\n")
	s := NewServer(ws, true)
	resp := rpc(s, "tools/call", map[string]any{
		"name":      "record_decision",
		"arguments": map[string]any{"project": "demo", "content": "采用独立日文件"},
	})
	if _, has := resp["error"]; has {
		t.Fatalf("record_decision failed: %v", resp["error"])
	}
	res := resp["result"].(map[string]any)
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "snapshot:") {
		t.Errorf("应携带快照 id: %s", text)
	}
	data, _ := os.ReadFile(filepath.Join(ws, "Projects", "demo", "project.md"))
	body := string(data)
	if !strings.Contains(body, "## 资料与决策\n\n- 2026-01-01 · 旧决策\n- "+time.Now().Format("2006-01-02")+" · 采用独立日文件") {
		t.Errorf("决策应按日期前缀追加到章节末尾:\n%s", body)
	}
}

func TestRecordDecision_CreatesHeadingWhenMissing(t *testing.T) {
	ws := newTestVault(t)
	addProject(t, ws, "demo", "---\nname: demo\nstatus: 进行中\n---\n# demo\n\n正文\n")
	s := NewServer(ws, true)
	toolText(t, s, "record_decision", map[string]any{"project": "demo", "content": "首条决策"})
	data, _ := os.ReadFile(filepath.Join(ws, "Projects", "demo", "project.md"))
	body := string(data)
	if !strings.Contains(body, "\n\n## 资料与决策\n\n- "+time.Now().Format("2006-01-02")+" · 首条决策\n") {
		t.Errorf("标题缺失时应文末补建:\n%s", body)
	}
}

func TestRecordDecision_ProjectMissing(t *testing.T) {
	s := NewServer(newTestVault(t), true)
	resp := rpc(s, "tools/call", map[string]any{
		"name":      "record_decision",
		"arguments": map[string]any{"project": "ghost", "content": "x"},
	})
	res := resp["result"].(map[string]any)
	if isError, _ := res["isError"].(bool); !isError {
		t.Errorf("项目不存在应 isError: %v", res)
	}
}

func TestUpdateProjectStatus_UpdatesAndAliases(t *testing.T) {
	ws := newTestVault(t)
	// 用中文别名「状态」与「下一步行动」书写 frontmatter，验证兼容
	addProject(t, ws, "demo", "---\n名称: demo\n状态: 进行中\n下一步行动: 发布 v0.2\n---\n# demo\n")
	s := NewServer(ws, true)
	toolText(t, s, "update_project_status", map[string]any{"project": "demo", "status": "已完成"})
	data, _ := os.ReadFile(filepath.Join(ws, "Projects", "demo", "project.md"))
	body := string(data)
	if !strings.Contains(body, "状态: 已完成") || strings.Contains(body, "进行中") {
		t.Errorf("别名键应被原位更新:\n%s", body)
	}
	if strings.Contains(body, "nextStep:") || strings.Contains(body, "status:") {
		t.Errorf("不应新增重复的英文键:\n%s", body)
	}

	// next_step 更新别名键
	toolText(t, s, "update_project_status", map[string]any{"project": "demo", "status": "已完成", "next_step": "归档"})
	data, _ = os.ReadFile(filepath.Join(ws, "Projects", "demo", "project.md"))
	if !strings.Contains(string(data), "下一步行动: 归档") {
		t.Errorf("next_step 应更新到别名键:\n%s", data)
	}
}

func TestUpdateProjectStatus_InsertsMissingField(t *testing.T) {
	ws := newTestVault(t)
	addProject(t, ws, "demo", "---\ntitle: demo\n---\n# demo\n")
	s := NewServer(ws, true)
	toolText(t, s, "update_project_status", map[string]any{"project": "demo", "status": "进行中", "next_step": "写测试"})
	data, _ := os.ReadFile(filepath.Join(ws, "Projects", "demo", "project.md"))
	body := string(data)
	if !strings.Contains(body, "status: 进行中\nnextStep: 写测试\n---") {
		t.Errorf("缺失字段应按序插入 frontmatter 末尾:\n%s", body)
	}
	if !strings.Contains(body, "title: demo") {
		t.Errorf("原有字段不应丢失:\n%s", body)
	}
}

func TestUpdateProjectStatus_CreatesFileWhenDirExists(t *testing.T) {
	ws := newTestVault(t)
	if err := os.MkdirAll(filepath.Join(ws, "Projects", "empty"), 0750); err != nil {
		t.Fatal(err)
	}
	s := NewServer(ws, true)
	text := toolText(t, s, "update_project_status", map[string]any{"project": "empty", "status": "进行中"})
	if !strings.Contains(text, "Created") {
		t.Errorf("目录存在但档案缺失应创建: %s", text)
	}
	data, err := os.ReadFile(filepath.Join(ws, "Projects", "empty", "project.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "status: 进行中") {
		t.Errorf("创建的档案应含 status:\n%s", data)
	}
}

func TestUpdateProjectStatus_ProjectDirMissing(t *testing.T) {
	s := NewServer(newTestVault(t), true)
	resp := rpc(s, "tools/call", map[string]any{
		"name":      "update_project_status",
		"arguments": map[string]any{"project": "ghost", "status": "进行中"},
	})
	res := resp["result"].(map[string]any)
	if isError, _ := res["isError"].(bool); !isError {
		t.Errorf("项目目录不存在应 isError: %v", res)
	}
}

func TestStructuredWrites_DisabledByDefault(t *testing.T) {
	s := NewServer(newTestVault(t), false)
	for _, name := range []string{"append_worklog", "record_decision", "update_project_status"} {
		args := map[string]any{"content": "x", "project": "demo", "status": "s"}
		resp := rpc(s, "tools/call", map[string]any{"name": name, "arguments": args})
		res, ok := resp["result"].(map[string]any)
		if !ok {
			t.Fatalf("%s: 应返回 result: %v", name, resp)
		}
		if isError, _ := res["isError"].(bool); !isError {
			t.Errorf("%s 写禁用时 isError 应为 true", name)
		}
	}
}

// ---------------------------------------------------------------------------
// P1：项目上下文聚合、交付物登记与复盘追加
// ---------------------------------------------------------------------------

func newProjectVault(t *testing.T) string {
	t.Helper()
	dir := newTestVault(t)
	files := map[string]string{
		"Projects/alpha/project.md":         "---\nname: Alpha Project\nstatus: 进行中\nphase: 执行\ncoreQuestion: 先做哪个功能？\nbackground: 本地优先工作台。\nnextStep: 完成交付物面板\ntechStack: [Go, Vue]\n---\n\n# Alpha Project\n\n## 资料与决策\n\n- 2026-11-01 · 选择本地优先架构\n",
		"Projects/alpha/Tasks.md":           "# Alpha Project · 任务\n\n- [ ] [US] 完成上下文条\n- [x] [US] 完成模型\n",
		"Projects/alpha/Deliverables/方案.md": "---\ndeliverableType: report\nstage: draft\nversion: v1\n---\n\n# 方案\n",
		"Projects/alpha/复盘.md":              "---\ntags: [复盘]\n---\n\n# Alpha Project · 复盘\n\n## 复盘记录\n\n- 2026-11-10 · 提前拆小任务有效\n",
	}
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestGetProjectContextAggregatesWorkingState(t *testing.T) {
	s := NewServer(newProjectVault(t), false)
	text := toolText(t, s, "get_project_context", map[string]any{"project": "alpha"})
	for _, want := range []string{
		`"phase": "执行"`,
		`"coreQuestion": "先做哪个功能？"`,
		"完成上下文条",
		"方案",
		"选择本地优先架构",
		"Projects/alpha/复盘.md",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("context missing %q:/n%s", want, text)
		}
	}
	if strings.Contains(text, "完成模型") {
		t.Errorf("completed tasks must not appear in openTasks:/n%s", text)
	}
}

func TestRegisterDeliverableCreatesAndUpdates(t *testing.T) {
	vault := newProjectVault(t)
	s := NewServer(vault, true)
	text := toolText(t, s, "register_deliverable", map[string]any{
		"project": "alpha", "title": "新版方案", "type": "report", "stage": "draft", "version": "v1",
	})
	if !strings.Contains(text, "Created deliverable") {
		t.Fatalf("unexpected create result: %s", text)
	}
	path := filepath.Join(vault, "Projects", "alpha", "Deliverables", "新版方案.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "deliverableType: report") || !strings.Contains(string(body), "stage: draft") {
		t.Fatalf("deliverable frontmatter missing: %s", body)
	}

	text = toolText(t, s, "register_deliverable", map[string]any{
		"project": "alpha", "title": "新版方案", "stage": "final",
	})
	if !strings.Contains(text, "Updated deliverable") {
		t.Fatalf("unexpected update result: %s", text)
	}
	body, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "stage: final") {
		t.Fatalf("stage was not updated: %s", body)
	}
}

func TestAppendRetroAddsDatedLine(t *testing.T) {
	vault := newProjectVault(t)
	s := NewServer(vault, true)
	toolText(t, s, "append_retro", map[string]any{"project": "alpha", "content": "下次提前拆任务"})
	body, err := os.ReadFile(filepath.Join(vault, "Projects", "alpha", "复盘.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "下次提前拆任务") {
		t.Fatalf("retrospective line missing: %s", body)
	}
}

func TestUpdateProjectStatusSetsPhase(t *testing.T) {
	vault := newProjectVault(t)
	s := NewServer(vault, true)
	toolText(t, s, "update_project_status", map[string]any{
		"project": "alpha", "status": "已完成", "phase": "复盘",
	})
	body, err := os.ReadFile(filepath.Join(vault, "Projects", "alpha", "project.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "phase: 复盘") {
		t.Fatalf("phase not written: %s", body)
	}
}
