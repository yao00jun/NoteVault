package service

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeNote 在工作区写一篇笔记，自动建父目录。
func writeNote(t *testing.T, ws, relPath, content string) string {
	t.Helper()
	full := filepath.Join(ws, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	return full
}

// findRecord 按相对路径取记录。
func findRecord(t *testing.T, records []*NoteRecord, path string) *NoteRecord {
	t.Helper()
	for _, r := range records {
		if r.Path == path {
			return r
		}
	}
	var have []string
	for _, r := range records {
		have = append(have, r.Path)
	}
	t.Fatalf("找不到记录 %q，实际有 %v", path, have)
	return nil
}

func TestPropertyIndex_ScanExtractsFrontMatterAndImplicit(t *testing.T) {
	ws := t.TempDir()
	writeNote(t, ws, "读书/深度学习.md", `---
title: 深度学习圣经
status: reading
rating: 4.5
tags: [AI, 论文]
---
# 会被 front matter title 覆盖的标题

正文里还有 #机器学习 标签，以及 [[另一篇]] 链接。

- [ ] 读第三章
- [x] 读第一章
- [ ] 做笔记
`)

	idx := newPropertyIndexer()
	records, err := idx.scan(ws)
	if err != nil {
		t.Fatalf("scan 失败: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("记录数 = %d, want 1", len(records))
	}

	r := findRecord(t, records, "读书/深度学习.md")

	// front matter 属性
	if v := r.Get("status"); v.Str != "reading" {
		t.Errorf("status = %+v", v)
	}
	if v := r.Get("rating"); v.Kind != KindNumber || v.Num != 4.5 {
		t.Errorf("rating = %+v", v)
	}

	// 标题：front matter title 优先于 H1
	if r.Title != "深度学习圣经" {
		t.Errorf("Title = %q, want 深度学习圣经（front matter 应优先于 H1）", r.Title)
	}
	if v := r.Get(PropFileTitle); v.Str != "深度学习圣经" {
		t.Errorf("%s = %+v", PropFileTitle, v)
	}

	// 隐式文件属性
	if v := r.Get(PropFilePath); v.Str != "读书/深度学习.md" {
		t.Errorf("%s = %+v（必须是斜杠相对路径）", PropFilePath, v)
	}
	if v := r.Get(PropFileName); v.Str != "深度学习.md" {
		t.Errorf("%s = %+v", PropFileName, v)
	}
	if v := r.Get(PropFileBasename); v.Str != "深度学习" {
		t.Errorf("%s = %+v", PropFileBasename, v)
	}
	if v := r.Get(PropFileExt); v.Str != ".md" {
		t.Errorf("%s = %+v", PropFileExt, v)
	}
	if v := r.Get(PropFileFolder); v.Str != "读书" {
		t.Errorf("%s = %+v", PropFileFolder, v)
	}
	if v := r.Get(PropFileSize); v.Kind != KindNumber || v.Num <= 0 {
		t.Errorf("%s = %+v", PropFileSize, v)
	}
	if v := r.Get(PropFileMtime); v.Kind != KindDate || v.Date.IsZero() {
		t.Errorf("%s = %+v", PropFileMtime, v)
	}

	// 标签：front matter ∪ 行内
	tags := r.Get(PropFileTags)
	if tags.Kind != KindList {
		t.Fatalf("%s.Kind = %v, want list", PropFileTags, tags.Kind)
	}
	want := []string{"AI", "机器学习", "论文"}
	if !reflect.DeepEqual(tags.List, want) {
		t.Errorf("%s = %v, want %v（front matter 与行内标签应合并去重并排序）", PropFileTags, tags.List, want)
	}

	// 出链
	if v := r.Get(PropFileLinks); v.Num != 1 {
		t.Errorf("%s = %v, want 1", PropFileLinks, v.Num)
	}

	// todo 统计
	if v := r.Get(PropTodoTotal); v.Num != 3 {
		t.Errorf("%s = %v, want 3", PropTodoTotal, v.Num)
	}
	if v := r.Get(PropTodoDone); v.Num != 1 {
		t.Errorf("%s = %v, want 1", PropTodoDone, v.Num)
	}
	if v := r.Get(PropTodoPending); v.Num != 2 {
		t.Errorf("%s = %v, want 2", PropTodoPending, v.Num)
	}
}

func TestPropertyIndex_TitleFallbackChain(t *testing.T) {
	ws := t.TempDir()
	writeNote(t, ws, "只有h1.md", "# 我是 H1\n正文")
	writeNote(t, ws, "什么都没有.md", "就是一段正文，没有标题也没有 front matter")
	writeNote(t, ws, "空title.md", "---\ntitle:\n---\n# H1 兜底\n")

	idx := newPropertyIndexer()
	records, err := idx.scan(ws)
	if err != nil {
		t.Fatalf("scan 失败: %v", err)
	}

	if got := findRecord(t, records, "只有h1.md").Title; got != "我是 H1" {
		t.Errorf("只有 H1 时 Title = %q, want 我是 H1", got)
	}
	if got := findRecord(t, records, "什么都没有.md").Title; got != "什么都没有" {
		t.Errorf("无标题时 Title = %q, want 文件名", got)
	}
	// front matter 里 title 为空不能把标题清空，要退回 H1
	if got := findRecord(t, records, "空title.md").Title; got != "H1 兜底" {
		t.Errorf("空 title 时 Title = %q, want H1 兜底", got)
	}
}

func TestPropertyIndex_SkipsDotDirsAndNonMarkdown(t *testing.T) {
	ws := t.TempDir()
	writeNote(t, ws, "正常.md", "内容")
	writeNote(t, ws, ".notevault/内部.md", "不该被扫到")
	writeNote(t, ws, ".git/objects/x.md", "不该被扫到")
	writeNote(t, ws, "图片说明.txt", "不该被扫到")
	writeNote(t, ws, "另一种扩展名.markdown", "该被扫到")

	idx := newPropertyIndexer()
	records, err := idx.scan(ws)
	if err != nil {
		t.Fatalf("scan 失败: %v", err)
	}

	got := make([]string, 0, len(records))
	for _, r := range records {
		got = append(got, r.Path)
	}
	want := []string{"另一种扩展名.markdown", "正常.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("扫描结果 = %v, want %v", got, want)
	}
}

func TestPropertyIndex_ResultIsSortedByPath(t *testing.T) {
	ws := t.TempDir()
	for _, name := range []string{"c.md", "a.md", "b.md", "z/y.md"} {
		writeNote(t, ws, name, "x")
	}

	idx := newPropertyIndexer()
	records, err := idx.scan(ws)
	if err != nil {
		t.Fatalf("scan 失败: %v", err)
	}
	got := make([]string, 0, len(records))
	for _, r := range records {
		got = append(got, r.Path)
	}
	want := []string{"a.md", "b.md", "c.md", "z/y.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("顺序 = %v, want %v（结果必须稳定，否则表格每次刷新都在跳）", got, want)
	}
}

func TestPropertyIndex_CacheAndInvalidate(t *testing.T) {
	ws := t.TempDir()
	writeNote(t, ws, "a.md", "---\nstatus: reading\n---\n")

	idx := newPropertyIndexer()
	if _, err := idx.scan(ws); err != nil {
		t.Fatalf("首次 scan 失败: %v", err)
	}

	// 缓存期内新增文件不应立刻可见（证明缓存生效）
	writeNote(t, ws, "b.md", "---\nstatus: done\n---\n")
	cached, err := idx.scan(ws)
	if err != nil {
		t.Fatalf("二次 scan 失败: %v", err)
	}
	if len(cached) != 1 {
		t.Errorf("缓存期内记录数 = %d, want 1（缓存没生效）", len(cached))
	}

	// 失效后应看到新文件
	idx.invalidate(ws)
	fresh, err := idx.scan(ws)
	if err != nil {
		t.Fatalf("失效后 scan 失败: %v", err)
	}
	if len(fresh) != 2 {
		t.Errorf("失效后记录数 = %d, want 2", len(fresh))
	}
}

func TestPropertyIndex_CountWordsHandlesCJK(t *testing.T) {
	idx := newPropertyIndexer()

	// 纯中文：按字计
	if got := idx.countWords("中文分词测试"); got != 6 {
		t.Errorf("纯中文词数 = %d, want 6", got)
	}
	// 纯英文：按空白分词
	if got := idx.countWords("hello world foo"); got != 3 {
		t.Errorf("纯英文词数 = %d, want 3", got)
	}
	// 中英混排：中文 4 字 + 英文 2 词
	if got := idx.countWords("使用 Vue 开发"); got != 5 {
		t.Errorf("混排词数 = %d, want 5（使用+开发=4 字，Vue=1 词）", got)
	}
	if got := idx.countWords(""); got != 0 {
		t.Errorf("空内容词数 = %d, want 0", got)
	}
}

func TestPropertyIndex_EmptyWorkspace(t *testing.T) {
	idx := newPropertyIndexer()
	records, err := idx.scan(t.TempDir())
	if err != nil {
		t.Fatalf("空工作区 scan 不该报错: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("记录数 = %d, want 0", len(records))
	}
}

func TestCollectProperties(t *testing.T) {
	ws := t.TempDir()
	writeNote(t, ws, "a.md", "---\nstatus: reading\ntags: [AI]\nrating: 5\n---\n")
	writeNote(t, ws, "b.md", "---\nstatus: done\ntags: [AI, 论文]\n---\n")
	writeNote(t, ws, "c.md", "---\nstatus: reading\n---\n")

	idx := newPropertyIndexer()
	records, err := idx.scan(ws)
	if err != nil {
		t.Fatalf("scan 失败: %v", err)
	}

	metas := collectProperties(records)
	byName := make(map[string]*PropertyMeta, len(metas))
	for _, m := range metas {
		byName[m.Name] = m
	}

	status, ok := byName["status"]
	if !ok {
		t.Fatal("缺少 status 属性统计")
	}
	if status.Count != 3 {
		t.Errorf("status.Count = %d, want 3", status.Count)
	}
	if status.Kind != KindString {
		t.Errorf("status.Kind = %v, want string", status.Kind)
	}
	if status.Implicit {
		t.Error("status 不应被标记为隐式属性")
	}
	if !reflect.DeepEqual(status.Samples, []string{"done", "reading"}) {
		t.Errorf("status.Samples = %v, want [done reading]（去重 + 排序）", status.Samples)
	}

	if rating, ok := byName["rating"]; !ok || rating.Count != 1 || rating.Kind != KindNumber {
		t.Errorf("rating 统计 = %+v", rating)
	}

	// 列表类型的样例值要摊平成单个元素，用户筛的是"含某个标签"
	tags, ok := byName["tags"]
	if !ok {
		t.Fatal("缺少 tags 属性统计")
	}
	if !reflect.DeepEqual(tags.Samples, []string{"AI", "论文"}) {
		t.Errorf("tags.Samples = %v, want [AI 论文]（列表应摊平）", tags.Samples)
	}

	// 隐式属性要被正确标记
	if fp, ok := byName[PropFilePath]; !ok || !fp.Implicit {
		t.Errorf("%s 应被标记为隐式属性，得到 %+v", PropFilePath, fp)
	}

	// 排序：自定义属性排在隐式属性之前
	firstImplicit := -1
	lastCustom := -1
	for i, m := range metas {
		if m.Implicit && firstImplicit < 0 {
			firstImplicit = i
		}
		if !m.Implicit {
			lastCustom = i
		}
	}
	if firstImplicit >= 0 && lastCustom >= 0 && lastCustom > firstImplicit {
		t.Errorf("排序错误：自定义属性（末位 %d）应全部排在隐式属性（首位 %d）之前", lastCustom, firstImplicit)
	}
}

func TestCollectProperties_AllImplicitPresent(t *testing.T) {
	ws := t.TempDir()
	writeNote(t, ws, "a.md", "内容")

	idx := newPropertyIndexer()
	records, err := idx.scan(ws)
	if err != nil {
		t.Fatalf("scan 失败: %v", err)
	}
	metas := collectProperties(records)
	present := make(map[string]bool, len(metas))
	for _, m := range metas {
		present[m.Name] = true
	}
	// 每个隐式属性都必须无条件出现，否则前端属性下拉会时有时无
	for _, name := range implicitProps {
		if !present[name] {
			t.Errorf("隐式属性 %q 未出现在统计里", name)
		}
	}
}

func TestForEachFileBoundedIndexed_PreservesOrder(t *testing.T) {
	files := make([]string, 200)
	for i := range files {
		files[i] = formatInt(int64(i))
	}
	got := make([]string, len(files))
	forEachFileBoundedIndexed(files, func(i int, fp string) {
		got[i] = fp
	})
	if !reflect.DeepEqual(got, files) {
		t.Error("并发写入后顺序错乱：每个 worker 只该写自己那一格")
	}
}

func TestFormatInt(t *testing.T) {
	cases := map[int64]string{0: "0", 7: "7", 42: "42", 1024: "1024", -5: "-5", 9007199254740993: "9007199254740993"}
	for in, want := range cases {
		if got := formatInt(in); got != want {
			t.Errorf("formatInt(%d) = %q, want %q", in, got, want)
		}
	}
}
