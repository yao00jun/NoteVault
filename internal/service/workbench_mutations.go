package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Resolve every existing path component before writing; Markdown operations must
// not follow a workspace link into another directory or a Windows alternate stream.
func workbenchPath(workspace, relative string) (string, string, error) {
	if strings.TrimSpace(workspace) == "" {
		return "", "", fmt.Errorf("请先打开工作区")
	}
	relative = strings.ReplaceAll(relative, "\\", "/")
	if relative == "" || strings.HasPrefix(relative, "/") || strings.ContainsAny(relative, ":\x00") {
		return "", "", fmt.Errorf("非法工作区相对路径：%s", relative)
	}
	for _, component := range strings.Split(relative, "/") {
		if component == ".." {
			return "", "", fmt.Errorf("路径越出工作区：%s", relative)
		}
	}
	relative = path.Clean(relative)
	if relative == "." {
		return "", "", fmt.Errorf("文件路径不能为空")
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return "", "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("工作区目录不可用：%s", workspace)
	}
	current := root
	components := strings.Split(relative, "/")
	for i, component := range components {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", "", fmt.Errorf("工作台文件路径不能经过符号链接：%s", relative)
		}
		if i < len(components)-1 && !info.IsDir() {
			return "", "", fmt.Errorf("父路径不是目录：%s", relative)
		}
	}
	return current, relative, nil
}

type workbenchChange struct {
	full          string
	relative      string
	before        string
	after         string
	existed       bool
	mode          os.FileMode
	requireParent bool
}

func readWorkbenchChange(workspace, relative string) (workbenchChange, error) {
	full, relative, err := workbenchPath(workspace, relative)
	if err != nil {
		return workbenchChange{}, err
	}
	change := workbenchChange{full: full, relative: relative, mode: 0644}
	info, err := os.Lstat(full)
	if os.IsNotExist(err) {
		return change, nil
	}
	if err != nil {
		return workbenchChange{}, err
	}
	if !info.Mode().IsRegular() {
		return workbenchChange{}, fmt.Errorf("目标不是普通 Markdown 文件：%s", relative)
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return workbenchChange{}, err
	}
	change.before, change.after, change.existed, change.mode = string(content), string(content), true, info.Mode().Perm()
	return change, nil
}

func verifyWorkbenchChange(change workbenchChange) error {
	current, err := os.ReadFile(change.full)
	if !change.existed && os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !change.existed || string(current) != change.before {
		return fmt.Errorf("文件已在其他位置修改，请刷新后重试：%s", change.relative)
	}
	return nil
}

// Verify all Markdown changes before committing. A failed later write rolls back
// earlier writes only while they still contain our exact new text.
func commitWorkbenchChanges(changes ...workbenchChange) error {
	for _, change := range changes {
		if err := verifyWorkbenchChange(change); err != nil {
			return err
		}
	}
	written := make([]workbenchChange, 0, len(changes))
	rollback := func(cause error) error {
		for i := len(written) - 1; i >= 0; i-- {
			change := written[i]
			current, err := os.ReadFile(change.full)
			if err != nil || string(current) != change.after {
				cause = errors.Join(cause, fmt.Errorf("文件已再次变化，未覆盖它的最新内容：%s", change.relative))
				continue
			}
			if change.existed {
				err = atomicWrite(change.full, []byte(change.before), change.mode)
			} else {
				err = os.Remove(change.full)
			}
			if err != nil {
				cause = errors.Join(cause, err)
			}
		}
		return cause
	}
	for _, change := range changes {
		if change.requireParent {
			info, err := os.Stat(filepath.Dir(change.full))
			if err != nil || !info.IsDir() {
				return rollback(fmt.Errorf("项目目录已不存在，请刷新后重试：%s", path.Dir(change.relative)))
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(change.full), 0750); err != nil {
				return rollback(err)
			}
		}
		if err := verifyWorkbenchChange(change); err != nil {
			return rollback(err)
		}
		if err := atomicWrite(change.full, []byte(change.after), change.mode); err != nil {
			return rollback(err)
		}
		written = append(written, change)
	}
	return nil
}

func workbenchNewline(content string) string {
	for _, line := range splitWorkbenchLines(content) {
		if line.ending != "" {
			return line.ending
		}
	}
	return "\n"
}

func appendWorkbenchMarkdown(content, extra, newline string) string {
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += newline
	}
	return content + extra + newline
}

