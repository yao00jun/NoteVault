package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractFrontMatterTags(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "空内容",
			content: "",
			want:    []string{},
		},
		{
			name:    "无 front matter（只有行内标签）",
			content: "这是一篇普通文档，没有 front matter\n#标签1 #标签2",
			want:    []string{},
		},
		{
			name: "单行数组格式",
			content: `---
title: 测试
tags: [编程, Go, 笔记]
---

正文内容 #额外标签`,
			want: []string{"编程", "Go", "笔记"}, // 不含行内 #额外标签
		},
		{
			name: "多行格式",
			content: `---
title: 测试
tags:
  - 编程
  - Go
  - 笔记
---

正文内容`,
			want: []string{"编程", "Go", "笔记"},
		},
		{
			name: "单值格式",
			content: `---
tags: 单一标签
---`,
			want: []string{"单一标签"},
		},
		{
			name: "带引号",
			content: `---
tags: ["编程", 'Go', "笔记"]
---`,
			want: []string{"编程", "Go", "笔记"},
		},
	}

	// 期望结果不关心顺序，比较时排序
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFrontMatterTags(tt.content)
			if !equalUnordered(got, tt.want) {
				t.Errorf("extractFrontMatterTags() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractTags(t *testing.T) {
	s := NewTagService()

	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "行内标签",
			content: "今天学习了 #Go 和 #Python 还有 #日语",
			want:    []string{"Go", "Python", "日语"},
		},
		{
			name: "YAML front matter + 行内",
			content: `---
tags: [编程, 工作]
---

今日总结 #复盘`,
			want: []string{"编程", "工作", "复盘"},
		},
		{
			name:    "无标签",
			content: "没有标签的纯文本。",
			want:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.extractTags(tt.content)
			if !equalUnordered(got, tt.want) {
				t.Errorf("extractTags() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestTagServiceEndToEnd 端到端测试 - 写文件到临时目录再扫描
func TestTagServiceEndToEnd(t *testing.T) {
	tmp := t.TempDir()

	// 创建两个文件：含 front matter + 行内
	mustWrite(t, filepath.Join(tmp, "doc1.md"), `---
tags: [编程, Go]
---

#标题1

正文内容
`)

	mustWrite(t, filepath.Join(tmp, "doc2.md"), `#标题2

这是一个使用 #Python 和 #编程 的文档。`)

	mustWrite(t, filepath.Join(tmp, "doc3.txt"), "不应被扫描的非 markdown 文件")

	s := NewTagService()
	tags, err := s.GetAllTags(tmp)
	if err != nil {
		t.Fatalf("GetAllTags failed: %v", err)
	}

	tagMap := make(map[string]int)
	for _, tag := range tags {
		tagMap[tag.Name] = tag.Count
	}

	checks := map[string]int{
		"编程":     2, // front matter + 行内
		"Go":     1, // front matter
		"Python": 1, // 行内
	}

	if len(tags) < len(checks) {
		t.Errorf("预期至少 %d 个标签，实际得到 %d", len(checks), len(tags))
	}
	for name, want := range checks {
		if got, ok := tagMap[name]; !ok {
			t.Errorf("缺少标签 #%s", name)
		} else if got != want {
			t.Errorf("标签 #%s 计数 = %d, want %d", name, got, want)
		}
	}
}

// TestGetFilesByTag 测试按标签查找文件
func TestGetFilesByTag(t *testing.T) {
	tmp := t.TempDir()

	mustWrite(t, filepath.Join(tmp, "a.md"), "# 文档A\n#Go #编程 笔记\n内容")
	mustWrite(t, filepath.Join(tmp, "b.md"), "# 文档B\n#Python #编程 进阶\n内容")
	mustWrite(t, filepath.Join(tmp, "c.md"), "# 文档C\n无标签\n内容")

	s := NewTagService()

	// 查找 #编程 标签
	files, err := s.GetFilesByTag(tmp, "编程")
	if err != nil {
		t.Fatalf("GetFilesByTag failed: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files with tag '编程', got %d", len(files))
	}

	// 查找 #Go 标签（大小写不敏感）
	files, err = s.GetFilesByTag(tmp, "go")
	if err != nil {
		t.Fatalf("GetFilesByTag failed: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file with tag 'go', got %d", len(files))
	}
	if files[0].Title != "文档A" {
		t.Fatalf("expected title '文档A', got '%s'", files[0].Title)
	}

	// 查找不存在的标签
	files, err = s.GetFilesByTag(tmp, "不存在的标签")
	if err != nil {
		t.Fatalf("GetFilesByTag failed: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files for nonexistent tag, got %d", len(files))
	}

	// 带前缀 # 的标签名也能匹配
	files, err = s.GetFilesByTag(tmp, "#Python")
	if err != nil {
		t.Fatalf("GetFilesByTag with # prefix failed: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file for '#Python', got %d", len(files))
	}
}

// mustWrite 写入测试文件
func mustWrite(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("写入失败 %s: %v", path, err)
	}
}

// equalUnordered 比较两个字符串切片（顺序无关）
func equalUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int)
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
		if m[s] < 0 {
			return false
		}
	}
	return true
}

// 以下两个用例原位于 apperrors_test.go（与错误测试混装），迁移时按包归属并入本文件。
// 它们依赖同包的 mustWrite 与 NewTagService，故留在 internal/service。

func TestTagCache_TTLExpiry(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# A\n#tag1")

	ts := NewTagService()
	tags, err := ts.GetAllTags(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].Name != "tag1" {
		t.Fatalf("expected tag1, got %v", tags)
	}

	// 修改文件（加新标签），但缓存未过期 → 应返回旧结果
	time.Sleep(10 * time.Millisecond)
	mustWrite(t, filepath.Join(dir, "a.md"), "# A\n#tag1 #tag2")
	tags, _ = ts.GetAllTags(dir)
	if len(tags) != 1 {
		t.Fatalf("cache should still return 1 tag, got %d", len(tags))
	}

	// 手动失效缓存 → 应返回新结果
	ts.InvalidateCache(dir)
	tags, _ = ts.GetAllTags(dir)
	if len(tags) != 2 {
		t.Fatalf("after invalidate, expected 2 tags, got %d", len(tags))
	}
}

func TestTagCache_GetFilesByTag(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# A\n#project #important")
	mustWrite(t, filepath.Join(dir, "b.md"), "# B\n#project")

	ts := NewTagService()

	// 查 project 标签
	files, err := ts.GetFilesByTag(dir, "project")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files with 'project' tag, got %d", len(files))
	}

	// 查不存在的标签
	files, _ = ts.GetFilesByTag(dir, "nonexistent")
	if len(files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(files))
	}

	// 带 # 前缀
	files, _ = ts.GetFilesByTag(dir, "#project")
	if len(files) != 2 {
		t.Fatalf("expected 2 files with '#project' (prefix stripped), got %d", len(files))
	}
}
