package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 检索质量评测：Recall@5 / MRR@5（P1-3 / P1-3b 的验收数字）
//
// 背景：P1-3（混合检索）与 P1-3b（Rerank）此前只有「编译通过」，没有召回数字。
// 按 docs 自己的规矩「没有数字等于没做」，这两项等于没验收。本文件提供可复跑
// 的评测口径，把「语义检索到底有没有用」变成可量化、可回归的结论。
//
//   - 语料：evalCorpus，14 篇中文/中英混排笔记，覆盖 8 个主题 + 干扰项
//   - 标注：evalQueries，14 条 query 带 qrels（相关文档 relPath），分三类：
//     字面（BM25 本该命中）/ 同义改写 / 跨语言（BM25 词面零重叠）
//   - 指标：Recall@5 = |相关 ∩ Top5| / |相关|
//           MRR@5    = mean(1 / 首个相关结果排名)，未进 Top5 记 0
//   - 分组（arm）：① BM25 基线 ② 混合检索 ③ 混合检索 + Rerank
//
// 两个入口：
//  1. TestRetrievalEval_Plumbing_SyntheticEmbedder —— 始终执行，用确定性合成向量
//     验证评测管线本身接通（向量 leg 确实参与 RRF 融合）。合成向量只有字面
//     n-gram 重合、**不具备真实语义**，故其数字不代表语义召回能力。
//  2. TestRetrievalEval_Ollama_BGEM3 —— 需 NV_OLLAMA_TEST=1 + 本机 Ollama +
//     bge-m3，产出真实召回数字，并断言「混合检索 Recall@5 不劣于 BM25 基线」。
//
// 环境变量（仅第 2 个入口）：
//
//	NV_EVAL_EMBED_BASE    embedding 端点，默认 http://127.0.0.1:11434/v1
//	NV_EVAL_EMBED_MODEL   embedding 模型，默认 bge-m3
//	NV_EVAL_RERANK_BASE   rerank 端点，默认空（需显式填写）
//	NV_EVAL_RERANK_MODEL  rerank 模型，默认空（空则跳过第 ③ 组）
//	NV_EVAL_RERANK_PROVIDER  rerank provider，默认 cohere
//	NV_EVAL_RERANK_APIKEY    rerank API Key（provider 为非本机端点时必须，如硅基流动 sk-...）
// ---------------------------------------------------------------------------

