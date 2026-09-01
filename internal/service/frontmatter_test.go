package service

import (
	"reflect"
	"testing"
	"time"
)

func TestSplitFrontMatter(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantFound bool
		wantFM    string
		wantBody  string
	}{
		{
			name:      "标准 front matter",
			content:   "---\ntitle: 测试\n---\n正文内容",
			wantFound: true,
			wantFM:    "title: 测试",
			wantBody:  "正文内容",
		},
		{
			name:      "无 front matter",
			content:   "# 标题\n正文",
			wantFound: false,
			wantFM:    "",
			wantBody:  "# 标题\n正文",
		},
		{
			name:      "CRLF 换行",
			content:   "---\r\ntitle: 测试\r\n---\r\n正文",
			wantFound: true,
			wantFM:    "title: 测试",
			wantBody:  "正文",
		},
		{
			name:      "BOM 开头",
			content:   "\ufeff---\ntitle: 测试\n---\n正文",
			wantFound: true,
			wantFM:    "title: 测试",
			wantBody:  "正文",
		},
		{
			name: "水平分隔线不算 front matter",
			// `----` 后面不是换行，不该被当成 front matter 起始
			content:   "----\n正文",
			wantFound: false,
			wantBody:  "----\n正文",
		},
		{
			name:      "未闭合",
			content:   "---\ntitle: 测试\n没有结束标记",
			wantFound: false,
			wantBody:  "---\ntitle: 测试\n没有结束标记",
		},
		{
			name:      "正文里有 --- 分隔线",
			content:   "---\ntitle: A\n---\n正文一\n\n---\n\n正文二",
			wantFound: true,
			wantFM:    "title: A",
			wantBody:  "正文一\n\n---\n\n正文二",
		},
		{
			name:      "空 front matter",
			content:   "---\n---\n正文",
			wantFound: true,
			wantFM:    "",
			wantBody:  "正文",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body, found := SplitFrontMatter(tt.content)
			if found != tt.wantFound {
				t.Fatalf("found = %v, want %v", found, tt.wantFound)
			}
			if found && fm != tt.wantFM {
				t.Errorf("fm = %q, want %q", fm, tt.wantFM)
			}
			if body != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

func TestParseFrontMatter_Scalars(t *testing.T) {
	fm := `title: 深度学习笔记
rating: 4.5
count: 12
published: true
draft: no
created: 2026-08-31
updated: 2026-08-31 14:30
deadline: 2026-08-31T14:30:00Z
version: 1.2.3
empty:
nulled: null
quoted: "带 引号 的值"
single: '单引号'`

	props := ParseFrontMatter(fm)

	check := func(key string, kind PropKind) PropValue {
		t.Helper()
		v, ok := props[key]
		if !ok {
			t.Fatalf("缺少属性 %q", key)
		}
		if v.Kind != kind {
			t.Errorf("%s.Kind = %v, want %v (raw=%q)", key, v.Kind, kind, v.Raw)
		}
		return v
	}

	if v := check("title", KindString); v.Str != "深度学习笔记" {
		t.Errorf("title = %q", v.Str)
	}
	if v := check("rating", KindNumber); v.Num != 4.5 {
		t.Errorf("rating = %v", v.Num)
	}
	if v := check("count", KindNumber); v.Num != 12 {
		t.Errorf("count = %v", v.Num)
	}
	if v := check("published", KindBool); !v.Bool {
		t.Errorf("published = %v", v.Bool)
	}
	if v := check("draft", KindBool); v.Bool {
		t.Errorf("draft = %v, want false（no 应识别为 false）", v.Bool)
	}
	if v := check("created", KindDate); v.Date.Format("2006-01-02") != "2026-08-31" {
		t.Errorf("created = %v", v.Date)
	}
	if v := check("updated", KindDate); v.Date.Hour() != 14 || v.Date.Minute() != 30 {
		t.Errorf("updated = %v", v.Date)
	}
	if v := check("deadline", KindDate); v.Date.UTC().Hour() != 14 {
		t.Errorf("deadline = %v", v.Date)
	}
	// 版本号形如 1.2.3，ParseFloat 会失败，必须落到 string 而不是被当日期
	if v := check("version", KindString); v.Str != "1.2.3" {
		t.Errorf("version = %q", v.Str)
	}
	if v := check("quoted", KindString); v.Str != "带 引号 的值" {
		t.Errorf("quoted = %q（引号应被剥掉）", v.Str)
	}
	if v := check("single", KindString); v.Str != "单引号" {
		t.Errorf("single = %q", v.Str)
	}
	if v, ok := props["nulled"]; !ok || !v.IsNull {
		t.Errorf("nulled 应为 null 值，得到 %+v", v)
	}
}

func TestParseFrontMatter_Lists(t *testing.T) {
	tests := []struct {
		name string
		fm   string
		key  string
		want []string
	}{
		{
			name: "内联数组",
			fm:   "tags: [AI, 论文, 深度学习]",
			key:  "tags",
			want: []string{"AI", "论文", "深度学习"},
		},
		{
			name: "内联数组带引号且元素含逗号",
			fm:   `tags: ["a, b", c]`,
			key:  "tags",
			want: []string{"a, b", "c"},
		},
		{
			name: "块式数组",
			fm:   "tags:\n  - AI\n  - 论文",
			key:  "tags",
			want: []string{"AI", "论文"},
		},
		{
			name: "块式数组无缩进",
			fm:   "tags:\n- AI\n- 论文",
			key:  "tags",
			want: []string{"AI", "论文"},
		},
		{
			name: "块式数组中间有空行",
			fm:   "tags:\n  - AI\n\n  - 论文",
			key:  "tags",
			want: []string{"AI", "论文"},
		},
		{
			name: "空数组",
			fm:   "tags: []",
			key:  "tags",
			want: []string{},
		},
		{
			name: "只写了键没写值",
			fm:   "tags:\nstatus: reading",
			key:  "tags",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := ParseFrontMatter(tt.fm)
			v, ok := props[tt.key]
			if !ok {
				t.Fatalf("缺少属性 %q，实际有 %v", tt.key, keysOf(props))
			}
			if v.Kind != KindList {
				t.Fatalf("Kind = %v, want list", v.Kind)
			}
			if !reflect.DeepEqual(v.List, tt.want) {
				t.Errorf("List = %v, want %v", v.List, tt.want)
			}
		})
	}
}

func TestParseFrontMatter_BlockListFollowedByKey(t *testing.T) {
	// 块式数组结束后紧跟其他键，两者都要正确解析
	props := ParseFrontMatter("tags:\n  - AI\n  - 论文\nstatus: reading\nrating: 5")

	tags, ok := props["tags"]
	if !ok || !reflect.DeepEqual(tags.List, []string{"AI", "论文"}) {
		t.Errorf("tags = %+v", tags)
	}
	if v, ok := props["status"]; !ok || v.Str != "reading" {
		t.Errorf("status = %+v", v)
	}
	if v, ok := props["rating"]; !ok || v.Num != 5 {
		t.Errorf("rating = %+v", v)
	}
}

func TestParseFrontMatter_NestedMap(t *testing.T) {
	props := ParseFrontMatter("author:\n  name: 张三\n  email: a@b.c\nstatus: reading")

	if v, ok := props["author.name"]; !ok || v.Str != "张三" {
		t.Errorf("author.name = %+v, 实际键 %v", v, keysOf(props))
	}
	if v, ok := props["author.email"]; !ok || v.Str != "a@b.c" {
		t.Errorf("author.email = %+v", v)
	}
	// 退出嵌套作用域后，同级键不能被加上前缀
	if _, bad := props["author.status"]; bad {
		t.Error("status 被错误地归到 author 下")
	}
	if v, ok := props["status"]; !ok || v.Str != "reading" {
		t.Errorf("status = %+v", v)
	}
}

func TestParseFrontMatter_InlineMap(t *testing.T) {
	props := ParseFrontMatter("author: {name: 张三, email: a@b.c}")
	if v, ok := props["author.name"]; !ok || v.Str != "张三" {
		t.Errorf("author.name = %+v, 实际键 %v", v, keysOf(props))
	}
	if v, ok := props["author.email"]; !ok || v.Str != "a@b.c" {
		t.Errorf("author.email = %+v", v)
	}
}

func TestParseFrontMatter_Comments(t *testing.T) {
	props := ParseFrontMatter(`# 这是整行注释
status: reading  # 行尾注释
title: C#入门
url: "http://a.b/#frag"
tags: [AI, ML] # 备注`)

	if v, ok := props["status"]; !ok || v.Str != "reading" {
		t.Errorf("status = %+v（行尾注释应剥掉）", v)
	}
	// C# 里的 # 前面不是空白，不能当注释剥
	if v, ok := props["title"]; !ok || v.Str != "C#入门" {
		t.Errorf("title = %+v（C# 的井号不该被当注释）", v)
	}
	if v, ok := props["url"]; !ok || v.Str != "http://a.b/#frag" {
		t.Errorf("url = %+v（引号内的井号不该被当注释）", v)
	}
	if v, ok := props["tags"]; !ok || !reflect.DeepEqual(v.List, []string{"AI", "ML"}) {
		t.Errorf("tags = %+v", v)
	}
	if _, bad := props["# 这是整行注释"]; bad {
		t.Error("整行注释被当成键解析了")
	}
}

func TestParseFrontMatter_ColonEdgeCases(t *testing.T) {
	props := ParseFrontMatter(`time: 10:30 的会议
url: http://example.com/x
"weird:key": v`)

	// 冒号后必须是空白才算分隔符，所以 10:30 不会被截断
	if v, ok := props["time"]; !ok || v.Str != "10:30 的会议" {
		t.Errorf("time = %+v, 实际键 %v", v, keysOf(props))
	}
	if v, ok := props["url"]; !ok || v.Str != "http://example.com/x" {
		t.Errorf("url = %+v（URL 里的冒号不该当分隔符）", v)
	}
	if v, ok := props["weird:key"]; !ok || v.Str != "v" {
		t.Errorf("weird:key = %+v", v)
	}
}

func TestParseFrontMatter_MalformedIsBestEffort(t *testing.T) {
	// 坏一行不该影响其他行——这是不用完整 YAML 库的核心理由
	props := ParseFrontMatter(`status: reading
这是一行没有冒号的垃圾
- 孤立的数组项
rating: 5`)

	if v, ok := props["status"]; !ok || v.Str != "reading" {
		t.Errorf("status = %+v", v)
	}
	if v, ok := props["rating"]; !ok || v.Num != 5 {
		t.Errorf("rating = %+v（坏行之后的键仍应解析出来）", v)
	}
}

func TestParseFrontMatter_Empty(t *testing.T) {
	if got := ParseFrontMatter(""); len(got) != 0 {
		t.Errorf("空输入应返回空表，得到 %v", got)
	}
	if got := ParseFrontMatter("   \n  \n"); len(got) != 0 {
		t.Errorf("纯空白应返回空表，得到 %v", got)
	}
}

func TestPropValue_IsEmpty(t *testing.T) {
	tests := []struct {
		name string
		v    PropValue
		want bool
	}{
		{"null", nullValue(), true},
		{"空串", stringValue(""), true},
		{"空白串", stringValue("   "), true},
		{"非空串", stringValue("x"), false},
		{"空列表", listValue(nil, ""), true},
		{"非空列表", listValue([]string{"a"}, "a"), false},
		// 关键：0 和 false 不算空
		{"数字零不算空", numberValue(0, "0"), false},
		{"布尔 false 不算空", boolValue(false, "false"), false},
		{"日期不算空", dateValue(time.Now(), "now"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.v.IsEmpty(); got != tt.want {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPropValue_StringValue(t *testing.T) {
	if got := listValue([]string{"a", "b"}, "").StringValue(); got != "a, b" {
		t.Errorf("列表展示 = %q, want %q", got, "a, b")
	}
	if got := boolValue(true, "yes").StringValue(); got != "true" {
		t.Errorf("布尔展示 = %q, want %q", got, "true")
	}
	if got := nullValue().StringValue(); got != "" {
		t.Errorf("null 展示 = %q, want 空串", got)
	}
	if got := numberValue(4.5, "4.5").StringValue(); got != "4.5" {
		t.Errorf("数字展示 = %q", got)
	}
}

func keysOf(m map[string]PropValue) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
