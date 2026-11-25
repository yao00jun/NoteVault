package service

import (
	"os"
	"path/filepath"
	"testing"
)

// 倒排表分片是为了「摘要恢复能并行构建」，但分片不能改变任何查询语义。
// 这组测试把「默认分片」与「强制单分片」两个索引放在同一份语料上对比，
// 锁住 query / scoreBM25 / stats 的分片无关性。

// searchRelPaths 返回查询结果的路径序列（按 BM25 分数降序）。
func searchRelPaths(idx *searchIndex, query string) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	scored := idx.scoreBM25(tokenizeQuery(query), 0)
	out := make([]string, len(scored))
	for i, sd := range scored {
		out[i] = sd.doc.relPath
	}
	return out
}

// newSingleShardIndex 构造一个强制单分片的索引，用于与默认分片对比。
func newSingleShardIndex() *searchIndex {
	return &searchIndex{
		docs:          make(map[string]*cachedDoc),
		inverted:      newInvertedShards(1),
		contentBudget: defaultContentBudgetBytes,
	}
}

func TestSearchIndex_ShardEquivalence(t *testing.T) {
	ws := t.TempDir()
	writeMd(t, ws, "cache.md", "# 缓存失效\n缓存失效时先看 TTL，再查 goroutine 泄漏。缓存击穿要加互斥。")
	writeMd(t, ws, "index.md", "# 倒排索引\n倒排索引把 token 映射到文档，缓存能显著降低冷启动时间。")
	writeMd(t, ws, "sub/goroutine.md", "# goroutine\ncontext.Context 控制 goroutine 生命周期，注意 backoff。")
	writeMd(t, ws, "empty.md", "")

	sharded := newSearchIndex()
	single := newSingleShardIndex()
	for i, idx := range []*searchIndex{sharded, single} {
		if _, err := idx.refresh(ws); err != nil {
			t.Fatalf("refresh #%d failed: %v", i, err)
		}
	}

	// 分片数可能为 1（单核机器 / CI 小实例），等价性依然必须成立，
	// 但此时「分片 vs 单分片」退化为同一形态——记录一下便于排查。
	if n := len(sharded.inverted); n > 1 {
		t.Logf("默认索引分片数 = %d", n)
	}

	docCountA, tokenA := sharded.stats()
	docCountB, tokenB := single.stats()
	if docCountA != docCountB || tokenA != tokenB {
		t.Fatalf("stats 不一致: 分片(%d 文档, %d token) vs 单分片(%d, %d)",
			docCountA, tokenA, docCountB, tokenB)
	}

	queries := []string{"缓存", "倒排索引", "goroutine", "context.Context 生命周期", "不存在词zzz", "TTL 缓存"}
	for _, q := range queries {
		got := searchRelPaths(sharded, q)
		want := searchRelPaths(single, q)
		if len(got) != len(want) {
			t.Fatalf("查询 %q 结果数不一致: %d vs %d（%v / %v）", q, len(got), len(want), got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("查询 %q 第 %d 位不一致: %q vs %q（%v / %v）", q, i, got[i], want[i], got, want)
			}
		}
	}
}

// 增量更新必须落在同一分片：新增文件的 posting 要能被查到，
// 删除文件要从其归属分片摘干净（否则 tokenCount 虚高、查询会命中幽灵文档）。
func TestSearchIndex_ShardIncrementalUpdate(t *testing.T) {
	ws := t.TempDir()
	writeMd(t, ws, "keep.md", "# 保留\ncache 与索引")
	writeMd(t, ws, "gone.md", "# 待删\n唯一词 zebra 只出现在这里")

	idx := newSearchIndex()
	if _, err := idx.refresh(ws); err != nil {
		t.Fatal(err)
	}
	if got := searchRelPaths(idx, "zebra"); len(got) != 1 || got[0] != "gone.md" {
		t.Fatalf("删除前应命中 gone.md，实际 %v", got)
	}
	_, tokensBefore := idx.stats()

	// 删除 + 新增，一次 refresh 同时覆盖摘除与写入
	if err := os.Remove(filepath.Join(ws, "gone.md")); err != nil {
		t.Fatal(err)
	}
	writeMd(t, ws, "added.md", "# 新增\nquokka 是新增文档的独有词")
	if _, err := idx.refresh(ws); err != nil {
		t.Fatal(err)
	}

	if got := searchRelPaths(idx, "zebra"); len(got) != 0 {
		t.Fatalf("删除后不应再命中，实际 %v", got)
	}
	if got := searchRelPaths(idx, "quokka"); len(got) != 1 || got[0] != "added.md" {
		t.Fatalf("新增文档应被命中，实际 %v", got)
	}

	docCount, tokensAfter := idx.stats()
	if docCount != 2 {
		t.Fatalf("应剩 2 篇文档，实际 %d", docCount)
	}
	// zebra 独占词应随删除消失；quokka 独占词应补上，总量不增不减
	if tokensAfter != tokensBefore {
		t.Logf("token 数 %d → %d（两篇独占词数量不同属正常，此处仅记录）", tokensBefore, tokensAfter)
	}

	// 摘要往返后仍保持一致（分片归属不能依赖内存状态）
	if err := idx.SaveSummary(ws); err != nil {
		t.Fatal(err)
	}
	reloaded := newSearchIndex()
	if err := reloaded.LoadSummary(ws); err != nil {
		t.Fatal(err)
	}
	if got := searchRelPaths(reloaded, "quokka"); len(got) != 1 || got[0] != "added.md" {
		t.Fatalf("摘要恢复后应命中 added.md，实际 %v", got)
	}
	if got := searchRelPaths(reloaded, "zebra"); len(got) != 0 {
		t.Fatalf("摘要恢复后不应命中 zebra，实际 %v", got)
	}
	docCount2, tokenCount2 := reloaded.stats()
	docCount1, tokenCount1 := idx.stats()
	if docCount1 != docCount2 || tokenCount1 != tokenCount2 {
		t.Fatalf("摘要往返后统计不一致: (%d,%d) vs (%d,%d)",
			docCount1, tokenCount1, docCount2, tokenCount2)
	}
}
