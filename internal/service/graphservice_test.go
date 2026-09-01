package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func nodeTitles(nodes []*GraphNode) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Title)
	}
	return out
}

func TestGraphService_GetGraph(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "A.md"), "# A\n链接到 [[B]] 和 [[C]]")
	writeFile(t, filepath.Join(dir, "B.md"), "# B\n回链 [[A]]")
	writeFile(t, filepath.Join(dir, "C.md"), "# C\n未链接")
	writeFile(t, filepath.Join(dir, "sub", "D.md"), "# D\n指向 [[不存在的笔记]] 和 [[A]]")
	writeFile(t, filepath.Join(dir, "note.txt"), "[[A]]") // 非 md 应忽略

	svc := NewGraphService()
	data, err := svc.GetGraph(dir)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// 节点：A,B,C,D + 未解析虚拟节点 "不存在的笔记" = 5
	if len(data.Nodes) != 5 {
		t.Errorf("expected 5 nodes, got %d: %v", len(data.Nodes), nodeTitles(data.Nodes))
	}

	var unresolved *GraphNode
	for _, n := range data.Nodes {
		if n.ID == "unresolved:不存在的笔记" {
			unresolved = n
		}
	}
	if unresolved == nil {
		t.Errorf("unresolved node not found")
	} else if unresolved.Resolved {
		t.Errorf("unresolved node should have Resolved=false")
	}

	// 边：A->B, A->C, B->A, D->A, D->unresolved = 5
	if len(data.Edges) != 5 {
		t.Errorf("expected 5 edges, got %d", len(data.Edges))
	}

	degreeOf := map[string]int{}
	for _, n := range data.Nodes {
		degreeOf[n.ID] = n.Degree
	}
	if degreeOf["A.md"] < 1 {
		t.Errorf("A should have degree>=1, got %d", degreeOf["A.md"])
	}
}

func TestGraphService_GetGraph_Empty(t *testing.T) {
	dir := t.TempDir()
	svc := NewGraphService()
	data, err := svc.GetGraph(dir)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}
	if len(data.Nodes) != 0 || len(data.Edges) != 0 {
		t.Errorf("expected empty graph, got %d nodes %d edges", len(data.Nodes), len(data.Edges))
	}
}

func TestGraphService_GetGraph_SkipHidden(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".trash", "X.md"), "# X\n[[Y]]")
	writeFile(t, filepath.Join(dir, "Y.md"), "# Y")
	svc := NewGraphService()
	data, err := svc.GetGraph(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range data.Nodes {
		if n.ID == "X.md" || strings.Contains(n.ID, "trash") {
			t.Errorf("hidden dir file should be skipped, got %s", n.ID)
		}
	}
}

func TestParseWikiLinkTarget(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		file   string
		anchor string
		block  string
		alias  string
	}{
		{"纯文件名", "note", "note", "", "", ""},
		{"文件名+别名", "note|别名", "note", "", "", "别名"},
		{"文件名+锚点", "note#标题", "note", "标题", "", ""},
		{"文件名+锚点+别名", "note#标题|别名", "note", "标题", "", "别名"},
		{"文件名+块ID", "note^block1", "note", "", "block1", ""},
		{"文件名+块ID+别名", "note^block1|别名", "note", "", "block1", "别名"},
		{"同文件锚点", "#标题", "", "标题", "", ""},
		{"同文件锚点+别名", "#标题|别名", "", "标题", "", "别名"},
		{"同文件块ID", "^block1", "", "", "block1", ""},
		{"带扩展名", "note.md", "note.md", "", "", ""},
		{"前后空白", "  note  #  标题  |  别名  ", "note", "标题", "", "别名"},
		{"仅别名", "|别名", "", "", "", "别名"},
		{"空串", "", "", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, a, b, al := parseWikiLinkTarget(c.input)
			if f != c.file || a != c.anchor || b != c.block || al != c.alias {
				t.Errorf("parseWikiLinkTarget(%q) = (%q,%q,%q,%q), want (%q,%q,%q,%q)",
					c.input, f, a, b, al, c.file, c.anchor, c.block, c.alias)
			}
		})
	}
}

func TestGraphService_GetGraph_StripsAnchorAndBlock(t *testing.T) {
	dir := t.TempDir()
	// A 含锚点链接 [[B#某节]]、块链接 [[B^blk1]]、嵌入 ![[B]]
	writeFile(t, filepath.Join(dir, "A.md"), "# A\n[[B#某节]] [[B^blk1]] ![[B]]")
	writeFile(t, filepath.Join(dir, "B.md"), "# B\n回链 [[A]]")

	svc := NewGraphService()
	data, err := svc.GetGraph(dir)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// 边：A->B（三种链接都收敛到同一目标，去重后一条）+ B->A = 2 条
	if len(data.Edges) != 2 {
		t.Errorf("expected 2 edges (A->B dedup + B->A), got %d: %+v", len(data.Edges), data.Edges)
	}
}

