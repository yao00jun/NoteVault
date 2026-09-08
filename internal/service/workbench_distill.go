package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// DistillRequest turns a saved project note into portable learning Markdown.
type DistillRequest struct {
	SourceFile   string `json:"sourceFile"`
	TargetMode   string `json:"targetMode"`
	TargetFolder string `json:"targetFolder"`
	TargetTitle  string `json:"targetTitle"`
	Question     string `json:"question"`
	Answer       string `json:"answer"`
	Summary      string `json:"summary"`
}

// DistillKnowledge appends linked learning assets without replacing project
// notes, existing chapters, or the later review state of a retried interview card.
func (s *TodoService) DistillKnowledge(workspacePath string, req DistillRequest) error {
	s.workbenchMu.Lock()
	defer s.workbenchMu.Unlock()
	changes, err := prepareWorkbenchDistill(workspacePath, req, time.Now())
	if err != nil {
		return err
	}
	return commitWorkbenchDistill(changes)
}

// Preparation reads every participant before writing any of them. The target is
// committed before its source badge, so a surviving partial write can be retried.
func prepareWorkbenchDistill(workspacePath string, req DistillRequest, now time.Time) ([]workbenchChange, error) {
	req, err := normalizeWorkbenchDistill(req)
	if err != nil {
		return nil, err
	}
	source, err := readWorkbenchDistillChange(workspacePath, req.SourceFile)
	if err != nil {
		return nil, err
	}
	if !source.existed {
		return nil, fmt.Errorf("找不到来源项目 Markdown 文件：%s", req.SourceFile)
	}
	source.requireParent = true
	target, err := readWorkbenchDistillChange(workspacePath, req.TargetFolder+"/"+req.TargetTitle+".md")
	if err != nil {
		return nil, err
	}
	parts := strings.Split(source.relative, "/")
	project, projectFolder := parts[1], strings.Join(parts[:2], "/")
	id := workbenchDistillID(req)
	marker := "<!-- notevault-distill: v1:" + id + " -->"
	completed := hasWorkbenchDistillMarker(target.before, marker, false)
	newline := workbenchNewline(target.before)
	if !target.existed {
		newline = workbenchNewline(source.before)
	}
	if req.TargetMode == "book" {
		if target.existed && !completed {
			return nil, fmt.Errorf("目标章节已存在，请更换笔记标题：%s", target.relative)
		}
		if !completed {
			file := newWorkbenchFile(source.relative, source.before, "")
			tags, _ := json.Marshal(workbenchList(file.props, "tags", "标签"))
			target.after = strings.Join([]string{
				"---",
				"type: tech-note",
				"title: " + strconv.Quote(req.TargetTitle),
				"sourceProject: " + strconv.Quote("[["+projectFolder+"/project.md|"+project+"]]"),
				"distilledAt: " + now.Format("2006-01-02"),
				"tags: " + string(tags),
				"---",
				marker,
				"> 📌 **实战案例来源**：[[" + source.relative + "|" + project + "项目实战]]",
				"",
				"# " + req.TargetTitle,
				"",
				req.Summary,
				"",
			}, "\n")
			target.after = strings.ReplaceAll(target.after, "\n", newline)
		}
	} else if !completed {
		question := "[Q" + id[:16] + "] " + req.Question
		file := newWorkbenchFile(target.relative, target.before, "")
		for i, line := range file.lines {
			if heading := workbenchHeadingRE.FindStringSubmatch(line.text); file.visible[i] && heading != nil && strings.HasPrefix(heading[2], "[Q"+id[:16]+"]") {
				return nil, fmt.Errorf("目标题库中已有相同题卡编号，请检查后重试：%s", target.relative)
			}
		}
		state, _ := json.Marshal(struct {
			Level    string `json:"level"`
			Interval int    `json:"interval"`
			Due      string `json:"due"`
			Reps     int    `json:"reps"`
			Source   string `json:"source"`
		}{"掌握", 7, now.AddDate(0, 0, 7).Format("2006-01-02"), 1, projectFolder})
		backlink := "> 📌 **实战案例来源**：[[" + source.relative + "|" + project + "案例]]"
		block := strings.Join([]string{marker, "### " + question, "<!-- srs: " + string(state) + " -->", "", "#### 核心要点：", req.Answer, "", backlink}, "\n")
		content := target.before
		if !target.existed {
			content = "# " + req.TargetTitle + newline
		}
		target.after = appendWorkbenchMarkdown(content, newline+strings.ReplaceAll(block, "\n", newline), newline)
		updated := newWorkbenchFile(target.relative, target.after, "")
		cards, _ := parseWorkbenchCards(updated)
		newSRSCount := 0
		for i, line := range updated.lines {
			if i >= len(file.lines) && updated.visible[i] {
				newSRSCount += len(workbenchSRSRE.FindAllString(line.text, -1))
			}
		}
		found := false
		for _, card := range cards {
			if card.Question == question && strings.Contains(card.Answer, backlink) {
				found = true
			}
		}
		last := len(updated.lines) - 1
		if !found || newSRSCount != 1 || last < 0 || !updated.visible[last] || !hasWorkbenchDistillMarker(target.after, marker, false) {
			return nil, fmt.Errorf("题卡无法完整显示，请检查题库和解答中的标题、代码块或 HTML 注释")
		}
	}
	if !hasWorkbenchDistillMarker(source.before, marker, true) {
		badge := "> 💡 **技术资产沉淀**：已沉淀至 [[" + target.relative + "|" + path.Base(req.TargetFolder) + " - " + req.TargetTitle + "]] " + marker
		newline := workbenchNewline(source.before)
		source.after = appendWorkbenchMarkdown(source.before, newline+badge, newline)
		if !hasWorkbenchDistillMarker(source.after, marker, true) {
			return nil, fmt.Errorf("来源文档末尾有未闭合的代码块或 HTML 注释，无法添加知识链接")
		}
	}
	changes := []workbenchChange{}
	if req.TargetMode == "book" {
		book, err := readWorkbenchDistillChange(workspacePath, req.TargetFolder+"/book.md")
		if err != nil {
			return nil, err
		}
		if !book.existed {
			book.after = strings.Join([]string{
				"---", "type: book", "title: " + strconv.Quote(path.Base(req.TargetFolder)), "status: 在学",
				"projects: [" + strconv.Quote(project) + "]", "---", "", "# " + path.Base(req.TargetFolder), "",
			}, newline)
		}
		changes = append(changes, book)
	}
	return append(changes, target, source), nil
}

