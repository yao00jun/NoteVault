package service

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// 性能热点基准（性能专项）
//
// 覆盖 searchindex / qnaservice / snapshotservice 三处热点：
//   - 分词：tokenizeIndex（gse CutAll，索引与查询的真实路径）与 tokenize（bigram 兜底）；
//   - 建索引：100 / 1000 / 5000 三档（5000 是 maxScanFiles 的扫描上限）；
//   - 索引摘要落盘 / 重载（代码注释里点名的「最主要性能热点」）；
//   - BM25 打分与关键词检索（纯内存 kernel 与 SearchService.Search 全链路各一组）；
//   - 混合检索 HybridSearch（纯 BM25 与「BM25 + 向量」两条 arm，向量端点用
//     retrieval_eval_test.go 的确定性 syntheticEmbedder，不依赖 Ollama）；
//   - 快照 diff（行级 LCS）与回滚。
//
// 夹具约定：
//   - 语料由固定种子的 math/rand 生成（perfSeed），任何机器上逐字节相同；
//   - 词汇表复用 searchbench_test.go 的 benchTopics / benchNoise（20 个主题簇），
//     不另造一套词表；
//   - 现有 helper（writeMd / mustWrite / writeTestFile）只收 *testing.T，Benchmark
//     用不了，所以本文件自带一个 testing.TB 版的落盘函数。
//
// 跑法见 benchmarks/README.md。
// ---------------------------------------------------------------------------

// perfSeed 性能夹具的固定随机种子。刻意不用 time.Now：基线要能跨机器、跨次运行复现。
const perfSeed = 20261126

// perfSizes 建索引 / 检索基准的三档文档数。
var perfSizes = []struct {
	name string
	docs int
}{
	{"100", 100},
	{"1000", 1000},
	{"5000", 5000},
}

// perfDoc 一篇生成好的仿真笔记。
type perfDoc struct {
	rel     string // 工作区相对路径（正斜杠）
	content string // Markdown 正文
}

// perfAllDocs 一次性生成最大档（5000 篇）并缓存；小档直接取前缀切片。
// 生成器不依赖文档总数，所以「100 档」恰好是「5000 档」的前 100 篇，各档语料嵌套一致。
var perfAllDocs = sync.OnceValue(func() []perfDoc { return perfGenerateDocs(perfSizes[len(perfSizes)-1].docs) })

// perfEnglishTerms 技术笔记里常见的英文术语，模拟中英混排的真实写法。
var perfEnglishTerms = []string{
	"context.Context", "goroutine", "backoff", "idempotency", "checkpoint",
	"throughput", "middleware", "SLA", "pipeline", "registry",
}

// perfRareSeq 「稀有词」查询指向的文档序号（其 frontmatter 里的 id 在全库唯一）。
const perfRareSeq = 7

// perfQuery 一条基准查询。text 全部是语料里真实出现的词。
type perfQuery struct {
	name string
	text string
}

// perfQueries 按命中面（倒排表长度）分层的查询集，从最稀疏到最稠密：
//
//	rare    单篇文档独有的编号 → 倒排表长度 1
//	topic   主题关键词          → 约 1/20 的文档（外加少量跨主题串扰）
//	multi   两个主题词 + 空格   → 多 token 查询
//	english 英文术语            → 单一主题内的英文 token
//	common   所有文档共有的噪音词 → 候选集 ≈ 全库（最坏情况）
var perfQueries = []perfQuery{
	{"rare", fmt.Sprintf("%04d", perfRareSeq)},
	{"topic", benchTopics[0].literal},
	{"multi", benchTopics[0].multi},
	{"english", "TTL"},
	{"common", "实际项目"},
}

// sink 变量：接收基准循环的产物，防止编译器把被测调用整体优化掉。
var (
	perfSinkTokens       []string
	perfSinkDocCount     int
	perfSinkTokenCount   int
	perfSinkResults      []*SearchResult
	perfSinkDiff         *DiffResult
	perfSinkSnapshotDiff *SnapshotDiff
	perfSinkRestoreBytes int64
)

