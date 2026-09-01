package service

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 检索质量基准（P0-0）
//
// 目的：给 P0 的检索改进提供可复现的量化基线。没有这张表，"检索变好了"只是感觉。
//
// 设计要点：
//  1. ground truth 是程序化确定的——每篇文档的目录名即其所属主题簇，
//     不需要人工标注 1000 篇文档的期望结果；
//  2. 查询分三组，分别暴露不同层次的问题：
//     - literal  用词直接来自文档 → 测排序质量（长文档霸榜）
//     - multi    多词短语          → 测现有实现能否处理词间空格
//     - semantic 同义改写          → 字面检索必然失败，为 P1-3 向量检索留基线
//  3. fixture 复用（按 seed 落盘），但每次跑基准前删掉该工作区的索引摘要，
//     保证测到的是真冷启动而不是从磁盘恢复。
// ---------------------------------------------------------------------------

type benchTopic struct {
	dir      string   // 目录名，同时作为 ground truth 标识
	keywords []string // 正文中会出现的词
	tags     []string
	literal  string // 字面查询：直接取 keywords[0]
	multi    string // 多词查询：keywords 里两个词拼起来（含空格）
	semantic string // 语义查询：同义改写，字面与文档用词不重合
}

// benchTopics 20 个主题簇，中英混排，模拟真实技术知识库
var benchTopics = []benchTopic{
	{"cache-invalidation", []string{"缓存失效", "过期策略", "TTL"}, []string{"缓存", "架构"}, "缓存失效", "缓存 过期策略", "缓存里的旧数据怎么自动清掉"},
	{"distributed-consensus", []string{"分布式一致性", "共识算法", "quorum"}, []string{"分布式", "架构"}, "分布式一致性", "分布式 共识算法", "多个节点怎么就一个值达成一致"},
	{"database-index", []string{"数据库索引", "执行计划", "查询优化"}, []string{"数据库", "性能"}, "数据库索引", "数据库 执行计划", "怎么让查询跑得更快"},
	{"authentication", []string{"身份认证", "会话管理", "JWT"}, []string{"安全", "后端"}, "身份认证", "身份认证 会话管理", "登录状态怎么保持住"},
	{"container-orchestration", []string{"容器编排", "服务部署", "滚动更新"}, []string{"运维", "云原生"}, "容器编排", "容器编排 服务部署", "一堆服务怎么自动管起来"},
	{"vector-search", []string{"向量检索", "余弦相似度", "召回率"}, []string{"AI", "检索"}, "向量检索", "向量检索 召回率", "按意思找东西而不是按字面"},
	{"frontend-performance", []string{"首屏加载", "打包体积", "懒加载"}, []string{"前端", "性能"}, "首屏加载", "首屏加载 打包体积", "页面打开太慢怎么办"},
	{"error-handling", []string{"错误处理", "重试机制", "熔断降级"}, []string{"架构", "可靠性"}, "错误处理", "错误处理 重试机制", "服务挂了怎么不连累上游"},
	{"data-backup", []string{"数据备份", "快照恢复", "容灾方案"}, []string{"运维", "数据"}, "数据备份", "数据备份 快照恢复", "数据误删了怎么找回来"},
	{"code-refactor", []string{"代码重构", "分层解耦", "技术债"}, []string{"工程", "质量"}, "代码重构", "代码重构 分层解耦", "代码太乱了怎么收拾"},
	{"knowledge-management", []string{"双向链接", "知识图谱", "卡片笔记"}, []string{"知识管理", "方法论"}, "双向链接", "双向链接 知识图谱", "个人笔记怎么串成体系"},
	{"testing-strategy", []string{"单元测试", "覆盖率", "集成测试"}, []string{"工程", "质量"}, "单元测试", "单元测试 覆盖率", "怎么确保改动没弄坏东西"},
	{"concurrency", []string{"并发编程", "竞态条件", "互斥锁"}, []string{"编程", "并发"}, "并发编程", "并发编程 竞态条件", "多个线程同时改一个东西怎么办"},
	{"message-queue", []string{"消息队列", "削峰填谷", "异步解耦"}, []string{"架构", "中间件"}, "消息队列", "消息队列 削峰填谷", "流量突然暴涨怎么扛住"},
	{"monitoring", []string{"监控告警", "可观测性", "指标采集"}, []string{"运维", "可靠性"}, "监控告警", "监控告警 可观测性", "线上出事了怎么第一时间知道"},
	{"security-hardening", []string{"安全加固", "注入防护", "权限控制"}, []string{"安全"}, "安全加固", "安全加固 注入防护", "怎么防止被人攻击"},
	{"version-control", []string{"版本控制", "合并冲突", "分支策略"}, []string{"工程", "协作"}, "版本控制", "版本控制 合并冲突", "几个人同时改代码怎么不乱"},
	{"machine-learning", []string{"模型训练", "过拟合", "特征工程"}, []string{"AI", "算法"}, "模型训练", "模型训练 过拟合", "模型效果不好怎么调"},
	{"api-design", []string{"接口设计", "幂等性", "版本兼容"}, []string{"后端", "设计"}, "接口设计", "接口设计 幂等性", "对外接口怎么定才合理"},
	{"local-first", []string{"本地优先", "数据主权", "离线可用"}, []string{"架构", "隐私"}, "本地优先", "本地优先 数据主权", "数据放在自己手里不依赖云"},
}

