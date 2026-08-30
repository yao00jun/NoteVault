package service

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/notevault/notevault/internal/core"
)

// ---------------------------------------------------------------------------
// 分词器
// ---------------------------------------------------------------------------

// tokenize 将文本拆分为小写 token。
// 拉丁文/数字/下划线连续串作为一个 token；
// CJK 汉字每个字符单独作为一个 token；
// 其他字符（空格、标点、连字符等）作为分隔符。
// 这种策略使得搜索 "wiki" 能命中含 "wiki-link" 的文档（倒排表筛出候选，
// 再由 strings.Count 精确匹配确认）。
func tokenize(s string) []string {
	s = strings.ToLower(s)
	tokens := make([]string, 0, len(s)/4)
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			tokens = append(tokens, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			flush()
			tokens = append(tokens, string(r))
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

// ---------------------------------------------------------------------------
// 缓存文档
// ---------------------------------------------------------------------------

// cachedDoc 缓存单个 Markdown 文件的索引元数据，正文按需加载。
//
// 内存模型（2026-08-29 重构）：
//   - 常驻：relPath / modTime / title / tokenSet。反向索引与候选筛选只依赖它们，
//     摘要持久化与冷启动恢复也只恢复这些字段；
//   - 按需：content / contentLower。仅在文档真正成为查询候选（或进入问答上下文）时
//     才 ReadFile，并受所属索引的内容预算约束（见 enforceContentBudget）。
//
// 重构前的问题：refresh 会把所有「content 为空」的文档一次性补读进内存
// （阶段 2 的 doc.content == "" → toFill）。这意味着冷启动后整库正文 + 小写副本
// 常驻内存（5000 文档 ≈ 2 倍正文大小），摘要省下的只有 tokenize 的 CPU，内存仍随库
// 体积线性增长。现在冷启动只建 tokenSet，正文留给真正被查询的文档。
type cachedDoc struct {
	relPath string    // 相对路径（正斜杠分隔）
	modTime time.Time // 文件最后修改时间
	title   string    // 文档标题（首个 "# " 行或文件名）

	// tokenSet 去重后的 token 集合（用于增量删除）。
	// 构建完成后不再修改，可在持有 idx.mu 读锁时安全读取。
	tokenSet map[string]bool

	// contentMu 保护下面与正文相关的全部字段。
	// 锁序约定：idx.mu → doc.contentMu；反向取锁必然死锁，切勿违反。
	contentMu sync.Mutex
	absPath   string // 绝对路径（用于按需读正文）；摘要恢复的文档初值为空，由 refresh 补挂
	content   string // 原始内容（按需加载）
	// contentLower 小写内容（按需加载，供 strings.Count 做大小写不敏感计数）
	contentLower string
	// contentLoaded 区分「尚未加载」与「正文本身为空」——空文件不应被反复读盘
	contentLoaded bool
	// removed 该文档已被 refresh 重建或删除；旧指针可能仍被查询持有，据此判定失效
	removed bool
	// lastUsed 上次被访问的时间（UnixNano），用于超预算时按 LRU 淘汰
	lastUsed int64
}

// errDocStale 表示文档已被并发的 refresh 重建或删除，调用方应跳过该候选。
var errDocStale = errors.New("search: document superseded by refresh")

// contentOnce 返回文档的正文与小写正文，必要时按需从磁盘加载。
//
// 调用者不得持有 idx.mu —— 本方法会做磁盘 IO，持索引锁做 IO 会阻塞 refresh。
// 返回的 string 不可变，释放锁之后仍可安全使用。
func (d *cachedDoc) contentOnce(idx *searchIndex) (content, contentLower string, err error) {
	d.contentMu.Lock()
	defer d.contentMu.Unlock()
	if d.removed {
		return "", "", errDocStale
	}
	if !d.contentLoaded {
		if d.absPath == "" {
			return "", "", errDocStale
		}
		data, readErr := os.ReadFile(d.absPath)
		if readErr != nil {
			return "", "", readErr
		}
		d.content = string(data)
		d.contentLower = strings.ToLower(d.content)
		d.contentLoaded = true
		// 正文与小写副本各占一份，计入两倍
		idx.accountContentBytes(int64(len(d.content)) * 2)
	}
	d.lastUsed = time.Now().UnixNano()
	return d.content, d.contentLower, nil
}

// releaseContentLocked 丢弃正文并归还预算；调用者须持有 d.contentMu。
func (d *cachedDoc) releaseContentLocked(idx *searchIndex) {
	if !d.contentLoaded {
		return
	}
	idx.accountContentBytes(-int64(len(d.content)) * 2)
	d.content = ""
	d.contentLower = ""
	d.contentLoaded = false
}

// lastUsedAt 返回上次访问时间；供淘汰排序使用（自带加锁）
func (d *cachedDoc) lastUsedAt() int64 {
	d.contentMu.Lock()
	defer d.contentMu.Unlock()
	return d.lastUsed
}

// contentOf 按需取文档正文，是索引对外暴露的正文访问入口。
// 调用者不得持有 idx.mu —— 可能触发磁盘 IO。
// 返回 (原始正文, 小写正文, error)；error 非 nil 时调用方应跳过该文档。
func (idx *searchIndex) contentOf(doc *cachedDoc) (string, string, error) {
	return doc.contentOnce(idx)
}

// ---------------------------------------------------------------------------
// 搜索索引（per-workspace）
// ---------------------------------------------------------------------------

// searchIndex 维护单个工作区的内存反向索引。
// 首次搜索时全量构建，后续搜索仅按 modtime 增量更新变更文件。
type searchIndex struct {
	mu            sync.RWMutex
	docs          map[string]*cachedDoc      // relPath → doc
	inverted      map[string]map[string]bool // token(lowercase) → set(relPath)
	summaryLoaded bool                       // 标记是否已尝试加载磁盘摘要（幂等）
	// refreshMu 串行化 refresh，避免并发搜索重复做同样的扫描与 IO。
	// 它不阻塞查询（query 只取读锁），因此不影响搜索并发度。
	refreshMu sync.Mutex
	// dirty 标记自上次落盘以来索引是否变更；lastSave 为上次落盘时间。
	// 用于把「每次搜索都落盘」改为「脏 + 节流」落盘。
	dirty    bool
	lastSave time.Time

	// 正文缓存的内存账本：contentBytes 为当前占用，contentBudget 为上限。
	// 二者共同保证「正文内存可预期」，而不是随知识库体积线性增长。
	contentBytes  atomic.Int64
	contentBudget int64
}

// defaultContentBudgetBytes 单个工作区索引的正文缓存预算。
// 正文与小写副本各存一份，故实际占用约为正文总量的 2 倍；这里按「2 倍后」的字节数计账。
// 超出后按 LRU 淘汰至预算的一半，避免大库场景下正文无限膨胀。
const defaultContentBudgetBytes = 32 << 20 // 32 MiB

// accountContentBytes 增减正文账本（正数记账、负数归还）
func (idx *searchIndex) accountContentBytes(delta int64) {
	idx.contentBytes.Add(delta)
}

// enforceContentBudget 在正文缓存超预算时，按 lastUsed 升序淘汰至预算的一半。
// 调用者不得持有 idx.mu —— 本方法内部会短暂取读锁做快照。
func (idx *searchIndex) enforceContentBudget() {
	budget := idx.contentBudget
	if budget <= 0 {
		return
	}
	if idx.contentBytes.Load() <= budget {
		return
	}
	target := budget / 2

	idx.mu.RLock()
	snapshot := make([]*cachedDoc, 0, len(idx.docs))
	for _, doc := range idx.docs {
		snapshot = append(snapshot, doc)
	}
	idx.mu.RUnlock()

	sort.Slice(snapshot, func(i, j int) bool {
		return snapshot[i].lastUsedAt() < snapshot[j].lastUsedAt()
	})
	for _, doc := range snapshot {
		if idx.contentBytes.Load() <= target {
			return
		}
		doc.contentMu.Lock()
		doc.releaseContentLocked(idx)
		doc.contentMu.Unlock()
	}
}

// summarySaveInterval 索引摘要的最小落盘间隔。
// 搜索是高频操作（前端按输入实时搜索），无条件每次落盘会重复序列化整份摘要
// （约 8MB / 5000 文档），因此只在索引确实变更且距上次落盘超过该间隔时才写盘。
const summarySaveInterval = 60 * time.Second

func newSearchIndex() *searchIndex {
	return &searchIndex{
		docs:          make(map[string]*cachedDoc),
		inverted:      make(map[string]map[string]bool),
		summaryLoaded: false,
		contentBudget: defaultContentBudgetBytes,
	}
}

// searchIndexCache 全局缓存：workspacePath → *searchIndex
var searchIndexCache sync.Map

// getSearchIndex 按 workspacePath 获取或创建索引
func getSearchIndex(workspacePath string) *searchIndex {
	v, _ := searchIndexCache.LoadOrStore(workspacePath, newSearchIndex())
	return v.(*searchIndex)
}

// ClearSearchIndex 清除指定工作区的搜索索引
func ClearSearchIndex(workspacePath string) {
	searchIndexCache.Delete(workspacePath)
}

// ClearAllSearchIndexes 清除所有搜索索引（用于测试）
func ClearAllSearchIndexes() {
	searchIndexCache.Range(func(key, _ interface{}) bool {
		searchIndexCache.Delete(key)
		return true
	})
}

// FlushSearchIndex 强制把指定工作区的索引摘要落盘。
// 常规搜索走节流保存（maybeSaveSummary），不会每次都写盘；
// 这个方法供「应用退出 / 切换工作区」等需要确保落盘的时机调用。
func FlushSearchIndex(workspacePath string) error {
	v, ok := searchIndexCache.Load(workspacePath)
	if !ok {
		return nil
	}
	idx := v.(*searchIndex)
	idx.mu.Lock()
	idx.dirty = false
	idx.lastSave = time.Now()
	idx.mu.Unlock()
	return idx.SaveSummary(workspacePath)
}

// ---------------------------------------------------------------------------
// 增量更新
// ---------------------------------------------------------------------------

// refresh 增量更新索引：扫描工作区，仅重新索引新增/变更文件，移除已删除文件。
// 返回 scanComplete 表示扫描是否完整（未触及 maxScanFiles 上限）。
func (idx *searchIndex) refresh(workspacePath string) (scanComplete bool, err error) {
	// refreshMu 只串行化 refresh 自身，不阻塞查询（query 取读锁即可）。
	idx.refreshMu.Lock()
	defer idx.refreshMu.Unlock()

	// -------------------------------------------------------------------
	// 阶段 1（无锁）：扫描目录树。
	// Walk 回调已提供 FileInfo，无需再对每个文件单独 os.Stat（省一轮 IO）。
	// -------------------------------------------------------------------
	type scanned struct {
		absPath string
		modTime time.Time
	}
	current := make(map[string]scanned)
	scanComplete = true

	walkErr := filepath.Walk(workspacePath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
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
			if info.Size() > maxSearchFileSize {
				return nil
			}
			if len(current) >= maxScanFiles {
				scanComplete = false
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(workspacePath, path)
			rel = filepath.ToSlash(rel)
			current[rel] = scanned{absPath: path, modTime: info.ModTime()}
		}
		return nil
	})
	if walkErr != nil {
		return false, walkErr
	}

	// -------------------------------------------------------------------
	// 阶段 2（读锁）：与现有索引比对，算出待删除 / 待重建 / 待补内容的集合。
	// -------------------------------------------------------------------
	idx.mu.RLock()
	var toRemove []*cachedDoc
	if scanComplete {
		// 仅在完整扫描时移除已删除文件（避免因触及上限误删未扫描到的文件）
		for rel, doc := range idx.docs {
			if _, exists := current[rel]; !exists {
				toRemove = append(toRemove, doc)
			}
		}
	}
	var toBuild []string  // 新增或 modtime 变化 → 需完整重建（读正文 + tokenize）
	var toRelink []string // 命中缓存 → 补挂绝对路径（摘要恢复的文档 absPath 为空）
	for rel, sc := range current {
		doc, cached := idx.docs[rel]
		if !cached || !doc.modTime.Equal(sc.modTime) {
			toBuild = append(toBuild, rel)
			continue
		}
		// 正文按需加载：命中缓存时不再补读正文，只保证 absPath 可用
		// （首次查询该文档时才 ReadFile）。
		doc.contentMu.Lock()
		needsRelink := doc.absPath != sc.absPath
		doc.contentMu.Unlock()
		if needsRelink {
			toRelink = append(toRelink, rel)
		}
	}
	idx.mu.RUnlock()

	// -------------------------------------------------------------------
	// 阶段 3（无锁 IO）：磁盘读与分词是最耗时的部分，全部放在锁外完成。
	// -------------------------------------------------------------------
	type builtDoc struct {
		rel string
		doc *cachedDoc
	}
	built := make([]builtDoc, 0, len(toBuild))
	for _, rel := range toBuild {
		sc := current[rel]
		content, readErr := os.ReadFile(sc.absPath)
		if readErr != nil {
			continue
		}
		contentStr := string(content)

		// 提取标题
		title := filepath.Base(rel)
		for _, line := range strings.Split(contentStr, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "# ") {
				title = strings.TrimPrefix(trimmed, "# ")
				break
			}
		}

		// 分词并构建 token 集合
		tokens := tokenize(contentStr)
		tokenSet := make(map[string]bool, len(tokens))
		for _, t := range tokens {
			tokenSet[t] = true
		}

		built = append(built, builtDoc{rel: rel, doc: &cachedDoc{
			relPath: rel,
			absPath: sc.absPath,
			modTime: sc.modTime,
			title:   title,
			// 正文已在手上（刚读过），先留着；是否保留由 enforceContentBudget 决定
			content:       contentStr,
			contentLower:  strings.ToLower(contentStr),
			contentLoaded: true,
			tokenSet:      tokenSet,
			lastUsed:      time.Now().UnixNano(),
		}})
	}

	// -------------------------------------------------------------------
	// 阶段 4（写锁）：仅应用增量 diff，把持锁时间压到最短。
	// -------------------------------------------------------------------
	idx.mu.Lock()
	for _, doc := range toRemove {
		idx.removeDocLocked(doc)
		idx.dirty = true
	}
	for _, b := range built {
		// 重新索引前先删除旧 token
		if old, ok := idx.docs[b.rel]; ok {
			idx.removeDocLocked(old)
		}
		for token := range b.doc.tokenSet {
			if idx.inverted[token] == nil {
				idx.inverted[token] = make(map[string]bool)
			}
			idx.inverted[token][b.rel] = true
		}
		idx.docs[b.rel] = b.doc
		// 文档尚未发布到 idx.docs 之外，此处记账无需 contentMu
		idx.accountContentBytes(int64(len(b.doc.content)) * 2)
		idx.dirty = true
	}
	for _, rel := range toRelink {
		if doc, ok := idx.docs[rel]; ok {
			doc.contentMu.Lock()
			doc.absPath = current[rel].absPath
			doc.contentMu.Unlock()
		}
	}
	idx.mu.Unlock()

	// 正文预算在锁外执行：内部需要取读锁做快照，持写锁调用会自锁
	idx.enforceContentBudget()

	return scanComplete, nil
}

