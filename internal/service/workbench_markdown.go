package service

import (
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	workbenchTaskRE     = regexp.MustCompile(`^(?:\x{FEFF})?([ \t]*)[-*+][ \t]+\[([ xX])\](?:[ \t]+(.*))?$`)
	workbenchKindRE     = regexp.MustCompile(`^\[(US|DTS|额外|(?i:Extra))\][ \t]*`)
	workbenchMetadataRE = regexp.MustCompile(`(?:^|[ \t]+)#(due|date|scheduled|done|priority|project|status)/([^ \t]+)`)
	workbenchBlockerRE  = regexp.MustCompile(`[（(]卡点[：:][ \t]*`)
	workbenchProgressRE = regexp.MustCompile(`<!--[ \t]*progress:[ \t]*(\{.*?\})[ \t]*-->`)
	workbenchStatusRE   = regexp.MustCompile(`(?:\[status::[ \t]*([a-z_-]+)\]|[（(]status:[ \t]*([a-z_-]+)[）)]|\bstatus:[ \t]*([a-z_-]+))`)
	workbenchFenceRE    = regexp.MustCompile("^[ \\t]*(`{3,}|~{3,})(.*)$")
	workbenchDateRE     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	workbenchHeadingRE  = regexp.MustCompile(`^(?:\x{FEFF})?[ \t]{0,3}(#{1,6})[ \t]+(.+?)(?:[ \t]+#+)?[ \t]*$`)
)

type workbenchLine struct {
	text   string
	ending string
}

type workbenchFile struct {
	path       string
	content    string
	modifiedAt string
	lines      []workbenchLine
	visible    []bool
	props      map[string]PropValue
}

// Keep each original line ending: replacing one task must not reformat its note.
func splitWorkbenchLines(content string) []workbenchLine {
	lines := make([]workbenchLine, 0)
	for content != "" {
		end := strings.IndexByte(content, '\n')
		if end < 0 {
			lines = append(lines, workbenchLine{text: content})
			break
		}
		text, ending := content[:end], "\n"
		if strings.HasSuffix(text, "\r") {
			text, ending = strings.TrimSuffix(text, "\r"), "\r\n"
		}
		lines = append(lines, workbenchLine{text: text, ending: ending})
		content = content[end+1:]
	}
	return lines
}

func joinWorkbenchLines(lines []workbenchLine) string {
	var out strings.Builder
	for _, line := range lines {
		out.WriteString(line.text)
		out.WriteString(line.ending)
	}
	return out.String()
}

func newWorkbenchFile(relative, content, modifiedAt string) workbenchFile {
	fm, _, _ := SplitFrontMatter(content)
	file := workbenchFile{path: relative, content: content, modifiedAt: modifiedAt, lines: splitWorkbenchLines(content), props: ParseFrontMatter(fm)}
	file.visible = workbenchVisibleLines(file.lines, content)
	return file
}

func workbenchVisibleLines(lines []workbenchLine, content string) []bool {
	visible := make([]bool, len(lines))
	_, body, hasFrontmatter := SplitFrontMatter(content)
	frontmatterLines := 0
	if hasFrontmatter {
		frontmatterLines = strings.Count(content, "\n") - strings.Count(body, "\n")
	}
	var fence byte
	fenceLength := 0
	inComment := false
	for i, line := range lines {
		if i < frontmatterLines {
			continue
		}
		text := strings.TrimPrefix(line.text, "\ufeff")
		if !inComment {
			if match := workbenchFenceRE.FindStringSubmatch(text); match != nil {
				if fence == 0 {
					fence, fenceLength = match[1][0], len(match[1])
					continue
				}
				if match[1][0] == fence && len(match[1]) >= fenceLength && strings.TrimSpace(match[2]) == "" {
					fence = 0
					continue
				}
			}
		}
		if fence != 0 {
			continue
		}
		// Single-line comments stay visible to the task/SRS metadata parsers.
		// Scan every boundary: a closed comment can be followed by an open one.
		startedInComment := inComment
		remaining := text
		for {
			if inComment {
				end := strings.Index(remaining, "-->")
				if end < 0 {
					break
				}
				remaining, inComment = remaining[end+3:], false
			} else {
				start := strings.Index(remaining, "<!--")
				if start < 0 {
					break
				}
				remaining, inComment = remaining[start+4:], true
			}
		}
		if startedInComment || inComment {
			continue
		}
		visible[i] = true
	}
	return visible
}

