package service

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// rec 构造一条测试记录，props 用 "key=value" 形式的 front matter 片段描述。
func rec(path string, fm string) *NoteRecord {
	props := ParseFrontMatter(fm)
	title := path
	if v, ok := props["title"]; ok {
		title = v.StringValue()
	}
	props[PropFilePath] = stringValue(path)
	props[PropFileTitle] = stringValue(title)
	// tags 同步到 file.tags，模拟索引层行为
	if v, ok := props["tags"]; ok && v.Kind == KindList {
		props[PropFileTags] = v
	} else {
		props[PropFileTags] = listValue(nil, "")
	}
	return &NoteRecord{Path: path, Title: title, Props: props}
}

// runOn 在记录集上跑一个筛选条件组，返回命中的路径。
func runOn(records []*NoteRecord, g BaseFilterGroup) []string {
	res := runQuery(records, BaseDef{Filters: g}, BaseView{Type: ViewTable, Columns: []string{PropFileTitle}}, nil)
	out := make([]string, 0, len(res.Rows))
	for _, r := range res.Rows {
		out = append(out, r.Path)
	}
	return out
}

func one(prop, op, value string) BaseFilterGroup {
	return BaseFilterGroup{Conditions: []BaseFilter{{Property: prop, Operator: op, Value: value}}}
}

