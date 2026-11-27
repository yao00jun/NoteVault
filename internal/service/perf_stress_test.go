package service

import (
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 大库压测：真实容量数据
//
// 默认跳过，仅当 NV_STRESS_TEST=1 时运行（慢，且会往临时目录写约 50MB 笔记）：
//
//	NV_STRESS_TEST=1 go test ./internal/service/ -run TestStressIndexLargeVault -v -count=1 -timeout 20m
//
// 测什么：20000 篇文档（每篇 1.5~3KB，中文为主）的
//   - 建索引耗时、峰值内存（runtime.ReadMemStats 的 HeapAlloc / Sys）、常驻内存；
//   - 索引摘要（Gob）序列化后的字节数；
//   - 10 个典型查询的 P50 / P95 延迟（**全链路**：含 walk 全库与即时 snippet）；
//
// 这里刻意不测「检索内核」的延迟：本机 time.Now() 的最小非零步进约 525µs
//（实测 20 万次采样 199998 次读数为 0），亚毫秒的 scoreBM25 会被量化成
// 0 或 0.5ms，得出「P50 = 0µs、P95 = 500µs」这种双峰假数据。
// 内核延迟看 BenchmarkSearch 的 bm25/* 分项（testing.B 内部多轮折算，不受此限）。
//   - 落盘后重新加载的耗时。
//
// 分两段：
//  1. 真实路径——直接调 refresh。2026-11 起 maxScanFiles=50000，20000 篇全量
//     可索引（scanComplete=true）；上限仅剩防病态护栏作用，这一段反映当前版本的真实行为；
//  2. 推算路径——绕过 walk 直接把 20000 篇灌进索引（stressPopulateUncapped），
//     用来回答「上限再放开之后」的容量问题。它不复刻正文的常驻缓存，内存一项偏低。
//
// 清理：笔记目录用 t.TempDir()；索引摘要落在 %APPDATA% 而非工作区，显式删除。
// ---------------------------------------------------------------------------

const (
	stressDocCount = 20000

	// 查询延迟：每个查询先预热再计时。
	// 轮数取 20 而不是 100：全链路 Search 在没有 watcher 时**每次都要 walk 全库**
	//（2 万次 stat），5000 篇档实测单次约 400ms，100 轮 × 10 查询 × 4 个阶段
	// 会把整轮压测顶到 20 分钟超时。20 轮 × 10 查询 = 200 个样本，算 P95 足够。
	stressQueryWarmup = 5
	stressQueryRounds = 20
)

// stressQueries 压测用的 10 个典型查询：perfQueries 的 5 个分层查询 + 5 个主题字面查询。
var stressQueries = []perfQuery{
	{"rare", fmt.Sprintf("%04d", perfRareSeq)},
	{"topic", "缓存失效"},
	{"multi", "缓存 过期策略"},
	{"english", "TTL"},
	{"common", "实际项目"},
	{"topic-consensus", "分布式一致性"},
	{"topic-db", "数据库索引 执行计划"},
	{"topic-auth", "身份认证"},
	{"topic-vector", "向量检索"},
	{"topic-monitoring", "监控告警"},
}

func TestStressIndexLargeVault(t *testing.T) {
	if os.Getenv("NV_STRESS_TEST") != "1" {
		t.Skip("set NV_STRESS_TEST=1 to run the large-vault stress test (slow; writes ~50MB of temp notes)")
	}
	initGseSegmenter()
	stressLog(t, "=== 大库压测 ===")
	stressLog(t, "[环境] %s/%s NumCPU=%d 文档数=%d", runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), stressDocCount)

	// ---------------------------------------------------------------------
	// 语料
	// ---------------------------------------------------------------------
	genStart := time.Now()
	docs := perfGenerateDocs(stressDocCount)
	var totalBytes uint64
	for i := range docs {
		totalBytes += uint64(len(docs[i].content))
	}
	genElapsed := time.Since(genStart)

	writeStart := time.Now()
	ws := perfWriteWorkspace(t, docs)
	stressLog(t, "[语料] 生成 %v；落盘 %v → %s", genElapsed, time.Since(writeStart), ws)
	stressLog(t, "[语料] 正文合计 %.1f MB，平均 %.0f 字节/篇", mb(totalBytes), float64(totalBytes)/float64(len(docs)))

	t.Cleanup(func() {
		if err := os.Remove(summaryPathFor(ws)); err != nil && !os.IsNotExist(err) {
			stressLog(t, "[清理] 删除索引摘要失败: %v", err)
			return
		}
		stressLog(t, "[清理] 索引摘要已删除；笔记目录由 t.TempDir 在测试结束后清理")
	})

	// ---------------------------------------------------------------------
	// 1. 真实路径：refresh（受 maxScanFiles 扫描上限约束）
	// ---------------------------------------------------------------------
	runtime.GC()
	base := readStressMem()
	sampler := startStressMemSampler()

	idx := newSearchIndex()
	buildStart := time.Now()
	scanComplete, err := idx.refresh(ws)
	if err != nil {
		t.Fatal(err)
	}
	buildElapsed := time.Since(buildStart)

	peak := sampler.Stop()
	runtime.GC()
	resident := readStressMem().since(base)
	docCount, tokenCount := idx.stats()

	stressLog(t, "[真实路径] 建索引 %v", buildElapsed)
	stressLog(t, "[真实路径] 实际索引 %d 篇 / %d 个 token；磁盘上有 %d 篇；scanComplete=%v",
		docCount, tokenCount, len(docs), scanComplete)
	if !scanComplete {
		stressLog(t, "[真实路径] ⚠ 触及 maxScanFiles=%d 扫描上限，%d 篇未被索引（搜不到）",
			maxScanFiles, len(docs)-docCount)
	}
	stressLog(t, "[真实路径] 内存：峰值 HeapAlloc +%.1f MB / Sys +%.1f MB；常驻（GC 后 HeapAlloc 增量）%.1f MB，%.0f 字节/篇（含正文缓存，上限 %.0f MB）",
		mb(peak.heapAlloc-base.heapAlloc), mb(peak.sys-base.sys), mb(resident.heapAlloc),
		float64(resident.heapAlloc)/float64(docCount), float64(defaultContentBudgetBytes)/(1<<20))

	// 摘要落盘
	saveStart := time.Now()
	if err := idx.SaveSummary(ws); err != nil {
		t.Fatal(err)
	}
	saveElapsed := time.Since(saveStart)
	realSummaryBytes := summarySize(t, ws)
	stressLog(t, "[真实路径] 摘要落盘 %v，%.2f MB（%.0f 字节/篇，Gob）", saveElapsed,
		mb(realSummaryBytes), float64(realSummaryBytes)/float64(docCount))

	// 查询（稳态：正文已在内存里）。summaryLoaded 置位是为了让 Search 入口跳过 LoadSummary——
	// 否则它会把刚落盘的摘要再灌一遍，用「不带正文的文档」换掉带正文的，稳态就变成读盘延迟了。
	idx.summaryLoaded = true
	reg := NewSearchIndexRegistry()
	reg.indexes[ws] = idx
	svc := NewSearchServiceWithRegistry(NewFileService(), reg)
	measureStressQueries(t, "真实·稳态", svc.Search, ws)

	// 重载
	reloaded, loadElapsed, reloadElapsed, reloadedDocs := stressReload(t, ws)
	stressLog(t, "[真实路径] 重载：LoadSummary %v（%d 篇）+ refresh %v = 冷启动合计 %v",
		loadElapsed, reloadedDocs, reloadElapsed, loadElapsed+reloadElapsed)

	// 重载后的查询：正文不在内存，命中结果按需读盘——这才是每次启动后首批查询的真实延迟
	reg = NewSearchIndexRegistry()
	reg.indexes[ws] = reloaded
	svc = NewSearchServiceWithRegistry(NewFileService(), reg)
	measureStressQueries(t, "真实·重载后", svc.Search, ws)

	// 上限的直接证据：按目录名排序靠后的主题整个没进索引
	if !scanComplete {
		late := stressQueries[len(stressQueries)-1]
		if hits := countHits(t, svc.Search, ws, late.text); hits == 0 {
			stressLog(t, "[真实路径] ⚠ 查询 %q（磁盘上有 %d 篇相关笔记）命中 0 条——该主题目录排在扫描上限之后",
				late.text, stressDocCount/len(benchTopics))
		}
	}

	// ---------------------------------------------------------------------
	// 2. 推算路径：绕过扫描上限，20000 篇全部进索引
	// ---------------------------------------------------------------------
	if err := os.Remove(summaryPathFor(ws)); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	base = readStressMem()
	sampler = startStressMemSampler()

	uncapped := newSearchIndex()
	populateStart := time.Now()
	stressPopulateUncapped(t, uncapped, ws, docs)
	populateElapsed := time.Since(populateStart)

	peak = sampler.Stop()
	runtime.GC()
	resident = readStressMem().since(base)
	docCount, tokenCount = uncapped.stats()

	stressLog(t, "[推算路径] 建索引 %v（%.1f µs/篇）", populateElapsed,
		float64(populateElapsed.Microseconds())/float64(docCount))
	stressLog(t, "[推算路径] 索引 %d 篇 / %d 个 token", docCount, tokenCount)
	stressLog(t, "[推算路径] 内存：峰值 HeapAlloc +%.1f MB / Sys +%.1f MB；常驻 %.1f MB，%.0f 字节/篇（不含正文缓存）",
		mb(peak.heapAlloc-base.heapAlloc), mb(peak.sys-base.sys), mb(resident.heapAlloc),
		float64(resident.heapAlloc)/float64(docCount))

	saveStart = time.Now()
	if err := uncapped.SaveSummary(ws); err != nil {
		t.Fatal(err)
	}
	saveElapsed = time.Since(saveStart)
	uncappedSummaryBytes := summarySize(t, ws)
	stressLog(t, "[推算路径] 摘要落盘 %v，%.2f MB（%.0f 字节/篇，Gob）", saveElapsed,
		mb(uncappedSummaryBytes), float64(uncappedSummaryBytes)/float64(docCount))

	uncapped.summaryLoaded = true
	reg = NewSearchIndexRegistry()
	reg.indexes[ws] = uncapped
	svc = NewSearchServiceWithRegistry(NewFileService(), reg)
	measureStressQueries(t, "推算·稳态", svc.Search, ws)

	reloaded, loadElapsed, reloadElapsed, reloadedDocs = stressReload(t, ws)
	stressLog(t, "[推算路径] 重载：LoadSummary %v（%d 篇）+ refresh %v = 冷启动合计 %v",
		loadElapsed, reloadedDocs, reloadElapsed, loadElapsed+reloadElapsed)

	reg = NewSearchIndexRegistry()
	reg.indexes[ws] = reloaded
	svc = NewSearchServiceWithRegistry(NewFileService(), reg)
	measureStressQueries(t, "推算·重载后", svc.Search, ws)
}

