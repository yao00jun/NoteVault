package service

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/notevault/notevault/internal/core"
	"github.com/notevault/notevault/internal/infra/schema"
)

// ---------------------------------------------------------------------------
// 分词器
// ---------------------------------------------------------------------------

// tokenize 将文本拆分为小写 token。
//
// 分词策略（2026-08-31 由「CJK 单字」改为「CJK 二元切分」）：
//   - 拉丁文/数字/下划线连续串作为一个 token；
//   - CJK 连续段长度 >= 2 时产出全部 bigram（滑动窗口），长度 == 1 时产出该单字；
//   - 其他字符（空格、标点、连字符等）作为分隔符。
//
// 为什么改成 bigram：
//  1. 单字区分度太低。搜「缓存失效」切成 缓/存/失/效 四个单字后，
//     含「存储」「失败」「失望」的文档都会命中，倒排表几乎失去筛选能力；
//  2. bigram（缓存/存失/失效）保留相邻关系，是中文检索的标准做法；
//  3. 顺带支持多词查询——查询与文档用同一套切分，词间空格不再是障碍；
//  4. token 数量几乎不变：n 个连续汉字的单字 token 是 n 个，bigram 是 n-1 个，
//     因此倒排表与索引摘要的体积不受影响。
//
// 例：「缓存失效」→ [缓存, 存失, 失效]；「Hello 世界」→ [hello, 世界]
func tokenize(s string) []string {
	s = strings.ToLower(s)
	tokens := make([]string, 0, len(s)/4)

	var latin strings.Builder // 拉丁/数字/下划线缓冲
	var han []rune            // 连续 CJK 缓冲

	flushLatin := func() {
		if latin.Len() > 0 {
			tokens = append(tokens, latin.String())
			latin.Reset()
		}
	}
	flushHan := func() {
		if len(han) == 1 {
			// 孤立单字：没有相邻字可组 bigram，只能自己作为一个 token
			tokens = append(tokens, string(han[0]))
		}
		for i := 0; i+1 < len(han); i++ {
			tokens = append(tokens, string(han[i:i+2]))
		}
		han = han[:0]
	}

	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r):
			flushLatin()
			han = append(han, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			flushHan()
			latin.WriteRune(r)
		default:
			flushLatin()
			flushHan()
		}
	}
	flushLatin()
	flushHan()
	return tokens
}

// ---------------------------------------------------------------------------
// 缓存文档
// ---------------------------------------------------------------------------

