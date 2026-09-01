package service

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// 声明式查询模型（.nvbase 的核心结构）
// ---------------------------------------------------------------------------

// 筛选运算符。
//
// 刻意保持小而正交：每个运算符在每种属性类型下的语义都必须能一句话说清，
// 说不清的（比如 list 上的 gt）就不提供，宁可让用户换个写法，
// 也不要给出一个"能跑但结果莫名其妙"的查询。
const (
	OpEq          = "eq"          // 等于（类型感知）
	OpNe          = "ne"          // 不等于
	OpContains    = "contains"    // 字符串含子串 / 列表含元素
	OpNotContains = "notContains" //
	OpStartsWith  = "startsWith"  //
	OpEndsWith    = "endsWith"    //
	OpGt          = "gt"          // 大于（数字 / 日期 / 字符串字典序）
	OpGte         = "gte"         //
	OpLt          = "lt"          //
	OpLte         = "lte"         //
	OpEmpty       = "empty"       // 为空（缺失 / null / 空串 / 空数组）
	OpNotEmpty    = "notEmpty"    //
	OpIn          = "in"          // 值 ∈ Values
	OpNotIn       = "notIn"       //
	OpRegex       = "regex"       // 正则匹配展示文本
)

// allOperators 供前端下拉与校验使用。
var allOperators = []string{
	OpEq, OpNe, OpContains, OpNotContains, OpStartsWith, OpEndsWith,
	OpGt, OpGte, OpLt, OpLte, OpEmpty, OpNotEmpty, OpIn, OpNotIn, OpRegex,
}

// 视图类型。
const (
	ViewTable = "table"
	ViewBoard = "board"
	ViewList  = "list"
)

// 逻辑连接词。
const (
	ConjAnd = "and"
	ConjOr  = "or"
)

// BaseFilter 是一条筛选条件。
//
// Value 用字符串承载而不是 any：这个结构要穿过 JSON 落盘、再穿过 Wails 到前端，
// any 在这条链路上会退化成 float64/string/bool，类型信息反而更乱。
// 统一用字符串存原文，比较时按**目标属性的类型**强转 —— 类型的权威来源是数据，不是查询。
type BaseFilter struct {
	Property string   `json:"property"`
	Operator string   `json:"operator"`
	Value    string   `json:"value,omitempty"`
	Values   []string `json:"values,omitempty"` // 仅 in / notIn 使用
}

// BaseFilterGroup 是可嵌套的条件组。
type BaseFilterGroup struct {
	Conjunction string            `json:"conjunction"` // and（默认）| or
	Conditions  []BaseFilter      `json:"conditions"`
	Groups      []BaseFilterGroup `json:"groups"`
}

// IsEmpty 判断这一组是否没有任何有效约束。
func (g BaseFilterGroup) IsEmpty() bool {
	if len(g.Conditions) > 0 {
		return false
	}
	for _, sub := range g.Groups {
		if !sub.IsEmpty() {
			return false
		}
	}
	return true
}

// BaseSort 是一条排序规则。
type BaseSort struct {
	Property string `json:"property"`
	Desc     bool   `json:"desc"`
}

// BaseView 是一个视图配置。
type BaseView struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Type    string     `json:"type"` // table | board | list
	Columns []string   `json:"columns"`
	Sort    []BaseSort `json:"sort"`
	GroupBy string     `json:"groupBy"`
	Limit   int        `json:"limit"` // 0 = 用默认上限
}

// BaseDef 是一个 .nvbase 文件的完整内容。
//
// 命名没跟 schema.BaseDefinition 一致（那个是 Descriptor 变量名），
// 避免同名标识符在两个包里指代不同东西。
type BaseDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Folder      string          `json:"folder,omitempty"` // 限定子目录，空 = 整个工作区
	Filters     BaseFilterGroup `json:"filters"`
	Views       []BaseView      `json:"views"`
}

// ---------------------------------------------------------------------------
// 查询结果
// ---------------------------------------------------------------------------

