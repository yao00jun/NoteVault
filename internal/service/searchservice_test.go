package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearch_EmptyQuery(t *testing.T) {
	s := NewSearchService(NewFileService())
	got, err := s.Search(t.TempDir(), "   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty query should return 0 results, got %d", len(got))
	}
}

func TestSearch_NoMatch(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# 标题\n这是一些内容，没有关键词。")
	s := NewSearchService(NewFileService())
	// 查询与文档不共享任何 bigram。
	// 注意：不能用「不存在的关键词xyz」——它与文档共享「关键」「键词」两个
	// bigram，BM25 下属于合理的部分匹配，会召回该文档（见下方 PartialMatch 测试）。
	got, err := s.Search(dir, "zzzzqqqq")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 results, got %d", len(got))
	}
}

// TestSearch_PartialMatchIsAllowed 记录 P0-1 后的行为变更。
//
// 旧实现要求查询串在正文里**完整连续出现**，因此搜「不存在的关键词xyz」
// 在含「关键词」的文档上返回 0 条。BM25 是部分匹配打分，共享 bigram 就会召回。
// 这是刻意的：完全匹配才能召回的话，「Python 教程」这种多词查询就永远搜不到
// 只有「Python」的文档（实测旧实现多词查询 20/20 全部零结果）。
func TestSearch_PartialMatchIsAllowed(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# 标题\n这是一些内容，没有关键词。")
	s := NewSearchService(NewFileService())
	got, err := s.Search(dir, "不存在的关键词xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("partial match should still recall the doc, got %d", len(got))
	}
}

// TestSearch_MultiWordQuery 多词查询（词间空格）必须能召回。
// 旧实现把整个查询（含空格）当成一个子串去 strings.Count，必然匹配不到。
func TestSearch_MultiWordQuery(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# 缓存策略\n讨论缓存失效与过期策略的取舍。")
	mustWrite(t, filepath.Join(dir, "b.md"), "# 无关\n今天天气不错。")
	s := NewSearchService(NewFileService())

	got, err := s.Search(dir, "缓存 过期策略")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Path != "a.md" {
		t.Fatalf("multi-word query failed, got %+v", got)
	}
}

// TestSearch_GetIndexStats_ReportsSkipped 验证超限文件不再被静默跳过（P0-5）。
//
// 旧行为：超过 maxSearchFileSize 的文件直接 return nil，用户完全不知情，
// 会以为「搜不到就是没有」。现在必须能在统计里查到跳过数量。
func TestSearch_GetIndexStats_ReportsSkipped(t *testing.T) {
	ClearAllSearchIndexes()
	dir := t.TempDir()

	mustWrite(t, filepath.Join(dir, "normal.md"), "# 正常\n这是普通大小的笔记。")
	// 造一个超过 maxSearchFileSize（2MB）的文件
	big := strings.Repeat("填充内容填充内容填充内容填充内容\n", 200000)
	mustWrite(t, filepath.Join(dir, "huge.md"), "# 超大\n"+big)

	s := NewSearchService(NewFileService())
	stats, err := s.GetIndexStats(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.DocCount != 1 {
		t.Errorf("expected 1 indexed doc, got %d", stats.DocCount)
	}
	if stats.SkippedCount != 1 {
		t.Errorf("expected 1 skipped oversize file, got %d", stats.SkippedCount)
	}
	if !stats.ScanComplete {
		t.Errorf("scan should be complete (limit not reached)")
	}
}

// TestSearch_GetIndexStats_NoWorkspace 空工作区路径应报错而不是返回零值
func TestSearch_GetIndexStats_NoWorkspace(t *testing.T) {
	s := NewSearchService(NewFileService())
	if _, err := s.GetIndexStats("  "); err == nil {
		t.Fatalf("expected error for empty workspace")
	}
}

// TestSearch_BM25PenalizesLongSpam 是 P0-1 的核心回归测试。
//
// 短文档关键词密度高、绝对词频低；长文档反过来。
// 按 matchCount（词频）排序时长文档会霸榜，BM25 的长度归一要能压住它。
func TestSearch_BM25PenalizesLongSpam(t *testing.T) {
	ClearAllSearchIndexes()
	dir := t.TempDir()

	// 短而高度相关：关键词只出现 2 次，但全文才 40 字
	mustWrite(t, filepath.Join(dir, "short.md"), "# 缓存\n缓存失效的处理。缓存失效很关键。")
	// 长而低密度：关键词出现 6 次（比短文档多），但夹在 3000 字的填充里
	var long strings.Builder
	long.WriteString("# 杂记\n")
	for i := 0; i < 300; i++ {
		long.WriteString("这是一段与主题无关的填充内容，用于拉长文档篇幅。")
	}
	for i := 0; i < 6; i++ {
		long.WriteString("另外也提到缓存失效这一点。")
	}
	mustWrite(t, filepath.Join(dir, "long.md"), long.String())

	s := NewSearchService(NewFileService())
	got, err := s.Search(dir, "缓存失效")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}
	if got[0].Path != "short.md" {
		t.Fatalf("BM25 should rank the dense short doc first, got %q first", got[0].Path)
	}
}