func commitWorkbenchDistill(changes []workbenchChange) error {
	changed := make([]workbenchChange, 0, len(changes))
	for _, change := range changes {
		if err := verifyWorkbenchChange(change); err != nil {
			return err
		}
		if !change.existed || change.after != change.before {
			changed = append(changed, change)
		}
	}
	if err := commitWorkbenchChanges(changed...); err != nil {
		return fmt.Errorf("知识沉淀未全部保存，请刷新来源与目标文件确认后重试：%w", err)
	}
	return nil
}

func readWorkbenchDistillChange(workspacePath, relative string) (workbenchChange, error) {
	if _, err := confineToWorkspace(workspacePath, filepath.FromSlash(relative)); err != nil {
		return workbenchChange{}, err
	}
	// readWorkbenchChange also calls workbenchPath to reject symlinks and ADS.
	return readWorkbenchChange(workspacePath, relative)
}

func normalizeWorkbenchDistill(req DistillRequest) (DistillRequest, error) {
	req.SourceFile = strings.ReplaceAll(req.SourceFile, "\\", "/")
	req.TargetFolder = strings.ReplaceAll(req.TargetFolder, "\\", "/")
	for _, relative := range []string{req.SourceFile, req.TargetFolder} {
		for _, component := range strings.Split(relative, "/") {
			if !workbenchDistillFilename(component) {
				return req, fmt.Errorf("路径含有不安全的文件名：%s", relative)
			}
		}
	}
	sourceParts := strings.Split(req.SourceFile, "/")
	ext := strings.ToLower(path.Ext(req.SourceFile))
	if len(sourceParts) < 3 || !strings.EqualFold(sourceParts[0], "Projects") || (ext != ".md" && ext != ".markdown") {
		return req, fmt.Errorf("知识来源必须是 Projects/<项目>/ 下的 Markdown 文件")
	}
	targetParts := strings.Split(req.TargetFolder, "/")
	if len(targetParts) != 2 || !strings.EqualFold(targetParts[0], "Learning") {
		return req, fmt.Errorf("目标分册必须位于 Learning/<分册> 目录")
	}
	req.TargetTitle = strings.TrimSpace(req.TargetTitle)
	if ext := strings.ToLower(path.Ext(req.TargetTitle)); ext == ".md" || ext == ".markdown" {
		req.TargetTitle = req.TargetTitle[:len(req.TargetTitle)-len(ext)]
	}
	if !workbenchDistillFilename(req.TargetTitle) {
		return req, fmt.Errorf("请使用有效的笔记标题，不能包含路径、链接符号或保留文件名")
	}
	if strings.EqualFold(req.TargetTitle, "book") || (req.TargetMode == "book" && (strings.EqualFold(req.TargetTitle, "sources") || strings.EqualFold(req.TargetTitle, "ai-plan"))) {
		return req, fmt.Errorf("该标题属于分册元数据文件，请更换笔记标题")
	}
	req.Summary = strings.Trim(strings.ReplaceAll(req.Summary, "\r\n", "\n"), "\r\n")
	req.Answer = strings.Trim(strings.ReplaceAll(req.Answer, "\r\n", "\n"), "\r\n")
	if strings.ContainsRune(req.Summary+req.Answer, '\x00') || !utf8.ValidString(req.Summary+req.Answer) {
		return req, fmt.Errorf("沉淀正文必须是有效的文本")
	}
	switch req.TargetMode {
	case "book":
		if strings.TrimSpace(req.Summary) == "" {
			return req, fmt.Errorf("请填写沉淀正文")
		}
		req.Question, req.Answer = "", ""
	case "interview":
		if targetParts[1] != "面试宝典" {
			return req, fmt.Errorf("面试题卡必须保存到 Learning/面试宝典")
		}
		question, err := workbenchSingleLine(req.Question, false)
		invalidControl := strings.IndexFunc(question, func(char rune) bool {
			return unicode.IsControl(char) || char == '\u2028' || char == '\u2029'
		}) >= 0
		if err != nil || invalidControl || strings.Contains(question, "<!--") || !utf8.ValidString(question) {
			return req, fmt.Errorf("请填写不含 HTML 注释的单行面试题面")
		}
		if strings.TrimSpace(req.Answer) == "" {
			return req, fmt.Errorf("请填写核心解答提要")
		}
		req.Question, req.Summary = question, ""
	default:
		return req, fmt.Errorf("未知知识沉淀模式：%s", req.TargetMode)
	}
	return req, nil
}