// removeDocLocked 从索引中移除文档（调用者须持有写锁）
//
// 除了摘除倒排表项，还会把文档标记为 removed 并归还其正文内存。
// 标记是必要的：并发查询可能仍持有该文档的指针（候选集在锁外才取正文），
// 有了 removed 标记，contentOnce 就能识别出「这份旧正文已失效」而不是拿它去算结果。
func (idx *searchIndex) removeDocLocked(doc *cachedDoc) {
	for token := range doc.tokenSet {
		if postings, ok := idx.inverted[token]; ok {
			delete(postings, doc.relPath)
			if len(postings) == 0 {
				delete(idx.inverted, token)
			}
		}
	}
	delete(idx.docs, doc.relPath)
	doc.contentMu.Lock()
	doc.removed = true
	doc.releaseContentLocked(idx)
	doc.contentMu.Unlock()
}

// ---------------------------------------------------------------------------
// 查询
// ---------------------------------------------------------------------------

// query 使用反向索引快速筛选候选文档。
// 返回包含任意 query token 的文档列表，调用者须持有读锁。
func (idx *searchIndex) query(queryLower string) []*cachedDoc {
	queryTokens := tokenize(queryLower)
	if len(queryTokens) == 0 {
		return nil
	}

	// 收集包含任意 query token 的候选文档
	candidateSet := make(map[string]bool)
	for _, qt := range queryTokens {
		if postings, ok := idx.inverted[qt]; ok {
			for rel := range postings {
				candidateSet[rel] = true
			}
		}
	}

	candidates := make([]*cachedDoc, 0, len(candidateSet))
	for rel := range candidateSet {
		if doc, ok := idx.docs[rel]; ok {
			candidates = append(candidates, doc)
		}
	}
	return candidates
}

