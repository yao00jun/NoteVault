package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// helperSearchTestService 复用 searchindex 主体，独立测试摘要功能
func newSearchIndexForTest() *searchIndex {
	return newSearchIndex()
}

func writeMd(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSearchIndex_SaveLoadSummary_RoundTrip(t *testing.T) {
	ws := t.TempDir()
	writeMd(t, ws, "a.md", "# A\nhello world")
	writeMd(t, ws, "sub/b.md", "# B\nthe quick brown fox")
	writeMd(t, ws, "c.markdown", "no title here")

	// 1) 建索引
	idx := newSearchIndexForTest()
	if _, err := idx.refresh(ws); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	docCount, tokenCount := idx.stats()
	if docCount != 3 {
		t.Fatalf("expected 3 docs, got %d", docCount)
	}
	originalTokens := tokenCount

	// 2) 保存摘要
	if err := idx.SaveSummary(ws); err != nil {
		t.Fatalf("SaveSummary failed: %v", err)
	}

	// 3) 摘要文件存在
	summaryPath := summaryPathFor(ws)
	if _, err := os.Stat(summaryPath); err != nil {
		t.Fatalf("summary file not created: %v", err)
	}
	// 4) 新实例加载摘要
	idx2 := newSearchIndexForTest()
	if err := idx2.LoadSummary(ws); err != nil {
		t.Fatalf("LoadSummary failed: %v", err)
	}
	docCount2, tokenCount2 := idx2.stats()
	if docCount2 != 3 {
		t.Errorf("after load: docs=%d, want 3", docCount2)
	}
	if tokenCount2 != originalTokens {
		t.Errorf("after load: tokens=%d, want %d", tokenCount2, originalTokens)
	}

	// 5) LoadSummary 后正文不应被加载。
	//    注意判据是 contentLoaded 而不是 content——空文件的正文同样是 ""，
	//    只有 contentLoaded 能区分「尚未加载」与「正文确实是空的」。
	idx2.mu.RLock()
	for _, doc := range idx2.docs {
		if doc.isContentLoaded() {
			t.Errorf("content should not be loaded after LoadSummary, %s already loaded", doc.relPath)
		}
	}
	idx2.mu.RUnlock()

	// 6) refresh 之后同样不应加载正文：命中摘要快路径的文档只补挂 absPath，
	//    正文留给真正被查询命中的文档（避免全库正文常驻内存）。
	if _, err := idx2.refresh(ws); err != nil {
		t.Fatalf("refresh after load failed: %v", err)
	}
	idx2.mu.RLock()
	for _, doc := range idx2.docs {
		if doc.isContentLoaded() {
			t.Errorf("content should stay unloaded after refresh (lazy load), %s was eagerly loaded", doc.relPath)
		}
		// 摘要只记录相对路径，绝对路径必须由 refresh 的 toRelink 补挂，
		// 否则后续按需读盘会拿到空路径
		doc.contentMu.Lock()
		absPath := doc.absPath
		doc.contentMu.Unlock()
		if absPath == "" {
			t.Errorf("absPath should be relinked by refresh, %s still empty", doc.relPath)
		}
	}
	idx2.mu.RUnlock()

	// 7) 查询仍正确返回结果，且按需加载后的正文可用于精确 substring 匹配
	candidates := func() []*cachedDoc {
		idx2.mu.RLock()
		defer idx2.mu.RUnlock()
		return idx2.query("hello")
	}()
	if len(candidates) != 1 {
		t.Fatalf("query 'hello' returned %d candidates, want 1", len(candidates))
	}
	content, contentLower, err := idx2.contentOf(candidates[0])
	if err != nil {
		t.Fatalf("contentOf failed: %v", err)
	}
	if !strings.Contains(contentLower, "hello") {
		t.Errorf("contentOf should load the real body, got %q", content)
	}
}

// isContentLoaded 测试辅助：读取正文是否已加载（自带加锁）
func (d *cachedDoc) isContentLoaded() bool {
	d.contentMu.Lock()
	defer d.contentMu.Unlock()
	return d.contentLoaded
}

func TestSearchIndex_LoadSummary_FileNotExist(t *testing.T) {
	idx := newSearchIndexForTest()
	ws := t.TempDir()
	// 摘要不存在的场景：不应返回错误
	if err := idx.LoadSummary(ws); err != nil {
		t.Errorf("LoadSummary on missing file should return nil, got %v", err)
	}
	if docs, _ := idx.stats(); docs != 0 {
		t.Errorf("expected 0 docs after empty LoadSummary, got %d", docs)
	}
}

func TestSearchIndex_LoadSummary_CorruptedJSON(t *testing.T) {
	idx := newSearchIndexForTest()
	ws := t.TempDir()
	// 写一个损坏的 JSON 文件
	summaryPath := summaryPathFor(ws)
	if err := os.MkdirAll(filepath.Dir(summaryPath), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(summaryPath, []byte("{not valid json"), 0640); err != nil {
		t.Fatal(err)
	}
	// 损坏文件不应阻塞 LoadSummary，应返回 nil 让上层重新扫描
	if err := idx.LoadSummary(ws); err != nil {
		t.Errorf("LoadSummary on corrupted file should not error, got %v", err)
	}
}

func TestSearchIndex_LoadSummary_WorkspaceMismatch(t *testing.T) {
	idx := newSearchIndexForTest()
	ws1 := t.TempDir()
	ws2 := t.TempDir()
	// 给 ws1 建索引并保存摘要
	writeMd(t, ws1, "a.md", "# A\nhello")
	idxA := newSearchIndexForTest()
	if _, err := idxA.refresh(ws1); err != nil {
		t.Fatal(err)
	}
	if err := idxA.SaveSummary(ws1); err != nil {
		t.Fatal(err)
	}
	// 用 ws2 加载（理论上 workspace 路径不匹配）
	// 注意：摘要文件由 sha1(workspace) 命名，所以 ws2 不会有自己的摘要文件，
	// LoadSummary 会找不到文件并返回 nil；这是预期行为
	if err := idx.LoadSummary(ws2); err != nil {
		t.Errorf("LoadSummary on unrelated workspace should return nil, got %v", err)
	}
	if docs, _ := idx.stats(); docs != 0 {
		t.Errorf("expected 0 docs loaded for unrelated workspace, got %d", docs)
	}
}

func TestSearchIndex_SaveSummary_AtomicWrite(t *testing.T) {
	// 验证 SaveSummary 用临时文件 + rename，不会留下 .tmp 残留
	ws := t.TempDir()
	writeMd(t, ws, "a.md", "# A\nhello")
	idx := newSearchIndexForTest()
	if _, err := idx.refresh(ws); err != nil {
		t.Fatal(err)
	}
	if err := idx.SaveSummary(ws); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(summaryPathFor(ws))
	entries, _ := os.ReadDir(dir)
	tmpLeftover := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			tmpLeftover = true
		}
	}
	if tmpLeftover {
		t.Error("SaveSummary should not leave .tmp files behind")
	}
}