func workbenchSingleLine(value string, allowEmpty bool) (string, error) {
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("请使用单行文字")
	}
	value = strings.TrimSpace(value)
	if !allowEmpty && value == "" {
		return "", fmt.Errorf("内容不能为空")
	}
	return value, nil
}

func workbenchAppendMetadata(line, metadata string) string {
	trimmed := strings.TrimRight(line, " \t")
	return trimmed + " " + metadata + line[len(trimmed):]
}

func workbenchRemoveTag(line, key, value string) string {
	return workbenchMetadataRE.ReplaceAllStringFunc(line, func(metadata string) string {
		match := workbenchMetadataRE.FindStringSubmatch(metadata)
		if match[1] == key && (value == "" || match[2] == value) {
			return ""
		}
		return metadata
	})
}

// UpdateWorkbenchTask compares the current source line before touching the note.
func (s *TodoService) UpdateWorkbenchTask(workspacePath, filePath string, lineIndex int, expectedLine, action, value, date string) error {
	day, err := workbenchDate(date)
	if err != nil {
		return err
	}
	if action != "toggle" && action != "progress" && action != "blocker" {
		return fmt.Errorf("未知任务操作：%s", action)
	}
	if action != "toggle" {
		value, err = workbenchSingleLine(value, action == "blocker")
		if err != nil {
			return err
		}
	}
	s.workbenchMu.Lock()
	defer s.workbenchMu.Unlock()
	return s.updateWorkbenchTask(workspacePath, filePath, lineIndex, expectedLine, action, value, day)
}

func (s *TodoService) updateWorkbenchTask(workspacePath, filePath string, lineIndex int, expectedLine, action, value string, day time.Time) error {
	change, err := readWorkbenchChange(workspacePath, filePath)
	if err != nil {
		return err
	}
	if !change.existed || (strings.ToLower(path.Ext(change.relative)) != ".md" && strings.ToLower(path.Ext(change.relative)) != ".markdown") {
		return fmt.Errorf("找不到任务的 Markdown 文件")
	}
	file := newWorkbenchFile(change.relative, change.before, "")
	if lineIndex < 0 || lineIndex >= len(file.lines) || file.lines[lineIndex].text != expectedLine {
		return fmt.Errorf("任务已在其他位置修改，请刷新后重试")
	}
	var task *TodoItem
	for _, candidate := range parseWorkbenchTasks(file) {
		if candidate.LineIndex == lineIndex {
			task = candidate
			break
		}
	}
	if task == nil {
		return fmt.Errorf("目标行已不是待办任务，请刷新后重试")
	}
	date := day.Format("2006-01-02")
	switch action {
	case "toggle":
		line := file.lines[lineIndex].text
		checkbox := strings.Index(line, "[")
		mark := "x"
		if task.Completed {
			mark = " "
		}
		line = line[:checkbox+1] + mark + line[checkbox+2:]
		line = workbenchRemoveTag(line, "done", "")
		if !task.Completed {
			line = workbenchAppendMetadata(line, "#done/"+date)
		}
		file.lines[lineIndex].text = line
	case "blocker":
		line := removeWorkbenchBlockers(file.lines[lineIndex].text)
		line = workbenchRemoveTag(line, "status", "blocked")
		line = workbenchStatusRE.ReplaceAllStringFunc(line, func(status string) string {
			for _, field := range workbenchStatusRE.FindStringSubmatch(status)[1:] {
				if field == "blocked" {
					return ""
				}
			}
			return status
		})
		if value != "" {
			line = workbenchAppendMetadata(line, "(卡点: "+escapeWorkbenchBlocker(value)+")")
		}
		file.lines[lineIndex].text = line
	case "progress":
		id := make([]byte, 12)
		if _, err := rand.Read(id); err != nil {
			return err
		}
		now := time.Now()
		at := time.Date(day.Year(), day.Month(), day.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), time.Local)
		progress := WorkbenchProgress{ID: hex.EncodeToString(id), TaskID: task.ID, FilePath: task.FilePath, TaskTitle: task.Title, Project: task.Project, Note: value, At: at.Format(time.RFC3339Nano)}
		metadata, err := json.Marshal(progress)
		if err != nil {
			return err
		}
		indent := task.SourceLine[:len(task.SourceLine)-len(strings.TrimLeft(task.SourceLine, " \t"))] + "  "
		text := indent + "- " + at.Format("2006-01-02 15:04") + " · " + html.EscapeString(value) + " <!-- progress: " + string(metadata) + " -->"
		ending := file.lines[lineIndex].ending
		if ending == "" {
			file.lines[lineIndex].ending = workbenchNewline(change.before)
		}
		insert := workbenchLine{text: text, ending: ending}
		file.lines = append(file.lines[:lineIndex+1], append([]workbenchLine{insert}, file.lines[lineIndex+1:]...)...)
	}
	change.after = joinWorkbenchLines(file.lines)
	return commitWorkbenchChanges(change)
}

