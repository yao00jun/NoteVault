package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestToolsListHasSix(t *testing.T) {
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
	if len(tools) != 6 {
		t.Errorf("tool count = %d, want 6", len(tools))
	}
	want := map[string]bool{
		"list_notes": true, "read_note": true, "search_notes": true,
		"get_tags": true, "get_backlinks": true, "create_note": true,
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
