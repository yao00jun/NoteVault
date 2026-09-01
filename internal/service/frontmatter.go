package service

import (
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// 属性值：带类型的 front matter 取值
// ---------------------------------------------------------------------------

// PropKind 是属性值的推断类型。
//
// 为什么必须带类型：Bases 的筛选与排序要按语义比较，不能一律当字符串。
// 字符串比较下 "10" < "9"、"2026-01-02" 与 "2026-1-2" 不等价、
// true 和 "true" 无法区分——这些在「按 rating 排序」「筛 published=true」
// 这类真实用法里全是错的结果。
type PropKind string

const (
	// KindString 普通文本
	KindString PropKind = "string"
	// KindNumber 整数或浮点
	KindNumber PropKind = "number"
	// KindBool true / false / yes / no
	KindBool PropKind = "bool"
	// KindDate 日期或日期时间
	KindDate PropKind = "date"
	// KindList 数组（内联 [a, b] 或块式 - a）
	KindList PropKind = "list"
)

// PropValue 是一个 front matter 属性的解析结果。
//
// Raw 始终保留原始文本，用于展示与「原样回写」；其余字段按 Kind 填充。
// 刻意不用 any 存值：JSON 往前端过一遍后 any 会退化成 float64/string，
// 类型信息丢失，前端只能再猜一遍。
type PropValue struct {
	Kind PropKind `json:"kind"`
	Raw  string   `json:"raw"`

	Str    string    `json:"str,omitempty"`
	Num    float64   `json:"num,omitempty"`
	Bool   bool      `json:"bool,omitempty"`
	Date   time.Time `json:"date,omitempty"`
	List   []string  `json:"list,omitempty"`
	IsNull bool      `json:"isNull,omitempty"`
}

// StringValue 返回用于展示的文本形式。
func (v PropValue) StringValue() string {
	if v.IsNull {
		return ""
	}
	switch v.Kind {
	case KindList:
		return strings.Join(v.List, ", ")
	case KindBool:
		if v.Bool {
			return "true"
		}
		return "false"
	default:
		return v.Raw
	}
}

// IsEmpty 判定属性是否「视为空」。
//
// 空的定义要和用户直觉一致：缺失、null、空串、空数组都算空；
// 但数字 0 和布尔 false **不算空**——这是很多查询工具的经典坑，
// 「rating 为 0」不该被 is-empty 筛掉。
func (v PropValue) IsEmpty() bool {
	if v.IsNull {
		return true
	}
	switch v.Kind {
	case KindList:
		return len(v.List) == 0
	case KindNumber, KindBool, KindDate:
		return false
	default:
		return strings.TrimSpace(v.Str) == ""
	}
}

// nullValue 返回表示「属性不存在」的值。
func nullValue() PropValue {
	return PropValue{Kind: KindString, IsNull: true}
}

// stringValue 构造一个字符串属性值。
func stringValue(s string) PropValue {
	return PropValue{Kind: KindString, Raw: s, Str: s}
}

// numberValue 构造一个数字属性值。
func numberValue(n float64, raw string) PropValue {
	return PropValue{Kind: KindNumber, Raw: raw, Num: n, Str: raw}
}

// boolValue 构造一个布尔属性值。
func boolValue(b bool, raw string) PropValue {
	return PropValue{Kind: KindBool, Raw: raw, Bool: b, Str: raw}
}

// dateValue 构造一个日期属性值。
func dateValue(t time.Time, raw string) PropValue {
	return PropValue{Kind: KindDate, Raw: raw, Date: t, Str: raw, Num: float64(t.Unix())}
}

// listValue 构造一个列表属性值。
func listValue(items []string, raw string) PropValue {
	if items == nil {
		items = []string{}
	}
	return PropValue{Kind: KindList, Raw: raw, List: items, Str: strings.Join(items, ", ")}
}

// ---------------------------------------------------------------------------
// 标量推断
// ---------------------------------------------------------------------------

// dateLayouts 是支持的日期格式，按「越具体越靠前」排列。
//
// 只收常见的几种：YAML 生态里日期写法极多，全支持会把非日期的东西
// （比如版本号 "1.2.3"、编号 "2026"）误判成日期，反而更难排查。
var dateLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006/01/02",
}