// stats 返回索引统计信息
func (idx *searchIndex) stats() (docCount, tokenCount int) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.docs), len(idx.inverted)
}

// ---------------------------------------------------------------------------
// 索引摘要持久化（冷启动加速）
//
// 现有反向索引是纯内存的，每次进程启动首次搜索都要扫描全工作区重建。
// 引入 SQLite+FTS5 的收益主要也是冷启动加速（详见 docs/SQLITE_FTS5_EVAL.md），
// 但其迁移成本远高于引入「索引摘要持久化」。
//
// 摘要策略：
//   - 启动时（首次搜索前）调用 LoadSummary 加载历史 tokenSet
//   - 扫描时按 modtime 比对，仅对变更文件重新读内容 + tokenize
//   - 未变更文件直接复用历史 tokenSet，跳过 ReadFile（节省 IO 与 tokenize CPU）
//   - 一次 refresh 完成后调用 SaveSummary 持久化最新摘要
//
// 摘要文件：%APPDATA%/NoteVault/search-index/<workspace-hash>.json
// 文件大小：约 8MB / 5000 文档（仅 tokenSet，不含 content/contentLower）
// ---------------------------------------------------------------------------

// indexSummary 摘要中每条记录的形态
type indexSummaryEntry struct {
	RelPath  string    `json:"relPath"`
	Title    string    `json:"title"`
	ModTime  time.Time `json:"modTime"`
	TokenSet []string  `json:"tokenSet"`
}