func workbenchDistillFilename(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || strings.ContainsAny(name, `<>:"/\|?*#[]`) {
		return false
	}
	for _, char := range name {
		if unicode.IsControl(char) || char == '\u2028' || char == '\u2029' {
			return false
		}
	}
	stem := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || stem == "CONIN$" || stem == "CONOUT$" {
		return false
	}
	if strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT") {
		n := stem[3:]
		if len([]rune(n)) == 1 && strings.ContainsAny(n, "123456789¹²³") {
			return false
		}
	}
	return true
}

func workbenchDistillID(req DistillRequest) string {
	data, _ := json.Marshal(req)
	digest := sha256.Sum256(append([]byte("notevault-distill:v1\n"), data...))
	return hex.EncodeToString(digest[:])
}

// Only exact comments in live Markdown count. A copied code sample or an inline
// mention of an ID must never authorise replacing an existing chapter.
func hasWorkbenchDistillMarker(content, marker string, badge bool) bool {
	file := newWorkbenchFile("", content, "")
	for i, line := range file.lines {
		if !file.visible[i] || strings.HasPrefix(line.text, "    ") || strings.HasPrefix(line.text, "\t") {
			continue
		}
		text := strings.TrimSpace(line.text)
		if (!badge && text == marker) || (badge && strings.HasPrefix(text, "> 💡 **技术资产沉淀**：") && strings.HasSuffix(text, " "+marker)) {
			return true
		}
	}
	return false
}