// perfGenerateDocs 生成 n 篇可复现的仿真笔记：中文为主，带 frontmatter、标题、
// 英文术语与 Markdown 标记（小节标题 / 任务列表 / 代码块 / 引用 / 双链），
// 每篇 1.5~3KB，按主题簇分目录。
func perfGenerateDocs(n int) []perfDoc {
	rng := rand.New(rand.NewSource(perfSeed))
	docs := make([]perfDoc, 0, n)
	for i := 0; i < n; i++ {
		topic := benchTopics[i%len(benchTopics)]
		docs = append(docs, perfDoc{
			rel:     fmt.Sprintf("%s/note-%05d.md", topic.dir, i),
			content: perfBuildDoc(rng, topic, i, 1500+rng.Intn(1500)),
		})
	}
	return docs
}

// perfBuildDoc 生成一篇笔记正文，长度写到约 targetBytes 字节为止。
func perfBuildDoc(rng *rand.Rand, topic benchTopic, seq, targetBytes int) string {
	kw := topic.keywords
	pick := func() string { return kw[rng.Intn(len(kw))] }
	noise := func() string { return benchNoise[rng.Intn(len(benchNoise))] }

	var b strings.Builder
	b.Grow(targetBytes + 512)
	fmt.Fprintf(&b, "---\ntags: [%s]\nid: note-%04d\n---\n\n", strings.Join(topic.tags, ", "), seq)
	fmt.Fprintf(&b, "# %s（%d）\n\n", kw[0], seq)
	fmt.Fprintf(&b, "本文讨论%s相关的实践，重点记录 `%s` 与 **%s** 的取舍。\n\n",
		kw[0], perfEnglishTerms[rng.Intn(len(perfEnglishTerms))], kw[1])

	for b.Len() < targetBytes {
		switch rng.Intn(10) {
		case 0:
			fmt.Fprintf(&b, "\n## %s：%s\n\n", pick(), noise())
		case 1:
			fmt.Fprintf(&b, "- %s\n- [ ] 待办：确认%s的边界条件\n- [x] 已完成：阅读%s的官方文档\n\n",
				noise(), pick(), pick())
		case 2:
			fmt.Fprintf(&b, "```go\n// %s 的最小示例\nfunc handle(ctx context.Context) error {\n\treturn withRetry(ctx, backoff)\n}\n```\n\n",
				pick())
		case 3:
			fmt.Fprintf(&b, "> %s\n\n相关：[[%s（%d）]]、[[%s]]\n\n", noise(), kw[0], seq+1, pick())
		default:
			b.WriteString(noise())
			b.WriteString(noise())
			if rng.Intn(4) == 0 {
				b.WriteString("核心是" + pick() + "。")
			}
			if rng.Intn(20) == 0 {
				// 跨主题串扰：让主题词的倒排表不只含本主题文档
				b.WriteString("另外也涉及" + benchTopics[rng.Intn(len(benchTopics))].keywords[0] + "的内容。")
			}
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

// perfWriteWorkspace 把 docs 落盘成一个临时工作区（测试/基准结束自动清理）。
func perfWriteWorkspace(tb testing.TB, docs []perfDoc) string {
	tb.Helper()
	ws := tb.TempDir()
	for _, d := range docs {
		full := filepath.Join(ws, filepath.FromSlash(d.rel))
		if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
			tb.Fatalf("创建夹具目录失败: %v", err)
		}
		if err := os.WriteFile(full, []byte(d.content), 0644); err != nil {
			tb.Fatalf("写入夹具失败: %v", err)
		}
	}
	return ws
}

// perfWriteFile 在工作区内写单个文件（testing.TB 版，供 Benchmark 使用）。
func perfWriteFile(tb testing.TB, ws, rel, content string) {
	tb.Helper()
	full := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
		tb.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		tb.Fatalf("写入文件失败: %v", err)
	}
}

// perfRemoveSummaryOnExit 在测试/基准结束时删除工作区对应的索引摘要。
//
// 摘要落在 %APPDATA%/NoteVault/search-index 而不是工作区目录里，TempDir 的清理
// 够不着它；不显式删的话每跑一次基准就残留一个（5000 文档约 8MB 的）文件。
func perfRemoveSummaryOnExit(tb testing.TB, ws string) {
	tb.Helper()
	tb.Cleanup(func() { _ = os.Remove(summaryPathFor(ws)) })
}

// ---------------------------------------------------------------------------
// 分词
// ---------------------------------------------------------------------------

type perfText struct {
	name string
	text string
}

// perfTokenizeTexts 分词基准的四段输入。长中文 / 混排 / 纯英文都拉到 2KB 量级，
// 对齐一篇笔记的典型段落；短中文单独一组，看固定开销。
var perfTokenizeTexts = func() []perfText {
	rng := rand.New(rand.NewSource(perfSeed))
	kw := benchTopics[0].keywords
	enTerms := perfEnglishTerms

	var zh, en strings.Builder
	for zh.Len() < 2048 {
		zh.WriteString(benchNoise[rng.Intn(5)]) // benchNoise 前 5 句是中文
		zh.WriteString("核心是" + kw[rng.Intn(len(kw))] + "。")
	}
	for en.Len() < 2048 {
		en.WriteString(benchNoise[5+rng.Intn(5)]) // 后 5 句是英文
		en.WriteString("The " + enTerms[rng.Intn(len(enTerms))] + " setting matters here. ")
	}
	return []perfText{
		{"short-zh", "缓存失效是分布式系统里的经典难题，常见做法是给条目设置 TTL，到期自动淘汰。"},
		{"long-zh", zh.String()},
		{"mixed", perfAllDocs()[perfRareSeq].content},
		{"en", en.String()},
	}
}()

// BenchmarkTokenizeIndex 索引侧 / 查询侧的真实分词路径（gse CutAll）。
func BenchmarkTokenizeIndex(b *testing.B) {
	initGseSegmenter() // 预热词典，别把一次性加载算进首次迭代
	for _, tt := range perfTokenizeTexts {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tt.text)))
			for i := 0; i < b.N; i++ {
				perfSinkTokens = tokenizeIndex(tt.text)
			}
		})
	}
}