// BaseCell 是表格里的一个单元格。
//
// 同时带 Display 和结构化字段：Display 给前端直接渲染，
// Kind/List/Num 给前端做类型化渲染（标签做成 chip、数字右对齐、布尔画勾）。
type BaseCell struct {
	Property string   `json:"property"`
	Kind     PropKind `json:"kind"`
	Display  string   `json:"display"`
	List     []string `json:"list,omitempty"`
	Num      float64  `json:"num,omitempty"`
	Bool     bool     `json:"bool,omitempty"`
	Empty    bool     `json:"empty,omitempty"`
}

// BaseRow 是查询结果的一行（一篇笔记）。
type BaseRow struct {
	Path  string     `json:"path"`
	Title string     `json:"title"`
	Cells []BaseCell `json:"cells"`
}

// BaseGroup 是分组结果的一组。
type BaseGroup struct {
	Key   string     `json:"key"`
	Label string     `json:"label"`
	Count int        `json:"count"`
	Rows  []*BaseRow `json:"rows"`
}

// BaseResult 是一次查询的完整输出。
type BaseResult struct {
	ViewID    string       `json:"viewId"`
	ViewName  string       `json:"viewName"`
	ViewType  string       `json:"viewType"`
	Columns   []string     `json:"columns"`
	Rows      []*BaseRow   `json:"rows"`
	Groups    []*BaseGroup `json:"groups"`
	Total     int          `json:"total"`
	Returned  int          `json:"returned"`
	Truncated bool         `json:"truncated"`
	Scanned   int          `json:"scanned"`
	Warnings  []string     `json:"warnings"`
}

// defaultRowLimit 是单次查询返回的默认行数上限。
//
// 设上限不是怕后端慢（扫描本身有缓存），是怕前端：几万行一次性塞进 DOM
// 会让表格彻底卡死。超出时通过 Truncated 明确告诉用户，而不是静默截断。
const defaultRowLimit = 2000

// emptyGroupLabel 是分组值为空时的显示名。
const emptyGroupLabel = "（未设置）"

// ---------------------------------------------------------------------------
// 谓词编译
// ---------------------------------------------------------------------------

// predicate 是编译后的筛选谓词。
type predicate func(r *NoteRecord) bool

// compileCtx 收集编译期的告警。
//
// 查询工具最糟的失败模式是「静默返回空结果」——用户以为没有匹配的笔记，
// 实际是属性名打错了。所有可疑之处都要沿这条通道冒到前端。
type compileCtx struct {
	known    map[string]bool
	warnings []string
}

func (c *compileCtx) warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	for _, existing := range c.warnings {
		if existing == msg {
			return // 同一个属性名在多个条件里打错，只报一次
		}
	}
	c.warnings = append(c.warnings, msg)
}

// compileGroup 把条件组编译成单个谓词。
func compileGroup(g BaseFilterGroup, ctx *compileCtx) predicate {
	var preds []predicate
	for _, cond := range g.Conditions {
		if p := compileFilter(cond, ctx); p != nil {
			preds = append(preds, p)
		}
	}
	for _, sub := range g.Groups {
		if sub.IsEmpty() {
			continue
		}
		preds = append(preds, compileGroup(sub, ctx))
	}

	if len(preds) == 0 {
		return func(*NoteRecord) bool { return true }
	}
	if len(preds) == 1 {
		return preds[0]
	}

	if strings.EqualFold(g.Conjunction, ConjOr) {
		return func(r *NoteRecord) bool {
			for _, p := range preds {
				if p(r) {
					return true
				}
			}
			return false
		}
	}
	// 默认 and：连接词为空或拼错时按 and 处理，
	// 因为「更严格」的默认值不会给出意料之外的多余结果
	if g.Conjunction != "" && !strings.EqualFold(g.Conjunction, ConjAnd) {
		ctx.warn("未知的连接词 %q，已按 and 处理", g.Conjunction)
	}
	return func(r *NoteRecord) bool {
		for _, p := range preds {
			if !p(r) {
				return false
			}
		}
		return true
	}
}

