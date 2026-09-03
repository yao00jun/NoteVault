package service

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// SearchIndexStats 描述索引的覆盖情况，供状态栏提示（P0-5）
type SearchIndexStats struct {
	DocCount     int  `json:"docCount"`     // 已索引文档数
	TokenCount   int  `json:"tokenCount"`   // 倒排表 token 数
	ScanComplete bool `json:"scanComplete"` // false = 触及 maxScanFiles 上限，仍有文件未扫
	SkippedCount int  `json:"skippedCount"` // 因体积超过 maxSearchFileSize 被跳过的文件数
}

// GetIndexStats 返回当前工作区索引的覆盖情况。
//
// 前端应当据此在状态栏提示用户「有 N 个文件未被索引」，
// 而不是让超限静默发生。
func (s *SearchService) GetIndexStats(workspacePath string) (*SearchIndexStats, error) {
	if strings.TrimSpace(workspacePath) == "" {
		return nil, fmt.Errorf("未选择工作区")
	}

	idx := s.idxOf(workspacePath)
	_ = idx.LoadSummary(workspacePath)
	if _, err := idx.refresh(workspacePath); err != nil {
		return nil, err
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return &SearchIndexStats{
		DocCount:     len(idx.docs),
		TokenCount:   len(idx.inverted),
		ScanComplete: idx.scanComplete,
		SkippedCount: idx.skippedOversize,
	}, nil
}

// SearchResult 表示一个搜索结果
type SearchResult struct {
	Path       string    `json:"path"`
	Title      string    `json:"title"`
	Snippet    string    `json:"snippet"`
	MatchCount int       `json:"matchCount"`
	ModTime    time.Time `json:"modTime"` // P0-4：供前端「按最近修改」排序
}

// 搜索健壮性常量
const (
	maxSearchFileSize = 2 * 1024 * 1024 // 超过 2MB 的文件跳过
	maxScanFiles      = 5000            // 单次扫描最多处理的文件数

	// defaultMaxSearchResults 单次搜索返回的最大结果数默认上限。
	// 原先是包级 var（供测试临时调小），属于进程级全局可变状态：
	// 任一用例改值都会影响同进程内其它用例，且并行测试下不可靠。
	// 现改为 SearchService.maxResults 实例字段，测试按实例注入。
	defaultMaxSearchResults = 200

	// defaultEagerSnippetLimit 是 Search/HybridSearch 在返回结果时「即时生成
	// snippet」的条数上限。超出部分 snippet 留空，由前端滚动到可视区时按需
	// 调 GetSearchSnippet 取（P0 基线表点明的 p95 优化点：瓶颈不在 BM25 打分，
	// 而在给 Top-200 结果逐个读正文生成 snippet）。
	// 程序化消费者（MCP server）没有前端懒加载，用 NewSearchServiceForFullSnippets
	// 把该上限提到整型上界，一次拿全。
	defaultEagerSnippetLimit = 50
)

// SearchService 提供全文搜索功能，基于内存反向索引。
// 首次搜索全量构建索引，后续仅按 modtime 增量更新变更文件，
// 避免每次搜索都全量读盘。
type SearchService struct {
	fileService *FileService
	// indexes 索引注册表（E-4）。nil 时回落到包级默认注册表，
	// 这样零值实例（测试里直接 &SearchService{}）依然可用。
	indexes *SearchIndexRegistry
	// maxResults 结果截断上限；<=0 时回落 defaultMaxSearchResults。
	// 刻意不加导出 setter：Wails 会把 Service 的导出方法全部绑成前端 API，
	// 仅为测试开洞会污染绑定契约（见 ports_contract_test.go）。
	maxResults int
	// eagerSnippetLimit 「即时生成 snippet」的条数上限；<=0 时回落 defaultEagerSnippetLimit。
	// 刻意不加导出 setter：Wails 会把 Service 的导出方法全部绑成前端 API，
	// 仅为测试/MCP 开洞会污染绑定契约（见 ports_contract_test.go）。
	eagerSnippetLimit int
}

// NewSearchService 创建搜索服务实例（索引走包级默认注册表）
func NewSearchService(fileService *FileService) *SearchService {
	return &SearchService{
		fileService:       fileService,
		maxResults:        defaultMaxSearchResults,
		eagerSnippetLimit: defaultEagerSnippetLimit,
	}
}

// NewSearchServiceWithRegistry 创建搜索服务实例，并显式指定索引注册表。
//
// 生产装配（container.go）应始终用这个构造函数：让索引的所有权与释放
// 时机集中到容器持有的那一个注册表上，而不是散落在全局。
func NewSearchServiceWithRegistry(fileService *FileService, indexes *SearchIndexRegistry) *SearchService {
	return &SearchService{
		fileService:       fileService,
		indexes:           indexes,
		maxResults:        defaultMaxSearchResults,
		eagerSnippetLimit: defaultEagerSnippetLimit,
	}
}

// NewSearchServiceForFullSnippets 创建「即时生成全部 snippet」的实例，供程序化
// 消费者（如 MCP server）使用——它们没有前端滚动懒加载，需要一次拿全。
// 这是一个包级构造函数而非 SearchService 的导出方法，因此不会被 Wails 绑成前端 API。
func NewSearchServiceForFullSnippets(fileService *FileService) *SearchService {
	return &SearchService{
		fileService:       fileService,
		maxResults:        defaultMaxSearchResults,
		eagerSnippetLimit: math.MaxInt,
	}
}

// idxOf 返回工作区对应的索引。nil 注册表回落到包级默认注册表。
func (s *SearchService) idxOf(workspacePath string) *searchIndex {
	if s.indexes != nil {
		return s.indexes.Get(workspacePath)
	}
	return defaultIndexRegistry.Get(workspacePath)
}

// resultCap 返回生效的结果上限（零值或非法值回落默认值，保证零值实例可用）
func (s *SearchService) resultCap() int {
	if s.maxResults > 0 {
		return s.maxResults
	}
	return defaultMaxSearchResults
}

// eagerSnippetCap 返回生效的「即时生成 snippet」条数上限
// （零值或非法值回落 defaultEagerSnippetLimit，保证零值实例可用）
func (s *SearchService) eagerSnippetCap() int {
	if s.eagerSnippetLimit > 0 {
		return s.eagerSnippetLimit
	}
	return defaultEagerSnippetLimit
}

// Search 在工作区中搜索关键词
// workspacePath: 工作区根目录
// query: 搜索关键词
// 返回匹配的文件列表（已按匹配次数降序、并截断到结果上限）
func (s *SearchService) Search(workspacePath string, query string) ([]*SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []*SearchResult{}, nil
	}

	// 获取/创建索引并增量更新
	idx := s.idxOf(workspacePath)
	// 冷启动加速：先尝试从磁盘加载索引摘要（已加载则幂等 no-op）
	_ = idx.LoadSummary(workspacePath)
	if _, err := idx.refresh(workspacePath); err != nil {
		return nil, err
	}
	// 索引若发生变更，按需（节流）落盘摘要，下次启动可跳过大部分 ReadFile。
	// 注意：不能每次搜索都无脑落盘——那会把整份摘要（约 8MB / 5000 文档）
	// 反复序列化写盘，拖慢实时搜索。需要确保落盘时用 FlushSearchIndex。
	idx.maybeSaveSummary(workspacePath)

	queryLower := strings.ToLower(query)
	queryTokens := tokenize(queryLower)
	if len(queryTokens) == 0 {
		return []*SearchResult{}, nil
	}

	// BM25 打分。全程在内存中完成——只依赖 tokenFreq / docLen / 倒排表长度，
	// 不读任何正文。这与旧实现有本质区别：
	//   旧：对每个候选文档 ReadFile + strings.Count，IO 随候选集线性增长
	//       （1000 文档库实测 p95 ≈ 100ms，因为中文字面查询的候选集几乎等于全库）；
	//   新：打分零 IO，只有最终进入结果集的文档才读盘生成摘要片段。
	// 打分在锁内（取读锁，不阻塞并发查询），读盘一律在锁外。
	idx.mu.RLock()
	scored := idx.scoreBM25(queryTokens, s.resultCap())
	idx.mu.RUnlock()

	// 两段式 snippet：前 eagerSnippetCap 条结果即时读正文生成 snippet（覆盖首屏，
	// 不依赖滚动）；其余结果 snippet 留空，交给前端滚动到可视区时按需调
	// GetSearchSnippet 取。这样把「给 Top-200 逐个读正文」的 p95 瓶颈从搜索
	// 关键路径上挪走（P0 基线表点明的优化点）。matchCount 在两段下都用内存里的
	// tokenFreq 之和估算（正文未读时本就只能如此）。
	eagerCap := s.eagerSnippetCap()
	results := make([]*SearchResult, 0, len(scored))
	for i, sd := range scored {
		var snippet string
		var matchCount int

		if i < eagerCap {
			content, contentLower, err := sd.doc.contentOnce(idx)
			if err != nil {
				// 文档已被并发 refresh 重建/删除（errDocStale），或正文读取失败 → 跳过
				continue
			}
			// 优先用整串出现次数（与用户直觉一致）；多词查询整串匹配不到时
			// 回落到各 token 词频之和，避免显示「匹配 0 次」这种反直觉结果。
			matchCount = strings.Count(contentLower, queryLower)
			if matchCount == 0 {
				for _, qt := range queryTokens {
					matchCount += sd.doc.tokenFreq[qt]
				}
			}
			// 用首个查询 token 定位片段：多词查询时整串（含空格）在正文里
			// 几乎不可能连续出现，按整串定位会退化成「返回开头 100 字」。
			snippet = extractSnippet(content, queryTokens[0], 50)
		} else {
			for _, qt := range queryTokens {
				matchCount += sd.doc.tokenFreq[qt]
			}
		}

		results = append(results, &SearchResult{
			Path:       sd.doc.relPath,
			Title:      sd.doc.title,
			Snippet:    snippet,
			MatchCount: matchCount,
			ModTime:    sd.doc.modTime,
		})
	}

	// 本次查询按需加载了一批正文，若因此超出预算则在此淘汰（锁外执行）。
	// 不放在 contentOnce 内部是为了保持 idx.mu → doc.contentMu 的锁序。
	idx.enforceContentBudget()

	return results, nil
}

