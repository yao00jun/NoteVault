package service

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// 搜索索引注册表（E-4）
// ---------------------------------------------------------------------------
//
// 背景：索引原本存放在包级全局 sync.Map 里，由 getSearchIndex() 惰性创建。
// 这带来两个问题：
//   1. 没有释放路径——切走工作区后索引仍常驻内存，正文缓存预算是按「单个工作区」
//      计的，开的库越多，总内存占用越不可控；
//   2. 所有权隐式——谁创建、谁该释放、生命周期多久，全靠读代码推断，
//      无法在装配层（container.go）显式表达。
//
// 改造后：索引归 SearchIndexRegistry 所有，注册表由容器创建并注入给
// WorkspaceService / SearchService / QnAService 三方共享。
// WorkspaceService 在切换、删除工作区时调用 Evict 主动释放。
//
// 并发语义（重要）：
//   - 注册表自身的 mu 只保护 map 结构，不保护索引内部（索引内部有自己的锁）。
//   - Evict 只是把指针从表里摘掉，不强制中断正在进行的查询。已拿到
//     *searchIndex 指针的 goroutine 仍可安全用下去，由 GC 在最后一个引用消失后回收。
//     这是刻意的：强行等待查询结束会引入死锁与长尾延迟，收益却只是「早一点释放内存」。
//   - 因此 Evict 后若立刻 Get 同一路径，会拿到一个新的空索引；旧的那份正在被谁用着
//     不影響正确性，只是它后续的落盘可能覆盖新索引的摘要。为避免这种竞态，
//     Evict 会先 Flush（把旧索引的摘要落盘并清 dirty），再摘除。

// SearchIndexRegistry 管理 workspacePath → *searchIndex 的映射及其生命周期。
type SearchIndexRegistry struct {
	mu      sync.Mutex
	indexes map[string]*searchIndex
}

// NewSearchIndexRegistry 创建一个空的索引注册表。
func NewSearchIndexRegistry() *SearchIndexRegistry {
	return &SearchIndexRegistry{
		indexes: make(map[string]*searchIndex),
	}
}

// Get 获取工作区对应的索引，不存在则创建。
//
// 同一个 workspacePath 多次调用返回同一个实例（索引的增量更新与正文缓存
// 都依赖这个身份稳定性）。
func (r *SearchIndexRegistry) Get(workspacePath string) *searchIndex {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.indexes == nil {
		r.indexes = make(map[string]*searchIndex)
	}
	idx, ok := r.indexes[workspacePath]
	if !ok {
		idx = newSearchIndex()
		r.indexes[workspacePath] = idx
	}
	return idx
}

// Flush 把指定工作区的索引摘要强制落盘，但保留内存中的索引。
//
// 用于「应用退出 / 切走工作区但仍可能切回」等需要确保落盘、又不必丢弃缓存的时机。
// 索引不存在时返回 nil（不是错误）：没有索引自然没有需要落盘的东西。
func (r *SearchIndexRegistry) Flush(workspacePath string) error {
	r.mu.Lock()
	idx, ok := r.indexes[workspacePath]
	r.mu.Unlock()
	if !ok {
		return nil
	}
	idx.mu.Lock()
	idx.dirty = false
	idx.lastSave = time.Now()
	idx.mu.Unlock()
	return idx.SaveSummary(workspacePath)
}

// Evict 落盘并移除指定工作区的索引，释放其内存。
//
// 与 Flush 的区别：Flush 只写盘不释放；Evict 写盘后从表中摘除，
// 下次访问该工作区会重建索引（磁盘摘要存在时从摘要恢复，不必全量重扫）。
func (r *SearchIndexRegistry) Evict(workspacePath string) error {
	r.mu.Lock()
	idx, ok := r.indexes[workspacePath]
	if !ok {
		r.mu.Unlock()
		return nil
	}
	delete(r.indexes, workspacePath)
	r.mu.Unlock()

	// 摘除后再落盘，避免持锁做 IO（SaveSummary 会读索引并写文件）。
	idx.mu.Lock()
	idx.dirty = false
	idx.lastSave = time.Now()
	idx.mu.Unlock()
	return idx.SaveSummary(workspacePath)
}

// FlushAll 落盘所有工作区的索引，保留内存。
// 用于应用退出前统一持久化。
func (r *SearchIndexRegistry) FlushAll() error {
	for _, p := range r.Paths() {
		if err := r.Flush(p); err != nil {
			return fmt.Errorf("flush index %s: %w", p, err)
		}
	}
	return nil
}

// EvictAll 落盘并移除全部索引。主要用于测试隔离。
func (r *SearchIndexRegistry) EvictAll() error {
	var firstErr error
	for _, p := range r.Paths() {
		if err := r.Evict(p); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Paths 返回当前持有索引的工作区路径（已排序，便于断言与日志稳定）。
func (r *SearchIndexRegistry) Paths() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	paths := make([]string, 0, len(r.indexes))
	for p := range r.indexes {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// Len 返回当前持有的索引数量。
func (r *SearchIndexRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.indexes)
}

// ---------------------------------------------------------------------------
// 包级默认注册表
// ---------------------------------------------------------------------------
//
// 存在理由：service 包内仍有一批零值可用的 Service（测试里直接 &QnAService{}），
// 它们拿不到容器注入的注册表。与其让它们 panic 或静默失效，不如给一个包级
// 兜底注册表——语义与旧版全局 map 一致，但现在它是一个显式对象，
// 有 Get/Evict/Flush 的清晰边界，测试也能用 EvictAll 干净地隔离。
//
// 生产路径（经 container.go 装配）不走这里：三个 Service 都持有注入的实例。

// defaultIndexRegistry 包级兜底注册表。
var defaultIndexRegistry = NewSearchIndexRegistry()

// getSearchIndex 按 workspacePath 从默认注册表获取或创建索引。
//
// 仅用于未注入注册表的零值 Service；注入过的 Service 应使用自身的 indexes 字段。
func getSearchIndex(workspacePath string) *searchIndex {
	return defaultIndexRegistry.Get(workspacePath)
}

// ClearSearchIndex 清除指定工作区的搜索索引（兼容入口，转发到默认注册表）。
func ClearSearchIndex(workspacePath string) {
	_ = defaultIndexRegistry.Evict(workspacePath)
}

// ClearAllSearchIndexes 清除所有搜索索引（用于测试，转发到默认注册表）。
func ClearAllSearchIndexes() {
	_ = defaultIndexRegistry.EvictAll()
}

// FlushSearchIndex 强制把指定工作区的索引摘要落盘（转发到默认注册表）。
func FlushSearchIndex(workspacePath string) error {
	return defaultIndexRegistry.Flush(workspacePath)
}