// compileFilter 把单条条件编译成谓词。返回 nil 表示这条条件无效、应被忽略。
func compileFilter(f BaseFilter, ctx *compileCtx) predicate {
	prop := strings.TrimSpace(f.Property)
	if prop == "" {
		ctx.warn("有一条筛选条件没有指定属性，已忽略")
		return nil
	}
	if ctx.known != nil && !ctx.known[prop] {
		ctx.warn("属性 %q 在当前工作区里不存在，该条件不会匹配任何笔记", prop)
	}

	op := strings.TrimSpace(f.Operator)
	if op == "" {
		op = OpEq
	}

	switch op {
	case OpEmpty:
		return func(r *NoteRecord) bool { return r.Get(prop).IsEmpty() }
	case OpNotEmpty:
		return func(r *NoteRecord) bool { return !r.Get(prop).IsEmpty() }

	case OpRegex:
		re, err := regexp.Compile(f.Value)
		if err != nil {
			ctx.warn("正则 %q 无法编译（%v），该条件已忽略", f.Value, err)
			return nil
		}
		return func(r *NoteRecord) bool {
			v := r.Get(prop)
			if v.IsNull {
				return false
			}
			if v.Kind == KindList {
				for _, item := range v.List {
					if re.MatchString(item) {
						return true
					}
				}
				return false
			}
			return re.MatchString(v.StringValue())
		}

	case OpIn, OpNotIn:
		values := f.Values
		if len(values) == 0 && f.Value != "" {
			// 兼容前端只填了 Value 的情况：按逗号拆
			for _, part := range strings.Split(f.Value, ",") {
				if p := strings.TrimSpace(part); p != "" {
					values = append(values, p)
				}
			}
		}
		if len(values) == 0 {
			ctx.warn("属性 %q 的 %s 条件没有候选值，已忽略", prop, op)
			return nil
		}
		set := make(map[string]bool, len(values))
		for _, v := range values {
			set[strings.ToLower(strings.TrimSpace(v))] = true
		}
		want := op == OpIn
		return func(r *NoteRecord) bool {
			v := r.Get(prop)
			if v.IsNull {
				return !want // null 不属于任何候选集合
			}
			hit := false
			if v.Kind == KindList {
				for _, item := range v.List {
					if set[strings.ToLower(item)] {
						hit = true
						break
					}
				}
			} else {
				hit = set[strings.ToLower(v.StringValue())]
			}
			return hit == want
		}

	case OpEq, OpNe, OpContains, OpNotContains, OpStartsWith, OpEndsWith, OpGt, OpGte, OpLt, OpLte:
		want := parseScalar(f.Value)
		return func(r *NoteRecord) bool {
			return matchValue(r.Get(prop), op, f.Value, want)
		}

	default:
		ctx.warn("未知的运算符 %q，该条件已忽略", op)
		return nil
	}
}