// benchNoise 跨主题共享的填充句。刻意让所有主题共用，
// 这样噪音词不会反过来成为区分特征。
var benchNoise = []string{
	"这个问题在实际项目中经常遇到。",
	"需要注意的是，方案选型要结合实际场景。",
	"从长期维护的角度看，简单往往比精巧更可靠。",
	"团队在这个点上踩过坑，记录一下避免重复。",
	"相关讨论散落在多个地方，这里做一次汇总。",
	"There is no silver bullet for this kind of problem.",
	"The trade-off here is between complexity and flexibility.",
	"We measured the impact before rolling it out to production.",
	"文档会随实践持续更新，以下结论基于当前版本。",
	"实施前建议先在小范围验证，再逐步推广。",
}

const (
	benchDocsPerTopic = 50 // 20 × 50 = 1000 篇
	benchSpamPerTopic = 3  // 每主题 3 篇「刷子文档」，共 60 篇干扰项
	benchSeed         = 42
)

// benchMarker 描述 fixture 的生成参数。参数变了就整体重建，
// 避免新旧语料混在一个目录里导致基准不可比。
func benchMarker() string {
	return fmt.Sprintf("seed=%d topics=%d per=%d spam=%d",
		benchSeed, len(benchTopics), benchDocsPerTopic, benchSpamPerTopic)
}

// benchCorpusDir 返回 fixture 根目录（按 seed 复用，避免每次跑基准都重新生成）
func benchCorpusDir() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("notevault-bench-%d", benchSeed))
}

