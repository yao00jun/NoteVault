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

// ---------------------------------------------------------------------------
// 隐式属性命名空间
// ---------------------------------------------------------------------------

// 隐式属性名。这些不来自 front matter，而是从文件系统与正文推导出来的。
//
// 统一加 `file.` / `todo.` 前缀有两个作用：
//  1. 和用户自定义的 front matter 键彻底隔离，笔记里写 `name: x` 不会覆盖文件名；
//  2. 前端属性选择器可以按前缀分组展示。
const (
	PropFilePath     = "file.path"     // 相对工作区的斜杠路径
	PropFileName     = "file.name"     // 含扩展名的文件名
	PropFileBasename = "file.basename" // 不含扩展名
	PropFileExt      = "file.ext"      // 扩展名（含点）
	PropFileFolder   = "file.folder"   // 所在目录的相对路径，根目录为空串
	PropFileTitle    = "file.title"    // front matter title > 首个 H1 > basename
	PropFileSize     = "file.size"     // 字节
	PropFileMtime    = "file.mtime"    // 修改时间
	PropFileWords    = "file.words"    // 正文词数（中日韩按字计）
	PropFileTags     = "file.tags"     // front matter tags ∪ 行内 #tag
	PropFileLinks    = "file.links"    // 出链数量（[[wikilink]]）
	PropTodoTotal    = "todo.total"
	PropTodoDone     = "todo.done"
	PropTodoPending  = "todo.pending"
)

// implicitProps 是全部隐式属性名，供属性选择器与测试引用。
var implicitProps = []string{
	PropFilePath, PropFileName, PropFileBasename, PropFileExt, PropFileFolder,
	PropFileTitle, PropFileSize, PropFileMtime, PropFileWords, PropFileTags,
	PropFileLinks, PropTodoTotal, PropTodoDone, PropTodoPending,
}

// ---------------------------------------------------------------------------
// 记录
// ---------------------------------------------------------------------------

// NoteRecord 是一篇笔记在结构化视图里的「一行」。
//
// Props 里同时包含 front matter 属性与隐式属性，查询引擎只认这一张表，
// 不需要知道某个属性是从哪来的。
type NoteRecord struct {
	Path  string               `json:"path"`
	Title string               `json:"title"`
	Props map[string]PropValue `json:"props"`
}

// Get 取属性值，不存在时返回 null 值而非 zero value。
//
// 返回 null 而不是 (v, ok) 是为了让筛选器代码保持线性：
// is-empty 对缺失属性应该为真，eq 对缺失属性应该为假，两者都能从 null 值直接推出。
func (r *NoteRecord) Get(name string) PropValue {
	if r.Props == nil {
		return nullValue()
	}
	if v, ok := r.Props[name]; ok {
		return v
	}
	return nullValue()
}

// PropertyMeta 描述工作区里一个属性的统计信息，供前端属性选择器使用。
type PropertyMeta struct {
	Name     string   `json:"name"`
	Kind     PropKind `json:"kind"`     // 出现最多的类型
	Count    int      `json:"count"`    // 有该属性的笔记数
	Implicit bool     `json:"implicit"` // 是否为 file.* / todo.* 隐式属性
	Samples  []string `json:"samples"`  // 最多 8 个样例值，便于填筛选条件
}

// ---------------------------------------------------------------------------
// 扫描
// ---------------------------------------------------------------------------

// propertyIndexTTL 与 tagCacheTTL 一致：结构化视图和标签视图的数据新鲜度要求相同。
const propertyIndexTTL = 30 * time.Second

// propertyScanResult 是一次全量扫描的结果。
type propertyScanResult struct {
	records   []*NoteRecord
	scannedAt time.Time
}

type propertyCache struct {
	mu   sync.Mutex
	data map[string]*propertyScanResult
}

func newPropertyCache() *propertyCache {
	return &propertyCache{data: make(map[string]*propertyScanResult)}
}

func (c *propertyCache) get(workspacePath string) *propertyScanResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.data[workspacePath]
	if !ok || time.Since(r.scannedAt) > propertyIndexTTL {
		return nil
	}
	return r
}

func (c *propertyCache) set(workspacePath string, r *propertyScanResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r.scannedAt = time.Now()
	c.data[workspacePath] = r
}

func (c *propertyCache) invalidate(workspacePath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, workspacePath)
}

// propertyIndexer 负责把工作区扫描成 NoteRecord 列表。
type propertyIndexer struct {
	cache      *propertyCache
	inlineTag  *regexp.Regexp
	todoLine   *regexp.Regexp
	wikiLink   *regexp.Regexp
	cjkChar    *regexp.Regexp
	headingOne *regexp.Regexp
}