// matchValue 执行单个值的比较。
//
// 类型判定原则：**以数据的类型为准，把查询值强转过去**。
// 反过来（以查询值为准）会让同一个查询在不同笔记上走不同分支，行为无法预测。
func matchValue(v PropValue, op string, rawWant string, want PropValue) bool {
	// null 只对 ne 为真：缺失的属性"不等于"任何具体值，
	// 但也不"大于/小于/包含"任何值
	if v.IsNull {
		return op == OpNe
	}

	switch op {
	case OpContains, OpNotContains:
		hit := containsValue(v, rawWant)
		return hit == (op == OpContains)

	case OpStartsWith:
		return strings.HasPrefix(strings.ToLower(v.StringValue()), strings.ToLower(rawWant))
	case OpEndsWith:
		return strings.HasSuffix(strings.ToLower(v.StringValue()), strings.ToLower(rawWant))
	}

	// 列表的相等语义：任一元素相等即算命中。
	// 这是为标签场景服务的——`tags eq AI` 用户想表达的是"打了 AI 标签"，
	// 而不是"标签列表恰好只有 AI 一个"。
	if v.Kind == KindList {
		switch op {
		case OpEq, OpNe:
			hit := false
			for _, item := range v.List {
				if strings.EqualFold(item, strings.TrimSpace(rawWant)) {
					hit = true
					break
				}
			}
			return hit == (op == OpEq)
		default:
			// 列表上的大小比较没有公认语义，退化为按元素数量比较
			return compareNumbers(float64(len(v.List)), want.Num, op)
		}
	}

	switch v.Kind {
	case KindNumber:
		if want.Kind != KindNumber {
			// 查询值不是数字（比如笔误 "5a"）：退化为字符串比较，
			// 而不是判 false —— 后者会让用户以为"没有匹配的数据"
			return compareStrings(v.StringValue(), rawWant, op)
		}
		return compareNumbers(v.Num, want.Num, op)

	case KindDate:
		if want.Kind != KindDate {
			return compareStrings(v.StringValue(), rawWant, op)
		}
		return compareNumbers(float64(v.Date.Unix()), float64(want.Date.Unix()), op)

	case KindBool:
		if want.Kind == KindBool {
			switch op {
			case OpEq:
				return v.Bool == want.Bool
			case OpNe:
				return v.Bool != want.Bool
			}
		}
		return compareStrings(v.StringValue(), rawWant, op)

	default:
		return compareStrings(v.StringValue(), rawWant, op)
	}
}

// containsValue 实现 contains 语义：列表按元素、其余按子串，均不区分大小写。
func containsValue(v PropValue, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return true // 空关键词不构成约束
	}
	lowerWant := strings.ToLower(want)
	if v.Kind == KindList {
		for _, item := range v.List {
			lower := strings.ToLower(item)
			// 先精确后子串：`tags contains AI` 要能命中 "AI"，
			// 也要能命中 "AI-Agent"（用户按前缀找一类标签是常见用法）
			if lower == lowerWant || strings.Contains(lower, lowerWant) {
				return true
			}
		}
		return false
	}
	return strings.Contains(strings.ToLower(v.StringValue()), lowerWant)
}

func compareNumbers(a, b float64, op string) bool {
	switch op {
	case OpEq:
		return a == b
	case OpNe:
		return a != b
	case OpGt:
		return a > b
	case OpGte:
		return a >= b
	case OpLt:
		return a < b
	case OpLte:
		return a <= b
	}
	return false
}

func compareStrings(a, b string, op string) bool {
	// 大小写不敏感：笔记里的 status 写成 Reading / reading 是常事，
	// 让用户为此纠结不如直接抹平
	la, lb := strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	switch op {
	case OpEq:
		return la == lb
	case OpNe:
		return la != lb
	case OpGt:
		return la > lb
	case OpGte:
		return la >= lb
	case OpLt:
		return la < lb
	case OpLte:
		return la <= lb
	}
	return false
}

// ---------------------------------------------------------------------------
// 排序
// ---------------------------------------------------------------------------

// sortRecords 按规则排序（稳定排序，保证同键顺序可预测）。
func sortRecords(records []*NoteRecord, rules []BaseSort) {
	if len(rules) == 0 {
		// 无规则时按路径，保证结果稳定
		sort.SliceStable(records, func(i, j int) bool { return records[i].Path < records[j].Path })
		return
	}
	sort.SliceStable(records, func(i, j int) bool {
		for _, rule := range rules {
			av, bv := records[i].Get(rule.Property), records[j].Get(rule.Property)

			// 空值不参与方向翻转：降序时也必须垫底。
			// 若交给 compareForSort 的返回值再取反，"无评分"的笔记会被顶到降序第一行。
			aEmpty, bEmpty := av.IsEmpty(), bv.IsEmpty()
			if aEmpty || bEmpty {
				if aEmpty == bEmpty {
					continue // 两边都空 → 这一键无区分度，交给下一个排序键
				}
				return bEmpty // b 空 → a 在前
			}

			c := compareForSort(av, bv)
			if c == 0 {
				continue
			}
			if rule.Desc {
				return c > 0
			}
			return c < 0
		}
		// 全部键相等时按路径兜底，避免同键行在多次查询间乱跳
		return records[i].Path < records[j].Path
	})
}

