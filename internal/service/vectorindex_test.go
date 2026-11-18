package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// vectorIndex 契约测试
//
// 目的不是验证 chromem 的行为（那是 vectorstore_*_test.go 的事），而是锁住
// 「QnAService 只依赖接口」这一结构约束：把 fake 索引塞进缓存，问答链路必须
// 原样工作。这样将来替换实现（自研分片、ANN 索引）时，回归面收敛在一个文件。
// ---------------------------------------------------------------------------

// fakeVectorIndex 是 vectorIndex 的测试替身：不碰磁盘、不做 embedding，
// 只记录调用次数并返回预置命中。
type fakeVectorIndex struct {
	mu        sync.Mutex
	syncs     int
	searches  int
	lastK     int
	hits      []vectorHit
	searchErr error
}

// 编译期断言：替身与真实实现受同一契约约束。
var _ vectorIndex = (*fakeVectorIndex)(nil)

func (f *fakeVectorIndex) Count() int { return len(f.hits) }

func (f *fakeVectorIndex) Sync(context.Context, string, *FileService, Embedder, EmbeddingConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncs++
	return nil
}

func (f *fakeVectorIndex) Search(_ context.Context, _ string, k int) ([]vectorHit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.searches++
	f.lastK = k
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	hits := f.hits
	if k > 0 && len(hits) > k {
		hits = hits[:k]
	}
	return hits, nil
}

func (f *fakeVectorIndex) calls() (syncs, searches int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.syncs, f.searches
}

// newVectorIndexTestWorkspace 造一个 BM25 与查询词面零重叠的工作区。
func newVectorIndexTestWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"a-note.md": "# 缓存失效\n\n缓存失效是分布式系统里的经典难题，常见做法是设置 TTL 到期淘汰。",
		"b-note.md": "# 购物清单\n\n牛奶、鸡蛋、面包。",
	}
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("写入夹具失败（%s）：%v", rel, err)
		}
	}
	return dir
}

// 注入 fake 索引后，向量独有的文档必须经 RRF 融合出现在结果里，
// 且整条链路应当驱动 Sync + Search 各一次（证明调用走的是接口）。
func TestVectorIndex_QnAChainUsesInterface(t *testing.T) {
	ClearAllSearchIndexes()
	dir := newVectorIndexTestWorkspace(t)

	const query = "how to invalidate cache"
	fake := &fakeVectorIndex{
		hits: []vectorHit{{
			RelPath:    "a-note.md",
			Heading:    "缓存失效",
			ChunkIndex: 0,
			Content:    "缓存失效是分布式系统里的经典难题",
			Similarity: 0.91,
		}},
	}

	fs := NewFileServiceWithHistory(NewSnapshotService())
	svc := NewQnAServiceWithRegistry(fs, nil, tokenEmbedder{dim: 16}, nil)
	// 预置缓存 = 模拟「索引已建好」，跳过真实构建（fake 不落盘）。
	svc.vectorCache[dir+"|contract-model"] = fake

	// 基线：纯 BM25 对该查询零命中，否则下面的断言无法区分召回来源。
	bm25Only, err := NewQnAService().HybridSearch(dir, query, "", "", "", RerankConfig{})
	if err != nil {
		t.Fatalf("BM25 基线失败：%v", err)
	}
	if len(bm25Only) != 0 {
		t.Fatalf("前提不成立：BM25 基线应零命中，实际 %d 条", len(bm25Only))
	}

	res, err := svc.HybridSearch(dir, query, "contract://local", "contract-model", "", RerankConfig{})
	if err != nil {
		t.Fatalf("混合检索失败：%v", err)
	}
	if len(res) == 0 || normRelPath(res[0].Path) != "a-note.md" {
		t.Fatalf("fake 索引的命中未被融合进结果：%+v", res)
	}
	syncs, searches := fake.calls()
	if syncs != 1 || searches != 1 {
		t.Fatalf("期望 Sync/Search 各 1 次，实际 %d/%d", syncs, searches)
	}
}

// 检索失败必须静默降级为纯 BM25（返回空结果而不是报错），
// 这是「语义增强不能让功能残废」的红线在接口层的体现。
func TestVectorIndex_SearchFailureDegradesToBM25(t *testing.T) {
	ClearAllSearchIndexes()
	dir := newVectorIndexTestWorkspace(t)

	fake := &fakeVectorIndex{searchErr: errors.New("boom")}
	fs := NewFileServiceWithHistory(NewSnapshotService())
	svc := NewQnAServiceWithRegistry(fs, nil, tokenEmbedder{dim: 16}, nil)
	svc.vectorCache[dir+"|broken-model"] = fake

	res, err := svc.HybridSearch(dir, "how to invalidate cache", "broken://local", "broken-model", "", RerankConfig{})
	if err != nil {
		t.Fatalf("向量 leg 失败不应向上报错，实际：%v", err)
	}
	if len(res) != 0 {
		t.Fatalf("向量 leg 失败且 BM25 零命中时应返回空结果，实际：%+v", res)
	}
	if _, searches := fake.calls(); searches != 1 {
		t.Fatalf("期望检索被尝试 1 次，实际 %d", searches)
	}
}