func TestSearch_BasicMatchAndTitle(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "note.md"), "# 我的笔记\n这里包含 apple 这个关键词，apple 很重要。")
	s := NewSearchService(NewFileService())
	got, err := s.Search(dir, "apple")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if got[0].Title != "我的笔记" {
		t.Fatalf("expected title '我的笔记', got %q", got[0].Title)
	}
	if got[0].MatchCount != 2 {
		t.Fatalf("expected 2 matches, got %d", got[0].MatchCount)
	}
	if !strings.Contains(got[0].Snippet, "apple") {
		t.Fatalf("snippet should contain 'apple', got %q", got[0].Snippet)
	}
}

// TestSearch_NoGarbledUTF8 验证 snippet 不会被切到多字节字符中间导致 JSON 序列化替换为 U+FFFD
func TestSearch_NoGarbledUTF8(t *testing.T) {
	dir := t.TempDir()
	// 中文文档，"修改"出现一次，前后都是中文（每字 3 字节 UTF-8）
	mustWrite(t, filepath.Join(dir, "SQL 基础.md"), strings.Repeat("中", 100)+"\n# SQL 基础：用于定义、查询和修改关系型数据。SELECT、JOIN、GROUP BY、INSERT。\n"+strings.Repeat("字", 100))
	s := NewSearchService(nil)
	got, err := s.Search(dir, "修改")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	snippet := got[0].Snippet
	// 关键断言：snippet 不应包含 UTF-8 替换字符 U+FFFD（"�"）
	if strings.ContainsRune(snippet, '\uFFFD') {
		t.Errorf("snippet contains U+FFFD replacement char (UTF-8 byte split), got %q", snippet)
	}
	// 也应包含高亮词
	if !strings.Contains(snippet, "修改") {
		t.Errorf("snippet should contain query '修改', got %q", snippet)
	}
}

func TestSearch_SkipHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	// 普通文件命中
	mustWrite(t, filepath.Join(dir, "visible.md"), "# 可见\nsecret 在这里。")
	// 隐藏目录中的文件不应被搜索到
	hidden := filepath.Join(dir, ".trash")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(hidden, "deleted.md"), "# 已删除\nsecret 在这里。")
	s := NewSearchService(NewFileService())
	got, err := s.Search(dir, "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("hidden dir should be skipped: expected 1, got %d", len(got))
	}
	if strings.Contains(got[0].Path, ".trash") {
		t.Fatalf("result should not come from .trash, got %q", got[0].Path)
	}
}

func TestSearch_SkipLargeFiles(t *testing.T) {
	dir := t.TempDir()
	// 构造一个 > 2MB 的文件，且包含关键词
	big := strings.Repeat("无关的填充内容。", 200000) + "needle 是我们要找的关键词。"
	mustWrite(t, filepath.Join(dir, "big.md"), big)
	// 普通小文件命中
	mustWrite(t, filepath.Join(dir, "small.md"), "# 小文件\nneedle 在这里。")
	s := NewSearchService(NewFileService())
	got, err := s.Search(dir, "needle")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("large file should be skipped: expected 1, got %d", len(got))
	}
	if got[0].Path != "small.md" {
		t.Fatalf("expected small.md, got %q", got[0].Path)
	}
}

func TestSearch_ResultCap(t *testing.T) {
	s := NewSearchService(NewFileService())
	// 按实例注入较小的结果上限（原先是改包级 var，会污染同进程其它用例），
	// 同时避免在沙箱中创建 250 个文件导致超时。
	s.maxResults = 10

	dir := t.TempDir()
	// 生成超过上限数量的小文件，全部命中
	for i := 0; i < s.maxResults+20; i++ {
		name := filepath.Join(dir, "file"+itoa(i)+".md")
		mustWrite(t, name, "# 文档\ncommonword 关键词。")
	}
	got, err := s.Search(dir, "commonword")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) > s.maxResults {
		t.Fatalf("results should be capped at %d, got %d", s.maxResults, len(got))
	}
}

// TestSearchResultCap_Fallback 零值实例（未走构造函数）应回落默认上限而不是返回空结果
func TestSearchResultCap_Fallback(t *testing.T) {
	if got := (&SearchService{}).resultCap(); got != defaultMaxSearchResults {
		t.Fatalf("zero-value resultCap = %d, want %d", got, defaultMaxSearchResults)
	}
	if got := NewSearchService(nil).resultCap(); got != defaultMaxSearchResults {
		t.Fatalf("constructor resultCap = %d, want %d", got, defaultMaxSearchResults)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b strings.Builder
	for i > 0 {
		b.WriteByte(byte('0' + i%10))
		i /= 10
	}
	return b.String()
}
