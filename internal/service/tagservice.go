package service

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// TagInfo 表示一个标签及其使用次数
type TagInfo struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// TagFileInfo 表示包含某个标签的文件信息
type TagFileInfo struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

// ---------------------------------------------------------------------------
// TTL 缓存
// ---------------------------------------------------------------------------

const tagCacheTTL = 30 * time.Second

// tagScanResult 缓存一次完整标签扫描的结果
type tagScanResult struct {
	tags       []*TagInfo
	filesByTag map[string][]*TagFileInfo // lowercase tag → files
	scannedAt  time.Time
}

// tagCache 为每个 workspacePath 缓存扫描结果，TTL 过期后自动失效
type tagCache struct {
	mu   sync.Mutex
	data map[string]*tagScanResult // workspacePath → result
}

func newTagCache() *tagCache {
	return &tagCache{data: make(map[string]*tagScanResult)}
}

func (c *tagCache) get(workspacePath string) *tagScanResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.data[workspacePath]
	if !ok || time.Since(r.scannedAt) > tagCacheTTL {
		return nil
	}
	return r
}

func (c *tagCache) set(workspacePath string, r *tagScanResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r.scannedAt = time.Now()
	c.data[workspacePath] = r
}

func (c *tagCache) invalidate(workspacePath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, workspacePath)
}

// ---------------------------------------------------------------------------
// TagService
// ---------------------------------------------------------------------------

// TagService 提供标签管理功能，带 30s TTL 缓存
type TagService struct {
	tagRegex *regexp.Regexp
	cache    *tagCache
}

// NewTagService 创建标签服务实例
func NewTagService() *TagService {
	return &TagService{
		tagRegex: regexp.MustCompile(`#([\p{Han}\w\-]+)`),
		cache:    newTagCache(),
	}
}

// InvalidateCache 清除指定工作区的标签缓存
func (s *TagService) InvalidateCache(workspacePath string) {
	s.cache.invalidate(workspacePath)
}