// evalCorpus 是评测语料：relPath → Markdown 正文。
//
// 刻意让「跨语言」类 query 在正文里没有对应英文词面（缓存文档不出现 cache、
// 性能文档不出现 profile/memory、错误文档不出现 retry/backoff），否则 BM25
// 会靠字面命中，评测就失去区分度，量不出语义检索的真实增益。
var evalCorpus = map[string]string{
	"go-concurrency.md": `# Go 并发编程

Go 用 goroutine 实现并发，它是轻量级线程，由运行时调度。
channel 用于多个 goroutine 之间传递数据，select 可以同时等待多个 channel。
sync.WaitGroup 用来等待一组任务全部结束。`,

	"cache-invalidation.md": `# 缓存失效

缓存失效是分布式系统里的经典难题。常见做法包括给条目设置 TTL，到期自动淘汰；
容量不足时用 LRU 淘汰最近最少使用的数据；写穿与写回决定数据一致性的强弱。
此外还要防范穿透、击穿与雪崩三种典型故障。`,

	"db-index.md": `# 数据库索引

查询变慢时优先看执行计划。B+ 树索引适合范围扫描，覆盖索引可以避免回表，
联合索引遵循最左前缀原则。索引并非越多越好，过多会拖慢写入速度。`,

	"deploy-ops.md": `# 部署与运维

systemd 能在进程崩溃后自动重新拉起，并支持开机自启。
nginx 作为反向代理把请求转发到后端端口。滚动更新配合健康检查，
可以在用户无感知的情况下完成发版。`,

	"vector-search.md": `# 向量检索

把文本 embedding 成向量之后，可以用余弦相似度衡量语义距离。
HNSW 是常见的近似最近邻索引结构，在召回率与查询延迟之间做权衡。
这类检索擅长处理字面写法不一样、但含义接近的内容。`,

	"auth-jwt.md": `# 身份认证

JWT 把用户身份签进令牌里，服务端无需保存状态即可校验。
OAuth2 用于第三方授权，刷新令牌可在访问令牌过期后续期。
会话管理还要处理主动登出与令牌吊销。`,

	"testing-strategy.md": `# 测试策略

单元测试覆盖纯函数与边界条件，集成测试验证模块之间的真实交互。
回归测试用于确认改动没有破坏既有行为。覆盖率是参考指标而非目标。
mock 与打桩用来隔离外部依赖。`,

	"error-handling.md": `# 错误处理

错误要逐层包装以保留上下文。调用外部接口失败时可以考虑再次发起调用，
但需要指数退避并加抖动，避免把下游打垮。连续失败超过阈值应触发熔断。`,

	"git-workflow.md": `# Git 协作

主干开发配合短生命周期分支。rebase 可以把一串提交整理成线性历史，
cherry-pick 能把某个提交单独搬到别的分支上。合并之前要做代码评审。`,

	"perf-profiling.md": `# 性能剖析

pprof 可以采集 CPU 与堆内存的剖面数据，火焰图用于定位热点函数。
基准测试用来量化优化效果。频繁分配临时对象会加重垃圾回收压力。`,

	"frontend-state.md": `# 前端状态管理

多个组件需要共享数据时，可以把状态提升到公共父组件，或使用 Pinia 这类
集中式状态库。响应式系统会追踪依赖，派生数据应当用计算属性而不是手动同步。`,

	"shopping.md": `# 购物清单

牛奶、鸡蛋、面包、咖啡豆、西红柿。`,

	"meeting-notes.md": `# 周会记录

本周讨论了下季度排期，确定了评审时间点。下周继续跟进插件市场的接入方案。`,

	"notes/term.md": `# 术语表

goroutine：Go 语言中的轻量级线程。
channel：多个 goroutine 之间传递数据的管道。
TTL：存活时间，到期之后自动失效。`,
}

// 评测样本的类别标签。
const (
	evalKindLiteral    = "字面"
	evalKindParaphrase = "同义改写"
	evalKindCrossLing  = "跨语言"
)

// evalQuery 是一条标注样本。relevant 是人工标注的相关文档 relPath（qrels）。
type evalQuery struct {
	query    string
	relevant []string
	kind     string
}

// evalQueries 是评测集。三类样本的混合比例决定评测的区分度：
// 字面类 BM25 本该命中（用来确认基线没坏），同义改写与跨语言类才是
// 语义检索应当拉开差距的地方。
var evalQueries = []evalQuery{
	// —— 字面：BM25 应能命中，用于确认基线可用 ——
	{"goroutine 与 channel 的用法", []string{"go-concurrency.md"}, evalKindLiteral},
	{"缓存雪崩怎么防", []string{"cache-invalidation.md"}, evalKindLiteral},
	{"购物清单上都有什么", []string{"shopping.md"}, evalKindLiteral},
	{"TTL 代表什么意思", []string{"cache-invalidation.md", "notes/term.md"}, evalKindLiteral},

	// —— 同义改写：词面部分重叠，BM25 可能命中也可能漏 ——
	{"数据库查询变慢要怎么优化", []string{"db-index.md"}, evalKindParaphrase},
	{"服务崩了怎么让它自己起来", []string{"deploy-ops.md"}, evalKindParaphrase},
	{"怎么确保改动不会破坏既有行为", []string{"testing-strategy.md"}, evalKindParaphrase},
	{"把某个提交单独挪到别的分支", []string{"git-workflow.md"}, evalKindParaphrase},
	{"多个组件要共享同一份数据", []string{"frontend-state.md"}, evalKindParaphrase},
	{"怎么找含义接近但写法不一样的笔记", []string{"vector-search.md"}, evalKindParaphrase},

	// —— 跨语言：英文 query 对中文正文，BM25 词面零重叠 ——
	{"how to invalidate cache", []string{"cache-invalidation.md"}, evalKindCrossLing},
	{"keep user logged in", []string{"auth-jwt.md"}, evalKindCrossLing},
	{"retry with exponential backoff", []string{"error-handling.md"}, evalKindCrossLing},
	{"how to profile memory usage", []string{"perf-profiling.md"}, evalKindCrossLing},
}