// compareForSort 返回 -1/0/1。
//
// 空值处理：**无论升序降序，空值一律排最后**。这是数据工具的通行做法，
// 也符合直觉——用户按"评分降序"看的是有评分的笔记，不是把没评分的顶到最前面。
func compareForSort(a, b PropValue) int {
	aEmpty, bEmpty := a.IsEmpty(), b.IsEmpty()
	switch {
	case aEmpty && bEmpty:
		return 0
	case aEmpty:
		return 1
	case bEmpty:
		return -1
	}

	// 类型一致时按类型比，不一致时退化为字符串（混排属性无公认序）
	if a.Kind == b.Kind {
		switch a.Kind {
		case KindNumber:
			return cmpFloat(a.Num, b.Num)
		case KindDate:
			return cmpFloat(float64(a.Date.Unix()), float64(b.Date.Unix()))
		case KindBool:
			switch {
			case a.Bool == b.Bool:
				return 0
			case !a.Bool:
				return -1
			default:
				return 1
			}
		case KindList:
			// 列表按首元素排，其次按长度——比按 "a, b, c" 拼接串排更符合直觉
			af, bf := "", ""
			if len(a.List) > 0 {
				af = strings.ToLower(a.List[0])
			}
			if len(b.List) > 0 {
				bf = strings.ToLower(b.List[0])
			}
			if af != bf {
				return strings.Compare(af, bf)
			}
			return cmpFloat(float64(len(a.List)), float64(len(b.List)))
		}
	}
	return strings.Compare(strings.ToLower(a.StringValue()), strings.ToLower(b.StringValue()))
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// 执行
// ---------------------------------------------------------------------------

// runQuery 在给定记录集上执行一个视图，返回结果。
//
// 拆成独立函数（不挂在 BaseService 上）是为了让查询逻辑可以脱离文件系统单测：
// 造一批 NoteRecord 直接跑，不用铺磁盘。
func runQuery(records []*NoteRecord, def BaseDef, view BaseView, known map[string]bool) *BaseResult {
	ctx := &compileCtx{known: known}

	// 目录范围限定
	scoped := records
	if folder := strings.Trim(strings.TrimSpace(def.Folder), "/"); folder != "" {
		scoped = make([]*NoteRecord, 0, len(records))
		prefix := folder + "/"
		for _, r := range records {
			if strings.HasPrefix(r.Path, prefix) {
				scoped = append(scoped, r)
			}
		}
		if len(scoped) == 0 {
			ctx.warn("目录 %q 下没有任何笔记", folder)
		}
	}

	// 筛选
	pred := compileGroup(def.Filters, ctx)
	matched := make([]*NoteRecord, 0, len(scoped))
	for _, r := range scoped {
		if pred(r) {
			matched = append(matched, r)
		}
	}

	// 排序
	sortRecords(matched, view.Sort)

	// 列：未指定时给一套通用默认列
	columns := view.Columns
	if len(columns) == 0 {
		columns = defaultColumns(view.Type)
	}

	limit := view.Limit
	if limit <= 0 {
		limit = defaultRowLimit
	}

	result := &BaseResult{
		ViewID:   view.ID,
		ViewName: view.Name,
		ViewType: normalizeViewType(view.Type, ctx),
		Columns:  columns,
		Total:    len(matched),
		Scanned:  len(records),
		Rows:     []*BaseRow{},
		Groups:   []*BaseGroup{},
		Warnings: []string{},
	}

	groupBy := strings.TrimSpace(view.GroupBy)
	if groupBy == "" {
		truncated := matched
		if len(truncated) > limit {
			truncated = truncated[:limit]
			result.Truncated = true
		}
		for _, r := range truncated {
			result.Rows = append(result.Rows, buildRow(r, columns))
		}
		result.Returned = len(result.Rows)
	} else {
		if known != nil && !known[groupBy] {
			ctx.warn("分组属性 %q 在当前工作区里不存在", groupBy)
		}
		result.Groups, result.Returned, result.Truncated = buildGroups(matched, columns, groupBy, limit)
	}

	if ctx.warnings != nil {
		result.Warnings = ctx.warnings
	}
	return result
}

// normalizeViewType 校正视图类型，未知类型退化为表格。
func normalizeViewType(t string, ctx *compileCtx) string {
	switch t {
	case ViewTable, ViewBoard, ViewList:
		return t
	case "":
		return ViewTable
	default:
		ctx.warn("未知的视图类型 %q，已按表格渲染", t)
		return ViewTable
	}
}

// defaultColumns 给未配置列的视图一套默认列。
func defaultColumns(viewType string) []string {
	switch viewType {
	case ViewBoard, ViewList:
		return []string{PropFileTitle, PropFileTags, PropFileMtime}
	default:
		return []string{PropFileTitle, PropFileTags, PropFileFolder, PropFileMtime}
	}
}

// buildRow 按列求值构造一行。
func buildRow(r *NoteRecord, columns []string) *BaseRow {
	row := &BaseRow{Path: r.Path, Title: r.Title, Cells: make([]BaseCell, 0, len(columns))}
	for _, col := range columns {
		v := r.Get(col)
		cell := BaseCell{
			Property: col,
			Kind:     v.Kind,
			Display:  v.StringValue(),
			Num:      v.Num,
			Bool:     v.Bool,
			Empty:    v.IsEmpty(),
		}
		if v.Kind == KindList {
			cell.List = v.List
		}
		row.Cells = append(row.Cells, cell)
	}
	return row
}

// buildGroups 按属性分组。
//
// 列表属性会让一篇笔记落进多个组（打了 AI 和 论文 两个标签就两边都出现）——
// 这是标签看板想要的行为，不是 bug。
func buildGroups(records []*NoteRecord, columns []string, groupBy string, limit int) (groups []*BaseGroup, returned int, truncated bool) {
	type bucket struct {
		label   string
		records []*NoteRecord
	}
	buckets := make(map[string]*bucket)
	var order []string

	push := func(key, label string, r *NoteRecord) {
		b, ok := buckets[key]
		if !ok {
			b = &bucket{label: label}
			buckets[key] = b
			order = append(order, key)
		}
		b.records = append(b.records, r)
	}

	for _, r := range records {
		v := r.Get(groupBy)
		if v.IsEmpty() {
			push("", emptyGroupLabel, r)
			continue
		}
		if v.Kind == KindList {
			for _, item := range v.List {
				key := strings.TrimSpace(item)
				if key == "" {
					continue
				}
				push(strings.ToLower(key), key, r)
			}
			continue
		}
		key := v.StringValue()
		push(strings.ToLower(key), key, r)
	}

	// 组排序：空值组永远垫底，其余按组内数量降序、同数按标签
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if (a == "") != (b == "") {
			return b == ""
		}
		if len(buckets[a].records) != len(buckets[b].records) {
			return len(buckets[a].records) > len(buckets[b].records)
		}
		return buckets[a].label < buckets[b].label
	})

	groups = make([]*BaseGroup, 0, len(order))
	budget := limit
	for _, key := range order {
		b := buckets[key]
		g := &BaseGroup{Key: key, Label: b.label, Count: len(b.records), Rows: []*BaseRow{}}
		for _, r := range b.records {
			if budget <= 0 {
				truncated = true
				break
			}
			g.Rows = append(g.Rows, buildRow(r, columns))
			budget--
			returned++
		}
		groups = append(groups, g)
	}
	return groups, returned, truncated
}
