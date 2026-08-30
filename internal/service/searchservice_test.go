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
	got, err := s.Search(dir, "不存在的关键词xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 results, got %d", len(got))
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