// evalK 是评测截断位（Recall@5 / MRR@5）。
const evalK = 5

// newEvalWorkspace 创建评测用临时工作区。
func newEvalWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range evalCorpus {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// normRelPath 统一路径分隔符，消除 Windows 与 qrels 之间的写法差异。
func normRelPath(p string) string {
	return strings.TrimPrefix(filepath.ToSlash(strings.ReplaceAll(p, "\\", "/")), "./")
}

// recallAtK = |相关 ∩ TopK| / |相关|。
func recallAtK(got []*SearchResult, relevant []string, k int) float64 {
	if len(relevant) == 0 {
		return 0
	}
	limit := k
	if len(got) < limit {
		limit = len(got)
	}
	hit := 0
	for _, want := range relevant {
		for i := 0; i < limit; i++ {
			if normRelPath(got[i].Path) == want {
				hit++
				break
			}
		}
	}
	return float64(hit) / float64(len(relevant))
}

// reciprocalRankAtK = 1 / 首个相关结果的排名；未进 TopK 记 0。
func reciprocalRankAtK(got []*SearchResult, relevant []string, k int) float64 {
	rank := firstRelevantRank(got, relevant, k)
	if rank == 0 {
		return 0
	}
	return 1 / float64(rank)
}

// firstRelevantRank 返回首个相关结果的 1-based 排名，未命中返回 0。
func firstRelevantRank(got []*SearchResult, relevant []string, k int) int {
	limit := k
	if len(got) < limit {
		limit = len(got)
	}
	for i := 0; i < limit; i++ {
		for _, want := range relevant {
			if normRelPath(got[i].Path) == want {
				return i + 1
			}
		}
	}
	return 0
}

// evalRow 是单条 query 的评测明细。
type evalRow struct {
	query  string
	kind   string
	rank   int
	recall float64
	rr     float64
}

// evalReport 是一个分组（arm）的评测结果。
type evalReport struct {
	name        string
	recallAtK   float64
	mrrAtK      float64
	rows        []evalRow
	recallByKind map[string]float64
}

// evalArm 描述一组待测配置。
type evalArm struct {
	name      string
	svc       *QnAService
	embBase   string
	embModel  string
	rerankCfg RerankConfig
}

// runEvalArm 跑完整个评测集并汇总。
func runEvalArm(t *testing.T, ws string, arm evalArm) evalReport {
	t.Helper()
	rep := evalReport{
		name:         arm.name,
		rows:         make([]evalRow, 0, len(evalQueries)),
		recallByKind: make(map[string]float64),
	}
	rSum, mSum := 0.0, 0.0
	kindTotal := make(map[string]float64)
	for _, q := range evalQueries {
		res, err := arm.svc.HybridSearch(ws, q.query, arm.embBase, arm.embModel, "", arm.rerankCfg)
		if err != nil {
			t.Fatalf("[%s] HybridSearch(%q) 失败：%v", arm.name, q.query, err)
		}
		r := recallAtK(res, q.relevant, evalK)
		m := reciprocalRankAtK(res, q.relevant, evalK)
		rSum += r
		mSum += m
		rep.recallByKind[q.kind] += r
		kindTotal[q.kind]++
		rep.rows = append(rep.rows, evalRow{
			query:  q.query,
			kind:   q.kind,
			rank:   firstRelevantRank(res, q.relevant, evalK),
			recall: r,
			rr:     m,
		})
	}
	n := float64(len(evalQueries))
	rep.recallAtK = rSum / n
	rep.mrrAtK = mSum / n
	for kind := range kindTotal {
		rep.recallByKind[kind] /= kindTotal[kind]
	}
	return rep
}

// logEvalReport 输出单分组的逐条明细与汇总。
func logEvalReport(t *testing.T, rep evalReport) {
	t.Helper()
	t.Logf("── %s ──", rep.name)
	t.Logf("  %-30s %-10s %-8s %-9s %-9s", "query", "类型", "首命中", "Recall@5", "MRR@5")
	for _, r := range rep.rows {
		rank := "未进Top5"
		if r.rank > 0 {
			rank = fmt.Sprintf("#%d", r.rank)
		}
		q := r.query
		if runes := []rune(q); len(runes) > 30 {
			q = string(runes[:29]) + "…"
		}
		t.Logf("  %-30s %-10s %-8s %-9.3f %-9.3f", q, r.kind, rank, r.recall, r.rr)
	}
	t.Logf("  汇总：Recall@5 = %.4f   MRR@5 = %.4f   (n=%d)",
		rep.recallAtK, rep.mrrAtK, len(rep.rows))
	t.Logf("  分类 Recall@5：字面 %.3f ｜ 同义改写 %.3f ｜ 跨语言 %.3f",
		rep.recallByKind[evalKindLiteral],
		rep.recallByKind[evalKindParaphrase],
		rep.recallByKind[evalKindCrossLing])
}

// logEvalComparison 输出多分组对比表与相对基线的增益。
func logEvalComparison(t *testing.T, reports []evalReport) {
	t.Helper()
	if len(reports) == 0 {
		return
	}
	base := reports[0]
	t.Logf("── 分组对比（基线：%s）──", base.name)
	t.Logf("  %-24s %-10s %-10s %-14s %-14s", "分组", "Recall@5", "MRR@5", "ΔRecall@5", "ΔMRR@5")
	for _, r := range reports {
		t.Logf("  %-24s %-10.4f %-10.4f %+14.4f %+14.4f",
			r.name, r.recallAtK, r.mrrAtK, r.recallAtK-base.recallAtK, r.mrrAtK-base.mrrAtK)
	}
}

// syntheticEmbedder 是确定性哈希嵌入器：把文本按字符 bigram 哈希到固定维度后
// L2 归一化。它只反映字面 n-gram 重合度，**不具备真实语义**，因此其评测数字
// 不能用来衡量语义召回增益。用途是在没有外部 embedding 服务时验证评测管线
// 本身接通，避免 harness 因长期无人运行而悄悄腐坏。
type syntheticEmbedder struct{ dim int }

func (s syntheticEmbedder) Embed(ctx context.Context, cfg EmbeddingConfig, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, txt := range texts {
		v := make([]float32, s.dim)
		rs := []rune(strings.ToLower(txt))
		for j := 0; j+1 < len(rs); j++ {
			h := fnv.New32a()
			_, _ = h.Write([]byte(string(rs[j : j+2])))
			v[int(h.Sum32()%uint32(s.dim))]++
		}
		out[i] = normalizeVector(v)
	}
	return out, nil
}

// envOrDefault 读取环境变量，未设置时回落到默认值。
func envOrDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// conceptEmbedder 是「按同义词表映射维度」的确定性测试 embedding。
//
// 每个维度对应一组同义触发词（可跨语言）；文本命中组内任一词则该维度置 1，
// 最后做 L2 归一化。这样就能在**不依赖任何外部模型**的前提下，构造出
// 「BM25 词面零重叠、但向量完全同向」的样本——等价于真实场景里的跨语言同义
// （英文 query 命中中文笔记），用来锁死「向量 leg 必须能补充 BM25 漏召的文档」。
type conceptEmbedder struct {
	dim     int
	aliases [][]string // aliases[dim] = 该维度的一组同义触发词
}

func (e conceptEmbedder) Embed(ctx context.Context, cfg EmbeddingConfig, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, txt := range texts {
		v := make([]float32, e.dim)
		lower := strings.ToLower(txt)
		for d, group := range e.aliases {
			if d >= e.dim {
				break
			}
			for _, alias := range group {
				if strings.Contains(lower, alias) {
					v[d] = 1
					break
				}
			}
		}
		out[i] = normalizeVector(v)
	}
	return out, nil
}

// TestQnAService_HybridSearch_VectorOnlyRecall 锁住一条回归：
// **纯向量召回的文档（BM25 词面零重叠）必须出现在 HybridSearch 的结果里。**
//
// 修复前，HybridSearch 只用 BM25 命中集构建 relPath → cachedDoc 映射，
// 结果映射阶段对找不到的文档直接 continue —— 于是向量 leg 算了分却贡献不了
// 任何新召回，混合检索退化成「只重排 BM25 命中集」。
// 2026-09-02 用 bge-m3 实测暴露：Recall@5 = 0.6429，与纯 BM25 完全持平，
// 跨语言类 0.000（4 条全灭）。修复后同口径 Recall@5 = 1.0000。
func TestQnAService_HybridSearch_VectorOnlyRecall(t *testing.T) {
	ClearAllSearchIndexes()

	// 语料刻意让目标文档不出现任何英文词面，保证 BM25 与 query 零重叠。
	dir := t.TempDir()
	files := map[string]string{
		"cache-invalidation.md": "# 缓存失效\n\n缓存失效是分布式系统里的经典难题，常见做法是设置 TTL 到期自动淘汰。",
		"go-basics.md":          "# Go 基础\n\nGo 是静态类型的编译型语言，支持 goroutine 与 channel。",
		"shopping.md":           "# 购物清单\n\n牛奶、鸡蛋、面包。",
	}
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	query := "how to invalidate cache"

	// 基线：纯 BM25 必须一条都搜不到，否则本用例就不是「向量独有召回」的场景。
	bm25Res, err := NewQnAService().HybridSearch(dir, query, "", "", "", RerankConfig{})
	if err != nil {
		t.Fatalf("BM25 基线 HybridSearch 失败：%v", err)
	}
	if len(bm25Res) != 0 {
		t.Fatalf("前提不成立：BM25 基线应零命中（词面需零重叠），实际返回 %d 条：%+v",
			len(bm25Res), bm25Res)
	}

	// 混合检索：向量 leg 必须把这篇文档捞出来。
	fs := NewFileServiceWithHistory(NewSnapshotService())
	emb := conceptEmbedder{
		dim: 3,
		aliases: [][]string{
			{"缓存失效", "invalidate cache", "cache invalidation"}, // dim 0
			{"goroutine"},                                          // dim 1
			{"牛奶"},                                                 // dim 2
		},
	}
	svc := NewQnAServiceWithRegistry(fs, nil, emb, nil)
	res, err := svc.HybridSearch(dir, query, "concept://local", "concept-v1", "", RerankConfig{})
	if err != nil {
		t.Fatalf("混合检索 HybridSearch 失败：%v", err)
	}
	if len(svc.vectorCache) == 0 {
		t.Fatal("向量库未构建，向量 leg 未生效，用例失去意义")
	}
	if len(res) == 0 {
		t.Fatal("混合检索应召回 BM25 漏掉的文档，实际返回空——向量独有结果被丢弃（回归）")
	}
	if normRelPath(res[0].Path) != "cache-invalidation.md" {
		t.Errorf("首位应为 cache-invalidation.md，实际 %q；完整结果：", res[0].Path)
		for i, r := range res {
			t.Errorf("  #%d %s", i+1, r.Path)
		}
	}
}

// TestRetrievalEval_Plumbing_SyntheticEmbedder 用合成向量跑通评测管线。
// 它验证的是 harness 自身（语料可索引、向量 leg 接进 RRF 融合、指标可计算），
// 而不是语义召回能力——真实数字见 TestRetrievalEval_Ollama_BGEM3。
func TestRetrievalEval_Plumbing_SyntheticEmbedder(t *testing.T) {
	ClearAllSearchIndexes()
	ws := newEvalWorkspace(t)
	fs := NewFileServiceWithHistory(NewSnapshotService())

	base := runEvalArm(t, ws, evalArm{
		name: "① BM25 基线",
		svc:  NewQnAService(),
	})

	hybridSvc := NewQnAServiceWithRegistry(fs, nil, syntheticEmbedder{dim: 128}, nil)
	hybrid := runEvalArm(t, ws, evalArm{
		name:     "② 混合检索（合成向量）",
		svc:      hybridSvc,
		embBase:  "synthetic://local",
		embModel: "synthetic-bigram",
	})

	logEvalReport(t, base)
	logEvalReport(t, hybrid)
	logEvalComparison(t, []evalReport{base, hybrid})

	// 基线可用性：语料若没被正确索引，评测集就失去意义。
	if base.recallAtK == 0 {
		t.Errorf("BM25 基线 Recall@5 = 0，语料未被正确索引，评测集失效")
	}
	// 向量 leg 必须真的构建过——否则「混合检索」分组只是 BM25 的别名。
	if len(hybridSvc.vectorCache) == 0 {
		t.Errorf("混合检索分组未构建向量库，向量 leg 没有接进融合，harness 失效")
	}
}

// TestRetrievalEval_Ollama_BGEM3 产出真实召回数字（需本机 Ollama + bge-m3）。
//
//	NV_OLLAMA_TEST=1 go test ./internal/service/ -run TestRetrievalEval_Ollama_BGEM3 -v -count=1
//
// 断言：混合检索的 Recall@5 不低于 BM25 基线。这是「语义增强不得拖累召回」
// 的另一面——既然开了语义，就不该比纯 BM25 更差。
func TestRetrievalEval_Ollama_BGEM3(t *testing.T) {
	if os.Getenv("NV_OLLAMA_TEST") == "" {
		t.Skip("set NV_OLLAMA_TEST=1 with Ollama + bge-m3 running to produce real recall numbers")
	}
	ClearAllSearchIndexes()
	ws := newEvalWorkspace(t)
	fs := NewFileServiceWithHistory(NewSnapshotService())

	embBase := envOrDefault("NV_EVAL_EMBED_BASE", "http://127.0.0.1:11434/v1")
	embModel := envOrDefault("NV_EVAL_EMBED_MODEL", "bge-m3")
	rerankBase := envOrDefault("NV_EVAL_RERANK_BASE", "")
	rerankModel := os.Getenv("NV_EVAL_RERANK_MODEL")
	rerankProvider := RerankProvider(envOrDefault("NV_EVAL_RERANK_PROVIDER", "cohere"))
	rerankAPIKey := os.Getenv("NV_EVAL_RERANK_APIKEY")

	base := runEvalArm(t, ws, evalArm{
		name: "① BM25 基线",
		svc:  NewQnAService(),
	})

	hybridSvc := NewQnAServiceWithRegistry(fs, nil, NewOllamaEmbeddingClient(), nil)
	hybrid := runEvalArm(t, ws, evalArm{
		name:     "② 混合检索 " + embModel,
		svc:      hybridSvc,
		embBase:  embBase,
		embModel: embModel,
	})

	reports := []evalReport{base, hybrid}

	if rerankModel != "" {
		rrSvc := NewQnAServiceWithRegistry(fs, nil, NewOllamaEmbeddingClient(), NewReranker())
		rr := runEvalArm(t, ws, evalArm{
			name:     "③ 混合检索 + Rerank " + rerankModel,
			svc:      rrSvc,
			embBase:  embBase,
			embModel: embModel,
			rerankCfg: RerankConfig{
				Provider: rerankProvider,
				BaseURL:  rerankBase,
				Model:    rerankModel,
				APIKey:   rerankAPIKey,
			},
		})
		reports = append(reports, rr)
	}

	for _, r := range reports {
		logEvalReport(t, r)
	}
	logEvalComparison(t, reports)

	if len(hybridSvc.vectorCache) == 0 {
		t.Errorf("混合检索分组未构建向量库：embedding 调用可能失败了，数字不可信")
	}
	if hybrid.recallAtK < base.recallAtK {
		t.Errorf("混合检索 Recall@5 (%.4f) 低于 BM25 基线 (%.4f)，语义增强反而拖累召回",
			hybrid.recallAtK, base.recallAtK)
	}
}