func newPropertyIndexer() *propertyIndexer {
	return &propertyIndexer{
		cache: newPropertyCache(),
		// 与 TagService 保持同一套标签语法，避免两个视图对"标签"的定义不一致
		inlineTag:  regexp.MustCompile(`#([\p{Han}\w\-]+)`),
		todoLine:   regexp.MustCompile(`(?m)^\s*[-*]\s+\[([ xX])\]\s+`),
		wikiLink:   regexp.MustCompile(`\[\[([^\]|#]+)`),
		cjkChar:    regexp.MustCompile(`[\p{Han}\p{Hiragana}\p{Katakana}\p{Hangul}]`),
		headingOne: regexp.MustCompile(`(?m)^#\s+(.+)$`),
	}
}

func (p *propertyIndexer) invalidate(workspacePath string) {
	p.cache.invalidate(workspacePath)
}

// scan 扫描工作区所有 Markdown 文件，产出记录列表（带 TTL 缓存）。
func (p *propertyIndexer) scan(workspacePath string) ([]*NoteRecord, error) {
	if cached := p.cache.get(workspacePath); cached != nil {
		return cached.records, nil
	}

	var allFiles []string
	err := filepath.Walk(workspacePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// 单个文件/目录读不了不该让整次扫描失败（权限、符号链接断裂等）
			return nil
		}
		if info.IsDir() {
			// 跳过点目录：.notevault 里是我们自己的元数据，.git 里是版本库
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

	records := make([]*NoteRecord, len(allFiles))
	forEachFileBoundedIndexed(allFiles, func(i int, fp string) {
		rec := p.buildRecord(workspacePath, fp)
		if rec != nil {
			records[i] = rec
		}
	})

	// 剔除读失败的空洞，并按路径排序保证结果稳定
	// （并发写入的顺序虽然由索引固定，但 Walk 的顺序在不同平台上不完全一致）
	out := make([]*NoteRecord, 0, len(records))
	for _, r := range records {
		if r != nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })

	p.cache.set(workspacePath, &propertyScanResult{records: out})
	return out, nil
}

// buildRecord 读取单个文件并构造记录。读失败返回 nil。
func (p *propertyIndexer) buildRecord(workspacePath, fullPath string) *NoteRecord {
	raw, err := os.ReadFile(fullPath)
	if err != nil {
		return nil
	}
	content := string(raw)

	fmText, body, _ := SplitFrontMatter(content)
	props := ParseFrontMatter(fmText)

	relPath, err := filepath.Rel(workspacePath, fullPath)
	if err != nil {
		return nil
	}
	relPath = filepath.ToSlash(relPath)

	name := filepath.Base(relPath)
	ext := filepath.Ext(name)
	basename := strings.TrimSuffix(name, ext)
	folder := filepath.ToSlash(filepath.Dir(relPath))
	if folder == "." {
		folder = ""
	}

	// 标题优先级：front matter title > 首个 H1 > 文件名
	// 这个顺序对应用户的显式意图强度，front matter 是明确写下来的，最优先
	title := basename
	if h1 := p.headingOne.FindStringSubmatch(body); h1 != nil {
		title = strings.TrimSpace(h1[1])
	}
	if v, ok := props["title"]; ok && !v.IsEmpty() {
		title = v.StringValue()
	}

	// 标签：front matter tags ∪ 正文行内 #tag
	tagSet := make(map[string]bool)
	if v, ok := props["tags"]; ok {
		switch v.Kind {
		case KindList:
			for _, t := range v.List {
				if t != "" {
					tagSet[t] = true
				}
			}
		default:
			if !v.IsEmpty() {
				tagSet[v.StringValue()] = true
			}
		}
	}
	for _, m := range p.inlineTag.FindAllStringSubmatch(body, -1) {
		if len(m) > 1 && m[1] != "" {
			tagSet[m[1]] = true
		}
	}
	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	sort.Strings(tags)

	// todo 统计
	var todoTotal, todoDone int
	for _, m := range p.todoLine.FindAllStringSubmatch(body, -1) {
		todoTotal++
		if len(m) > 1 && (m[1] == "x" || m[1] == "X") {
			todoDone++
		}
	}

	links := len(p.wikiLink.FindAllString(body, -1))

	var mtime time.Time
	var size int64
	if st, err := os.Stat(fullPath); err == nil {
		mtime = st.ModTime()
		size = st.Size()
	}

	// 隐式属性覆盖写入：即使 front matter 里有同名键也以隐式为准，
	// 因为它们都带 file./todo. 前缀，正常不会撞；撞了说明用户在冒充系统属性
	props[PropFilePath] = stringValue(relPath)
	props[PropFileName] = stringValue(name)
	props[PropFileBasename] = stringValue(basename)
	props[PropFileExt] = stringValue(ext)
	props[PropFileFolder] = stringValue(folder)
	props[PropFileTitle] = stringValue(title)
	props[PropFileSize] = numberValue(float64(size), formatInt(size))
	props[PropFileWords] = numberValue(float64(p.countWords(body)), "")
	props[PropFileTags] = listValue(tags, strings.Join(tags, ", "))
	props[PropFileLinks] = numberValue(float64(links), formatInt(int64(links)))
	props[PropTodoTotal] = numberValue(float64(todoTotal), formatInt(int64(todoTotal)))
	props[PropTodoDone] = numberValue(float64(todoDone), formatInt(int64(todoDone)))
	props[PropTodoPending] = numberValue(float64(todoTotal-todoDone), formatInt(int64(todoTotal-todoDone)))
	if !mtime.IsZero() {
		props[PropFileMtime] = dateValue(mtime, mtime.Format("2006-01-02 15:04"))
	} else {
		props[PropFileMtime] = nullValue()
	}
	// 数字属性的 Raw 需要能展示，words 上面留空了，这里补上
	if v := props[PropFileWords]; v.Raw == "" {
		v.Raw = formatInt(int64(v.Num))
		v.Str = v.Raw
		props[PropFileWords] = v
	}

	return &NoteRecord{Path: relPath, Title: title, Props: props}
}