func workbenchDate(value string) (time.Time, error) {
	if !workbenchDateRE.MatchString(value) {
		return time.Time{}, fmt.Errorf("日期必须为 YYYY-MM-DD：%s", value)
	}
	date, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("无效日期：%s", value)
	}
	return date, nil
}

func workbenchDailyDate(relative string) string {
	parts := strings.Split(relative, "/")
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Daily") {
		return ""
	}
	day := strings.TrimSuffix(parts[1], path.Ext(parts[1]))
	if _, err := workbenchDate(day); err != nil {
		return ""
	}
	return day
}

func readWorkbenchFiles(workspace string) ([]workbenchFile, []string, error) {
	if strings.TrimSpace(workspace) == "" {
		return nil, nil, fmt.Errorf("请先打开工作区")
	}
	info, err := os.Stat(workspace)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("工作区不是目录")
	}
	paths := make([]string, 0)
	warnings := make([]string, 0)
	err = filepath.WalkDir(workspace, func(full string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			warnings = append(warnings, fmt.Sprintf("无法读取 %s：%v", full, walkErr))
			return nil
		}
		if entry.IsDir() {
			if full != workspace && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(full))
		if ext == ".md" || ext == ".markdown" {
			paths = append(paths, full)
		}
		return nil
	})
	if err != nil {
		return nil, warnings, err
	}
	files := make([]workbenchFile, len(paths))
	readErrors := make([]error, len(paths))
	forEachFileBoundedIndexed(paths, func(i int, full string) {
		data, readErr := os.ReadFile(full)
		if readErr != nil {
			readErrors[i] = readErr
			return
		}
		stat, statErr := os.Stat(full)
		if statErr != nil {
			readErrors[i] = statErr
			return
		}
		relative, _ := filepath.Rel(workspace, full)
		files[i] = newWorkbenchFile(filepath.ToSlash(relative), string(data), stat.ModTime().Format(time.RFC3339Nano))
	})
	readable := make([]workbenchFile, 0, len(files))
	for i, file := range files {
		if readErrors[i] != nil {
			warnings = append(warnings, fmt.Sprintf("无法读取 %s：%v", paths[i], readErrors[i]))
			continue
		}
		readable = append(readable, file)
	}
	return readable, warnings, nil
}

func parseWorkbenchTask(relative string, index int, line string) *TodoItem {
	match := workbenchTaskRE.FindStringSubmatch(line)
	if match == nil {
		return nil
	}
	item := &TodoItem{ID: relative + ":" + strconv.Itoa(index), FilePath: relative, FileName: path.Base(relative), LineIndex: index,
		SourceLine: line, Completed: match[2] != " ", Priority: "medium", Type: "todo", Status: "todo", Progress: []WorkbenchProgress{}}
	body := strings.TrimSpace(match[3])
	if strings.HasPrefix(body, "!") {
		item.Priority = "high"
		body = strings.TrimSpace(strings.TrimLeft(body, "!"))
	}
	if kind := workbenchKindRE.FindStringSubmatch(body); kind != nil {
		item.Type = kind[1]
		if strings.EqualFold(item.Type, "Extra") {
			item.Type = "额外"
		}
		body = body[len(kind[0]):]
	}
	if strings.HasPrefix(body, "!") {
		item.Priority = "high"
		body = strings.TrimSpace(strings.TrimLeft(body, "!"))
	}
	parts := strings.Split(relative, "/")
	if len(parts) >= 3 && strings.EqualFold(parts[0], "Projects") {
		item.Project = parts[1]
		item.ProjectPath = strings.Join(parts[:2], "/") + "/project.md"
	}
	item.Date = workbenchDailyDate(relative)
	body = workbenchMetadataRE.ReplaceAllStringFunc(body, func(metadata string) string {
		m := workbenchMetadataRE.FindStringSubmatch(metadata)
		key, value := m[1], m[2]
		switch key {
		case "due", "done", "date", "scheduled":
			if _, err := workbenchDate(value); err != nil {
				return metadata
			}
			switch key {
			case "due":
				item.Due = value
			case "done":
				item.CompletedAt = value
			default:
				item.Date = value
			}
		case "priority":
			if value != "high" && value != "medium" && value != "low" {
				return metadata
			}
			item.Priority = value
		case "project":
			item.Project, item.ProjectPath = value, ""
		case "status":
			item.Status = value
		}
		return ""
	})
	for _, blocker := range workbenchBlockers(body) {
		item.Blocker = blocker.value
	}
	body = removeWorkbenchBlockers(body)
	body = workbenchStatusRE.ReplaceAllStringFunc(body, func(status string) string {
		for _, value := range workbenchStatusRE.FindStringSubmatch(status)[1:] {
			if value != "" {
				item.Status = value
			}
		}
		return ""
	})
	item.Title = strings.TrimSpace(body)
	item.Content = item.Title
	if item.Title == "" {
		return nil
	}
	if item.Blocker != "" {
		item.Status = "blocked"
	}
	if item.Completed {
		item.Status = "done"
		if item.CompletedAt == "" {
			item.CompletedAt = workbenchDailyDate(relative)
		}
	} else {
		item.CompletedAt = ""
	}
	return item
}