func (s *TodoService) ReadDailyReport(workspacePath, date string) (string, error) {
	if _, err := workbenchDate(date); err != nil {
		return "", err
	}
	s.workbenchMu.RLock()
	defer s.workbenchMu.RUnlock()
	change, err := readWorkbenchChange(workspacePath, "Daily/Reports/"+date+"-日报.md")
	if err != nil {
		return "", err
	}
	return change.before, nil
}

func (s *TodoService) SaveDailyReport(workspacePath, date, content, expectedContent string) (string, error) {
	if _, err := workbenchDate(date); err != nil {
		return "", err
	}
	s.workbenchMu.Lock()
	defer s.workbenchMu.Unlock()
	relative := "Daily/Reports/" + date + "-日报.md"
	change, err := readWorkbenchChange(workspacePath, relative)
	if err != nil {
		return "", err
	}
	if change.before != expectedContent {
		return "", fmt.Errorf("日报已在其他位置修改，请重新载入并合并后再保存")
	}
	change.after = content
	if err := commitWorkbenchChanges(change); err != nil {
		return "", err
	}
	return relative, nil
}

func (s *TodoService) AddWorkbenchTask(workspacePath, projectFolder, title, kind, due, date string) error {
	if _, err := workbenchDate(date); err != nil {
		return err
	}
	if due != "" {
		if _, err := workbenchDate(due); err != nil {
			return err
		}
	}
	title, err := workbenchSingleLine(title, false)
	if err != nil {
		return err
	}
	if strings.EqualFold(kind, "Extra") {
		kind = "额外"
	}
	if kind != "" && kind != "todo" && kind != "US" && kind != "DTS" && kind != "额外" {
		return fmt.Errorf("未知任务类型：%s", kind)
	}
	const generalProjectFolder = "Projects/通用事务"
	projectFolder = strings.ReplaceAll(projectFolder, "\\", "/")
	if projectFolder == "" {
		projectFolder = generalProjectFolder
	}
	parts := strings.Split(projectFolder, "/")
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Projects") || parts[1] == "" || parts[1] == "." || parts[1] == ".." {
		return fmt.Errorf("任务必须保存到 Projects 下的项目目录")
	}
	generalProject := strings.EqualFold(projectFolder, generalProjectFolder)
	if generalProject {
		projectFolder = generalProjectFolder
	}
	relative := projectFolder + "/Tasks.md"
	s.workbenchMu.Lock()
	defer s.workbenchMu.Unlock()
	change, err := readWorkbenchChange(workspacePath, relative)
	if err != nil {
		return err
	}
	change.requireParent = !generalProject
	line := "- [ ] "
	if kind != "" && kind != "todo" {
		line += "[" + kind + "] "
	}
	line += title + " #date/" + date
	if due != "" {
		line += " #due/" + due
	}
	newline := workbenchNewline(change.before)
	content := change.before
	if content == "" {
		content = "# 任务清单" + newline + newline
	}
	change.after = appendWorkbenchMarkdown(content, line, newline)
	file := newWorkbenchFile(change.relative, change.after, "")
	last := len(file.lines) - 1
	if last < 0 || !file.visible[last] || parseWorkbenchTask(change.relative, last, file.lines[last].text) == nil {
		return fmt.Errorf("任务无法显示，请检查标题以及文件末尾是否有未闭合的代码块或注释")
	}
	if generalProject {
		project, err := readWorkbenchChange(workspacePath, projectFolder+"/project.md")
		if err != nil {
			return err
		}
		if !project.existed {
			project.after = "---\ntitle: 通用事务\nstatus: 进行中\n---\n\n# 通用事务\n"
			return commitWorkbenchChanges(project, change)
		}
	}
	return commitWorkbenchChanges(change)
}