// indexSummaryFile 摘要文件结构
type indexSummaryFile struct {
	WorkspacePath string              `json:"workspacePath"`
	UpdatedAt     time.Time           `json:"updatedAt"`
	Entries       []indexSummaryEntry `json:"entries"`
}

// searchIndexDir 返回摘要目录：优先 %APPDATA%/NoteVault/search-index，回退 TempDir
func searchIndexDir() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "NoteVault", "search-index")
}

// summaryPathFor 计算工作区对应的摘要文件路径
// 用 sha1(workspacePath) 作为文件名，避免路径分隔符问题
func summaryPathFor(workspacePath string) string {
	h := sha1.Sum([]byte(filepath.ToSlash(filepath.Clean(workspacePath))))
	name := hex.EncodeToString(h[:]) + ".json"
	return filepath.Join(searchIndexDir(), name)
}

// SaveSummary 把当前索引的 tokenSet + 标题 + modtime 持久化
// 必须在持有写锁时调用；失败不阻塞搜索流程
func (idx *searchIndex) SaveSummary(workspacePath string) error {
	idx.mu.RLock()
	entries := make([]indexSummaryEntry, 0, len(idx.docs))
	for rel, doc := range idx.docs {
		tokens := make([]string, 0, len(doc.tokenSet))
		for t := range doc.tokenSet {
			tokens = append(tokens, t)
		}
		entries = append(entries, indexSummaryEntry{
			RelPath:  rel,
			Title:    doc.title,
			ModTime:  doc.modTime,
			TokenSet: tokens,
		})
	}
	idx.mu.RUnlock()

	summary := indexSummaryFile{
		WorkspacePath: filepath.ToSlash(filepath.Clean(workspacePath)),
		UpdatedAt:     time.Now(),
		Entries:       entries,
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return core.WrapError(core.ErrInternal, "序列化索引摘要失败", err)
	}
	dir := searchIndexDir()
	if err := os.MkdirAll(dir, 0750); err != nil {
		return core.WrapError(core.ErrPermission, "创建摘要目录失败: "+dir, err)
	}
	path := summaryPathFor(workspacePath)
	// 原子写：先写临时文件再重命名，避免半截文件被读（测试断言不残留 .tmp）
	if err := atomicWrite(path, data, 0640); err != nil {
		return core.OsToNVError(err, "写入摘要失败: "+path)
	}
	return nil
}

