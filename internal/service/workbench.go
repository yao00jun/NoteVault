package service

import (
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// WorkbenchTask enriches the existing todo contract without replacing legacy APIs.
type WorkbenchTask = TodoItem

type WorkbenchDocument struct {
	Path       string `json:"path"`
	Title      string `json:"title"`
	ModifiedAt string `json:"modifiedAt"`
}

type WorkbenchProgress struct {
	ID        string `json:"id"`
	TaskID    string `json:"taskId"`
	FilePath  string `json:"filePath"`
	TaskTitle string `json:"taskTitle"`
	Project   string `json:"project"`
	Note      string `json:"note"`
	At        string `json:"at"`
}

type WorkbenchProject struct {
	Name       string              `json:"name"`
	Path       string              `json:"path"`
	Folder     string              `json:"folder"`
	Status     string              `json:"status"`
	NextStep   string              `json:"nextStep"`
	TechStack  []string            `json:"techStack"`
	TaskIDs    []string            `json:"taskIds"`
	Notes      []WorkbenchDocument `json:"notes"`
	ModifiedAt string              `json:"modifiedAt"`
}

type WorkbenchBook struct {
	Name        string              `json:"name"`
	Path        string              `json:"path"`
	Folder      string              `json:"folder"`
	Status      string              `json:"status"`
	Progress    float64             `json:"progress"`
	Projects    []string            `json:"projects"`
	Chapters    []WorkbenchDocument `json:"chapters"`
	NoteCount   int                 `json:"noteCount"`
	ReviewCount int                 `json:"reviewCount"`
}

type InterviewCard struct {
	ID           string `json:"id"`
	FilePath     string `json:"filePath"`
	LineIndex    int    `json:"lineIndex"`
	Comment      string `json:"comment"`
	Question     string `json:"question"`
	Answer       string `json:"answer"`
	Level        string `json:"level"`
	Interval     int    `json:"interval"`
	Due          string `json:"due"`
	Reps         int    `json:"reps"`
	Failures     int    `json:"failures"`
	LastReviewed string `json:"lastReviewed"`
	Weak         bool   `json:"weak"`
}

type WorkbenchRadar struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type WorkbenchSnapshot struct {
	Date      string              `json:"date"`
	Tasks     []*WorkbenchTask    `json:"tasks"`
	Projects  []WorkbenchProject  `json:"projects"`
	Books     []WorkbenchBook     `json:"books"`
	Cards     []InterviewCard     `json:"cards"`
	Progress  []WorkbenchProgress `json:"progress"`
	Radar     WorkbenchRadar      `json:"radar"`
	Documents []WorkbenchDocument `json:"documents"`
	IndexedAt string              `json:"indexedAt"`
	Warnings  []string            `json:"warnings"`
}

// GetWorkbench derives every field from the current Markdown, without a cache or database.
func (s *TodoService) GetWorkbench(workspacePath, date string) (*WorkbenchSnapshot, error) {
	if _, err := workbenchDate(date); err != nil {
		return nil, err
	}
	s.workbenchMu.RLock()
	defer s.workbenchMu.RUnlock()
	files, warnings, err := readWorkbenchFiles(workspacePath)
	if err != nil {
		return nil, err
	}
	snapshot := &WorkbenchSnapshot{
		Date: date, Tasks: []*WorkbenchTask{}, Projects: []WorkbenchProject{}, Books: []WorkbenchBook{},
		Cards: []InterviewCard{}, Progress: []WorkbenchProgress{}, Documents: []WorkbenchDocument{},
		Radar: WorkbenchRadar{Path: "Learning/技术雷达.md"}, IndexedAt: time.Now().Format(time.RFC3339Nano), Warnings: warnings,
	}
	for _, file := range files {
		document := workbenchDocument(file)
		snapshot.Documents = append(snapshot.Documents, document)
		snapshot.Tasks = append(snapshot.Tasks, parseWorkbenchTasks(file)...)
		parts := strings.Split(file.path, "/")
		if len(parts) == 3 && strings.EqualFold(parts[0], "Projects") && strings.EqualFold(parts[2], "project.md") {
			name := workbenchText(file.props, "name", "title", "名称", "项目名称", "项目")
			if name == "" {
				name = workbenchHeadingTitle(file, parts[1])
			}
			snapshot.Projects = append(snapshot.Projects, WorkbenchProject{
				Name: name, Path: file.path, Folder: path.Dir(file.path),
				Status:    workbenchTextDefault(file.props, "进行中", "status", "状态"),
				NextStep:  workbenchText(file.props, "nextStep", "next_step", "下一步", "下一步行动"),
				TechStack: workbenchList(file.props, "techStack", "tech_stack", "stack", "技术栈", "关联技术"),
				TaskIDs:   []string{}, Notes: []WorkbenchDocument{}, ModifiedAt: file.modifiedAt,
			})
		}
		if len(parts) == 3 && strings.EqualFold(parts[0], "Learning") && strings.EqualFold(parts[2], "book.md") {
			name := workbenchText(file.props, "name", "title", "名称", "书名", "分册名", "分册名称")
			if name == "" {
				name = workbenchHeadingTitle(file, parts[1])
			}
			progress, _ := strconv.ParseFloat(strings.TrimSuffix(workbenchText(file.props, "progress", "进度", "研读进度", "阅读进度"), "%"), 64)
			progress = math.Max(0, math.Min(100, progress))
			snapshot.Books = append(snapshot.Books, WorkbenchBook{
				Name: name, Path: file.path, Folder: path.Dir(file.path),
				Status: workbenchTextDefault(file.props, "在学", "status", "状态"), Progress: progress,
				Projects: workbenchList(file.props, "projects", "relatedProjects", "关联项目", "项目"), Chapters: []WorkbenchDocument{},
			})
		}
		if len(parts) > 1 && strings.EqualFold(parts[0], "Learning") {
			cards, cardWarnings := parseWorkbenchCards(file)
			snapshot.Cards = append(snapshot.Cards, cards...)
			snapshot.Warnings = append(snapshot.Warnings, cardWarnings...)
		}
		if strings.EqualFold(file.path, "Learning/技术雷达.md") {
			snapshot.Radar = WorkbenchRadar{Path: file.path, Content: file.content}
		}
	}
	for i := range snapshot.Projects {
		project := &snapshot.Projects[i]
		for _, task := range snapshot.Tasks {
			if workbenchProjectMatches(task, *project) {
				task.Project, task.ProjectPath = project.Name, project.Path
				project.TaskIDs = append(project.TaskIDs, task.ID)
			}
		}
		for _, document := range snapshot.Documents {
			if !strings.HasPrefix(document.Path, project.Folder+"/") {
				continue
			}
			project.ModifiedAt = workbenchLatestTime(project.ModifiedAt, document.ModifiedAt)
			if document.Path != project.Path && !strings.EqualFold(path.Base(document.Path), "Tasks.md") {
				project.Notes = append(project.Notes, document)
			}
		}
	}
	for i := range snapshot.Books {
		book := &snapshot.Books[i]
		for _, document := range snapshot.Documents {
			if strings.HasPrefix(document.Path, book.Folder+"/") && document.Path != book.Path && document.Path != book.Folder+"/sources.md" && document.Path != book.Folder+"/ai-plan.md" {
				book.Chapters = append(book.Chapters, document)
			}
		}
		book.NoteCount = len(book.Chapters)
		for _, card := range snapshot.Cards {
			if workbenchCardBelongsToBook(card, *book, files) && card.LastReviewed != date && (card.Due == "" || card.Due <= date) {
				book.ReviewCount++
			}
		}
	}
	for _, task := range snapshot.Tasks {
		for i := range task.Progress {
			task.Progress[i].Project = task.Project
		}
		snapshot.Progress = append(snapshot.Progress, task.Progress...)
	}
	sort.SliceStable(snapshot.Cards, func(i, j int) bool { return snapshot.Cards[i].Due < snapshot.Cards[j].Due })
	sort.SliceStable(snapshot.Progress, func(i, j int) bool { return workbenchProgressBefore(snapshot.Progress[j], snapshot.Progress[i]) })
	return snapshot, nil
}

func workbenchProperty(props map[string]PropValue, aliases ...string) (PropValue, bool) {
	for _, alias := range aliases {
		if value, ok := props[alias]; ok && !value.IsEmpty() {
			return value, true
		}
	}
	normalize := func(key string) string {
		return strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(key))
	}
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, alias := range aliases {
		for _, key := range keys {
			if normalize(key) == normalize(alias) && !props[key].IsEmpty() {
				return props[key], true
			}
		}
	}
	return PropValue{}, false
}

