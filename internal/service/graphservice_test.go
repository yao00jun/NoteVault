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
