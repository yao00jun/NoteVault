package service

import (
	"sync"
	"testing"
)

// TestSearchIndexRegistry_GetReturnsSameInstance 索引身份必须稳定：
// 增量更新与正文缓存都建立在「同一个工作区复用同一个索引」之上。
func TestSearchIndexRegistry_GetReturnsSameInstance(t *testing.T) {
	r := NewSearchIndexRegistry()

	a := r.Get("/ws/one")
	b := r.Get("/ws/one")
	if a != b {
		t.Fatal("Get must return the same index instance for the same workspace path")
	}

	other := r.Get("/ws/two")
	if other == a {
		t.Fatal("different workspace paths must get different index instances")
	}
	if r.Len() != 2 {
		t.Fatalf("expected 2 indexes, got %d", r.Len())
	}
}

// TestSearchIndexRegistry_EvictReleases 释放后注册表不再持有该索引。
// 这是 E-4 的核心：切走工作区必须能真正回收内存。
func TestSearchIndexRegistry_EvictReleases(t *testing.T) {
	r := NewSearchIndexRegistry()

	old := r.Get("/ws/one")
	if err := r.Evict("/ws/one"); err != nil {
		t.Fatalf("Evict: %v", err)
	}
	if r.Len() != 0 {
		t.Fatalf("expected registry to be empty after Evict, got %d", r.Len())
	}

	// 再次 Get 应拿到全新实例——旧的那份已被摘除。
	fresh := r.Get("/ws/one")
	if fresh == old {
		t.Fatal("Evict must drop the old index; Get afterwards should build a new one")
	}
}

// TestSearchIndexRegistry_EvictIsIdempotent 释放不存在的路径不是错误：
// 调用方（工作区切换）不该为「索引还没建起来」这种正常情况处理错误分支。
func TestSearchIndexRegistry_EvictIsIdempotent(t *testing.T) {
	r := NewSearchIndexRegistry()
	if err := r.Evict("/never/used"); err != nil {
		t.Fatalf("Evict on unknown path should be a no-op, got %v", err)
	}
	if err := r.Flush("/never/used"); err != nil {
		t.Fatalf("Flush on unknown path should be a no-op, got %v", err)
	}
}

// TestSearchIndexRegistry_FlushKeepsIndex Flush 与 Evict 的分工：
// Flush 只写盘不释放，用于「退出前落盘但仍可能切回」的时机。
func TestSearchIndexRegistry_FlushKeepsIndex(t *testing.T) {
	r := NewSearchIndexRegistry()
	idx := r.Get("/ws/one")

	if err := r.Flush("/ws/one"); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if r.Len() != 1 {
		t.Fatalf("Flush must not release the index, got %d entries", r.Len())
	}
	if r.Get("/ws/one") != idx {
		t.Fatal("Flush must keep the same index instance (rebuild would lose the content cache)")
	}
}

// TestSearchIndexRegistry_PathsAreSorted 输出稳定，便于日志与断言。
func TestSearchIndexRegistry_PathsAreSorted(t *testing.T) {
	r := NewSearchIndexRegistry()
	for _, p := range []string{"/c", "/a", "/b"} {
		r.Get(p)
	}

	got := r.Paths()
	want := []string{"/a", "/b", "/c"}
	if len(got) != len(want) {
		t.Fatalf("Paths() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Paths() = %v, want sorted %v", got, want)
		}
	}
}

// TestSearchIndexRegistry_ConcurrentGet 注册表的 map 必须并发安全：
// 搜索是高频操作，且前端可能并行触发多个工作区的查询。
// 用 -race 跑才有意义。
func TestSearchIndexRegistry_ConcurrentGet(t *testing.T) {
	r := NewSearchIndexRegistry()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			path := "/ws/" + string(rune('a'+n%4))
			idx := r.Get(path)
			if idx == nil {
				t.Errorf("Get returned nil for %s", path)
			}
		}(i)
	}
	wg.Wait()

	if r.Len() != 4 {
		t.Fatalf("expected 4 distinct workspaces, got %d", r.Len())
	}
}

// TestSearchIndexRegistry_ZeroValueUsable 零值注册表不应 panic。
// 兜底是为了让未注入注册表的 Service 也能工作，panic 会把「配置疏漏」
// 变成「运行时崩溃」，这在桌面应用里是不可接受的。
func TestSearchIndexRegistry_ZeroValueUsable(t *testing.T) {
	var r SearchIndexRegistry
	if idx := r.Get("/ws/one"); idx == nil {
		t.Fatal("zero-value registry must lazily initialise its map instead of panicking")
	}
	if r.Len() != 1 {
		t.Fatalf("expected 1 index, got %d", r.Len())
	}
}

// TestSearchIndexSharing_SearchAndQnAUseInjectedRegistry 问答与搜索必须共用同一份索引，
// 否则内存占用翻倍，且一方的增量更新对另一方不可见。
func TestSearchIndexSharing_SearchAndQnAUseInjectedRegistry(t *testing.T) {
	r := NewSearchIndexRegistry()
	search := NewSearchServiceWithRegistry(nil, r)
	qna := NewQnAServiceWithRegistry(nil, r, nil, nil)

	// 未注入注册表时回落到默认注册表，不应 panic（零值可用性）
	fallback := NewSearchService(nil)
	if fallback.idxOf("/ws/fallback") == nil {
		t.Fatal("SearchService without registry should still return a usable index")
	}

	// 注入后的三方必须指向注册表里同一个实例
	if search.idxOf("/ws/one") != r.Get("/ws/one") {
		t.Fatal("SearchService must use the injected registry")
	}
	if qna.idxOf("/ws/one") != r.Get("/ws/one") {
		t.Fatal("QnAService must use the injected registry")
	}
	if search.idxOf("/ws/one") != qna.idxOf("/ws/one") {
		t.Fatal("SearchService and QnAService must share the same index instance")
	}
}