// stressPopulateUncapped 把 docs 直接灌进索引，绕过 refresh 的 maxScanFiles 扫描上限。
//
// 只复刻 refresh 阶段 3/4 的「读盘 → 提取标题 → 分词 → 词频 → 写倒排」，正文不留驻
// （refresh 会把正文留到预算为止，这里不计）。用途仅是推算「上限放开后」的容量，
// 不代表当前版本的真实行为。
func stressPopulateUncapped(t *testing.T, idx *searchIndex, ws string, docs []perfDoc) {
	t.Helper()
	for _, d := range docs {
		absPath := filepath.Join(ws, filepath.FromSlash(d.rel))
		data, err := os.ReadFile(absPath)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(absPath)
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)

		// 标题提取与 refresh 同口径：首个 "# " 行，没有则用文件名
		title := filepath.Base(d.rel)
		for _, line := range strings.Split(content, "\n") {
			if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "# ") {
				title = strings.TrimPrefix(trimmed, "# ")
				break
			}
		}

		tokens := tokenizeIndex(content)
		doc := &cachedDoc{
			relPath:   d.rel,
			absPath:   absPath,
			modTime:   info.ModTime(),
			title:     title,
			tokenFreq: newTokenTable(tokens),
			titleFreq: freqOf(tokenizeIndex(title)),
			docLen:    len(tokens),
			lastUsed:  time.Now().UnixNano(),
		}
		idx.mu.Lock()
		idx.addPostingsLocked(d.rel, doc.tokenFreq)
		idx.docs[d.rel] = doc
		idx.totalDocLen += int64(doc.docLen)
		idx.mu.Unlock()
	}
}

