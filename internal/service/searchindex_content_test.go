package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 正文按需加载 + 内存预算
//
// 背景：重构前 refresh 会把所有「content 为空」的文档一次性补读进内存，
// 冷启动后整库正文（含小写副本）常驻，内存随知识库体积线性增长。
// 现在只有真正参与查询的文档才会被读进内存，且受 contentBudget 约束。
// ---------------------------------------------------------------------------

// TestSearchIndex_SmallVaultKeepsContentWarm 小库在默认预算内应保留正文（避免重复读盘）
func TestSearchIndex_SmallVaultKeepsContentWarm(t *testing.T) {
	ws := t.TempDir()
	writeMd(t, ws, "a.md", "# A\nhello world")

	idx := newSearchIndexForTest() // 默认 32 MiB 预算
	if _, err := idx.refresh(ws); err != nil {
		t.Fatal(err)
	}
	idx.mu.RLock()
	doc := idx.docs["a.md"]
	idx.mu.RUnlock()
	if doc == nil {
		t.Fatal("a.md should be indexed")
	}
	if !doc.isContentLoaded() {
		t.Error("within the default budget, cold refresh should keep the body warm")
	}
}

// TestSearchIndex_ContentBudgetEvictsAndReloads 预算不足时按 LRU 淘汰，且淘汰不影响检索正确性
func TestSearchIndex_ContentBudgetEvictsAndReloads(t *testing.T) {
	ws := t.TempDir()
	// 20 篇 × 约 2.2KB，正文总量约 44KB（含小写副本约 88KB）
	body := strings.Repeat("alpha beta ", 200)
	for i := 0; i < 20; i++ {
		writeMd(t, ws, fmt.Sprintf("d%02d.md", i), "# D\n"+body)
	}

	idx := newSearchIndexForTest()
	idx.contentBudget = 8 * 1024 // 远小于总量，强制触发淘汰
	if _, err := idx.refresh(ws); err != nil {
		t.Fatal(err)
	}
	if got := idx.contentBytes.Load(); got > idx.contentBudget {
		t.Fatalf("after refresh: contentBytes=%d exceeds budget %d", got, idx.contentBudget)
	}
	// 淘汰只丢正文，索引元数据（tokenSet）必须完整
	if docs, _ := idx.stats(); docs != 20 {
		t.Fatalf("expected 20 docs indexed, got %d", docs)
	}

	// 被淘汰的正文在查询时按需重新读盘，结果不受影响
	idx.mu.RLock()
	candidates := idx.query("alpha")
	idx.mu.RUnlock()
	if len(candidates) != 20 {
		t.Fatalf("expected 20 candidates, got %d", len(candidates))
	}
	for _, doc := range candidates {
		content, _, err := idx.contentOf(doc)
		if err != nil {
			t.Fatalf("contentOf(%s) failed: %v", doc.relPath, err)
		}
		if !strings.Contains(content, "alpha") {
			t.Fatalf("contentOf(%s) returned unexpected body", doc.relPath)
		}
	}

	idx.enforceContentBudget()
	if got := idx.contentBytes.Load(); got > idx.contentBudget {
		t.Fatalf("after query: contentBytes=%d exceeds budget %d", got, idx.contentBudget)
	}
}

// TestSearchIndex_EmptyFileNotRepeatedlyRead 空文件必须被标记为「已加载」，
// 否则每次查询都会重复读盘（content 恒为 "" 无法区分加载与否）。
func TestSearchIndex_EmptyFileNotRepeatedlyRead(t *testing.T) {
	ws := t.TempDir()
	writeMd(t, ws, "empty.md", "")

	idx := newSearchIndexForTest()
	if _, err := idx.refresh(ws); err != nil {
		t.Fatal(err)
	}
	idx.mu.RLock()
	doc := idx.docs["empty.md"]
	idx.mu.RUnlock()
	if doc == nil {
		t.Fatal("empty.md should be indexed")
	}

	// 模拟正文被预算淘汰后的状态
	doc.contentMu.Lock()
	doc.releaseContentLocked(idx)
	doc.contentMu.Unlock()

	content, _, err := idx.contentOf(doc)
	if err != nil {
		t.Fatalf("contentOf failed: %v", err)
	}
	if content != "" {
		t.Fatalf("expected empty body, got %q", content)
	}
	if !doc.isContentLoaded() {
		t.Fatal("empty file must still be marked as loaded, otherwise it is re-read on every query")
	}

	// 把路径指向不存在的文件：如果第二次取用仍走读盘，这里就会报错
	doc.contentMu.Lock()
	doc.absPath = filepath.Join(ws, "definitely-missing.md")
	doc.contentMu.Unlock()
	if _, _, err := idx.contentOf(doc); err != nil {
		t.Fatalf("second contentOf should hit the cache instead of reading %s: %v", doc.absPath, err)
	}
}

// TestSearchIndex_RemovedDocIsStale 被 refresh 移除的文档，其旧指针不应再吐出正文
func TestSearchIndex_RemovedDocIsStale(t *testing.T) {
	ws := t.TempDir()
	writeMd(t, ws, "a.md", "# A\nhello world")

	idx := newSearchIndexForTest()
	if _, err := idx.refresh(ws); err != nil {
		t.Fatal(err)
	}
	idx.mu.RLock()
	doc := idx.docs["a.md"]
	idx.mu.RUnlock()
	if doc == nil {
		t.Fatal("a.md should be indexed")
	}

	// 直接从磁盘删掉并 refresh，旧指针会被标记 removed
	if err := os.Remove(filepath.Join(ws, "a.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.refresh(ws); err != nil {
		t.Fatal(err)
	}
	if _, _, err := idx.contentOf(doc); err == nil {
		t.Fatal("contentOf on a removed doc should fail (stale), got nil error")
	}
}