// scanAll 扫描工作区中所有 Markdown 文件的标签，返回汇总结果
func (s *TagService) scanAll(workspacePath string) (*tagScanResult, error) {
	// 检查缓存
	if cached := s.cache.get(workspacePath); cached != nil {
		return cached, nil
	}

	// 获取所有 Markdown 文件
	var allFiles []string
	err := filepath.Walk(workspacePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".md" || ext == ".markdown" {
			allFiles = append(allFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 并发提取标签（有界 worker 池；旧实现每文件一个 goroutine，
	// 而临界区本身串行，多出的并发只放大调度与内存开销）
	tagCount := make(map[string]int)
	filesByTag := make(map[string][]*TagFileInfo)
	var mu sync.Mutex

	forEachFileBounded(allFiles, func(fp string) {
		content, err := os.ReadFile(fp)
		if err != nil {
			return
		}
		contentStr := string(content)
		tags := s.extractTags(contentStr)

		// 提取标题
		title := filepath.Base(fp)
		for _, line := range strings.Split(contentStr, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "# ") {
				title = strings.TrimPrefix(trimmed, "# ")
				break
			}
		}

		relPath, _ := filepath.Rel(workspacePath, fp)
		relPath = filepath.ToSlash(relPath)

		mu.Lock()
		for _, tag := range tags {
			tagLower := strings.ToLower(tag)
			tagCount[tag]++
			filesByTag[tagLower] = append(filesByTag[tagLower], &TagFileInfo{
				Path:  relPath,
				Title: title,
			})
		}
		mu.Unlock()
	})

	// 转换为列表并按使用次数降序排序
	var result []*TagInfo
	for name, count := range tagCount {
		result = append(result, &TagInfo{Name: name, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Name < result[j].Name
	})

	scanResult := &tagScanResult{
		tags:       result,
		filesByTag: filesByTag,
	}
	s.cache.set(workspacePath, scanResult)
	return scanResult, nil
}

// GetAllTags 获取工作区中所有标签及其使用次数
func (s *TagService) GetAllTags(workspacePath string) ([]*TagInfo, error) {
	scanResult, err := s.scanAll(workspacePath)
	if err != nil {
		return nil, err
	}
	if scanResult.tags == nil {
		return []*TagInfo{}, nil
	}
	return scanResult.tags, nil
}

// GetFilesByTag 获取包含指定标签的所有文件
func (s *TagService) GetFilesByTag(workspacePath string, tag string) ([]*TagFileInfo, error) {
	tag = strings.TrimPrefix(tag, "#")
	tagLower := strings.ToLower(tag)

	scanResult, err := s.scanAll(workspacePath)
	if err != nil {
		return nil, err
	}

	files, ok := scanResult.filesByTag[tagLower]
	if !ok {
		return []*TagFileInfo{}, nil
	}
	if files == nil {
		return []*TagFileInfo{}, nil
	}
	return files, nil
}

// extractTags 从文本中提取所有标签
// 支持：
// 1. 行内 #标签 语法（中文/英文/数字/下划线/连字符）
// 2. YAML front matter 中的 tags: [a, b, c] 或 tags:\n  - a\n  - b
func (s *TagService) extractTags(content string) []string {
	tagSet := make(map[string]bool)

	// 1. 行内 #标签
	inlineMatches := s.tagRegex.FindAllStringSubmatch(content, -1)
	for _, match := range inlineMatches {
		if len(match) > 1 {
			tag := match[1]
			tagSet[tag] = true
		}
	}

	// 2. YAML front matter 标签
	fmTags := extractFrontMatterTags(content)
	for _, tag := range fmTags {
		tagSet[tag] = true
	}

	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	return tags
}

// extractFrontMatterTags 从 YAML front matter 提取 tags 数组
// 支持两种格式：
//
//	tags: [a, b, c]
//	tags:\n  - a\n  - b\n  - c
func extractFrontMatterTags(content string) []string {
	var result []string

	// 仅在开头扫描 front matter（最多 4KB 足够覆盖大部分场景）
	prefix := content
	if len(prefix) > 4096 {
		prefix = prefix[:4096]
	}

	// 必须以 --- 开头
	if !strings.HasPrefix(prefix, "---") {
		return result
	}

	// 找到结束的 ---
	rest := prefix[3:]
	endIdx := strings.Index(rest, "\n---")
	if endIdx == -1 {
		return result
	}
	fm := rest[:endIdx]

	// 解析 tags 字段
	lines := strings.Split(fm, "\n")
	inTags := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "tags:") {
			// 单行格式: tags: [a, b, c]
			value := strings.TrimPrefix(trimmed, "tags:")
			value = strings.TrimSpace(value)
			value = strings.TrimPrefix(value, "[")
			value = strings.TrimSuffix(value, "]")
			if strings.Contains(value, ",") {
				parts := strings.Split(value, ",")
				for _, p := range parts {
					p = strings.TrimSpace(p)
					p = strings.Trim(p, "\"'`")
					if p != "" {
						result = append(result, p)
					}
				}
				inTags = false
			} else if value != "" {
				// 单值 tags: tag1
				value = strings.Trim(value, "\"'`")
				result = append(result, value)
				inTags = false
			} else {
				// 多行格式: tags:\n  - a\n  - b
				inTags = true
			}
			continue
		}
		// 多行格式的子项
		if inTags {
			// 形如 "  - a" 或 "- a"
			cleaned := strings.TrimSpace(trimmed)
			cleaned = strings.TrimPrefix(cleaned, "- ")
			cleaned = strings.TrimPrefix(cleaned, "* ")
			cleaned = strings.Trim(cleaned, "\"'`")
			if cleaned != "" && !strings.Contains(cleaned, ":") {
				result = append(result, cleaned)
			}
			// 遇到非缩进行表示 tags 结束
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && cleaned != "" {
				inTags = false
			}
		}
	}
	return result
}