// stressReload 模拟进程重启后的冷启动：新索引从摘要恢复 + 一次 refresh（按 modtime 对齐，
// 未变更的文档不重新分词）。返回恢复后的索引、LoadSummary 耗时、refresh 耗时、恢复出的文档数。
func stressReload(t *testing.T, ws string) (idx *searchIndex, loadElapsed, refreshElapsed time.Duration, docs int) {
	t.Helper()
	idx = newSearchIndex()
	loadStart := time.Now()
	if err := idx.LoadSummary(ws); err != nil {
		t.Fatal(err)
	}
	loadElapsed = time.Since(loadStart)
	docs, _ = idx.stats()

	refreshStart := time.Now()
	if _, err := idx.refresh(ws); err != nil {
		t.Fatal(err)
	}
	return idx, loadElapsed, time.Since(refreshStart), docs
}

// measureStressQueries 对每个查询预热后计时 stressQueryRounds 轮，打印 P50 / P95。
func measureStressQueries(t *testing.T, label string, search func(ws, query string) ([]*SearchResult, error), ws string) {
	t.Helper()
	var all []float64
	for _, q := range stressQueries {
		hits := countHits(t, search, ws, q.text) // 预热
		latencies := make([]float64, stressQueryRounds)
		for i := range latencies {
			start := time.Now()
			hits = countHits(t, search, ws, q.text)
			latencies[i] = float64(time.Since(start).Nanoseconds()) / 1e6
		}
		sort.Float64s(latencies)
		all = append(all, latencies...)
		stressLog(t, "[查询·%s] %-18q 命中 %5d  P50 %7.2f ms  P95 %7.2f ms  max %7.2f ms",
			label, q.text, hits, percentile(latencies, 0.50), percentile(latencies, 0.95), latencies[len(latencies)-1])
	}
	sort.Float64s(all)
	stressLog(t, "[查询·%s] 汇总 %d 次：P50 %.2f ms  P95 %.2f ms", label, len(all), percentile(all, 0.50), percentile(all, 0.95))
}

