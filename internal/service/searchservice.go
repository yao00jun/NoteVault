package service

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// SearchResult 表示一个搜索结果
type SearchResult struct {
	Path       string `json:"path"`
	Title      string `json:"title"`
	Snippet    string `json:"snippet"`
	MatchCount int    `json:"matchCount"`
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
)

// SearchService 提供全文搜索功能，基于内存反向索引。
// 首次搜索全量构建索引，后续仅按 modtime 增量更新变更文件，
// 避免每次搜索都全量读盘。
type SearchService struct {
	fileService *FileService
	// maxResults 结果截断上限；<=0 时回落 defaultMaxSearchResults。
	// 刻意不加导出 setter：Wails 会把 Service 的导出方法全部绑成前端 API，
	// 仅为测试开洞会污染绑定契约（见 ports_contract_test.go）。
	maxResults int
}

// NewSearchService 创建搜索服务实例
func NewSearchService(fileService *FileService) *SearchService {
	return &SearchService{
		fileService: fileService,
		maxResults:  defaultMaxSearchResults,
	}
}

// resultCap 返回生效的结果上限（零值或非法值回落默认值，保证零值实例可用）
func (s *SearchService) resultCap() int {
	if s.maxResults > 0 {
		return s.maxResults
	}
	return defaultMaxSearchResults
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
	idx := getSearchIndex(workspacePath)
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

	// 用反向索引筛选候选文档。这里只取文档指针，不取正文——正文在锁外按需加载，
	// 避免把磁盘 IO 放进索引读锁内（那会阻塞并发的 refresh）。
	idx.mu.RLock()
	candidates := idx.query(queryLower)
	idx.mu.RUnlock()

	// 精确 substring 匹配。候选集通常远小于全库，因此只有真正参与打分的文档
	// 才会被读进内存；索引整体只常驻 tokenSet（见 cachedDoc 的内存模型说明）。
	results := make([]*SearchResult, 0, len(candidates))
	for _, doc := range candidates {
		content, contentLower, err := doc.contentOnce(idx)
		if err != nil {
			// 文档已被并发 refresh 重建/删除（errDocStale），或正文读取失败 → 跳过
			continue
		}
		matchCount := strings.Count(contentLower, queryLower)
		if matchCount == 0 {
			continue
		}

		results = append(results, &SearchResult{
			Path:       doc.relPath,
			Title:      doc.title,
			Snippet:    extractSnippet(content, query, 50),
			MatchCount: matchCount,
		})
	}

	// 本次查询按需加载了一批正文，若因此超出预算则在此淘汰（锁外执行）。
	// 不放在 contentOnce 内部是为了保持 idx.mu → doc.contentMu 的锁序。
	idx.enforceContentBudget()

	// 按匹配次数降序排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].MatchCount > results[j].MatchCount
	})

	// 截断到最大结果数
	if limit := s.resultCap(); len(results) > limit {
		results = results[:limit]
	}

	return results, nil
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