// maybeSaveSummary 在「索引已变更 且 距上次落盘超过 summarySaveInterval」时才落盘。
// 原先 Search 无条件 defer SaveSummary，等于每次按键搜索都把整份摘要
// （约 8MB / 5000 文档）序列化并写盘一次，是最主要的性能热点。
// 失败不阻塞搜索流程（与 SaveSummary 的调用方约定一致）。
func (idx *searchIndex) maybeSaveSummary(workspacePath string) {
	idx.mu.Lock()
	if !idx.dirty || time.Since(idx.lastSave) < summarySaveInterval {
		idx.mu.Unlock()
		return
	}
	idx.dirty = false
	idx.lastSave = time.Now()
	idx.mu.Unlock()

	_ = idx.SaveSummary(workspacePath)
}

// LoadSummary 从磁盘加载摘要到当前索引
// 仅恢复 tokenSet / title / modTime；absPath 与 content 留空
// （absPath 由 refresh 补挂，content 在查询命中时按需读取）
// 必须在调用 refresh 之前调用；调用者负责后续 refresh 时按 modtime 比对
// 幂等：多次调用只加载一次（基于 summaryLoaded 标志）
func (idx *searchIndex) LoadSummary(workspacePath string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if idx.summaryLoaded {
		// 已加载过摘要，避免重复填充 inverted map（重复会无脑叠加相同 token）
		return nil
	}
	idx.summaryLoaded = true

	path := summaryPathFor(workspacePath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 摘要不存在视为空，不是错误
		}
		return core.OsToNVError(err, "读取摘要失败: "+path)
	}
	var summary indexSummaryFile
	if err := json.Unmarshal(data, &summary); err != nil {
		// 损坏的摘要不应阻塞搜索，返回 nil 让上层重新扫描
		return nil
	}
	// 校验 workspacePath 一致（避免哈希碰撞误读其他工作区的摘要）
	expected := filepath.ToSlash(filepath.Clean(workspacePath))
	if summary.WorkspacePath != expected {
		return nil
	}
	for _, e := range summary.Entries {
		tokenSet := make(map[string]bool, len(e.TokenSet))
		for _, t := range e.TokenSet {
			tokenSet[t] = true
			if idx.inverted[t] == nil {
				idx.inverted[t] = make(map[string]bool)
			}
			idx.inverted[t][e.RelPath] = true
		}
		idx.docs[e.RelPath] = &cachedDoc{
			relPath:  e.RelPath,
			modTime:  e.ModTime,
			title:    e.Title,
			tokenSet: tokenSet,
			// absPath 留空：摘要只记录相对路径，绝对路径由后续 refresh 的 toRelink 补挂。
			// content / contentLower 也留空（contentLoaded=false），查询命中时才 ReadFile。
		}
	}
	return nil
}