// BenchmarkTokenizeBigram bigram 兜底分词（gse 初始化失败时才走），作为对照。
func BenchmarkTokenizeBigram(b *testing.B) {
	for _, tt := range perfTokenizeTexts {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tt.text)))
			for i := 0; i < b.N; i++ {
				perfSinkTokens = tokenize(tt.text)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 建索引与索引摘要
// ---------------------------------------------------------------------------

// BenchmarkIndexBuild 冷启动全量建索引：每轮迭代都从空索引 walk + 读盘 + 分词。
func BenchmarkIndexBuild(b *testing.B) {
	initGseSegmenter()
	for _, size := range perfSizes {
		ws := perfWriteWorkspace(b, perfAllDocs()[:size.docs])
		b.Run(size.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				idx := newSearchIndex()
				if _, err := idx.refresh(ws); err != nil {
					b.Fatal(err)
				}
				perfSinkDocCount, perfSinkTokenCount = idx.stats()
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(size.docs), "ns/doc")
		})
	}
}

// BenchmarkIndexSummary 索引摘要（Gob）的落盘与重载。
func BenchmarkIndexSummary(b *testing.B) {
	for _, size := range perfSizes {
		ws := perfWriteWorkspace(b, perfAllDocs()[:size.docs])
		perfRemoveSummaryOnExit(b, ws)
		idx := newSearchIndex()
		if _, err := idx.refresh(ws); err != nil {
			b.Fatal(err)
		}

		b.Run("save/"+size.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := idx.SaveSummary(ws); err != nil {
					b.Fatal(err)
				}
			}
		})
		info, err := os.Stat(summaryPathFor(ws))
		if err != nil {
			b.Fatal(err)
		}

		b.Run("load/"+size.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				loaded := newSearchIndex()
				if err := loaded.LoadSummary(ws); err != nil {
					b.Fatal(err)
				}
				perfSinkDocCount, perfSinkTokenCount = loaded.stats()
			}
			b.ReportMetric(float64(info.Size()), "summary-B")
		})
	}
}