func TestBaseQuery_StringOperators(t *testing.T) {
	records := []*NoteRecord{
		rec("a.md", "status: reading"),
		rec("b.md", "status: done"),
		rec("c.md", "status: Reading"), // 大小写不同
		rec("d.md", "other: x"),        // 无 status
	}

	tests := []struct {
		name string
		g    BaseFilterGroup
		want []string
	}{
		{"eq 大小写不敏感", one("status", OpEq, "reading"), []string{"a.md", "c.md"}},
		{"ne 含缺失属性", one("status", OpNe, "reading"), []string{"b.md", "d.md"}},
		{"contains", one("status", OpContains, "read"), []string{"a.md", "c.md"}},
		{"notContains", one("status", OpNotContains, "read"), []string{"b.md"}},
		{"startsWith", one("status", OpStartsWith, "re"), []string{"a.md", "c.md"}},
		{"endsWith", one("status", OpEndsWith, "ing"), []string{"a.md", "c.md"}},
		{"empty", one("status", OpEmpty, ""), []string{"d.md"}},
		{"notEmpty", one("status", OpNotEmpty, ""), []string{"a.md", "b.md", "c.md"}},
		{"regex", one("status", OpRegex, "^[Rr]ead"), []string{"a.md", "c.md"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runOn(records, tt.g); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("命中 = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBaseQuery_MissingPropertyOnlyMatchesNeAndEmpty(t *testing.T) {
	// 缺失属性的语义必须明确：只有 ne 和 empty 为真，其余一律为假。
	// 否则 "rating < 3" 会把没评分的笔记也筛进来，结果完全不可信。
	records := []*NoteRecord{rec("none.md", "other: x")}

	for _, op := range []string{OpEq, OpContains, OpStartsWith, OpEndsWith, OpGt, OpGte, OpLt, OpLte, OpRegex, OpIn} {
		if got := runOn(records, one("rating", op, "3")); len(got) != 0 {
			t.Errorf("运算符 %s 对缺失属性应无命中，得到 %v", op, got)
		}
	}
	if got := runOn(records, one("rating", OpNe, "3")); len(got) != 1 {
		t.Errorf("ne 对缺失属性应命中（缺失 ≠ 任何值），得到 %v", got)
	}
	if got := runOn(records, one("rating", OpEmpty, "")); len(got) != 1 {
		t.Errorf("empty 对缺失属性应命中，得到 %v", got)
	}
	if got := runOn(records, one("rating", OpNotIn, "3")); len(got) != 1 {
		t.Errorf("notIn 对缺失属性应命中，得到 %v", got)
	}
}

func TestBaseQuery_NumberOperators(t *testing.T) {
	records := []*NoteRecord{
		rec("r1.md", "rating: 1"),
		rec("r3.md", "rating: 3"),
		rec("r45.md", "rating: 4.5"),
		rec("r0.md", "rating: 0"),
	}

	tests := []struct {
		name string
		g    BaseFilterGroup
		want []string
	}{
		{"gt", one("rating", OpGt, "3"), []string{"r45.md"}},
		{"gte", one("rating", OpGte, "3"), []string{"r3.md", "r45.md"}},
		{"lt", one("rating", OpLt, "3"), []string{"r0.md", "r1.md"}},
		{"lte 含小数比较", one("rating", OpLte, "4.5"), []string{"r0.md", "r1.md", "r3.md", "r45.md"}},
		{"eq 小数", one("rating", OpEq, "4.5"), []string{"r45.md"}},
		// 关键：数字 0 不是"空"
		{"零不算空", one("rating", OpNotEmpty, ""), []string{"r0.md", "r1.md", "r3.md", "r45.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runOn(records, tt.g); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("命中 = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBaseQuery_NumberComparisonIsNotLexicographic(t *testing.T) {
	// 字符串比较下 "9" > "10"，这是"带类型的属性值"存在的根本理由
	records := []*NoteRecord{rec("nine.md", "n: 9"), rec("ten.md", "n: 10")}
	got := runOn(records, one("n", OpGt, "9"))
	if !reflect.DeepEqual(got, []string{"ten.md"}) {
		t.Errorf("n > 9 命中 = %v, want [ten.md]（若为 [] 说明退化成了字符串比较）", got)
	}
}

func TestBaseQuery_DateOperators(t *testing.T) {
	records := []*NoteRecord{
		rec("old.md", "created: 2026-01-01"),
		rec("mid.md", "created: 2026-06-15"),
		rec("new.md", "created: 2026-12-31"),
	}

	if got := runOn(records, one("created", OpGt, "2026-06-01")); !reflect.DeepEqual(got, []string{"mid.md", "new.md"}) {
		t.Errorf("日期 gt 命中 = %v", got)
	}
	if got := runOn(records, one("created", OpLt, "2026-06-01")); !reflect.DeepEqual(got, []string{"old.md"}) {
		t.Errorf("日期 lt 命中 = %v", got)
	}
	// 不同书写精度应可比较
	if got := runOn(records, one("created", OpGte, "2026-06-15 00:00")); !reflect.DeepEqual(got, []string{"mid.md", "new.md"}) {
		t.Errorf("跨精度日期比较命中 = %v", got)
	}
}

func TestBaseQuery_BoolOperators(t *testing.T) {
	records := []*NoteRecord{
		rec("p.md", "published: true"),
		rec("d.md", "published: false"),
		rec("y.md", "published: yes"),
		rec("n.md", "published: no"),
	}
	if got := runOn(records, one("published", OpEq, "true")); !reflect.DeepEqual(got, []string{"p.md", "y.md"}) {
		t.Errorf("bool eq true 命中 = %v, want [p.md y.md]（yes 应等价于 true）", got)
	}
	if got := runOn(records, one("published", OpEq, "false")); !reflect.DeepEqual(got, []string{"d.md", "n.md"}) {
		t.Errorf("bool eq false 命中 = %v", got)
	}
	// false 不算空
	if got := runOn(records, one("published", OpEmpty, "")); len(got) != 0 {
		t.Errorf("bool false 不该算空，得到 %v", got)
	}
}

func TestBaseQuery_ListOperators(t *testing.T) {
	records := []*NoteRecord{
		rec("a.md", "tags: [AI, 论文]"),
		rec("b.md", "tags: [AI-Agent]"),
		rec("c.md", "tags: [写作]"),
		rec("d.md", "other: x"),
	}

	tests := []struct {
		name string
		g    BaseFilterGroup
		want []string
	}{
		{"contains 精确元素", one("tags", OpContains, "AI"), []string{"a.md", "b.md"}},
		{"contains 中文元素", one("tags", OpContains, "论文"), []string{"a.md"}},
		{"eq 任一元素相等", one("tags", OpEq, "AI"), []string{"a.md"}},
		{"eq 不匹配前缀", one("tags", OpEq, "AI-"), []string{}},
		{"notContains", one("tags", OpNotContains, "AI"), []string{"c.md"}},
		{"empty", one("tags", OpEmpty, ""), []string{"d.md"}},
		{"in", BaseFilterGroup{Conditions: []BaseFilter{{Property: "tags", Operator: OpIn, Values: []string{"写作", "论文"}}}}, []string{"a.md", "c.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runOn(records, tt.g)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("命中 = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBaseQuery_InAcceptsCommaSeparatedValue(t *testing.T) {
	// 前端只填了 Value 时按逗号拆，避免"填了却没生效"
	records := []*NoteRecord{rec("a.md", "status: reading"), rec("b.md", "status: done"), rec("c.md", "status: idle")}
	g := BaseFilterGroup{Conditions: []BaseFilter{{Property: "status", Operator: OpIn, Value: "reading, done"}}}
	if got := runOn(records, g); !reflect.DeepEqual(got, []string{"a.md", "b.md"}) {
		t.Errorf("命中 = %v, want [a.md b.md]", got)
	}
}

func TestBaseQuery_ConjunctionAndOr(t *testing.T) {
	records := []*NoteRecord{
		rec("a.md", "status: reading\ntags: [AI]"),
		rec("b.md", "status: reading\ntags: [写作]"),
		rec("c.md", "status: done\ntags: [AI]"),
	}

	and := BaseFilterGroup{
		Conjunction: ConjAnd,
		Conditions: []BaseFilter{
			{Property: "status", Operator: OpEq, Value: "reading"},
			{Property: "tags", Operator: OpContains, Value: "AI"},
		},
	}
	if got := runOn(records, and); !reflect.DeepEqual(got, []string{"a.md"}) {
		t.Errorf("and 命中 = %v, want [a.md]", got)
	}

	or := and
	or.Conjunction = ConjOr
	if got := runOn(records, or); !reflect.DeepEqual(got, []string{"a.md", "b.md", "c.md"}) {
		t.Errorf("or 命中 = %v", got)
	}
}

func TestBaseQuery_NestedGroups(t *testing.T) {
	records := []*NoteRecord{
		rec("a.md", "status: reading\nrating: 5"),
		rec("b.md", "status: done\nrating: 5"),
		rec("c.md", "status: reading\nrating: 1"),
		rec("d.md", "status: archived\nrating: 1"),
	}

	// rating = 5 AND (status = reading OR status = done)
	g := BaseFilterGroup{
		Conjunction: ConjAnd,
		Conditions:  []BaseFilter{{Property: "rating", Operator: OpEq, Value: "5"}},
		Groups: []BaseFilterGroup{{
			Conjunction: ConjOr,
			Conditions: []BaseFilter{
				{Property: "status", Operator: OpEq, Value: "reading"},
				{Property: "status", Operator: OpEq, Value: "done"},
			},
		}},
	}
	if got := runOn(records, g); !reflect.DeepEqual(got, []string{"a.md", "b.md"}) {
		t.Errorf("嵌套组命中 = %v, want [a.md b.md]", got)
	}
}

func TestBaseQuery_EmptyFilterMatchesAll(t *testing.T) {
	records := []*NoteRecord{rec("a.md", ""), rec("b.md", "")}
	if got := runOn(records, BaseFilterGroup{}); len(got) != 2 {
		t.Errorf("空筛选应命中全部，得到 %v", got)
	}
	// 空的嵌套组也不该构成约束
	g := BaseFilterGroup{Groups: []BaseFilterGroup{{}, {}}}
	if got := runOn(records, g); len(got) != 2 {
		t.Errorf("空嵌套组应命中全部，得到 %v", got)
	}
}

func TestBaseQuery_Warnings(t *testing.T) {
	records := []*NoteRecord{rec("a.md", "status: reading")}
	known := knownProperties(records)

	cases := []struct {
		name     string
		def      BaseDef
		view     BaseView
		wantSubs string
	}{
		{
			name:     "属性不存在",
			def:      BaseDef{Filters: one("statuss", OpEq, "reading")},
			wantSubs: "statuss",
		},
		{
			name:     "正则非法",
			def:      BaseDef{Filters: one("status", OpRegex, "([")},
			wantSubs: "正则",
		},
		{
			name:     "未知运算符",
			def:      BaseDef{Filters: one("status", "近似等于", "x")},
			wantSubs: "运算符",
		},
		{
			name:     "空属性名",
			def:      BaseDef{Filters: one("", OpEq, "x")},
			wantSubs: "没有指定属性",
		},
		{
			name:     "未知连接词",
			def:      BaseDef{Filters: BaseFilterGroup{Conjunction: "xor", Conditions: []BaseFilter{{Property: "status", Operator: OpEq, Value: "reading"}, {Property: "status", Operator: OpNotEmpty}}}},
			wantSubs: "连接词",
		},
		{
			name:     "未知视图类型",
			def:      BaseDef{},
			view:     BaseView{Type: "甘特图"},
			wantSubs: "视图类型",
		},
		{
			name:     "目录为空",
			def:      BaseDef{Folder: "不存在的目录"},
			wantSubs: "没有任何笔记",
		},
		{
			name:     "分组属性不存在",
			def:      BaseDef{},
			view:     BaseView{Type: ViewBoard, GroupBy: "不存在的属性"},
			wantSubs: "分组属性",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			view := tt.view
			if view.Type == "" {
				view.Type = ViewTable
			}
			res := runQuery(records, tt.def, view, known)
			joined := strings.Join(res.Warnings, " | ")
			if !strings.Contains(joined, tt.wantSubs) {
				t.Errorf("告警 = %q, 应包含 %q（静默空结果是查询工具最糟的失败模式）", joined, tt.wantSubs)
			}
		})
	}
}

func TestBaseQuery_WarningsAreDeduplicated(t *testing.T) {
	records := []*NoteRecord{rec("a.md", "status: reading")}
	g := BaseFilterGroup{Conjunction: ConjOr, Conditions: []BaseFilter{
		{Property: "typo", Operator: OpEq, Value: "1"},
		{Property: "typo", Operator: OpEq, Value: "2"},
		{Property: "typo", Operator: OpEq, Value: "3"},
	}}
	res := runQuery(records, BaseDef{Filters: g}, BaseView{Type: ViewTable}, knownProperties(records))
	if len(res.Warnings) != 1 {
		t.Errorf("同一属性打错应只报一次，得到 %d 条：%v", len(res.Warnings), res.Warnings)
	}
}

func TestBaseQuery_InvalidRegexDoesNotSilentlyEmptyResult(t *testing.T) {
	// 正则编译失败时该条件被忽略，其余条件继续生效——
	// 而不是让整个查询返回空（用户会以为没有数据）
	records := []*NoteRecord{rec("a.md", "status: reading"), rec("b.md", "status: done")}
	g := BaseFilterGroup{Conjunction: ConjAnd, Conditions: []BaseFilter{
		{Property: "status", Operator: OpRegex, Value: "(["},
		{Property: "status", Operator: OpEq, Value: "reading"},
	}}
	if got := runOn(records, g); !reflect.DeepEqual(got, []string{"a.md"}) {
		t.Errorf("命中 = %v, want [a.md]", got)
	}
}

func TestBaseQuery_Sorting(t *testing.T) {
	records := []*NoteRecord{
		rec("a.md", "rating: 3"),
		rec("b.md", "rating: 10"),
		rec("c.md", "rating: 1"),
		rec("d.md", "other: x"), // 无 rating
	}

	asc := runQuery(records, BaseDef{}, BaseView{Type: ViewTable, Sort: []BaseSort{{Property: "rating"}}}, nil)
	gotAsc := pathsOf(asc.Rows)
	// 空值垫底，与升降序无关
	if !reflect.DeepEqual(gotAsc, []string{"c.md", "a.md", "b.md", "d.md"}) {
		t.Errorf("升序 = %v, want [c a b d]（无值的 d 应垫底）", gotAsc)
	}

	desc := runQuery(records, BaseDef{}, BaseView{Type: ViewTable, Sort: []BaseSort{{Property: "rating", Desc: true}}}, nil)
	gotDesc := pathsOf(desc.Rows)
	if !reflect.DeepEqual(gotDesc, []string{"b.md", "a.md", "c.md", "d.md"}) {
		t.Errorf("降序 = %v, want [b a c d]（无值的 d 仍应垫底）", gotDesc)
	}
}

func TestBaseQuery_MultiKeySortAndStableTiebreak(t *testing.T) {
	records := []*NoteRecord{
		rec("z.md", "status: reading\nrating: 5"),
		rec("a.md", "status: reading\nrating: 5"),
		rec("m.md", "status: done\nrating: 5"),
	}
	res := runQuery(records, BaseDef{}, BaseView{
		Type: ViewTable,
		Sort: []BaseSort{{Property: "status"}, {Property: "rating", Desc: true}},
	}, nil)
	got := pathsOf(res.Rows)
	// status 升序：done < reading；同 status 同 rating 时按路径兜底
	if !reflect.DeepEqual(got, []string{"m.md", "a.md", "z.md"}) {
		t.Errorf("多键排序 = %v, want [m a z]（同键必须按路径兜底，否则表格每次刷新都在跳）", got)
	}
}

func TestBaseQuery_SortByDateAndList(t *testing.T) {
	records := []*NoteRecord{
		rec("b.md", "created: 2026-06-01\ntags: [b, x, y]"),
		rec("a.md", "created: 2026-01-01\ntags: [a]"),
		rec("c.md", "created: 2026-12-01\ntags: [b, z]"),
	}

	byDate := runQuery(records, BaseDef{}, BaseView{Type: ViewTable, Sort: []BaseSort{{Property: "created"}}}, nil)
	if got := pathsOf(byDate.Rows); !reflect.DeepEqual(got, []string{"a.md", "b.md", "c.md"}) {
		t.Errorf("按日期排序 = %v", got)
	}

	// 列表按首元素，其次按长度
	byTags := runQuery(records, BaseDef{}, BaseView{Type: ViewTable, Sort: []BaseSort{{Property: "tags"}}}, nil)
	if got := pathsOf(byTags.Rows); !reflect.DeepEqual(got, []string{"a.md", "c.md", "b.md"}) {
		t.Errorf("按列表排序 = %v, want [a c b]（首元素 a<b；同为 b 时短的在前）", got)
	}
}

func TestBaseQuery_GroupBy(t *testing.T) {
	records := []*NoteRecord{
		rec("a.md", "status: reading"),
		rec("b.md", "status: reading"),
		rec("c.md", "status: done"),
		rec("d.md", "other: x"),
	}
	res := runQuery(records, BaseDef{}, BaseView{Type: ViewBoard, GroupBy: "status"}, nil)

	if len(res.Groups) != 3 {
		t.Fatalf("组数 = %d, want 3", len(res.Groups))
	}
	// 组序：数量降序，空值组垫底
	if res.Groups[0].Label != "reading" || res.Groups[0].Count != 2 {
		t.Errorf("第一组 = %+v, want reading×2", res.Groups[0])
	}
	if res.Groups[1].Label != "done" || res.Groups[1].Count != 1 {
		t.Errorf("第二组 = %+v, want done×1", res.Groups[1])
	}
	last := res.Groups[2]
	if last.Key != "" || last.Label != emptyGroupLabel || last.Count != 1 {
		t.Errorf("末组 = %+v, want 空值组垫底", last)
	}
	if res.Returned != 4 {
		t.Errorf("Returned = %d, want 4", res.Returned)
	}
	// 分组视图不该同时填 Rows
	if len(res.Rows) != 0 {
		t.Errorf("分组时 Rows 应为空，得到 %d 行", len(res.Rows))
	}
}

func TestBaseQuery_GroupByListPutsNoteInEveryGroup(t *testing.T) {
	// 一篇笔记打了两个标签，在标签看板里就该出现在两列——这是特性不是 bug
	records := []*NoteRecord{
		rec("a.md", "tags: [AI, 论文]"),
		rec("b.md", "tags: [AI]"),
	}
	res := runQuery(records, BaseDef{}, BaseView{Type: ViewBoard, GroupBy: "tags"}, nil)

	byLabel := make(map[string]int)
	for _, g := range res.Groups {
		byLabel[g.Label] = g.Count
	}
	if byLabel["AI"] != 2 {
		t.Errorf("AI 组 = %d, want 2", byLabel["AI"])
	}
	if byLabel["论文"] != 1 {
		t.Errorf("论文 组 = %d, want 1", byLabel["论文"])
	}
	if res.Total != 2 {
		t.Errorf("Total = %d, want 2（Total 是笔记数，不是分组后的条目数）", res.Total)
	}
}

func TestBaseQuery_LimitAndTruncation(t *testing.T) {
	records := make([]*NoteRecord, 10)
	for i := range records {
		records[i] = rec("n"+formatInt(int64(i))+".md", "status: x")
	}

	res := runQuery(records, BaseDef{}, BaseView{Type: ViewTable, Limit: 3}, nil)
	if len(res.Rows) != 3 || !res.Truncated {
		t.Errorf("行数 = %d, Truncated = %v, want 3/true", len(res.Rows), res.Truncated)
	}
	if res.Total != 10 {
		t.Errorf("Total = %d, want 10（Total 必须是截断前的总数，否则分页信息是错的）", res.Total)
	}

	// 分组视图的上限是跨组共享的总预算
	grouped := runQuery(records, BaseDef{}, BaseView{Type: ViewBoard, GroupBy: "status", Limit: 4}, nil)
	if grouped.Returned != 4 || !grouped.Truncated {
		t.Errorf("分组 Returned = %d, Truncated = %v, want 4/true", grouped.Returned, grouped.Truncated)
	}
}

func TestBaseQuery_FolderScope(t *testing.T) {
	records := []*NoteRecord{
		rec("读书/a.md", "x: 1"),
		rec("读书/子目录/b.md", "x: 1"),
		rec("工作/c.md", "x: 1"),
	}
	res := runQuery(records, BaseDef{Folder: "读书"}, BaseView{Type: ViewTable}, nil)
	if got := pathsOf(res.Rows); !reflect.DeepEqual(got, []string{"读书/a.md", "读书/子目录/b.md"}) {
		t.Errorf("目录范围 = %v", got)
	}
	// 带前后斜杠也要能用
	res2 := runQuery(records, BaseDef{Folder: "/读书/"}, BaseView{Type: ViewTable}, nil)
	if len(res2.Rows) != 2 {
		t.Errorf("带斜杠的目录范围行数 = %d, want 2", len(res2.Rows))
	}
}

func TestBaseQuery_CellsCarryTypeInfo(t *testing.T) {
	records := []*NoteRecord{rec("a.md", "title: 标题\nrating: 4.5\ndone: true\ntags: [AI, ML]\nmissing_check: x")}
	res := runQuery(records, BaseDef{}, BaseView{
		Type:    ViewTable,
		Columns: []string{PropFileTitle, "rating", "done", "tags", "nonexistent"},
	}, nil)

	if len(res.Rows) != 1 {
		t.Fatalf("行数 = %d", len(res.Rows))
	}
	cells := res.Rows[0].Cells
	if len(cells) != 5 {
		t.Fatalf("单元格数 = %d, want 5", len(cells))
	}
	if cells[0].Display != "标题" || cells[0].Kind != KindString {
		t.Errorf("标题单元格 = %+v", cells[0])
	}
	if cells[1].Kind != KindNumber || cells[1].Num != 4.5 {
		t.Errorf("数字单元格 = %+v（前端要靠 Kind 做右对齐）", cells[1])
	}
	if cells[2].Kind != KindBool || !cells[2].Bool {
		t.Errorf("布尔单元格 = %+v", cells[2])
	}
	if cells[3].Kind != KindList || !reflect.DeepEqual(cells[3].List, []string{"AI", "ML"}) {
		t.Errorf("列表单元格 = %+v（前端要靠 List 渲染 chip）", cells[3])
	}
	if !cells[4].Empty {
		t.Errorf("缺失属性的单元格应标记 Empty，得到 %+v", cells[4])
	}
}

func TestBaseQuery_DefaultColumnsWhenUnset(t *testing.T) {
	records := []*NoteRecord{rec("a.md", "")}
	res := runQuery(records, BaseDef{}, BaseView{Type: ViewTable}, nil)
	if len(res.Columns) == 0 {
		t.Fatal("未配置列时应给默认列，否则表格是空的")
	}
	if res.Columns[0] != PropFileTitle {
		t.Errorf("默认首列 = %q, want %q", res.Columns[0], PropFileTitle)
	}
}

func TestBaseQuery_ScannedReportsTotalCorpus(t *testing.T) {
	records := []*NoteRecord{rec("a.md", "s: 1"), rec("b.md", "s: 2"), rec("c.md", "s: 1")}
	res := runQuery(records, BaseDef{Filters: one("s", OpEq, "1")}, BaseView{Type: ViewTable}, nil)
	if res.Scanned != 3 {
		t.Errorf("Scanned = %d, want 3（应为语料总数，便于展示 2/3 匹配）", res.Scanned)
	}
	if res.Total != 2 {
		t.Errorf("Total = %d, want 2", res.Total)
	}
}

func TestCompareForSort_NullsAlwaysLast(t *testing.T) {
	null := nullValue()
	num := numberValue(1, "1")
	if compareForSort(null, num) <= 0 {
		t.Error("空值应排在有值之后")
	}
	if compareForSort(num, null) >= 0 {
		t.Error("有值应排在空值之前")
	}
	if compareForSort(null, null) != 0 {
		t.Error("两个空值应视为相等")
	}
}

func TestCompareForSort_MixedKindsFallsBackToString(t *testing.T) {
	// 同一属性在不同笔记里类型不一致（有人写 rating: 5，有人写 rating: 五星）
	// 不能 panic，也不能给出随机顺序
	a := numberValue(5, "5")
	b := stringValue("五星")
	if compareForSort(a, b) == 0 && compareForSort(b, a) == 0 {
		t.Error("混合类型应有确定的比较结果")
	}
	if compareForSort(a, b) != -compareForSort(b, a) {
		t.Error("比较必须反对称，否则排序结果不确定")
	}
}

func TestCompareForSort_BoolAndDate(t *testing.T) {
	if compareForSort(boolValue(false, "false"), boolValue(true, "true")) >= 0 {
		t.Error("false 应排在 true 之前")
	}
	early := dateValue(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "2026-01-01")
	late := dateValue(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), "2026-12-01")
	if compareForSort(early, late) >= 0 {
		t.Error("早的日期应排在前")
	}
}

func pathsOf(rows []*BaseRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Path)
	}
	return out
}
