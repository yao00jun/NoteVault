package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/notevault/notevault/internal/core"
)

// baseTestWorkspace 建一个有真实笔记的工作区，供端到端查询用。
func baseTestWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()

	writeNote(t, ws, "notes/deep-learning.md", `---
title: 深度学习入门
status: reading
rating: 5
tags: [AI, 机器学习]
---
# 深度学习入门

- [ ] 看完第三章
- [x] 装好环境
`)
	writeNote(t, ws, "notes/rust.md", `---
status: reading
rating: 3
tags: [编程, Rust]
---
# Rust 程序设计

正文。
`)
	writeNote(t, ws, "notes/transformer.md", `---
status: done
rating: 4
tags: [AI, 论文]
---
# Transformer 论文精读
`)
	writeNote(t, ws, "inbox/草稿.md", `# 随手记

没有 front matter，也没有标签。
`)
	return ws
}

// ---------------------------------------------------------------------------
// 文件名安全（这是安全边界，不是体验优化）
// ---------------------------------------------------------------------------

func TestSanitizeBaseName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"在读书单", "在读书单"},
		{"reading list", "reading list"},
		{"my-view_1", "my-view_1"},
		{"  前后空格  ", "前后空格"},
		{"", ""},
		{"   ", ""},
		{"---", ""},

		// 路径穿越：这些输入不做处理就能写到工作区外面
		{"../../etc/passwd", "etc-passwd"},
		{"..\\..\\windows\\system32", "windows-system32"},
		{"/absolute/path", "absolute-path"},
		{"a/b", "a-b"},
		{"a\\b", "a-b"},
		{"..", ""},
		{".", ""},

		// Windows 非法字符
		{"a:b*c?d\"e<f>g|h", "a-b-c-d-e-f-g-h"},

		// Windows 保留名建不出文件，加前缀绕开
		{"CON", "_CON"},
		{"nul", "_nul"},
		{"COM1", "_COM1"},
		{"LPT9", "_LPT9"},
		{"CONSOLE", "CONSOLE"}, // 只有精确匹配才算保留名
	}
	for _, c := range cases {
		if got := sanitizeBaseName(c.in); got != c.want {
			t.Errorf("sanitizeBaseName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeBaseName_CapsLength(t *testing.T) {
	got := sanitizeBaseName(strings.Repeat("a", 300))
	if len(got) != 80 {
		t.Errorf("超长名字应截到 80，得到 %d", len(got))
	}
}

func TestBasePath_StaysInsideBasesDir(t *testing.T) {
	ws := t.TempDir()
	dir := basesDir(ws)

	for _, name := range []string{"正常", "../escape", "a/b/c", "..\\..\\x"} {
		full, err := basePath(ws, name)
		if err != nil {
			continue // 被拒绝也是正确结果
		}
		rel, relErr := filepath.Rel(dir, full)
		if relErr != nil || strings.HasPrefix(rel, "..") || strings.Contains(rel, string(filepath.Separator)) {
			t.Errorf("basePath(%q) = %q 逃出了 bases 目录", name, full)
		}
	}
}

func TestBasePath_RejectsEmptyName(t *testing.T) {
	if _, err := basePath(t.TempDir(), "  "); err == nil {
		t.Error("空视图名应报错")
	}
	if _, err := basePath(t.TempDir(), "///"); err == nil {
		t.Error("全是非法字符的名字应报错")
	}
}

// ---------------------------------------------------------------------------
// 增删改查
// ---------------------------------------------------------------------------

func TestBaseService_SaveLoadListDelete(t *testing.T) {
	ws := t.TempDir()
	s := NewBaseService()

	// 还没建过视图不是错误
	list, err := s.ListBases(ws)
	if err != nil {
		t.Fatalf("空工作区 ListBases 应成功: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("空工作区应返回 0 个视图，得到 %d", len(list))
	}

	def := BaseDef{
		Name:        "在读",
		Description: "在读的书",
		Filters: BaseFilterGroup{
			Conjunction: ConjAnd,
			Conditions:  []BaseFilter{{Property: "status", Operator: OpEq, Value: "reading"}},
		},
		Views: []BaseView{{ID: "t", Name: "表格", Type: ViewTable}},
	}
	if err := s.SaveBase(ws, def); err != nil {
		t.Fatalf("SaveBase: %v", err)
	}

	list, err = s.ListBases(ws)
	if err != nil {
		t.Fatalf("ListBases: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("应有 1 个视图，得到 %d", len(list))
	}
	if list[0].Name != "在读" || list[0].ViewCount != 1 || list[0].FilterCount != 1 {
		t.Errorf("摘要不对: %+v", list[0])
	}
	if list[0].UpdatedAt.IsZero() {
		t.Error("摘要缺少 updatedAt——列表页要按时间排序")
	}

	got, err := s.LoadBase(ws, "在读")
	if err != nil {
		t.Fatalf("LoadBase: %v", err)
	}
	if got.Description != "在读的书" {
		t.Errorf("描述丢失: %+v", got)
	}

	if err := s.DeleteBase(ws, "在读"); err != nil {
		t.Fatalf("DeleteBase: %v", err)
	}
	if _, err := s.LoadBase(ws, "在读"); err == nil {
		t.Error("删掉之后不该还能读出来")
	}
}

func TestBaseService_SaveFillsDefaults(t *testing.T) {
	ws := t.TempDir()
	s := NewBaseService()

	// 一个视图都没有 → 打开后一片空白，用户以为坏了
	if err := s.SaveBase(ws, BaseDef{Name: "空的"}); err != nil {
		t.Fatalf("SaveBase: %v", err)
	}
	got, err := s.LoadBase(ws, "空的")
	if err != nil {
		t.Fatalf("LoadBase: %v", err)
	}
	if len(got.Views) == 0 {
		t.Fatal("没给视图时应补上默认视图")
	}
	if got.Filters.Conjunction != ConjAnd {
		t.Errorf("连接词应补 and，得到 %q", got.Filters.Conjunction)
	}

	// 视图缺 ID / Type / Name 时补齐，否则前端切换视图会撞空 key
	if err := s.SaveBase(ws, BaseDef{
		Name:  "缺字段",
		Views: []BaseView{{}, {}},
	}); err != nil {
		t.Fatalf("SaveBase: %v", err)
	}
	got, err = s.LoadBase(ws, "缺字段")
	if err != nil {
		t.Fatalf("LoadBase: %v", err)
	}
	seen := map[string]bool{}
	for _, v := range got.Views {
		if v.ID == "" || v.Type == "" || v.Name == "" {
			t.Errorf("视图字段没补齐: %+v", v)
		}
		if seen[v.ID] {
			t.Errorf("视图 ID 重复: %q", v.ID)
		}
		seen[v.ID] = true
	}
}

func TestBaseService_LoadMissingReturnsNotFound(t *testing.T) {
	_, err := NewBaseService().LoadBase(t.TempDir(), "不存在")
	if err == nil {
		t.Fatal("读不存在的视图应报错")
	}
	if !core.IsCode(err, core.ErrNotFound) {
		t.Errorf("错误码应是 ErrNotFound（前端要靠它区分「没有」和「坏了」），得到 %v", err)
	}
}

func TestBaseService_DeleteMissingReturnsNotFound(t *testing.T) {
	err := NewBaseService().DeleteBase(t.TempDir(), "不存在")
	if err == nil {
		t.Fatal("删不存在的视图应报错")
	}
	if !core.IsCode(err, core.ErrNotFound) {
		t.Errorf("错误码应是 ErrNotFound，得到 %v", err)
	}
}

func TestBaseService_Rename(t *testing.T) {
	ws := t.TempDir()
	s := NewBaseService()

	if err := s.SaveBase(ws, BaseDef{Name: "旧名"}); err != nil {
		t.Fatalf("SaveBase: %v", err)
	}
	if err := s.RenameBase(ws, "旧名", "新名"); err != nil {
		t.Fatalf("RenameBase: %v", err)
	}

	if _, err := s.LoadBase(ws, "旧名"); err == nil {
		t.Error("重命名后旧名还能读出来——留下了重复文件")
	}
	got, err := s.LoadBase(ws, "新名")
	if err != nil {
		t.Fatalf("LoadBase(新名): %v", err)
	}
	if got.Name != "新名" {
		t.Errorf("Name 字段没跟着改: %q", got.Name)
	}
}

func TestBaseService_RenameRejectsCollision(t *testing.T) {
	ws := t.TempDir()
	s := NewBaseService()
	for _, n := range []string{"甲", "乙"} {
		if err := s.SaveBase(ws, BaseDef{Name: n}); err != nil {
			t.Fatalf("SaveBase(%s): %v", n, err)
		}
	}
	err := s.RenameBase(ws, "甲", "乙")
	if err == nil {
		t.Fatal("改成已存在的名字应报错，而不是静默覆盖掉别人的视图")
	}
	if !core.IsCode(err, core.ErrAlreadyExists) {
		t.Errorf("错误码应是 ErrAlreadyExists，得到 %v", err)
	}
	// 冲突失败后两份都应完好
	for _, n := range []string{"甲", "乙"} {
		if _, err := s.LoadBase(ws, n); err != nil {
			t.Errorf("失败的重命名破坏了 %q: %v", n, err)
		}
	}
}

func TestBaseService_ListSkipsBrokenFiles(t *testing.T) {
	ws := t.TempDir()
	s := NewBaseService()
	if err := s.SaveBase(ws, BaseDef{Name: "好的"}); err != nil {
		t.Fatalf("SaveBase: %v", err)
	}

	dir := basesDir(ws)
	// 坏文件 + 非 .nvbase 文件都不该让整个列表打不开
	if err := os.WriteFile(filepath.Join(dir, "坏的"+baseFileExt), []byte("{ not json"), 0644); err != nil {
		t.Fatalf("写坏文件: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("写无关文件: %v", err)
	}

	list, err := s.ListBases(ws)
	if err != nil {
		t.Fatalf("一个视图坏了不该让 ListBases 失败: %v", err)
	}
	if len(list) != 1 || list[0].Name != "好的" {
		t.Errorf("应只列出可解析的视图，得到 %+v", list)
	}
}

func TestBaseService_ListRecoversMissingName(t *testing.T) {
	ws := t.TempDir()
	s := NewBaseService()
	if err := s.SaveBase(ws, BaseDef{Name: "有名字"}); err != nil {
		t.Fatalf("SaveBase: %v", err)
	}
	// 手写文件漏了 name：列表里不该是一行空白
	full := filepath.Join(basesDir(ws), "手写的"+baseFileExt)
	writeLegacyJSON(t, full, map[string]any{
		"schemaVersion": 1,
		"kind":          "base-definition",
		"updatedAt":     "2026-08-31T00:00:00Z",
		"data":          map[string]any{"views": []any{}},
	})

	list, err := s.ListBases(ws)
	if err != nil {
		t.Fatalf("ListBases: %v", err)
	}
	var found bool
	for _, b := range list {
		if b.Name == "手写的" {
			found = true
		}
		if b.Name == "" {
			t.Error("列表里出现了空名字")
		}
	}
	if !found {
		t.Errorf("漏了 name 的文件应用文件名兜底，得到 %+v", list)
	}
}

func TestCountFilters_CountsNestedGroups(t *testing.T) {
	g := BaseFilterGroup{
		Conditions: []BaseFilter{{Property: "a"}, {Property: "b"}},
		Groups: []BaseFilterGroup{
			{Conditions: []BaseFilter{{Property: "c"}}},
			{Groups: []BaseFilterGroup{{Conditions: []BaseFilter{{Property: "d"}, {Property: "e"}}}}},
		},
	}
	if got := countFilters(g); got != 5 {
		t.Errorf("countFilters = %d, want 5", got)
	}
}

// ---------------------------------------------------------------------------
// 端到端查询
// ---------------------------------------------------------------------------

func TestBaseService_RunBaseEndToEnd(t *testing.T) {
	ws := baseTestWorkspace(t)
	s := NewBaseService()

	def := BaseDef{
		Name: "在读的 AI",
		Filters: BaseFilterGroup{
			Conjunction: ConjAnd,
			Conditions: []BaseFilter{
				{Property: "status", Operator: OpEq, Value: "reading"},
				{Property: "tags", Operator: OpContains, Value: "AI"},
			},
		},
		Views: []BaseView{{
			ID:      "t",
			Type:    ViewTable,
			Columns: []string{PropFileTitle, "rating", "tags"},
			Sort:    []BaseSort{{Property: "rating", Desc: true}},
		}},
	}

	res, err := s.RunBase(ws, def, "t")
	if err != nil {
		t.Fatalf("RunBase: %v", err)
	}
	if res.Returned != 1 {
		t.Fatalf("应命中 1 条（只有 deep-learning 同时满足 reading + AI），得到 %d：%+v", res.Returned, res.Rows)
	}
	if !strings.Contains(res.Rows[0].Path, "deep-learning") {
		t.Errorf("命中的不是预期笔记: %s", res.Rows[0].Path)
	}
	if res.Rows[0].Title != "深度学习入门" {
		t.Errorf("标题应取 front matter 的 title，得到 %q", res.Rows[0].Title)
	}
	if res.Scanned != 4 {
		t.Errorf("Scanned = %d, want 4（扫描总数与命中数是两回事）", res.Scanned)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("正常查询不该有告警: %v", res.Warnings)
	}
	if len(res.Columns) != 3 {
		t.Errorf("列数 = %d, want 3", len(res.Columns))
	}
}

func TestBaseService_RunSavedBase(t *testing.T) {
	ws := baseTestWorkspace(t)
	s := NewBaseService()

	def := BaseDef{
		Name: "全部",
		Views: []BaseView{
			{ID: "a", Type: ViewTable, Sort: []BaseSort{{Property: PropFilePath}}},
			{ID: "b", Type: ViewList, Limit: 2},
		},
	}
	if err := s.SaveBase(ws, def); err != nil {
		t.Fatalf("SaveBase: %v", err)
	}

	res, err := s.RunSavedBase(ws, "全部", "b")
	if err != nil {
		t.Fatalf("RunSavedBase: %v", err)
	}
	if res.ViewID != "b" || res.ViewType != ViewList {
		t.Errorf("没按 viewID 选中视图: id=%q type=%q", res.ViewID, res.ViewType)
	}
	if res.Returned != 2 || !res.Truncated {
		t.Errorf("limit=2 应截断: returned=%d truncated=%v total=%d", res.Returned, res.Truncated, res.Total)
	}
	if res.Total != 4 {
		t.Errorf("Total 应是截断前的命中数 4，得到 %d", res.Total)
	}
}

func TestBaseService_RunSavedBaseMissing(t *testing.T) {
	if _, err := NewBaseService().RunSavedBase(t.TempDir(), "没有这个", ""); err == nil {
		t.Error("跑不存在的视图应报错")
	}
}

func TestBaseService_RunBaseRejectsEmptyWorkspace(t *testing.T) {
	s := NewBaseService()
	if _, err := s.RunBase("", BaseDef{Name: "x"}, ""); err == nil {
		t.Error("空工作区路径应报错，而不是去扫描当前目录")
	}
	if _, err := s.ListProperties(""); err == nil {
		t.Error("ListProperties 空路径应报错")
	}
	if _, err := s.ListBases(""); err == nil {
		t.Error("ListBases 空路径应报错")
	}
}

func TestBaseService_RunBaseFolderScope(t *testing.T) {
	ws := baseTestWorkspace(t)
	s := NewBaseService()

	res, err := s.RunBase(ws, BaseDef{
		Name:   "只看 notes",
		Folder: "notes",
		Views:  []BaseView{{ID: "t", Type: ViewTable}},
	}, "t")
	if err != nil {
		t.Fatalf("RunBase: %v", err)
	}
	if res.Returned != 3 {
		t.Fatalf("notes 下应有 3 条，得到 %d", res.Returned)
	}
	for _, r := range res.Rows {
		if !strings.HasPrefix(filepath.ToSlash(r.Path), "notes/") {
			t.Errorf("目录限定失效，混进了 %s", r.Path)
		}
	}
}

func TestPickView(t *testing.T) {
	views := []BaseView{{ID: "a"}, {ID: "b"}}

	if got := pickView(views, "b"); got.ID != "b" {
		t.Errorf("按 ID 命中失败: %+v", got)
	}
	if got := pickView(views, ""); got.ID != "a" {
		t.Errorf("空 ID 应取第一个: %+v", got)
	}
	// ID 对不上时退回第一个，而不是返回空视图导致前端渲染空表
	if got := pickView(views, "不存在"); got.ID != "a" {
		t.Errorf("未命中的 ID 应退回第一个: %+v", got)
	}
	if got := pickView(nil, ""); got.Type != ViewTable {
		t.Errorf("没有视图时应给默认表格: %+v", got)
	}
}

func TestKnownProperties_AlwaysIncludesImplicit(t *testing.T) {
	// 空工作区里筛 file.name 不该被报"属性不存在"
	known := knownProperties(nil)
	for _, name := range implicitProps {
		if !known[name] {
			t.Errorf("隐式属性 %q 应无条件视为已知", name)
		}
	}
}

// ---------------------------------------------------------------------------
// 属性 / 元信息接口
// ---------------------------------------------------------------------------

func TestBaseService_ListProperties(t *testing.T) {
	ws := baseTestWorkspace(t)
	s := NewBaseService()

	props, err := s.ListProperties(ws)
	if err != nil {
		t.Fatalf("ListProperties: %v", err)
	}

	byName := map[string]*PropertyMeta{}
	for _, p := range props {
		byName[p.Name] = p
	}
	for _, want := range []string{"status", "rating", "tags", PropFileTitle, PropTodoPending} {
		if byName[want] == nil {
			t.Errorf("属性列表缺少 %q", want)
		}
	}
	if m := byName["rating"]; m != nil {
		if m.Kind != KindNumber {
			t.Errorf("rating 类型 = %v, want number（类型来自数据，否则排序按字典序）", m.Kind)
		}
		if m.Count != 3 {
			t.Errorf("rating 出现次数 = %d, want 3", m.Count)
		}
		if m.Implicit {
			t.Error("rating 是用户属性，不该标成隐式")
		}
	}
	if m := byName[PropFileTitle]; m != nil && !m.Implicit {
		t.Error("file.title 应标成隐式属性")
	}
}

func TestBaseService_ListOperatorsAndViewTypes(t *testing.T) {
	s := NewBaseService()

	ops := s.ListOperators()
	if len(ops) != len(allOperators) {
		t.Errorf("运算符数量 = %d, want %d", len(ops), len(allOperators))
	}
	// 返回副本：前端拿到的切片被改动不该污染后端常量
	ops[0] = "tampered"
	if s.ListOperators()[0] == "tampered" {
		t.Error("ListOperators 应返回副本")
	}

	types := s.ListViewTypes()
	if len(types) != 3 {
		t.Fatalf("视图类型 = %v, want 3 种", types)
	}
}

func TestBaseService_NewBaseTemplate(t *testing.T) {
	s := NewBaseService()

	def := s.NewBaseTemplate("我的视图")
	if def.Name != "我的视图" {
		t.Errorf("Name = %q", def.Name)
	}
	if len(def.Views) == 0 {
		t.Error("新建模板应自带能跑的视图，而不是一张空表单")
	}
	if def.Filters.Conjunction != ConjAnd {
		t.Errorf("连接词 = %q, want and", def.Filters.Conjunction)
	}
	if got := s.NewBaseTemplate("   "); got.Name == "" {
		t.Error("空名字应给个默认名")
	}
}

func TestBaseService_TemplatesAllRunnable(t *testing.T) {
	ws := baseTestWorkspace(t)
	s := NewBaseService()

	tpls := s.ListTemplates()
	if len(tpls) == 0 {
		t.Fatal("内置模板不该为空——用户最大的门槛是不知道能用它干什么")
	}

	seen := map[string]bool{}
	for _, tpl := range tpls {
		if tpl.ID == "" || tpl.Title == "" || tpl.Description == "" {
			t.Errorf("模板信息不全: %+v", tpl)
		}
		if seen[tpl.ID] {
			t.Errorf("模板 ID 重复: %q", tpl.ID)
		}
		seen[tpl.ID] = true
		if len(tpl.Def.Views) == 0 {
			t.Errorf("模板 %q 没有视图", tpl.ID)
		}

		// 每个模板都必须能真的跑起来且不冒告警——
		// 内置模板里出现"属性不存在"是最尴尬的首次体验
		res, err := s.RunBase(ws, tpl.Def, "")
		if err != nil {
			t.Errorf("模板 %q 跑不起来: %v", tpl.ID, err)
			continue
		}
		if len(res.Warnings) != 0 {
			t.Errorf("模板 %q 冒了告警: %v", tpl.ID, res.Warnings)
		}
	}

	// 抽查语义：在读书单应命中两条 reading
	for _, tpl := range tpls {
		if tpl.ID != "reading" {
			continue
		}
		res, err := s.RunBase(ws, tpl.Def, "")
		if err != nil {
			t.Fatalf("RunBase: %v", err)
		}
		if res.Returned != 2 {
			t.Errorf("在读书单应命中 2 条，得到 %d", res.Returned)
		}
		// 按评分降序：5 分的在前
		if res.Returned == 2 && !strings.Contains(res.Rows[0].Path, "deep-learning") {
			t.Errorf("评分降序失效，首行是 %s", res.Rows[0].Path)
		}
	}
}

func TestBaseService_InvalidateCacheReflectsEdits(t *testing.T) {
	ws := t.TempDir()
	s := NewBaseService()
	writeNote(t, ws, "a.md", "---\nstatus: draft\n---\n# A\n")

	def := BaseDef{
		Name: "草稿",
		Filters: BaseFilterGroup{
			Conjunction: ConjAnd,
			Conditions:  []BaseFilter{{Property: "status", Operator: OpEq, Value: "draft"}},
		},
		Views: []BaseView{{ID: "t", Type: ViewTable}},
	}
	res, err := s.RunBase(ws, def, "t")
	if err != nil {
		t.Fatalf("RunBase: %v", err)
	}
	if res.Returned != 1 {
		t.Fatalf("首次应命中 1 条，得到 %d", res.Returned)
	}

	// 改成 published 后不清缓存，最多要等 30 秒才看到变化
	writeNote(t, ws, "a.md", "---\nstatus: published\n---\n# A\n")
	s.InvalidateCache(ws)

	res, err = s.RunBase(ws, def, "t")
	if err != nil {
		t.Fatalf("RunBase: %v", err)
	}
	if res.Returned != 0 {
		t.Errorf("清缓存后应命中 0 条，得到 %d（缓存没失效）", res.Returned)
	}
}
