package service

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// RoadmapService（学习路径进度投影）
//
// 路径文档（frontmatter 带 kind: roadmap 的 Learning 顶层 .md，如
// AI转型路径-13周.md）是学习路径的唯一数据源——本服务只做投影：
// 解析章节勾选状态为完成度/每轨进度/节奏对照，不回写文档。
//
// 文档结构约定（与 AI转型路径-13周.md 一致）：
//   - frontmatter：kind: roadmap、goal、deadline、weeklyHours
//   - 正文按 `## 标题` 分轨；勾选项 `- [ ]` / `- [x]`；估时 `#est/2.5h`
//   - 无勾选项的分节（如「总预算」说明段）不作为轨输出
// ---------------------------------------------------------------------------

// RoadmapTrack 是路径文档中一个分轨的进度投影。
type RoadmapTrack struct {
	Name       string  `json:"name"`
	Total      int     `json:"total"`
	Done       int     `json:"done"`
	Percent    int     `json:"percent"`
	TotalHours float64 `json:"totalHours"`
	DoneHours  float64 `json:"doneHours"`
	// Next 是本轨第一个未完成项的行内容（勾选标记已剥除，保留双链与估时标记）；
	// 分节内全部完成时为空串。
	Next      string  `json:"next,omitempty"`
	NextHours float64 `json:"nextHours,omitempty"`
}

// RoadmapProgress 是一份路径文档的完整投影。
type RoadmapProgress struct {
	Path        string         `json:"path"`
	Title       string         `json:"title"`
	Goal        string         `json:"goal"`
	Deadline    string         `json:"deadline"`
	WeeklyHours string         `json:"weeklyHours"`
	Tracks      []RoadmapTrack `json:"tracks"`
	Total       int            `json:"total"`
	Done        int            `json:"done"`
	Percent     int            `json:"percent"`
	TotalHours  float64        `json:"totalHours"`
	DoneHours   float64        `json:"doneHours"`
	// NextStep 是整份文档的第一个未完成项（跨轨按文档顺序）；全部完成时为空串。
	NextStep  string  `json:"nextStep,omitempty"`
	NextHours float64 `json:"nextHours,omitempty"`
}

// RoadmapService 把路径文档投影为进度数据。
type RoadmapService struct{}

// NewRoadmapService 构造路径进度服务（无状态，无外部依赖）。
func NewRoadmapService() *RoadmapService {
	return &RoadmapService{}
}

// roadmapEstRE 从勾选项里提取估时标记（#est/1.5h）。
var roadmapEstRE = regexp.MustCompile(`#est/(\d+(?:\.\d+)?)h`)

// roadmapDoneRE 匹配已完成勾选项。
var roadmapDoneRE = regexp.MustCompile(`^(\s*)- \[(x|X)\]`)

// roadmapPendingRE 匹配未完成勾选项。
var roadmapPendingRE = regexp.MustCompile(`^(\s*)- \[ \]`)