// countWords 统计词数：CJK 按字符计，其余按空白分词。
//
// 中英混排的笔记如果只按空白分词，中文整段会算成 1 个词，
// 「字数」这个属性就完全没用了。
func (p *propertyIndexer) countWords(body string) int {
	cjk := len(p.cjkChar.FindAllString(body, -1))
	// 去掉 CJK 字符后再按空白分词，避免重复计数
	nonCJK := p.cjkChar.ReplaceAllString(body, " ")
	return cjk + len(strings.Fields(nonCJK))
}

// formatInt 避免为了一个数字转字符串引入 strconv 之外的开销。
func formatInt(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// collectProperties 汇总记录里出现过的所有属性及统计信息。
func collectProperties(records []*NoteRecord) []*PropertyMeta {
	type acc struct {
		count   int
		kinds   map[PropKind]int
		samples []string
		seen    map[string]bool
	}
	stats := make(map[string]*acc)

	for _, r := range records {
		for name, v := range r.Props {
			a, ok := stats[name]
			if !ok {
				a = &acc{kinds: make(map[PropKind]int), seen: make(map[string]bool)}
				stats[name] = a
			}
			a.count++
			a.kinds[v.Kind]++
			// 样例值：列表类型摊平成单个元素，因为用户筛的是"含某个值"
			candidates := []string{v.StringValue()}
			if v.Kind == KindList {
				candidates = v.List
			}
			for _, c := range candidates {
				c = strings.TrimSpace(c)
				if c == "" || a.seen[c] || len(a.samples) >= 8 {
					continue
				}
				a.seen[c] = true
				a.samples = append(a.samples, c)
			}
		}
	}

	implicitSet := make(map[string]bool, len(implicitProps))
	for _, n := range implicitProps {
		implicitSet[n] = true
	}

	out := make([]*PropertyMeta, 0, len(stats))
	for name, a := range stats {
		// 主类型 = 出现次数最多的类型；打平时按类型名字典序，保证结果确定
		best, bestN := KindString, -1
		for k, n := range a.kinds {
			if n > bestN || (n == bestN && string(k) < string(best)) {
				best, bestN = k, n
			}
		}
		samples := a.samples
		if samples == nil {
			samples = []string{}
		}
		sort.Strings(samples)
		out = append(out, &PropertyMeta{
			Name:     name,
			Kind:     best,
			Count:    a.count,
			Implicit: implicitSet[name],
			Samples:  samples,
		})
	}

	// 排序：自定义属性在前（用户更关心自己写的），各组内按出现次数降序、同数按名字
	sort.Slice(out, func(i, j int) bool {
		if out[i].Implicit != out[j].Implicit {
			return !out[i].Implicit
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// forEachFileBoundedIndexed 与 forEachFileBounded 相同，但把下标一并传给 work。
//
// 需要下标是为了让并发写入 records[i] 无需加锁：每个 worker 只写自己那一格，
// 既省掉一把互斥锁，又让结果顺序与输入一致（可预测，便于测试）。
func forEachFileBoundedIndexed(files []string, work func(i int, filePath string)) {
	workers := maxConcurrentFileWorkers(len(files))
	if workers <= 1 {
		for i, f := range files {
			work(i, f)
		}
		return
	}

	type job struct {
		i  int
		fp string
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := range jobs {
				work(j.i, j.fp)
			}
		}()
	}
	for i, f := range files {
		jobs <- job{i: i, fp: f}
	}
	close(jobs)
	wg.Wait()
}
