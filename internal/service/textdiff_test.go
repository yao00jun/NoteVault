package service

import (
	"strings"
	"testing"
)

// opSummary 把 diff 结果压成 "±行号:文本" 的紧凑串，便于断言
func opSummary(ops []DiffOp) string {
	parts := make([]string, 0, len(ops))
	for _, op := range ops {
		switch op.Type {
		case DiffInsert:
			parts = append(parts, "+"+op.Text)
		case DiffDelete:
			parts = append(parts, "-"+op.Text)
		case DiffEqual:
			parts = append(parts, " "+op.Text)
		case DiffGap:
			parts = append(parts, "…")
		}
	}
	return strings.Join(parts, "|")
}

func TestDiffText_Identical(t *testing.T) {
	res := diffText("a\nb\nc", "a\nb\nc")
	if res.Added != 0 || res.Removed != 0 {
		t.Fatalf("相同内容不应有增删，得到 +%d -%d", res.Added, res.Removed)
	}
	// 完全相同时应折叠为单个 gap，而不是回传全文
	if len(res.Ops) != 1 || res.Ops[0].Type != DiffGap {
		t.Fatalf("相同内容应折叠为单个 gap，得到 %s", opSummary(res.Ops))
	}
	if res.Ops[0].Count != 3 {
		t.Fatalf("gap 应记录 3 行，得到 %d", res.Ops[0].Count)
	}
}

func TestDiffText_PureInsert(t *testing.T) {
	res := diffText("a\nc", "a\nb\nc")
	if res.Added != 1 || res.Removed != 0 {
		t.Fatalf("期望 +1 -0，得到 +%d -%d", res.Added, res.Removed)
	}
	if got := opSummary(res.Ops); got != " a|+b| c" {
		t.Fatalf("diff 序列不符: %s", got)
	}
}

func TestDiffText_PureDelete(t *testing.T) {
	res := diffText("a\nb\nc", "a\nc")
	if res.Added != 0 || res.Removed != 1 {
		t.Fatalf("期望 +0 -1，得到 +%d -%d", res.Added, res.Removed)
	}
	if got := opSummary(res.Ops); got != " a|-b| c" {
		t.Fatalf("diff 序列不符: %s", got)
	}
}

func TestDiffText_Replace(t *testing.T) {
	res := diffText("title\nold body\nfooter", "title\nnew body\nfooter")
	if res.Added != 1 || res.Removed != 1 {
		t.Fatalf("期望 +1 -1，得到 +%d -%d", res.Added, res.Removed)
	}
	summary := opSummary(res.Ops)
	if !strings.Contains(summary, "-old body") || !strings.Contains(summary, "+new body") {
		t.Fatalf("应同时含删除旧行与新增新行: %s", summary)
	}
}

func TestDiffText_FromEmpty(t *testing.T) {
	res := diffText("", "a\nb")
	if res.Added != 2 || res.Removed != 0 {
		t.Fatalf("空 → 两行应为 +2 -0，得到 +%d -%d", res.Added, res.Removed)
	}
}

func TestDiffText_ToEmpty(t *testing.T) {
	// 文件被删除的场景：应表达为「全部删除」，而不是报错或空 diff
	res := diffText("a\nb", "")
	if res.Added != 0 || res.Removed != 2 {
		t.Fatalf("两行 → 空应为 +0 -2，得到 +%d -%d", res.Added, res.Removed)
	}
}

// CRLF 归一化：Windows 编辑器写出的 \r\n 不能被算成整篇差异
func TestDiffText_IgnoresCarriageReturn(t *testing.T) {
	res := diffText("a\r\nb\r\nc", "a\nb\nc")
	if res.Added != 0 || res.Removed != 0 {
		t.Fatalf("仅换行符差异不应产生 diff，得到 +%d -%d", res.Added, res.Removed)
	}
}

// 末尾换行不应被算成额外空行差异
func TestDiffText_TrailingNewlineIgnored(t *testing.T) {
	res := diffText("a\nb\n", "a\nb")
	if res.Added != 0 || res.Removed != 0 {
		t.Fatalf("末尾换行不应产生 diff，得到 +%d -%d", res.Added, res.Removed)
	}
}

// 大文件里改一行：必须折叠掉远处的未变更区域，否则 IPC payload 会爆
func TestDiffText_CollapsesDistantContext(t *testing.T) {
	lines := make([]string, 400)
	for i := range lines {
		lines[i] = "line"
	}
	oldText := strings.Join(lines, "\n")
	lines[200] = "CHANGED"
	newText := strings.Join(lines, "\n")

	res := diffText(oldText, newText)
	if res.Added != 1 || res.Removed != 1 {
		t.Fatalf("期望 +1 -1，得到 +%d -%d", res.Added, res.Removed)
	}
	// 3 行上下文 × 2 侧 + 1 增 + 1 删 + 2 个 gap ≈ 10 个 op，绝不该是 400+
	if len(res.Ops) > 20 {
		t.Fatalf("未变更区域应被折叠，op 数 = %d（期望 ≤ 20）", len(res.Ops))
	}
	gapTotal := 0
	for _, op := range res.Ops {
		if op.Type == DiffGap {
			gapTotal += op.Count
		}
	}
	if gapTotal == 0 {
		t.Fatal("应存在记录折叠行数的 gap op")
	}
}

// 超出算力上限时退化为整块替换，并如实置 Truncated
func TestDiffText_TruncatesOversizedDiff(t *testing.T) {
	makeBody := func(tag string, n int) string {
		lines := make([]string, n)
		for i := range lines {
			lines[i] = tag + string(rune('a'+i%26)) + "-filler-line"
		}
		return strings.Join(lines, "\n")
	}
	// 2500 × 2500 = 6.25e6 > diffCellLimit(4e6)，且无公共前后缀可剥离
	res := diffText(makeBody("x", 2500), makeBody("y", 2500))
	if !res.Truncated {
		t.Fatal("超出 diffCellLimit 应置 Truncated=true")
	}
	if res.Added == 0 || res.Removed == 0 {
		t.Fatalf("退化路径仍须表达完整增删，得到 +%d -%d", res.Added, res.Removed)
	}
}

// 行号必须能对应回原文，否则前端渲染的行号栏是错的
func TestDiffText_LineNumbersAreAccurate(t *testing.T) {
	res := diffText("a\nb\nc\nd", "a\nB\nc\nd")
	for _, op := range res.Ops {
		switch op.Type {
		case DiffDelete:
			if op.Text == "b" && op.OldLine != 2 {
				t.Fatalf("删除行 b 应为旧文件第 2 行，得到 %d", op.OldLine)
			}
			if op.NewLine != 0 {
				t.Fatalf("删除行不应有新行号，得到 %d", op.NewLine)
			}
		case DiffInsert:
			if op.Text == "B" && op.NewLine != 2 {
				t.Fatalf("新增行 B 应为新文件第 2 行，得到 %d", op.NewLine)
			}
			if op.OldLine != 0 {
				t.Fatalf("新增行不应有旧行号，得到 %d", op.OldLine)
			}
		}
	}
}

func TestSplitLines(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"a\n", 1},
		{"a\nb", 2},
		{"a\n\nb", 3},
		{"a\r\nb\r\n", 2},
	}
	for _, c := range cases {
		if got := len(splitLines(c.in)); got != c.want {
			t.Errorf("splitLines(%q) 行数 = %d，期望 %d", c.in, got, c.want)
		}
	}
}