// GetRoadmapProgress 扫描 Learning 顶层（不含子文件夹）的 Markdown 文件，
// 返回所有带 `kind: roadmap` 标记的路径文档投影，按文件名排序。
func (s *RoadmapService) GetRoadmapProgress(workspacePath string) ([]RoadmapProgress, error) {
	learningDir := filepath.Join(workspacePath, "Learning")
	entries, err := os.ReadDir(learningDir)
	if os.IsNotExist(err) {
		return []RoadmapProgress{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 Learning 目录失败：%w", err)
	}

	out := []RoadmapProgress{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			continue
		}
		full := filepath.Join(learningDir, entry.Name())
		content, err := os.ReadFile(full)
		if err != nil {
			continue // 单文件读取失败不阻断整批
		}
		progress, ok := parseRoadmap("Learning/"+entry.Name(), string(content))
		if !ok {
			continue // 非 roadmap 文档静默跳过
		}
		out = append(out, *progress)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// parseRoadmap 解析单份路径文档；frontmatter 无 kind: roadmap 时返回 (nil, false)。
func parseRoadmap(relativePath, content string) (*RoadmapProgress, bool) {
	fm, body, found := SplitFrontMatter(content)
	if !found {
		return nil, false
	}
	props := ParseFrontMatter(fm)
	kind, kindOK := workbenchProperty(props, "kind", "类型")
	if kindOK && !kind.IsEmpty() && !strings.EqualFold(kind.StringValue(), "roadmap") {
		return nil, false
	}

	title := workbenchText(props, "name", "title", "名称", "标题")
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(relativePath), ".md")
	}
	goal := workbenchText(props, "goal", "目标")
	deadline := workbenchText(props, "deadline", "截止", "deadline")
	weeklyHours := workbenchText(props, "weeklyHours", "每周预算")

	progress := &RoadmapProgress{
		Path:        relativePath,
		Title:       title,
		Goal:        goal,
		Deadline:    deadline,
		WeeklyHours: weeklyHours,
		Tracks:      []RoadmapTrack{},
	}

	// 正文按 `## ` 标题分节；首个 ## 之前的导语不参与统计
	lines := strings.Split(body, "\n")
	sectionStart := -1
	sectionName := ""
	flush := func(end int) {
		if sectionStart < 0 {
			return
		}
		track := buildRoadmapTrack(sectionName, lines[sectionStart:end])
		if track.Total > 0 {
			progress.Tracks = append(progress.Tracks, track)
			progress.Total += track.Total
			progress.Done += track.Done
			progress.TotalHours += track.TotalHours
			progress.DoneHours += track.DoneHours
			if progress.NextStep == "" && track.Next != "" {
				progress.NextStep = track.Next
				progress.NextHours = track.NextHours
			}
		}
	}
	for i, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush(i)
			sectionStart = i + 1
			sectionName = strings.TrimSpace(strings.TrimPrefix(line, "## "))
		}
	}
	flush(len(lines))

	progress.Percent = percentOrZero(progress.Done, progress.Total)
	return progress, true
}

// buildRoadmapTrack 统计一个分节的勾选与估时，并记录第一个未完成项。
func buildRoadmapTrack(name string, lines []string) RoadmapTrack {
	track := RoadmapTrack{Name: name}
	for _, line := range lines {
		est := 0.0
		if match := roadmapEstRE.FindStringSubmatch(line); match != nil {
			// 正则已保证 match[1] 是合法数字，解析失败保持 0 即可
			est, _ = strconv.ParseFloat(match[1], 64)
			track.TotalHours += est
		}
		switch {
		case roadmapDoneRE.MatchString(line):
			track.Done++
			track.Total++
			track.DoneHours += est
		case roadmapPendingRE.MatchString(line):
			track.Total++
			if track.Next == "" {
				track.Next = strings.TrimSpace(roadmapPendingRE.ReplaceAllString(line, ""))
				track.NextHours = est
			}
		}
	}
	track.Percent = percentOrZero(track.Done, track.Total)
	return track
}

// percentOrZero 完成度百分比（0-100），total 为 0 时返回 0。
func percentOrZero(done, total int) int {
	if total <= 0 {
		return 0
	}
	return done * 100 / total
}

// roadmapChapterTargets 从工作区文件集合里收集「路径勾选项 → 双链目标」的投影：
// 目标名 -> 是否在任一路径文档中被勾选。只扫描 Learning 顶层带 kind: roadmap
// frontmatter 的 .md；同一目标被多篇路径引用时取并集（任一勾选即视为完成）。
// 同文档锚点（[[#标题]]）、块引用与无名目标不产生条目；目标是否属于某本书的
// 章节，由调用方结合文档列表解析（本函数不做文档存在性判断）。
func roadmapChapterTargets(files []workbenchFile) map[string]bool {
	targets := map[string]bool{}
	for _, file := range files {
		rel := strings.TrimPrefix(file.path, "Learning/")
		if !strings.HasPrefix(file.path, "Learning/") || strings.Contains(rel, "/") || !strings.EqualFold(filepath.Ext(file.path), ".md") {
			continue // 只看 Learning 顶层；book.md 与章节都在子文件夹
		}
		if kind, ok := workbenchProperty(file.props, "kind", "类型"); !ok || kind.IsEmpty() || !strings.EqualFold(kind.StringValue(), "roadmap") {
			continue
		}
		for i, line := range file.lines {
			if !file.visible[i] {
				continue
			}
			done := roadmapDoneRE.MatchString(line.text)
			if !done && !roadmapPendingRE.MatchString(line.text) {
				continue
			}
			for _, match := range wikiLinkRegex.FindAllStringSubmatch(line.text, -1) {
				name, _, _, _ := parseWikiLinkTarget(match[1])
				if name == "" {
					continue
				}
				if done {
					targets[name] = true
				} else if _, seen := targets[name]; !seen {
					targets[name] = false
				}
			}
		}
	}
	return targets
}
