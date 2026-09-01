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

// wikiLinkRegex 匹配 [[名称]] / [[名称|别名]] / [[名称#锚点]] / [[名称^块ID]]
//
// 排除换行；内部允许任意字符直到首个 ]。别名 / 锚点 / 块ID 由 parseWikiLinkTarget 细分。
var wikiLinkRegex = regexp.MustCompile(`\[\[([^\]\n]+)\]\]`)

// parseWikiLinkTarget 把一个 [[...]] 内部串拆分为 (file, anchor, blockID, alias)。
//
// 输入是 wikiLinkRegex 捕获组 1 的内容，例如：
//
//	note            → ("note", "", "", "")
//	note|别名        → ("note", "", "", "别名")
//	note#标题        → ("note", "标题", "", "")
//	note#标题|别名    → ("note", "标题", "", "别名")
//	note^block1     → ("note", "", "block1", "")
//	note^block1|别名 → ("note", "", "block1", "别名")
//	#标题            → ("", "标题", "", "")              // 同文件锚点
//	#标题|别名        → ("", "标题", "", "别名")
//	^block1         → ("", "", "block1", "")             // 同文件块引用
//	note.md         → ("note.md", "", "", "")            // 带扩展名也接受
//
// 行为约定（与 Obsidian 一致）：
//   - 别名 "|" 优先于锚点 "#" 和块 "^"，因为别名内可能含 #、^ 字符；
//     但实践中别名几乎不含这些字符，这里采用「先剥别名再剥锚点」的保守顺序，
//     与 Obsidian 官方文档示例一致。
//   - 锚点 "#" 优先于块 "^"：[[note#heading^block]] 视为锚点为 heading 的整段，
//     块 ID 仅作为定位辅助，file/anchor 仍是主键。
//   - 返回的 4 段都已经过 TrimSpace。
//
// 此函数是纯字符串解析，无副作用；图谱 / 反向链接 / 前端解析共用同一口径。
func parseWikiLinkTarget(target string) (file, anchor, blockID, alias string) {
	// 1) 先剥别名（| 后的整段视为显示文本）
	if idx := strings.Index(target, "|"); idx >= 0 {
		alias = strings.TrimSpace(target[idx+1:])
		target = strings.TrimSpace(target[:idx])
	}
	// 2) 块 ID（^xxx）：取最后一个 ^ 之后的串，与 Obsidian 一致（块 ID 仅在行尾）
	if idx := strings.LastIndex(target, "^"); idx >= 0 {
		blockID = strings.TrimSpace(target[idx+1:])
		target = strings.TrimSpace(target[:idx])
	}
	// 3) 锚点（#xxx）：取第一个 # 之后的串（标题里可能再次出现 #）
	if idx := strings.Index(target, "#"); idx >= 0 {
		anchor = strings.TrimSpace(target[idx+1:])
		target = strings.TrimSpace(target[:idx])
	}
	file = strings.TrimSpace(target)
	return
}

// embedRegex 匹配 ![[名称]]（嵌入语法），捕获组同 wikiLinkRegex。
// 图谱层把嵌入视为更紧的链接关系，与 [[名称]] 等价入边（由 edges map 去重）。
var embedRegex = regexp.MustCompile(`!\[\[([^\]\n]+)\]\]`)

// GetGraph 扫描工作区，构建笔记之间的关系图谱
// workspacePath: 工作区根目录
func (s *GraphService) GetGraph(workspacePath string) (*GraphData, error) {
	// base(无扩展名文件名) -> 相对路径 的映射，用于解析 [[链接]]
	files := make(map[string]string)
	// 大小写不敏感副本：[[Beta]] 也应能解析到 beta.md（Obsidian 行为）
	filesLower := make(map[string]string)
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
			filesLower[strings.ToLower(base)] = rel
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

		// 嵌入 ![[...]] 在 wiki-link 解析前先从内容中剥掉，避免重复匹配
		// （wikiLinkRegex 不带 ! 前缀，但会匹配 [[...]]；embedRegex 先捕获后用位置去重）
		embedMatches := embedRegex.FindAllStringSubmatchIndex(string(content), -1)
		// 嵌入里 g1 起始位置就是 [[...]] 内部串的起点；wikiLinkRegex 的 g1 起点与之相同
		seenEmbed := make(map[int]bool, len(embedMatches))
		for _, idx := range embedMatches {
			// idx: [fullStart, fullEnd, g1Start, g1End]
			seenEmbed[idx[2]] = true
		}
		// 合并嵌入目标到候选列表（图谱层把嵌入视为链接关系）
		type cand struct{ raw string }
		cands := make([]cand, 0, len(embedMatches)+8)
		for _, idx := range embedMatches {
			cands = append(cands, cand{raw: string(content[idx[2]:idx[3]])})
		}
		matches := wikiLinkRegex.FindAllStringSubmatchIndex(string(content), -1)
		for _, idx := range matches {
			if seenEmbed[idx[2]] {
				continue // 该 [[...]] 是嵌入的一部分，已在上面处理
			}
			cands = append(cands, cand{raw: string(content[idx[2]:idx[3]])})
		}
		for _, c := range cands {
			fileName, _, _, _ := parseWikiLinkTarget(c.raw)
			if fileName == "" {
				continue // 同文件锚点（[[#heading]]）不构成图谱边
			}

			var targetRel string
			if t, ok := files[fileName]; ok {
				targetRel = t
			} else if t, ok := files[filepath.Base(fileName)]; ok {
				// 容错：直接用文件名匹配（用户可能写了 path/to/note）
				targetRel = t
			} else if t, ok := filesLower[strings.ToLower(fileName)]; ok {
				// 大小写不敏感匹配：[[Beta]] 也能命中 beta.md
				targetRel = t
			} else {
				targetRel = "unresolved:" + fileName
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

// LinkCandidate 是编辑器 [[ 自动补全的候选项。
type LinkCandidate struct {
	Kind     string `json:"kind"`     // "file" | "heading"
	File     string `json:"file"`     // 相对路径，如 "Notes/foo.md"
	FileBase string `json:"fileBase"` // 无扩展名文件名，如 "foo"
	Heading  string `json:"heading"`  // heading 类型时为目标标题；file 类型为空
	Display  string `json:"display"`  // 人类可读标签（下拉里展示）
}

const maxLinkCandidates = 50

// mdFileEntry 是工作区内的一个 markdown 文件（相对路径 + 无扩展名文件名）。
type mdFileEntry struct {
	rel  string
	base string
}

// collectMarkdownFiles 遍历工作区，收集所有 .md / .markdown 文件。
// 跳过以 "." 开头的目录（.git / .notevault / node_modules 等）。
func collectMarkdownFiles(workspacePath string) ([]mdFileEntry, error) {
	var files []mdFileEntry
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
			files = append(files, mdFileEntry{rel: rel, base: base})
		}
		return nil
	})
	return files, err
}