// generateBenchCorpus 生成 fixture 工作区。已存在且标记匹配时直接复用。
func generateBenchCorpus(t testing.TB) string {
	t.Helper()
	root := benchCorpusDir()
	marker := filepath.Join(root, ".bench-marker")
	if want := benchMarker(); func() bool {
		b, err := os.ReadFile(marker)
		return err == nil && string(b) == want
	}() {
		return root
	}

	_ = os.RemoveAll(root)
	if err := os.MkdirAll(root, 0750); err != nil {
		t.Fatalf("创建 fixture 目录失败: %v", err)
	}

	rng := rand.New(rand.NewSource(benchSeed))
	for ti, topic := range benchTopics {
		dir := filepath.Join(root, topic.dir)
		if err := os.MkdirAll(dir, 0750); err != nil {
			t.Fatalf("创建主题目录失败: %v", err)
		}
		for i := 0; i < benchDocsPerTopic; i++ {
			// 长度分布：短 50% / 中 35% / 长 15%
			// 长文档是刻意的「噪音放大器」——它关键词密度低但绝对词频高，
			// 用来验证排序是否被长度带偏。
			var targetLen int
			switch r := rng.Float64(); {
			case r < 0.50:
				targetLen = 120 + rng.Intn(200)
			case r < 0.85:
				targetLen = 500 + rng.Intn(1000)
			default:
				targetLen = 2500 + rng.Intn(1500)
			}

			var b strings.Builder
			fmt.Fprintf(&b, "---\ntags: [%s]\n---\n\n", strings.Join(topic.tags, ", "))
			fmt.Fprintf(&b, "# %s（%d）\n\n", topic.keywords[0], i+1)

			// 开篇先挂上本主题的关键词，保证每篇都真的相关
			b.WriteString("本文讨论" + topic.keywords[0] + "相关的实践。\n\n")

			for b.Len() < targetLen {
				b.WriteString(benchNoise[rng.Intn(len(benchNoise))])
				b.WriteString(" ")
				if rng.Intn(4) == 0 {
					b.WriteString("核心是" + topic.keywords[rng.Intn(len(topic.keywords))] + "。")
				}
				if rng.Intn(20) == 0 {
					// 少量跨主题串扰：混入其它主题的词，制造真实的歧义环境
					other := benchTopics[rng.Intn(len(benchTopics))]
					if other.dir != topic.dir {
						b.WriteString("另外也涉及" + other.keywords[0] + "的内容。")
					}
				}
				if rng.Intn(12) == 0 {
					b.WriteString("\n\n## 小结\n\n")
				}
			}
			b.WriteString("\n")

			name := filepath.Join(dir, fmt.Sprintf("note-%04d.md", i))
			if err := os.WriteFile(name, []byte(b.String()), 0640); err != nil {
				t.Fatalf("写入 fixture 失败: %v", err)
			}
		}
		_ = ti

		// ---------------------------------------------------------------
		// 刷子文档：超长、关键词密度低但绝对词频高，且密集重复「其它主题」的词。
		//
		// 这是基准的区分度来源。没有它，字面查询 Recall@5 会直接拉满到 1.000，
		// 测不出排序问题。现实中的知识库就是这样：总有一批又长又杂的整理稿，
		// 什么词都沾一点，按词频排序时它们会挤掉真正相关的短笔记。
		// ---------------------------------------------------------------
		for k := 0; k < benchSpamPerTopic; k++ {
			var s strings.Builder
			fmt.Fprintf(&s, "---\ntags: [整理]\n---\n\n")
			fmt.Fprintf(&s, "# %s 相关资料汇编（%d）\n\n", topic.keywords[0], k+1)
			s.WriteString("本文围绕" + topic.keywords[0] + "做一次汇总。\n\n")

			for rep := 0; rep < 45; rep++ {
				other := benchTopics[rng.Intn(len(benchTopics))]
				if other.dir == topic.dir {
					continue
				}
				s.WriteString(benchNoise[rng.Intn(len(benchNoise))])
				s.WriteString("其中" + other.keywords[0] + "是关键，")
				s.WriteString(other.keywords[0] + "需要重点关注。")
			}
			s.WriteString("\n")

			name := filepath.Join(dir, fmt.Sprintf("spam-%02d.md", k))
			if err := os.WriteFile(name, []byte(s.String()), 0640); err != nil {
				t.Fatalf("写入刷子文档失败: %v", err)
			}
		}
	}

	if err := os.WriteFile(marker, []byte(benchMarker()), 0640); err != nil {
		t.Fatalf("写入 marker 失败: %v", err)
	}
	return root
}

// benchQuerySet 一组查询及其 ground truth 主题
type benchQuerySet struct {
	name    string
	queries []benchQuery
}

