package service

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// GraphNode 表示知识图谱中的一个笔记节点
type GraphNode struct {
	ID       string `json:"id"`       // 相对路径，作为唯一标识
	Title    string `json:"title"`    // 笔记标题（文件名或首个 # 标题）
	Path     string `json:"path"`     // 相对路径（未解析节点为 "unresolved:名称"）
	Degree   int    `json:"degree"`   // 连接数，用于节点大小
	Resolved bool   `json:"resolved"` // 是否为已存在的笔记（false 表示未解析的虚拟节点）
}

// GraphEdge 表示一条双向链接
type GraphEdge struct {
	Source string `json:"source"` // 源节点 ID
	Target string `json:"target"` // 目标节点 ID
}

// GraphData 知识图谱数据
type GraphData struct {
	Nodes []*GraphNode `json:"nodes"`
	Edges []*GraphEdge `json:"edges"`
}

// GraphService 提供知识图谱（双向链接关系）构建能力
type GraphService struct{}

// NewGraphService 创建图谱服务实例
func NewGraphService() *GraphService {
	return &GraphService{}
}

// wikiLinkRegex 匹配 [[名称]] / [[名称|别名]] / [[名称#锚点]]
var wikiLinkRegex = regexp.MustCompile(`\[\[([^\]\n]+)\]\]`)

// GetGraph 扫描工作区，构建笔记之间的关系图谱
// workspacePath: 工作区根目录
func (s *GraphService) GetGraph(workspacePath string) (*GraphData, error) {
	// base(无扩展名文件名) -> 相对路径 的映射，用于解析 [[链接]]
	files := make(map[string]string)
	var paths []string

	err := filepath.Walk(workspacePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".md" || ext == ".markdown" {
			rel, _ := filepath.Rel(workspacePath, path)
			rel = filepath.ToSlash(rel)
			base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			files[base] = rel
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 并发扫描每个文件中的双向链接（有界 worker 池；旧实现每文件一个 goroutine）
	edges := make(map[string]bool)
	degree := make(map[string]int)
	var mu sync.Mutex

	forEachFileBounded(paths, func(fp string) {
		content, err := os.ReadFile(fp)
		if err != nil {
			return
		}
		rel, _ := filepath.Rel(workspacePath, fp)
		rel = filepath.ToSlash(rel)

		matches := wikiLinkRegex.FindAllStringSubmatch(string(content), -1)
		for _, m := range matches {
			target := m[1]
			// 剥离别名与锚点
			if idx := strings.Index(target, "|"); idx >= 0 {
				target = target[:idx]
			}
			if idx := strings.Index(target, "#"); idx >= 0 {
				target = target[:idx]
			}
			target = strings.TrimSpace(target)
			if target == "" {
				continue
			}

			var targetRel string
			if t, ok := files[target]; ok {
				targetRel = t
			} else if t, ok := files[filepath.Base(target)]; ok {
				// 容错：直接用文件名匹配
				targetRel = t
			} else {
				targetRel = "unresolved:" + target
			}
			if targetRel == rel {
				continue // 忽略自链接
			}

			key := rel + "\x00" + targetRel
			mu.Lock()
			if !edges[key] {
				edges[key] = true
				degree[rel]++
				degree[targetRel]++
			}
			mu.Unlock()
		}
	})

	// 构建节点集合
	nodeMap := make(map[string]*GraphNode)
	addNode := func(id, title string, resolved bool) *GraphNode {
		if n, ok := nodeMap[id]; ok {
			return n
		}
		n := &GraphNode{ID: id, Title: title, Path: id, Resolved: resolved}
		nodeMap[id] = n
		return n
	}

	for _, rel := range files {
		base := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
		addNode(rel, base, true)
	}
	for key := range edges {
		parts := strings.SplitN(key, "\x00", 2)
		target := parts[1]
		if strings.HasPrefix(target, "unresolved:") {
			name := strings.TrimPrefix(target, "unresolved:")
			addNode(target, name, false)
		}
	}
	for id, d := range degree {
		if n, ok := nodeMap[id]; ok {
			n.Degree = d
		}
	}

	var edgeList []*GraphEdge
	for key := range edges {
		parts := strings.SplitN(key, "\x00", 2)
		edgeList = append(edgeList, &GraphEdge{Source: parts[0], Target: parts[1]})
	}

	nodes := make([]*GraphNode, 0, len(nodeMap))
	for _, n := range nodeMap {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Resolved != nodes[j].Resolved {
			return nodes[i].Resolved // 已解析节点排在前面
		}
		return nodes[i].Title < nodes[j].Title
	})

	return &GraphData{Nodes: nodes, Edges: edgeList}, nil
}