// ---------------------------------------------------------------------------
// 检索
// ---------------------------------------------------------------------------

// BenchmarkSearch 关键词检索。两条 arm：
//
//	bm25   scoreBM25 单独计时——纯内存打分 kernel（含排序、截断到结果上限）；
//	search SearchService.Search 全链路——另含 refresh 的全库 walk、词面计数与
//	       前 50 条结果的 snippet 生成。
func BenchmarkSearch(b *testing.B) {
	initGseSegmenter()
	for _, size := range perfSizes {
		ws := perfWriteWorkspace(b, perfAllDocs()[:size.docs])
		perfRemoveSummaryOnExit(b, ws) // Search 会按需把摘要落盘
		reg := NewSearchIndexRegistry()
		idx := reg.Get(ws)
		svc := NewSearchServiceWithRegistry(NewFileService(), reg)

		for _, q := range perfQueries {
			queryTokens := tokenizeQuery(strings.ToLower(q.text))

			b.Run("bm25/"+size.name+"/"+q.name, func(b *testing.B) {
				// setup：首次调用负责建索引，并记下命中数；ResetTimer 之后只测查询
				if _, err := idx.refresh(ws); err != nil {
					b.Fatal(err)
				}
				idx.mu.RLock()
				hits := len(idx.scoreBM25(queryTokens, defaultMaxSearchResults))
				idx.mu.RUnlock()
				b.ResetTimer()
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					idx.mu.RLock()
					scored := idx.scoreBM25(queryTokens, defaultMaxSearchResults)
					idx.mu.RUnlock()
					perfSinkDocCount = len(scored)
				}
				b.ReportMetric(float64(hits), "hits")
			})

			b.Run("search/"+size.name+"/"+q.name, func(b *testing.B) {
				// setup：首次调用负责建索引，并记下命中数；ResetTimer 之后只测查询
				res, err := svc.Search(ws, q.text)
				if err != nil {
					b.Fatal(err)
				}
				hits := len(res)
				b.ResetTimer()
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					res, err := svc.Search(ws, q.text)
					if err != nil {
						b.Fatal(err)
					}
					perfSinkResults = res
				}
				b.ReportMetric(float64(hits), "hits")
			})
		}
	}
}