// cachedDoc 缓存单个 Markdown 文件的索引元数据，正文按需加载。
//
// 内存模型（2026-08-29 重构）：
//   - 常驻：relPath / modTime / title / tokenFreq / docLen。
//     反向索引、候选筛选与 BM25 打分只依赖它们，
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

	// tokenFreq token → 出现次数。
	//
	// 原为 map[string]bool（仅集合），P0-1 升级为词频以支持 BM25 打分。
	// 没有保留两个 map：二者 key 集合完全相同，同存一份会让索引内存翻倍，
	// 而增量删除、倒排表维护都只需要遍历 key。
	//
	// 构建完成后不再修改，可在持有 idx.mu 读锁时安全读取。
	tokenFreq map[string]int

	// docLen 文档 token 总数（BM25 的长度归一用）。
	// 旧实现没有这个字段，因此排序无法区分「关键词密度高」与「文档本身长」，
	// 长文档仅凭篇幅就能在 matchCount 排序里霸榜。
	docLen int

	// titleFreq 标题的词频（P0-3 字段加权用）。
	//
	// 刻意不写进索引摘要：标题很短，从 doc.title 现算 tokenize 的成本可以忽略，
	// 而写进摘要会让格式再升一个版本、旧摘要全部失效。
	titleFreq map[string]int

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

	// watcher 事件驱动失效（P2 优化）：
	//   watchActive  = workspace watcher 正在监控该工作区（事件可靠到达）
	//   watchPending = watcher 收到过事件、尚未被 refresh 消化
	//   built        = 索引至少完整构建过一次（区分"空库"与"还没建"）
	// watchActive && !watchPending && built 时 refresh 可短路跳过全量 walk——
	// 索引与磁盘一致由 watcher 事件保证。事件到达时置 watchPending，
	// 下一次 refresh 照常 walk 消化变更后归零。walk 保留作兜底：
	// watcher 未启动/漏事件时行为与旧版完全一致。
	watchActive  bool
	watchPending bool
	built        bool

	// 正文缓存的内存账本：contentBytes 为当前占用，contentBudget 为上限。
	// 二者共同保证「正文内存可预期」，而不是随知识库体积线性增长。
	contentBytes  atomic.Int64
	contentBudget int64

	// BM25 统计（P0-1）
	//
	// totalDocLen = 全部文档 docLen 之和，用于算平均文档长度。
	// 另外两个统计量刻意不落字段，避免与源头失同步：
	//   - 文档总数 N       → len(idx.docs)
	//   - 某 token 的 df   → len(idx.inverted[token])
	totalDocLen int64

	// 索引覆盖范围（P0-5）。
	//
	// 旧实现里这两件事都是静默发生的：文件超过 maxSearchFileSize 被跳过、
	// 扫描触及 maxScanFiles 提前中断，用户完全不知情，
	// 会以为「搜不到就是没有」。现在记录下来，由状态栏显式提示。
	scanComplete    bool // 上次扫描是否完整（false = 触及 maxScanFiles 上限）
	skippedOversize int  // 因体积超过 maxSearchFileSize 被跳过的文件数
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

// 索引的存取与生命周期管理已移到 indexregistry.go（E-4）。
//
// 原先这里的包级 sync.Map 没有释放路径：切走工作区后索引仍常驻内存，
// 而正文缓存预算是按「单个工作区」计的，开的库越多总占用越不可控。
// 现在由 SearchIndexRegistry 统一持有，WorkspaceService 负责在切换 / 删除时释放。
// 兼容入口（getSearchIndex / ClearSearchIndex / ClearAllSearchIndexes /
// FlushSearchIndex）保留为转发到包级默认注册表，既有测试无需改动。

// ---------------------------------------------------------------------------
// 增量更新
// ---------------------------------------------------------------------------

// MarkWatchActive 声明 workspace watcher 已开始监控该工作区。
// 由 WorkspaceService 在启动 watcher 后调用一次。
func (idx *searchIndex) MarkWatchActive() {
	idx.mu.Lock()
	idx.watchActive = true
	idx.mu.Unlock()
}

// InvalidatePath 消化一个 watcher 事件：文件变更直接改写索引
// （单文件粒度，无需全量 walk）。relPath 为工作区相对路径（正斜杠），
// 目录级/无法定位的变更退化为「置 watchPending 让下次 refresh 走一遍 walk」。
// 返回 true 表示已精确消化。
func (idx *searchIndex) InvalidatePath(relPath string, deleted bool) bool {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	doc, ok := idx.docs[relPath]
	if deleted {
		if ok {
			idx.removeDocLocked(doc)
			idx.dirty = true
			return true
		}
		return false
	}
	// 变更/新增：只在文档已在索引时精确处理（modtime 置零强制下次 rebuild）。
	// 新增文件走 watchPending → refresh walk 兜底，避免在这里重复读盘 tokenize。
	if !ok {
		idx.watchPending = true
		return false
	}
	doc.contentMu.Lock()
	doc.modTime = time.Time{} // 零值 modtime 与磁盘比对必不相等 → 下次 refresh rebuild
	doc.contentMu.Unlock()
	idx.dirty = true
	return true
}

// markWatchPending 事件无法精确消化时调用：强制下次 refresh 走一遍 walk。
func (idx *searchIndex) markWatchPending() {
	idx.mu.Lock()
	idx.watchPending = true
	idx.mu.Unlock()
}