func countHits(t *testing.T, search func(ws, query string) ([]*SearchResult, error), ws, query string) int {
	t.Helper()
	res, err := search(ws, query)
	if err != nil {
		t.Fatalf("查询 %q 失败: %v", query, err)
	}
	return len(res)
}

// summarySize 返回工作区索引摘要文件的字节数。
func summarySize(t *testing.T, ws string) uint64 {
	t.Helper()
	info, err := os.Stat(summaryPathFor(ws))
	if err != nil {
		t.Fatal(err)
	}
	return uint64(info.Size())
}

// percentile 最近秩法：已排序样本的第 p 分位。
func percentile(sorted []float64, p float64) float64 {
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}

func mb(bytes uint64) float64 {
	return float64(bytes) / (1 << 20)
}

func stressLog(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Logf(format, args...)
	log.Printf(format, args...)
}

// ---------------------------------------------------------------------------
// 内存采样
// ---------------------------------------------------------------------------

// stressMem 某一时刻的内存快照。
type stressMem struct {
	heapAlloc uint64
	sys       uint64
}

// since 相对基线的增量。
func (m stressMem) since(base stressMem) stressMem {
	return stressMem{heapAlloc: m.heapAlloc - base.heapAlloc, sys: m.sys - base.sys}
}

func readStressMem() stressMem {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return stressMem{heapAlloc: ms.HeapAlloc, sys: ms.Sys}
}

// stressMemSampler 每 10ms 采样一次 HeapAlloc / Sys 并记录峰值。
// ReadMemStats 会短暂 STW，采样对建索引耗时有一点点干扰（量级远小于 10ms 间隔的抖动）。
type stressMemSampler struct {
	stop chan struct{}
	done chan struct{}
	mu   sync.Mutex
	peak stressMem
}

func startStressMemSampler() *stressMemSampler {
	s := &stressMemSampler{stop: make(chan struct{}), done: make(chan struct{}), peak: readStressMem()}
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				m := readStressMem()
				s.mu.Lock()
				s.peak.heapAlloc = max(s.peak.heapAlloc, m.heapAlloc)
				s.peak.sys = max(s.peak.sys, m.sys)
				s.mu.Unlock()
			}
		}
	}()
	return s
}

// Stop 停止采样并返回峰值。
func (s *stressMemSampler) Stop() stressMem {
	close(s.stop)
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peak
}