// parseScalar 把一个 YAML 标量文本推断成带类型的属性值。
//
// 顺序有讲究：bool → date → number → string。
// date 必须早于 number，否则 "2026-01-02" 会被 ParseFloat 拒绝后落到 string
// （不影响），但 "20260102" 这类纯数字反而不该当日期——所以 date 只认带分隔符的。
func parseScalar(raw string) PropValue {
	s := strings.TrimSpace(raw)
	s = unquote(s)

	if s == "" {
		return PropValue{Kind: KindString, Raw: raw, Str: ""}
	}
	// YAML 的显式空值
	if s == "null" || s == "~" {
		return nullValue()
	}

	// bool：只认 YAML 1.1 常见写法，不认 "on"/"off"
	// （"on" 在中文笔记里更可能是真的英文单词）
	switch strings.ToLower(s) {
	case "true", "yes":
		return boolValue(true, s)
	case "false", "no":
		return boolValue(false, s)
	}

	// date：要求含 - 或 / 分隔符，避免纯数字被误判
	if strings.ContainsAny(s, "-/") {
		for _, layout := range dateLayouts {
			if t, err := time.Parse(layout, s); err == nil {
				return dateValue(t, s)
			}
		}
	}

	// number
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return numberValue(n, s)
	}

	return stringValue(s)
}

// unquote 去掉包裹的单/双引号（仅在成对时）。
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	first, last := s[0], s[len(s)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// stripComment 去掉行尾 YAML 注释。
//
// 不能简单 strings.Index(line, "#")：`tags: [a, b] # 备注` 要剥，
// 但 `title: C#入门` 和 `note: "#hash"` 不能剥。规则：# 前必须是空白，
// 且不能落在引号内。
func stripComment(line string) string {
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch c {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if inSingle || inDouble {
				continue
			}
			if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
				return strings.TrimRight(line[:i], " \t")
			}
		}
	}
	return line
}