func parseWorkbenchTasks(file workbenchFile) []*TodoItem {
	tasks := make([]*TodoItem, 0)
	if strings.EqualFold(strings.Split(file.path, "/")[0], "Templates") {
		return tasks
	}
	type taskContext struct {
		task   *TodoItem
		indent int
	}
	stack := make([]taskContext, 0)
	for i, line := range file.lines {
		if !file.visible[i] {
			continue
		}
		if strings.TrimSpace(line.text) == "" {
			continue
		}
		indent := len(line.text) - len(strings.TrimLeft(line.text, " \t"))
		for len(stack) > 0 && indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		if task := parseWorkbenchTask(file.path, i, line.text); task != nil {
			tasks = append(tasks, task)
			stack = append(stack, taskContext{task: task, indent: indent})
			continue
		}
		if len(stack) == 0 {
			continue
		}
		if match := workbenchProgressRE.FindStringSubmatch(line.text); match != nil {
			var progress WorkbenchProgress
			if err := json.Unmarshal([]byte(match[1]), &progress); err != nil || progress.Note == "" {
				continue
			}
			if _, err := time.Parse(time.RFC3339Nano, progress.At); err != nil {
				continue
			}
			task := stack[len(stack)-1].task
			progress.TaskID, progress.FilePath, progress.TaskTitle, progress.Project = task.ID, task.FilePath, task.Title, task.Project
			if progress.ID == "" {
				progress.ID = file.path + ":progress:" + strconv.Itoa(i)
			}
			task.Progress = append(task.Progress, progress)
		}
	}
	for _, task := range tasks {
		sort.SliceStable(task.Progress, func(i, j int) bool {
			return workbenchProgressBefore(task.Progress[i], task.Progress[j])
		})
	}
	return tasks
}

func workbenchProgressBefore(left, right WorkbenchProgress) bool {
	a, _ := time.Parse(time.RFC3339Nano, left.At)
	b, _ := time.Parse(time.RFC3339Nano, right.At)
	return a.Before(b)
}

type workbenchBlockerSpan struct {
	start, end int
	value      string
}

func workbenchBlockers(line string) []workbenchBlockerSpan {
	spans := []workbenchBlockerSpan{}
	for _, start := range workbenchBlockerRE.FindAllStringIndex(line, -1) {
		if len(spans) > 0 && start[0] < spans[len(spans)-1].end {
			continue
		}
		depth, escaped := 1, false
		for offset, char := range line[start[1]:] {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == '(' || char == '（' {
				depth++
			} else if char == ')' || char == '）' {
				depth--
			}
			if depth == 0 {
				value := line[start[1] : start[1]+offset]
				value = strings.NewReplacer(`\\`, `\`, `\(`, `(`, `\)`, `)`, `\（`, `（`, `\）`, `）`).Replace(value)
				spans = append(spans, workbenchBlockerSpan{start: start[0], end: start[1] + offset + len(string(char)), value: strings.TrimSpace(html.UnescapeString(value))})
				break
			}
		}
	}
	return spans
}

func removeWorkbenchBlockers(line string) string {
	spans := workbenchBlockers(line)
	for i := len(spans) - 1; i >= 0; i-- {
		start := spans[i].start
		if start > 0 && line[start-1] == ' ' {
			start--
		}
		line = line[:start] + line[spans[i].end:]
	}
	return line
}

func escapeWorkbenchBlocker(value string) string {
	return strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`, `（`, `\（`, `）`, `\）`).Replace(html.EscapeString(value))
}