func TestGraphService_GetGraph_EmbedAsLink(t *testing.T) {
	dir := t.TempDir()
	// A 嵌入 B：图谱应识别为 A->B 的链接关系
	writeFile(t, filepath.Join(dir, "A.md"), "# A\n![[B]]")
	writeFile(t, filepath.Join(dir, "B.md"), "# B")
	writeFile(t, filepath.Join(dir, "C.md"), "# C")

	svc := NewGraphService()
	data, err := svc.GetGraph(dir)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	var hasAtoB bool
	for _, e := range data.Edges {
		if e.Source == "A.md" && e.Target == "B.md" {
			hasAtoB = true
		}
	}
	if !hasAtoB {
		t.Errorf("embed ![[B]] should produce edge A->B; edges=%+v", data.Edges)
	}
}

// 构造一个含多种文件的临时库，供 GetLinkCandidates 测试复用。
func linkCandidatesFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Alpha.md"), "# 标题一\n正文\n## 小节\n### 深层\n```\n# 代码里的标题不应出现\n```\n")
	writeFile(t, filepath.Join(dir, "Beta.md"), "---\ntitle: Beta\n---\n# 开头\n## Beta小节\n")
	writeFile(t, filepath.Join(dir, "sub", "Gamma.md"), "# Gamma 标题\n## G 小节\n")
	writeFile(t, filepath.Join(dir, "notes", "Daily.md"), "# 日记\n## 2026-09-01\n")
	return dir
}

func candidatesByKind(cands []*LinkCandidate) (files, headings []string) {
	for _, c := range cands {
		if c.Kind == "file" {
			files = append(files, c.File)
		} else if c.Kind == "heading" {
			headings = append(headings, c.File+"#"+c.Heading)
		}
	}
	return
}

func TestGetLinkCandidates_FileMode(t *testing.T) {
	dir := linkCandidatesFixture(t)
	svc := NewGraphService()

	// 空 query → 返回全部文件（file 类型）
	all, err := svc.GetLinkCandidates(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	files, _ := candidatesByKind(all)
	if len(files) != 4 {
		t.Errorf("empty query should return 4 files, got %d: %v", len(files), files)
	}

	// "beta" 过滤 → 仅 Beta.md
	beta, err := svc.GetLinkCandidates(dir, "beta")
	if err != nil {
		t.Fatal(err)
	}
	files, headings := candidatesByKind(beta)
	if len(files) != 1 || files[0] != "Beta.md" {
		t.Errorf("'beta' should match only Beta.md, got %v (headings=%v)", files, headings)
	}
	if len(headings) != 0 {
		t.Errorf("file mode should not return headings, got %v", headings)
	}
}

func TestGetLinkCandidates_HeadingMode(t *testing.T) {
	dir := linkCandidatesFixture(t)
	svc := NewGraphService()

	// "alpha#" → Alpha.md 内的全部标题（含代码块里的伪标题、frontmatter 外的真实标题）
	alpha, err := svc.GetLinkCandidates(dir, "alpha#")
	if err != nil {
		t.Fatal(err)
	}
	_, headings := candidatesByKind(alpha)
	want := []string{"Alpha.md#标题一", "Alpha.md#小节", "Alpha.md#深层"}
	if len(headings) != len(want) {
		t.Errorf("alpha# headings = %v, want %v", headings, want)
	}
	for _, w := range want {
		if !sliceContains(headings, w) {
			t.Errorf("missing heading %q in %v", w, headings)
		}
	}
	// 代码块里的 "# 代码里的标题不应出现" 必须被排除
	if sliceContains(headings, "Alpha.md#代码里的标题不应出现") {
		t.Errorf("code-fence heading must be excluded: %v", headings)
	}

	// "alpha#小节" → 仅匹配 "小节"
	sub, err := svc.GetLinkCandidates(dir, "alpha#小节")
	if err != nil {
		t.Fatal(err)
	}
	_, headings = candidatesByKind(sub)
	if len(headings) != 1 || headings[0] != "Alpha.md#小节" {
		t.Errorf("alpha#小节 should match only '小节', got %v", headings)
	}
}

func TestGetLinkCandidates_CrossFileHeading(t *testing.T) {
	dir := linkCandidatesFixture(t)
	svc := NewGraphService()

	// "#小节" → 跨文件匹配所有含 "小节" 的标题（Alpha 小节 + Beta Beta小节 + Gamma G小节）
	all, err := svc.GetLinkCandidates(dir, "#小节")
	if err != nil {
		t.Fatal(err)
	}
	_, headings := candidatesByKind(all)
	if len(headings) != 3 {
		t.Errorf("#小节 should match 3 headings across files, got %v", headings)
	}
}

func sliceContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