// splitInlineList 拆分内联数组 `[a, b, "c, d"]` 的元素。
//
// 手写而不是 strings.Split(",")：带引号的元素里可能含逗号。
func splitInlineList(body string) []string {
	var items []string
	var cur strings.Builder
	inSingle, inDouble := false, false

	flush := func() {
		item := strings.TrimSpace(cur.String())
		cur.Reset()
		item = unquote(item)
		if item != "" {
			items = append(items, item)
		}
	}

	for i := 0; i < len(body); i++ {
		c := body[i]
		switch c {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
			cur.WriteByte(c)
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
			cur.WriteByte(c)
		case ',':
			if inSingle || inDouble {
				cur.WriteByte(c)
				continue
			}
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return items
}

// ---------------------------------------------------------------------------
// front matter 解析
// ---------------------------------------------------------------------------

// frontMatterMaxBytes 限制 front matter 的扫描长度。
//
// front matter 本质是元数据，正常不会有几百 KB；设上限是防御性的：
// 遇到没有闭合 `---` 的文件（手写截断、编辑器崩溃残留）时，
// 不至于把整篇正文都拿去按 YAML 解析。
const frontMatterMaxBytes = 64 * 1024

// SplitFrontMatter 把内容切成 front matter 原文与正文。
//
// 返回 found=false 时 body 是完整内容（不含 front matter 的文件占多数，
// 这条路径必须零成本）。
func SplitFrontMatter(content string) (fm string, body string, found bool) {
	// 允许 UTF-8 BOM 开头：Windows 记事本存的 md 很常见
	content = strings.TrimPrefix(content, "\ufeff")

	if !strings.HasPrefix(content, "---") {
		return "", content, false
	}
	// `---` 后必须换行，排除 `----` 这类分隔线开头
	rest := content[3:]
	if !strings.HasPrefix(rest, "\n") && !strings.HasPrefix(rest, "\r\n") {
		return "", content, false
	}
	rest = strings.TrimLeft(rest, "\r")
	rest = strings.TrimPrefix(rest, "\n")

	scan := rest
	if len(scan) > frontMatterMaxBytes {
		scan = scan[:frontMatterMaxBytes]
	}

	// 找闭合的 `---`（必须独占一行）
	offset := 0
	for {
		idx := strings.Index(scan[offset:], "---")
		if idx == -1 {
			return "", content, false
		}
		pos := offset + idx
		// 行首校验
		atLineStart := pos == 0 || scan[pos-1] == '\n'
		if atLineStart {
			after := scan[pos+3:]
			trimmedAfter := strings.TrimLeft(after, "\r")
			if trimmedAfter == "" || strings.HasPrefix(trimmedAfter, "\n") {
				fmText := strings.TrimRight(scan[:pos], "\r\n")
				bodyText := strings.TrimPrefix(trimmedAfter, "\n")
				return fmText, bodyText, true
			}
		}
		offset = pos + 3
	}
}

// ParseFrontMatter 解析 front matter 为属性表。
//
// 这是一个**有意为之的迷你 YAML 子集**解析器，不引第三方 YAML 库，原因：
//  1. NoteVault 的 front matter 是人手写的元数据，用到的语法是 YAML 的极小子集
//     （标量、内联数组、块式数组、一层嵌套映射）；
//  2. 完整 YAML 库对不合法输入是「报错整块失败」，而笔记里出现半截 front matter
//     是常态——这里要的是尽最大努力解析出能用的部分，坏一行不影响其他行；
//  3. 少一个依赖，二进制体积和供应链面都小一圈。
//
// 嵌套映射按 `parent.child` 扁平化：Bases 的属性选择器是平铺列表，
// 扁平化后 `author.name` 可以直接当属性名用，不需要在查询语法里引入路径概念。
func ParseFrontMatter(fm string) map[string]PropValue {
	props := make(map[string]PropValue)
	if strings.TrimSpace(fm) == "" {
		return props
	}

	lines := strings.Split(strings.ReplaceAll(fm, "\r\n", "\n"), "\n")

	// 待收集的块式结构
	var pendingKey string // 等待块式数组/映射的键
	var pendingItems []string
	pendingIndent := -1
	// 嵌套映射的父键前缀
	var mapPrefix string
	mapIndent := -1

	flushPending := func() {
		if pendingKey != "" {
			props[pendingKey] = listValue(pendingItems, strings.Join(pendingItems, ", "))
		}
		pendingKey = ""
		pendingItems = nil
		pendingIndent = -1
	}

	for _, rawLine := range lines {
		line := stripComment(rawLine)
		trimmed := strings.TrimSpace(line)

		// 空行不终止块（YAML 允许数组项之间有空行）
		if trimmed == "" {
			continue
		}
		// 整行注释
		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		// 块式数组项
		if strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			if pendingKey != "" && (pendingIndent < 0 || indent >= pendingIndent) {
				item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
				item = unquote(item)
				if item != "" {
					pendingItems = append(pendingItems, item)
				}
				if pendingIndent < 0 {
					pendingIndent = indent
				}
				continue
			}
			// 没有归属的数组项：孤立语法，忽略
			continue
		}

		// 退出嵌套映射作用域
		if mapPrefix != "" && indent <= mapIndent {
			mapPrefix = ""
			mapIndent = -1
		}

		// key: value
		colon := indexOfKeyColon(trimmed)
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:colon])
		key = unquote(key)
		value := strings.TrimSpace(trimmed[colon+1:])
		if key == "" {
			continue
		}

		// 新键出现，先结算上一个块
		flushPending()

		fullKey := key
		if mapPrefix != "" {
			fullKey = mapPrefix + "." + key
		}

		// 内联数组
		if strings.HasPrefix(value, "[") {
			body := strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
			props[fullKey] = listValue(splitInlineList(body), value)
			continue
		}
		// 内联映射：{a: 1, b: 2} —— 拆成 parent.a / parent.b
		if strings.HasPrefix(value, "{") {
			body := strings.TrimSuffix(strings.TrimPrefix(value, "{"), "}")
			for _, pair := range splitInlineList(body) {
				if c := indexOfKeyColon(pair); c > 0 {
					k := strings.TrimSpace(pair[:c])
					v := strings.TrimSpace(pair[c+1:])
					if k != "" {
						props[fullKey+"."+unquote(k)] = parseScalar(v)
					}
				}
			}
			continue
		}

		// 空值：可能是块式数组，也可能是嵌套映射，看下一行才知道
		if value == "" {
			pendingKey = fullKey
			pendingItems = nil
			pendingIndent = -1
			// 同时预备成嵌套映射前缀；若下一行是 `- x` 则走数组分支
			if mapPrefix == "" {
				mapPrefix = fullKey
				mapIndent = indent
			}
			continue
		}

		props[fullKey] = parseScalar(value)
	}
	flushPending()

	// 只当过映射前缀、既没有子键也没有数组项的键：补一个空值，
	// 否则「写了 tags: 但没写内容」的文件在属性列表里会整键消失。
	if mapPrefix != "" {
		if _, ok := props[mapPrefix]; !ok {
			hasChild := false
			for k := range props {
				if strings.HasPrefix(k, mapPrefix+".") {
					hasChild = true
					break
				}
			}
			if !hasChild {
				props[mapPrefix] = listValue(nil, "")
			}
		}
	}

	return props
}

// indexOfKeyColon 找到 `key: value` 中作为分隔符的冒号位置。
//
// 不能用 strings.Index(":")：`title: 10:30 的会议` 的第一个冒号是对的，
// 但 `"a:b": v` 的第一个冒号在引号内。规则：跳过引号内的冒号，
// 且冒号后必须是空白或行尾（`http://x` 里的冒号后是 `/`，不算）。
func indexOfKeyColon(s string) int {
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case ':':
			if inSingle || inDouble {
				continue
			}
			if i == len(s)-1 || s[i+1] == ' ' || s[i+1] == '\t' {
				return i
			}
		}
	}
	return -1
}