func workbenchText(props map[string]PropValue, aliases ...string) string {
	if value, ok := workbenchProperty(props, aliases...); ok {
		return strings.TrimSpace(value.StringValue())
	}
	return ""
}

func workbenchTextDefault(props map[string]PropValue, fallback string, aliases ...string) string {
	if value := workbenchText(props, aliases...); value != "" {
		return value
	}
	return fallback
}

func workbenchList(props map[string]PropValue, aliases ...string) []string {
	result := []string{}
	value, ok := workbenchProperty(props, aliases...)
	if !ok {
		return result
	}
	items := value.List
	if value.Kind != KindList {
		items = strings.FieldsFunc(value.StringValue(), func(r rune) bool { return r == ',' || r == '，' || r == ';' || r == '；' })
	}
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func workbenchHeadingTitle(file workbenchFile, fallback string) string {
	for i, line := range file.lines {
		if file.visible[i] {
			if match := workbenchHeadingRE.FindStringSubmatch(line.text); match != nil {
				return strings.TrimSpace(match[2])
			}
		}
	}
	return fallback
}

func workbenchDocument(file workbenchFile) WorkbenchDocument {
	title := workbenchText(file.props, "title", "name", "名称", "项目名称", "书名")
	if title == "" {
		title = workbenchHeadingTitle(file, strings.TrimSuffix(path.Base(file.path), path.Ext(file.path)))
	}
	return WorkbenchDocument{Path: file.path, Title: title, ModifiedAt: file.modifiedAt}
}

func workbenchProjectMatches(task *TodoItem, project WorkbenchProject) bool {
	if task.ProjectPath != "" {
		return strings.EqualFold(task.ProjectPath, project.Path)
	}
	return task.Project != "" && (task.Project == project.Name || task.Project == path.Base(project.Folder) || strings.EqualFold(task.Project, project.Folder))
}

func workbenchLatestTime(a, b string) string {
	left, leftErr := time.Parse(time.RFC3339Nano, a)
	right, rightErr := time.Parse(time.RFC3339Nano, b)
	if rightErr == nil && (leftErr != nil || right.After(left)) {
		return b
	}
	return a
}

func workbenchCardBelongsToBook(card InterviewCard, book WorkbenchBook, files []workbenchFile) bool {
	if strings.HasPrefix(card.FilePath, book.Folder+"/") {
		return true
	}
	for _, file := range files {
		if file.path != card.FilePath {
			continue
		}
		for _, related := range workbenchList(file.props, "book", "books", "technology", "分册", "技术") {
			if related == book.Name || related == path.Base(book.Folder) || related == book.Folder {
				return true
			}
		}
	}
	return strings.HasPrefix(card.FilePath, "Learning/面试宝典/") && strings.HasPrefix(strings.ToLower(path.Base(card.FilePath)), strings.ToLower(path.Base(book.Folder)))
}