type benchQuery struct {
	text     string
	goldDir  string
	category string
}

func buildBenchQueries() []benchQuerySet {
	var literal, multi, semantic []benchQuery
	for _, tp := range benchTopics {
		literal = append(literal, benchQuery{text: tp.literal, goldDir: tp.dir, category: "literal"})
		multi = append(multi, benchQuery{text: tp.multi, goldDir: tp.dir, category: "multi"})
		semantic = append(semantic, benchQuery{text: tp.semantic, goldDir: tp.dir, category: "semantic"})
	}
	return []benchQuerySet{
		{name: "字面查询（用词来自文档）", queries: literal},
		{name: "多词查询（词间空格）", queries: multi},
		{name: "语义查询（同义改写）", queries: semantic},
	}
}

// benchRun 跑一组查询，返回 Recall@5 / MRR@5 / p95 延迟
func benchRun(t testing.TB, svc *SearchService, workspace string, set benchQuerySet) (recall, mrr, p95ms float64, zeroResult int) {
	t.Helper()
	const topK = 5
	var recallSum, mrrSum float64
	var latencies []float64

	for _, q := range set.queries {
		start := time.Now()
		results, err := svc.Search(workspace, q.text)
		latencies = append(latencies, float64(time.Since(start).Microseconds())/1000.0)
		if err != nil {
			t.Fatalf("搜索失败 %q: %v", q.text, err)
		}

		if len(results) == 0 {
			zeroResult++
			continue
		}

		hits := 0
		firstRank := 0
		for i, r := range results {
			if i >= topK {
				break
			}
			// ground truth：结果的目录名与查询所属主题一致
			if strings.HasPrefix(filepath.ToSlash(r.Path), q.goldDir+"/") {
				hits++
				if firstRank == 0 {
					firstRank = i + 1
				}
			}
		}
		recallSum += float64(hits) / float64(topK)
		if firstRank > 0 {
			mrrSum += 1.0 / float64(firstRank)
		}
	}

	n := float64(len(set.queries))
	sort.Float64s(latencies)
	p95idx := int(float64(len(latencies)) * 0.95)
	if p95idx >= len(latencies) {
		p95idx = len(latencies) - 1
	}
	return recallSum / n, mrrSum / n, latencies[p95idx], zeroResult
}

// TestSearchQualityBaseline 输出检索质量基线表。
// 这是 P0 的验收依据——改动前后各跑一次，对比数字。
func TestSearchQualityBaseline(t *testing.T) {
	workspace := generateBenchCorpus(t)

	// 清掉该工作区的索引摘要与内存索引，确保测到真冷启动
	_ = os.Remove(summaryPathFor(workspace))
	ClearAllSearchIndexes()

	svc := NewSearchService(NewFileService())

	coldStart := time.Now()
	if _, err := svc.Search(workspace, benchTopics[0].literal); err != nil {
		t.Fatalf("冷启动搜索失败: %v", err)
	}
	coldElapsed := time.Since(coldStart)

	docCount, tokenCount := getSearchIndex(workspace).stats()
	fmt.Printf("\n=== 检索质量基准 ===\n")
	fmt.Printf("corpus : %s\n", workspace)
	fmt.Printf("规模   : %d 文档 / %d token\n", docCount, tokenCount)
	fmt.Printf("冷启动 : %v\n\n", coldElapsed.Round(time.Millisecond))

	fmt.Printf("| 查询组 | Recall@5 | MRR@5 | p95 延迟 | 零结果数 |\n")
	fmt.Printf("|---|---|---|---|---|\n")
	for _, set := range buildBenchQueries() {
		r, m, p95, zero := benchRun(t, svc, workspace, set)
		fmt.Printf("| %s | %.3f | %.3f | %.2f ms | %d/%d |\n",
			set.name, r, m, p95, zero, len(set.queries))
	}
	fmt.Printf("\n")
}