// skipRefreshable 报告当前是否可以安全跳过 walk（watcher 活跃且无积压事件、
// 索引已构建过）。加锁快照，避免与 InvalidatePath 竞态。
func (idx *searchIndex) skipRefreshable() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.watchActive && !idx.watchPending && idx.built
}

// refresh 增量更新索引：扫描工作区，仅重新索引新增/变更文件，移除已删除文件。
// 返回 scanComplete 表示扫描是否完整（未触及 maxScanFiles 上限）。
func (idx *searchIndex) refresh(workspacePath string) (scanComplete bool, err error) {
	// refreshMu 只串行化 refresh 自身，不阻塞查询（query 取读锁即可）。
	idx.refreshMu.Lock()
	defer idx.refreshMu.Unlock()

	// watcher 活跃且无积压事件：索引与磁盘一致，跳过全量 walk。
	// 这是逐键实时搜索的性能关键——否则每敲一个字符就 stat 一遍全库。
	// （watchPending 在下面 walk 完成后归零。）
	if idx.skipRefreshable() {
		idx.mu.RLock()
		complete := idx.scanComplete
		idx.mu.RUnlock()
		return complete, nil
	}
	defer func() {
		idx.mu.Lock()
		idx.watchPending = false
		idx.mu.Unlock()
	}()

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
	// 跳过统计（P0-5）：这些数字会在阶段 4 随索引状态一起写回，
	// 让用户知道自己的库有多少内容没被索引，而不是以为「搜不到就是没有」。
	var skippedOversize int

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
				skippedOversize++
				return nil
			}
			if len(current) >= maxScanFiles {
				// 触及扫描上限，scanComplete=false 即代表「还有文件没扫到」
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
		tokenFreq := freqOf(tokens)

		built = append(built, builtDoc{rel: rel, doc: &cachedDoc{
			relPath: rel,
			absPath: sc.absPath,
			modTime: sc.modTime,
			title:   title,
			// 正文已在手上（刚读过），先留着；是否保留由 enforceContentBudget 决定
			content:       contentStr,
			contentLower:  strings.ToLower(contentStr),
			contentLoaded: true,
			tokenFreq:     tokenFreq,
			titleFreq:     freqOf(tokenize(title)),
			docLen:        len(tokens),
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
		// 重新索引前先删除旧 token（顺带归还该文档的 totalDocLen）
		if old, ok := idx.docs[b.rel]; ok {
			idx.removeDocLocked(old)
		}
		for token := range b.doc.tokenFreq {
			if idx.inverted[token] == nil {
				idx.inverted[token] = make(map[string]bool)
			}
			idx.inverted[token][b.rel] = true
		}
		idx.docs[b.rel] = b.doc
		idx.totalDocLen += int64(b.doc.docLen)
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

	// 覆盖范围随索引状态一起写回（P0-5）
	idx.mu.Lock()
	idx.scanComplete = scanComplete
	idx.skippedOversize = skippedOversize
	idx.built = true
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
	for token := range doc.tokenFreq {
		if postings, ok := idx.inverted[token]; ok {
			delete(postings, doc.relPath)
			if len(postings) == 0 {
				delete(idx.inverted, token)
			}
		}
	}
	delete(idx.docs, doc.relPath)
	idx.totalDocLen -= int64(doc.docLen)
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

// ---------------------------------------------------------------------------
// BM25 打分（P0-1）
// ---------------------------------------------------------------------------

const (
	// bm25K1 词频饱和参数。越大则词频越接近线性，越小则越早饱和。
	// 1.2 是 BM25 的经典取值：一篇文档里某词出现 10 次不应当比出现 3 次重要 3 倍。
	bm25K1 = 1.2
	// bm25B 长度归一强度。0 = 完全不管长度，1 = 完全按平均长度归一。
	// 0.75 是经典取值，足以压制「又长又杂的整理稿」仅凭篇幅霸榜。
	bm25B = 0.75
	// bm25TitleBoost 标题字段的加权系数（P0-3）。
	//
	// 标题里的词比正文里的词更能说明文档主题。取 3.0 的经验依据：
	// 足以让主题文档压过「正文里偶然提到一次」的长文档，
	// 又不至于让只改了标题的空壳文档霸榜。
	//
	// 这替换了旧实现里「标题命中额外 +2 分」的魔法常数——
	// 那个常数与词频、文档长度都不成比例，在不同规模的库上表现不一致。
	bm25TitleBoost = 3.0
)

// freqOf 把 token 序列统计成词频表
func freqOf(tokens []string) map[string]int {
	if len(tokens) == 0 {
		return nil
	}
	freq := make(map[string]int, len(tokens))
	for _, t := range tokens {
		freq[t]++
	}
	return freq
}

// scoredDoc 打分中间结构
type scoredDoc struct {
	doc   *cachedDoc
	score float64
}

// scoreBM25 用 BM25 给候选文档打分，返回按分数降序的列表。
//
// 调用者须持有 idx.mu 读锁——本方法只读 docs / inverted / totalDocLen，
// 不碰正文、不做 IO。这是相对旧实现最大的改变：
// 旧实现要对每个候选文档 ReadFile 再 strings.Count，IO 随候选集线性增长
// （实测 1000 文档库单次查询 p95 约 100ms）；BM25 打分全程在内存里完成，
// 只有最终进入结果集的少量文档才需要读正文生成摘要片段。
//
// limit <= 0 表示不截断。
func (idx *searchIndex) scoreBM25(queryTokens []string, limit int) []*scoredDoc {
	if len(queryTokens) == 0 || len(idx.docs) == 0 {
		return nil
	}

	n := float64(len(idx.docs))
	avgdl := float64(idx.totalDocLen) / n
	if avgdl < 1 {
		avgdl = 1
	}

	// 候选集 = 含任意查询 token 的文档并集（与旧实现的筛选口径一致）
	candidates := make(map[*cachedDoc]struct{}, 64)
	for _, qt := range queryTokens {
		for rel := range idx.inverted[qt] {
			if doc, ok := idx.docs[rel]; ok {
				candidates[doc] = struct{}{}
			}
		}
	}

	scored := make([]*scoredDoc, 0, len(candidates))
	for doc := range candidates {
		dl := float64(doc.docLen)
		if dl < 1 {
			dl = 1
		}
		var score float64
		for _, qt := range queryTokens {
			tfAll := float64(doc.tokenFreq[qt])
			if tfAll == 0 {
				continue
			}
			// 字段加权（P0-3）：tokenFreq 统计的是全文，标题行也在其中。
			// 先把标题部分剥离出来单独加权，否则标题词会被重复计入正文权重。
			tfTitle := float64(doc.titleFreq[qt])
			tfBody := tfAll - tfTitle
			if tfBody < 0 {
				// 标题来自文件名时它不在正文里，相减会得到负数
				tfBody = 0
			}
			tf := tfBody + tfTitle*bm25TitleBoost

			df := float64(len(idx.inverted[qt]))
			// BM25 的 IDF（带平滑项，避免 df=0 或 df=N 时发散）
			idf := math.Log(1 + (n-df+0.5)/(df+0.5))
			score += idf * (tf * (bm25K1 + 1)) / (tf + bm25K1*(1-bm25B+bm25B*dl/avgdl))
		}
		if score > 0 {
			scored = append(scored, &scoredDoc{doc: doc, score: score})
		}
	}

	// 分数相同时按路径排序，保证结果稳定（否则 map 遍历顺序会让输出抖动）
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].doc.relPath < scored[j].doc.relPath
	})

	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	return scored
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

// 摘要格式版本登记在 schema.SearchSummary（E-7 统一版本信封）：
//
// v1：仅 token 集合（indexSummaryEntry.TokenSet）
// v2：token 词频 + 文档长度（P0-1，BM25 打分需要这两项）
// v3：迁到统一版本信封（schemaVersion/kind/updatedAt/data）
//
// 版本不匹配的摘要一律丢弃重建，而不是尝试兼容——
// 摘要只是冷启动加速的缓存，重建的代价（一次全量扫描）远小于
// 为兼容旧格式在数据层引入分支的复杂度。
// 这也是本项目里唯一对 CompatLegacy / CompatOlder 也直接丢弃的落盘文件：
// 别处那些都是用户数据，丢了不可再生。

// indexSummary 摘要中每条记录的形态。
//
// Tokens 与 Freqs 是平行数组（而非 map[string]int），
// 目的是压缩序列化体积：5000 文档的摘要约 8MB，
// map 形态会把 key 重复写一遍，平行数组能省下接近一半。
type indexSummaryEntry struct {
	RelPath string    `json:"relPath"`
	Title   string    `json:"title"`
	ModTime time.Time `json:"modTime"`
	Tokens  []string  `json:"tokens"`
	Freqs   []int     `json:"freqs"`
	DocLen  int       `json:"docLen"`
}

// indexSummaryPayload 摘要文件的载荷（信封的 data 字段）。
//
// schemaVersion / kind / updatedAt 由 schema.Envelope 统一承载，这里不再重复。
type indexSummaryPayload struct {
	WorkspacePath string              `json:"workspacePath"`
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
		tokens := make([]string, 0, len(doc.tokenFreq))
		freqs := make([]int, 0, len(doc.tokenFreq))
		for t, f := range doc.tokenFreq {
			tokens = append(tokens, t)
			freqs = append(freqs, f)
		}
		entries = append(entries, indexSummaryEntry{
			RelPath: rel,
			Title:   doc.title,
			ModTime: doc.modTime,
			Tokens:  tokens,
			Freqs:   freqs,
			DocLen:  doc.docLen,
		})
	}
	idx.mu.RUnlock()

	data, err := schema.MarshalAs(schema.SearchSummary, indexSummaryPayload{
		WorkspacePath: filepath.ToSlash(filepath.Clean(workspacePath)),
		Entries:       entries,
	})
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
// 仅恢复 tokenFreq / docLen / title / modTime；absPath 与 content 留空
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
	summary, res, err := schema.UnmarshalAs[indexSummaryPayload](data, schema.SearchSummary)
	if err != nil {
		// 损坏的摘要不应阻塞搜索，返回 nil 让上层重新扫描
		return nil
	}
	// 版本不完全一致（v1/v2 旧摘要、裸格式、更高版本）一律丢弃：重建代价远小于兼容分支。
	// 这里不能静默沿用旧结构——v1 没有词频与文档长度，BM25 会拿着残缺数据算出错误分数。
	if res.Compat != schema.CompatExact {
		return nil
	}
	// 校验 workspacePath 一致（避免哈希碰撞误读其他工作区的摘要）
	expected := filepath.ToSlash(filepath.Clean(workspacePath))
	if summary.WorkspacePath != expected {
		return nil
	}
	for _, e := range summary.Entries {
		tokenFreq := make(map[string]int, len(e.Tokens))
		for i, t := range e.Tokens {
			f := 1
			if i < len(e.Freqs) {
				f = e.Freqs[i]
			}
			tokenFreq[t] = f
			if idx.inverted[t] == nil {
				idx.inverted[t] = make(map[string]bool)
			}
			idx.inverted[t][e.RelPath] = true
		}
		idx.docs[e.RelPath] = &cachedDoc{
			relPath: e.RelPath,
			modTime: e.ModTime,
			title:   e.Title,
			// absPath 留空：摘要只记录相对路径，绝对路径由后续 refresh 的 toRelink 补挂。
			// content / contentLower 也留空（contentLoaded=false），查询命中时才 ReadFile。
			tokenFreq: tokenFreq,
			// titleFreq 不落盘，从 title 现算——标题很短，重算成本可忽略，
			// 换来的是摘要格式不必再升版本。
			titleFreq: freqOf(tokenize(e.Title)),
			docLen:    e.DocLen,
		}
		idx.totalDocLen += int64(e.DocLen)
	}
	return nil
}