// GetSearchSnippet 按需为单个结果生成 snippet（前端滚动到可视区时调用）。
//
// 只读取这一个文件的正文并定位片段，不拖累 Search/HybridSearch 的首屏延迟。
// 与 Search 即时片段用同一套口径（首个查询 token 定位、半径 50），保证两者
// 视觉一致。未配置查询或文档不在索引中时返回空串/报错，由调用方决定降级展示。
func (s *SearchService) GetSearchSnippet(workspacePath, relPath, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil
	}

	idx := s.idxOf(workspacePath)
	_ = idx.LoadSummary(workspacePath)
	if _, err := idx.refresh(workspacePath); err != nil {
		return "", err
	}

	idx.mu.RLock()
	doc, ok := idx.docs[relPath]
	idx.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("文档不在索引中：%s", relPath)
	}

	content, _, err := doc.contentOnce(idx)
	if err != nil {
		return "", err
	}

	queryTokens := tokenize(strings.ToLower(query))
	if len(queryTokens) == 0 {
		return "", nil
	}
	return extractSnippet(content, queryTokens[0], 50), nil
}

// extractSnippet 提取匹配位置周围的文本片段
func extractSnippet(content string, query string, radius int) string {
	contentLower := strings.ToLower(content)
	queryLower := strings.ToLower(query)

	idx := strings.Index(contentLower, queryLower)
	if idx == -1 {
		// 没有匹配，返回前 100 字符
		if len(content) > 100 {
			return content[:100] + "..."
		}
		return content
	}

	start := idx - radius
	if start < 0 {
		start = 0
	}
	end := idx + len(queryLower) + radius
	if end > len(content) {
		end = len(content)
	}

	// 调整到 UTF-8 rune 边界，避免切到多字节字符中间导致 JSON 序列化时被替换为 U+FFFD
	for start > 0 && !utf8.RuneStart(content[start]) {
		start--
	}
	for end < len(content) && !utf8.RuneStart(content[end]) {
		end++
	}

	snippet := content[start:end]
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(content) {
		snippet = snippet + "..."
	}

	// 替换换行符为空格，保持单行
	snippet = strings.ReplaceAll(snippet, "\n", " ")
	return snippet
}