// BenchmarkHybridSearch 文档级混合检索（BM25 + 向量，RRF 融合）。
//
// 向量端点用 retrieval_eval_test.go 的 syntheticEmbedder（字符 bigram 哈希向量，
// 确定性、无外部依赖）；向量库走真实 chromem 持久化路径，在 setup 阶段建好。
func BenchmarkHybridSearch(b *testing.B) {
	initGseSegmenter()
	arms := []struct {
		name     string
		model    string
		embedder Embedder
	}{
		{"bm25-only", "", &NoopEmbedder{}},
		{"hybrid", "synthetic-bigram", syntheticEmbedder{dim: 128}},
	}
	for _, size := range perfSizes {
		for _, arm := range arms {
			ws := perfWriteWorkspace(b, perfAllDocs()[:size.docs])
			perfRemoveSummaryOnExit(b, ws)
			svc := NewQnAServiceWithRegistry(NewFileServiceWithHistory(NewSnapshotService()), nil, arm.embedder, nil)

			for _, q := range perfQueries {
				b.Run(arm.name+"/"+size.name+"/"+q.name, func(b *testing.B) {
					// setup：首次调用负责建 BM25 索引与向量库；ResetTimer 之后只测查询
					res, err := svc.HybridSearch(ws, q.text, "synthetic://local", arm.model, "", RerankConfig{})
					if err != nil {
						b.Fatal(err)
					}
					hits := len(res)
					b.ResetTimer()
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						res, err := svc.HybridSearch(ws, q.text, "synthetic://local", arm.model, "", RerankConfig{})
						if err != nil {
							b.Fatal(err)
						}
						perfSinkResults = res
					}
					b.ReportMetric(float64(hits), "hits")
				})
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 快照 diff 与回滚
// ---------------------------------------------------------------------------

// perfDiffVersions 生成同一篇笔记的两个版本，共 lines 行、改动其中 changed 行。
//
// insert=false：原地替换（两版行数相同）；insert=true：在改动行后面追加一行备注
// （新版多出 changed 行）。改动位置由固定种子随机散布，贴近「多次编辑后的两个存点」。
func perfDiffVersions(lines, changed int, insert bool) (oldText, newText string) {
	rng := rand.New(rand.NewSource(perfSeed))
	kw := benchTopics[13].keywords

	oldLines := make([]string, lines)
	for i := range oldLines {
		oldLines[i] = fmt.Sprintf("%d. %s（%s）", i, benchNoise[rng.Intn(len(benchNoise))], kw[rng.Intn(len(kw))])
	}
	newLines := append([]string(nil), oldLines...)
	for _, i := range rng.Perm(lines)[:changed] {
		if insert {
			newLines[i] += "\n> 新增备注：" + kw[(i+1)%len(kw)]
		} else {
			newLines[i] = fmt.Sprintf("%d. 已修订：%s（%s）", i, benchNoise[(i+3)%len(benchNoise)], kw[(i+2)%len(kw)])
		}
	}
	return strings.Join(oldLines, "\n"), strings.Join(newLines, "\n")
}

// BenchmarkSnapshotDiff 行级 LCS diff 的纯计算部分（2000 行笔记）。
//
// 报告 truncated 指标：为 1 表示 DP 格子数超过 diffCellLimit，结果退化成
// 「整块删除 + 整块新增」而不是逐行对齐。
func BenchmarkSnapshotDiff(b *testing.B) {
	for _, c := range []struct {
		name   string
		insert bool
	}{
		{"replace-5pct", false},
		{"insert-5pct", true},
	} {
		oldText, newText := perfDiffVersions(2000, 100, c.insert)
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				perfSinkDiff = diffText(oldText, newText)
			}
			b.ReportMetric(boolToFloat(perfSinkDiff.Truncated), "truncated")
		})
	}

	same, _ := perfDiffVersions(2000, 0, false)
	b.Run("identical", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			perfSinkDiff = diffText(same, same)
		}
	})
}

// BenchmarkSnapshotService 快照服务的读路径：服务层 diff（含两次 gzip 对象读取、
// 索引 JSON 解析）与回滚（含安全备份、原子写回）。被比对的文档为 2000 行、替换 5%。
func BenchmarkSnapshotService(b *testing.B) {
	const rel = "notes/long-note.md"
	dir := b.TempDir()
	s := NewSnapshotService()
	oldText, newText := perfDiffVersions(2000, 100, false)

	perfWriteFile(b, dir, rel, oldText)
	v1, err := s.captureSnapshot(dir, rel, oldText, SnapshotReasonManual)
	if err != nil {
		b.Fatal(err)
	}
	perfWriteFile(b, dir, rel, newText)
	v2, err := s.captureSnapshot(dir, rel, newText, SnapshotReasonManual)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("DiffSnapshots", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			diff, err := s.DiffSnapshots(dir, v1.ID, v2.ID)
			if err != nil {
				b.Fatal(err)
			}
			perfSinkSnapshotDiff = diff
		}
	})

	b.Run("DiffWithCurrent", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			diff, err := s.DiffWithCurrent(dir, v1.ID)
			if err != nil {
				b.Fatal(err)
			}
			perfSinkSnapshotDiff = diff
		}
	})

	// 回滚到 v1。每轮迭代前把文件改回 v2（停表），保证每次回滚都有「当前内容」需要备份；
	// 备份内容与已有快照相同会被去重，索引不会随迭代增长。
	b.Run("RestoreSnapshot", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			perfWriteFile(b, dir, rel, newText)
			b.StartTimer()
			res, err := s.RestoreSnapshot(dir, v1.ID)
			if err != nil {
				b.Fatal(err)
			}
			perfSinkRestoreBytes = res.Bytes
		}
	})
}

// boolToFloat 把布尔指标转成 0/1，供 ReportMetric 使用。
func boolToFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}
