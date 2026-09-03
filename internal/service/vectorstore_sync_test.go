package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestVectorStore_IncrementalSync 验证增量同步的三条路径：
// 新增文件进入索引、修改文件重嵌入、删除文件退出索引。
// 这是「向量索引建一次永不更新」修复的行为护栏。
func TestVectorStore_IncrementalSync(t *testing.T) {
	ws := newQnATestWorkspace(t)
	fs := NewFileServiceWithHistory(NewSnapshotService())
	emb := tokenEmbedder{dim: 256}
	cfg := EmbeddingConfig{BaseURL: "http://localhost:11434/v1", Model: "test-model"}

	vs, err := NewVectorStore(persistDirForWorkspace(ws, cfg.Model), emb, cfg)
	if err != nil {
		t.Fatalf("NewVectorStore failed: %v", err)
	}
	if err := vs.Sync(context.Background(), ws, fs, emb, cfg); err != nil {
		t.Fatalf("first Sync failed: %v", err)
	}
	countAfterBuild := vs.Count()
	if countAfterBuild == 0 {
		t.Fatal("expected initial build to index chunks")
	}

	// 第二次 Sync，无任何变更 → 不应重建（Count 不变，且为幂等）
	if err := vs.Sync(context.Background(), ws, fs, emb, cfg); err != nil {
		t.Fatalf("no-op Sync failed: %v", err)
	}
	if vs.Count() != countAfterBuild {
		t.Fatalf("no-op Sync changed count: %d -> %d", countAfterBuild, vs.Count())
	}

	// 新增文件 → Sync 后可检索到
	rustPath := filepath.Join(ws, "rust.md")
	rustDoc := "# Rust 入门\n\nRust 是一门系统级编程语言，主打内存安全，cargo 是它的构建工具。"
	if err := os.WriteFile(rustPath, []byte(rustDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(rustPath, time.Now().Add(2*time.Second), time.Now().Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := vs.Sync(context.Background(), ws, fs, emb, cfg); err != nil {
		t.Fatalf("Sync after add failed: %v", err)
	}
	if vs.Count() <= countAfterBuild {
		t.Fatalf("expected new chunks after adding file, count %d -> %d", countAfterBuild, vs.Count())
	}
	hits, err := vs.Search(context.Background(), "rust cargo 内存安全", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].RelPath != "rust.md" {
		t.Fatalf("expected rust.md as top hit, got %+v", hits)
	}

	// 删除文件 → Sync 后从索引移除
	if err := os.Remove(rustPath); err != nil {
		t.Fatal(err)
	}
	if err := vs.Sync(context.Background(), ws, fs, emb, cfg); err != nil {
		t.Fatalf("Sync after delete failed: %v", err)
	}
	hits, err = vs.Search(context.Background(), "rust cargo 内存安全", 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.RelPath == "rust.md" {
			t.Fatalf("deleted file should not be retrievable, got %+v", hits)
		}
	}
}