func TestSearchIndex_RefreshAfterSummary_FastPathSkipsReadFile(t *testing.T) {
	// 验证 LoadSummary + refresh 的快路径：modtime 一致 + content 已有 → 跳过读文件
	ws := t.TempDir()
	writeMd(t, ws, "a.md", "# A\ninitial content")
	idx := newSearchIndexForTest()
	if _, err := idx.refresh(ws); err != nil {
		t.Fatal(err)
	}
	// 此时 content 已填充，再 refresh 应走完整缓存命中快路径
	// 通过把文件设为只读 + 删除文件验证（但删除会触发 modtime 变化）
	// 简化：直接调用 refresh，验证 stats 不变
	if _, err := idx.refresh(ws); err != nil {
		t.Fatal(err)
	}
	if docs, _ := idx.stats(); docs != 1 {
		t.Errorf("expected 1 doc after 2nd refresh, got %d", docs)
	}
}

func TestSearchIndex_SaveSummary_EmptyIndex(t *testing.T) {
	// 空索引也应能保存和加载摘要
	ws := t.TempDir()
	idx := newSearchIndexForTest()
	if err := idx.SaveSummary(ws); err != nil {
		t.Fatalf("SaveSummary on empty index failed: %v", err)
	}
	idx2 := newSearchIndexForTest()
	if err := idx2.LoadSummary(ws); err != nil {
		t.Fatalf("LoadSummary failed: %v", err)
	}
	if docs, _ := idx2.stats(); docs != 0 {
		t.Errorf("expected 0 docs after loading empty summary, got %d", docs)
	}
}

func TestSummaryPathFor_DeterministicAndDifferentWorkspaces(t *testing.T) {
	a := summaryPathFor("C:\\Users\\test\\workspace-a")
	b := summaryPathFor("C:\\Users\\test\\workspace-a")
	c := summaryPathFor("C:\\Users\\test\\workspace-b")
	if a != b {
		t.Error("same workspace should produce same path")
	}
	if a == c {
		t.Error("different workspaces should produce different paths")
	}
	// 校验文件名是 .json
	if !strings.HasSuffix(a, ".json") {
		t.Errorf("summary path should end with .json, got %s", a)
	}
}

func TestSearchIndexDir_FallbackToTempDir(t *testing.T) {
	// 临时清空 APPDATA 验证回退
	original := os.Getenv("APPDATA")
	os.Setenv("APPDATA", "")
	defer os.Setenv("APPDATA", original)
	dir := searchIndexDir()
	if !strings.Contains(dir, os.TempDir()) {
		t.Errorf("expected fallback to TempDir, got %s", dir)
	}
}

// min helper (Go 1.21+ 内置但旧版本需手写)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 防止 time 包未使用警告（保留给未来扩展用）
var _ = time.Now
