package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTokenize_LatinAndCJK(t *testing.T) {
	tokens := tokenize("Hello 世界 apple-pie")
	// Latin: "hello", "apple", "pie" (连字符分隔)
	// CJK:  "世", "界"
	expect := []string{"hello", "世", "界", "apple", "pie"}
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

func TestSearchIndex_StaleCandidatesFilteredByExactMatch(t *testing.T) {
	ClearAllSearchIndexes()
	dir := t.TempDir()

	// 文档包含 "关" 和 "键" 但不包含 "关键词" 子串
	mustWrite(t, filepath.Join(dir, "a.md"), "# Test\n关 键 分开写了")

	idx := getSearchIndex(dir)
	idx.refresh(dir)

	// 查询 "关键词" — token: 关,键,词。文档有 关 和 键 token → 候选
	idx.mu.RLock()
	candidates := idx.query("关键词")
	idx.mu.RUnlock()

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	// 但精确 substring 匹配应返回 0。
	// 正文是按需加载的，必须先经 contentOf 取出再计数（直接读字段会拿到空串，
	// 那样这个断言就会「因为根本没内容」而通过，失去意义）。
	_, contentLower, err := idx.contentOf(candidates[0])
	if err != nil {
		t.Fatalf("contentOf failed: %v", err)
	}
	matchCount := strings.Count(contentLower, strings.ToLower("关键词"))
	if matchCount != 0 {
		t.Fatalf("exact match should be 0 for non-adjacent CJK, got %d", matchCount)
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
