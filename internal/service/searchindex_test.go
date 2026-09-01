package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTokenize_LatinAndCJK(t *testing.T) {
	tokens := tokenize("Hello 世界 apple-pie")
	// Latin: "hello", "apple", "pie" (连字符分隔)
	// CJK:  "世界" 是长度为 2 的连续段 → 产出 1 个 bigram
	expect := []string{"hello", "世界", "apple", "pie"}
	if len(tokens) != len(expect) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expect), len(tokens), tokens)
	}
	for i, e := range expect {
		if tokens[i] != e {
			t.Fatalf("token[%d]: expected %q, got %q", i, e, tokens[i])
		}
	}
}

func TestTokenize_UnderscoreIsPartOfToken(t *testing.T) {
	tokens := tokenize("my_var another_var")
	if len(tokens) != 2 || tokens[0] != "my_var" || tokens[1] != "another_var" {
		t.Fatalf("underscore should be part of token, got %v", tokens)
	}
}

func TestTokenize_EmptyAndSpecial(t *testing.T) {
	if len(tokenize("")) != 0 {
		t.Fatal("empty string should produce 0 tokens")
	}
	if len(tokenize("---...,,,;;;")) != 0 {
		t.Fatal("special chars only should produce 0 tokens")
	}
}

func TestSearchIndex_RefreshAndQuery(t *testing.T) {
	ClearAllSearchIndexes()
	dir := t.TempDir()

	mustWrite(t, filepath.Join(dir, "a.md"), "# Doc Alpha\nhello world apple")
	mustWrite(t, filepath.Join(dir, "b.md"), "# Doc Beta\nhello banana")

	idx := getSearchIndex(dir)
	_, err := idx.refresh(dir)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	docCount, tokenCount := idx.stats()
	if docCount != 2 {
		t.Fatalf("expected 2 docs, got %d", docCount)
	}
	if tokenCount == 0 {
		t.Fatal("expected non-zero token count")
	}

	// Query "apple" → should find only a.md
	idx.mu.RLock()
	candidates := idx.query("apple")
	idx.mu.RUnlock()
	if len(candidates) != 1 || candidates[0].relPath != "a.md" {
		t.Fatalf("expected a.md only, got %v", candidates)
	}
}

func TestSearchIndex_IncrementalUpdate(t *testing.T) {
	ClearAllSearchIndexes()
	dir := t.TempDir()

	// 初始文件
	mustWrite(t, filepath.Join(dir, "x.md"), "# X\ncontent here")
	idx := getSearchIndex(dir)
	idx.refresh(dir)

	docCount, _ := idx.stats()
	if docCount != 1 {
		t.Fatalf("expected 1 doc, got %d", docCount)
	}

	// 新增文件
	mustWrite(t, filepath.Join(dir, "y.md"), "# Y\nnew file")
	idx.refresh(dir)

	docCount, _ = idx.stats()
	if docCount != 2 {
		t.Fatalf("expected 2 docs after add, got %d", docCount)
	}

	// 修改 x.md — 等待 modtime 精度后再写
	time.Sleep(60 * time.Millisecond)
	mustWrite(t, filepath.Join(dir, "x.md"), "# X Updated\nchanged content")
	idx.refresh(dir)

	idx.mu.RLock()
	doc := idx.docs["x.md"]
	idx.mu.RUnlock()
	if doc.title != "X Updated" {
		t.Fatalf("expected title 'X Updated', got %q", doc.title)
	}

	// 删除 y.md
	os.Remove(filepath.Join(dir, "y.md"))
	idx.refresh(dir)

	docCount, _ = idx.stats()
	if docCount != 1 {
		t.Fatalf("expected 1 doc after delete, got %d", docCount)
	}
}

// TestSearchIndex_BigramAvoidsFalseCandidates 验证 bigram 切分不会产生伪候选。
//
// 这是 P0-1 把 CJK 从「单字」改为「二元切分」的直接收益：
// 单字切分下，文档「关 键 分开写了」会因为它含有关/键 两个单字而成为
// 「关键词」的候选，只能靠后续的精确子串匹配兜底；
// bigram 下查询切成 [关键, 键词]，与文档 token 无交集，从源头就不产生候选。
func TestSearchIndex_BigramAvoidsFalseCandidates(t *testing.T) {
	ClearAllSearchIndexes()
	dir := t.TempDir()

	// 「关」与「键」被空格分开，不构成 bigram「关键」
	mustWrite(t, filepath.Join(dir, "a.md"), "# Test\n关 键 分开写了")

	idx := getSearchIndex(dir)
	idx.refresh(dir)

	idx.mu.RLock()
	candidates := idx.query("关键词")
	idx.mu.RUnlock()

	if len(candidates) != 0 {
		t.Fatalf("expected 0 candidates, got %d", len(candidates))
	}
}

func TestClearSearchIndex(t *testing.T) {
	ClearAllSearchIndexes()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# A\nhello")

	idx := getSearchIndex(dir)
	idx.refresh(dir)

	docCount, _ := idx.stats()
	if docCount != 1 {
		t.Fatalf("expected 1 doc, got %d", docCount)
	}

	ClearSearchIndex(dir)

	// 获取新实例（应该是空的）
	idx2 := getSearchIndex(dir)
	docCount, _ = idx2.stats()
	if docCount != 0 {
		t.Fatalf("expected 0 docs after clear, got %d", docCount)
	}
}