// splitLinkQuery 把用户输入的链接文本（不含 [[ ]]）拆成文件部分与标题部分。
//   - "foo"       → ("foo", "", false)
//   - "foo#"      → ("foo", "", true)
//   - "foo#bar"   → ("foo", "bar", true)
//   - "#bar"      → ("", "bar", true)
func splitLinkQuery(query string) (filePart, headingPart string, hasHash bool) {
	if i := strings.Index(query, "#"); i >= 0 {
		return strings.TrimSpace(query[:i]), strings.TrimSpace(query[i+1:]), true
	}
	return strings.TrimSpace(query), "", false
}

// matchMarkdownFiles 按文件名 / 相对路径子串（大小写不敏感）过滤。
// part 为空时返回全部（用于 [[ 刚敲下时展示所有文件）。
func matchMarkdownFiles(files []mdFileEntry, part string) []mdFileEntry {
	if part == "" {
		return files
	}
	lp := strings.ToLower(part)
	var out []mdFileEntry
	for _, f := range files {
		if strings.Contains(strings.ToLower(f.base), lp) || strings.Contains(strings.ToLower(f.rel), lp) {
			out = append(out, f)
		}
	}
	return out
}

// extractHeadings 从 markdown 文件提取一级到六级标题文本。
// 跳过 YAML frontmatter 与围栏代码块（``` 或 ~~~），避免把 frontmatter 键或代码里的 # 当成标题。
func extractHeadings(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var headings []string
	lines := strings.Split(string(data), "\n")
	inFence := false
	inFrontmatter := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i == 0 && trimmed == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if trimmed == "---" {
				inFrontmatter = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if title != "" {
				headings = append(headings, title)
			}
		}
	}
	return headings, nil
}

// GetLinkCandidates 为编辑器的 [[ 自动补全提供候选。
// query 是用户输入的链接文本（不含 [[ 与 ]]）：
//   - ""        → 所有 markdown 文件（限前 maxLinkCandidates 条）
//   - "foo"     → 文件名 / 路径含 foo 的文件
//   - "foo#"    → foo 文件内的全部标题
//   - "foo#bar" → foo 文件内标题含 bar 的标题
//   - "#bar"    → 所有文件内标题含 bar 的标题
//
// 与 GetGraph 同口径：大小写不敏感、跳过隐藏目录。
func (s *GraphService) GetLinkCandidates(workspacePath, query string) ([]*LinkCandidate, error) {
	filePart, headingPart, hasHash := splitLinkQuery(strings.TrimSpace(query))
	files, err := collectMarkdownFiles(workspacePath)
	if err != nil {
		return nil, err
	}
	matched := matchMarkdownFiles(files, filePart)

	var out []*LinkCandidate

	// 标题模式：query 含 #，在匹配文件内找标题
	if hasHash {
		hp := strings.ToLower(headingPart)
		for _, f := range matched {
			heads, herr := extractHeadings(filepath.Join(workspacePath, filepath.FromSlash(f.rel)))
			if herr != nil {
				continue
			}
			for _, h := range heads {
				if hp == "" || strings.Contains(strings.ToLower(h), hp) {
					out = append(out, &LinkCandidate{
						Kind:     "heading",
						File:     f.rel,
						FileBase: f.base,
						Heading:  h,
						Display:  f.base + " › " + h,
					})
					if len(out) >= maxLinkCandidates {
						return out, nil
					}
				}
			}
		}
		return out, nil
	}

	// 文件模式
	for _, f := range matched {
		out = append(out, &LinkCandidate{
			Kind: "file", File: f.rel, FileBase: f.base, Display: f.rel,
		})
		if len(out) >= maxLinkCandidates {
			break
		}
	}
	return out, nil
}
