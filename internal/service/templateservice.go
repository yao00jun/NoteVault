package service

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ============================================================================
// TemplateService —— P2-2「模板系统」。
//
// 简化版 Templater：工作区根下的 Templates/ 目录里每个 .md 文件都是一个模板，
// 内容中的 {{占位符}} 在创建时被替换：
//   - 内置变量：{{title}}（目标文件名去扩展名）、{{date}}、{{time}}、{{datetime}}；
//   - 其余 {{word}} 视为自定义变量，由前端在创建对话框里向用户收集。
//
// 设计边界（S 规模，刻意不做）：
//   - 不做模板语法（条件/循环/脚本执行）——那是 Templater 插件的领域；
//   - 不做模板管理 UI（新建/编辑模板）——模板就是普通 md 文件，
//     用户直接在编辑器里维护，与「纯 Markdown、无锁定」承诺一致。
// ============================================================================

// templatesDirName 是模板目录名（Obsidian 同名约定，方便迁移）。
const templatesDirName = "Templates"

// 内置模板：随应用打包（internal/service/templates/*.md），
// 任何工作区开箱即得；工作区 Templates/ 下的同名模板覆盖内置版本。
//
//go:embed templates/*.md
var bundledTemplateFS embed.FS

// variablePattern 匹配 {{word}} 占位符；word 限字母开头、允许数字/下划线/连字符，
// 避免误吞 {{ }} 或 JSON 片段。
var variablePattern = regexp.MustCompile(`\{\{([a-zA-Z][a-zA-Z0-9_-]*)\}\}`)

// builtinVariables 是内置变量集合；出现在模板里时不向用户询问。
var builtinVariables = map[string]bool{
	"title":    true,
	"date":     true,
	"time":     true,
	"datetime": true,
}

// TemplateInfo 描述一个可用模板。
type TemplateInfo struct {
	Name        string   `json:"name"`                  // 模板名（文件名去扩展名），即模板 ID
	Variables   []string `json:"variables"`             // 需要用户填写的自定义变量（已排除内置）
	Builtin     bool     `json:"builtin"`               // 是否为未修改的应用内置模板
	Category    string   `json:"category,omitempty"`    // 用途分组；自定义模板可以省略
	Description string   `json:"description,omitempty"` // 使用说明
	Destination string   `json:"destination,omitempty"` // 建议的完整相对路径，可包含占位符
}

// bundledTemplates 读取全部内置模板（名字 + 变量清单），按名称排序。
func bundledTemplates() []*TemplateInfo {
	entries, err := fs.ReadDir(bundledTemplateFS, "templates")
	if err != nil {
		return nil
	}
	out := make([]*TemplateInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		if content, err := fs.ReadFile(bundledTemplateFS, "templates/"+entry.Name()); err == nil {
			out = append(out, describeTemplate(name, string(content), true))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// bundledTemplateContent 返回内置模板原文；不存在返回 ok=false。
func bundledTemplateContent(name string) (string, bool) {
	content, err := bundledTemplateFS.ReadFile("templates/" + name + ".md")
	if err != nil {
		return "", false
	}
	return string(content), true
}

// TemplateService 提供模板列表与从模板创建笔记。
type TemplateService struct {
	files *FileService
}

func NewTemplateService(files *FileService) *TemplateService {
	return &TemplateService{files: files}
}

// ListTemplates 列出可用模板 = 内置模板 + 工作区 Templates/ 下的模板，
// 工作区同名模板覆盖内置版本（用户自定义优先）。按名称排序。
func (s *TemplateService) ListTemplates(workspacePath string) ([]*TemplateInfo, error) {
	merged := map[string]*TemplateInfo{}
	for _, tpl := range bundledTemplates() {
		merged[tpl.Name] = tpl
	}

	root, err := templateWorkspaceRoot(workspacePath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if err := sourceCheckPath(root, templatesDirName); err != nil {
		return nil, fmt.Errorf("读取模板目录失败: %w", err)
	}
	dir, err := root.Open(templatesDirName)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取模板目录失败: %w", err)
	}
	if err == nil {
		defer dir.Close()
		entries, err := dir.ReadDir(-1)
		if err != nil {
			return nil, fmt.Errorf("读取模板目录失败: %w", err)
		}
		for _, entry := range entries {
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), ".md")
			if err := validateTemplateName(name); err != nil {
				continue
			}
			content, _, err := sourceReadRoot(root, templatesDirName+"/"+entry.Name(), sourceMaxFileBytes)
			if err != nil {
				return nil, fmt.Errorf("读取模板 %s 失败: %w", name, err)
			}
			bundled, exists := bundledTemplateContent(name)
			builtin := exists && normalizeScaffoldDefault(string(content)) == normalizeScaffoldDefault(bundled)
			merged[name] = describeTemplate(name, string(content), builtin)
		}
	}
	templates := make([]*TemplateInfo, 0, len(merged))
	for _, tpl := range merged {
		templates = append(templates, tpl)
	}
	sort.Slice(templates, func(i, j int) bool {
		return templates[i].Name < templates[j].Name
	})
	return templates, nil
}

// extractVariables 找出模板内容里的自定义变量（内置变量不算）。
func extractVariables(content string) []string {
	seen := map[string]bool{}
	vars := []string{}
	for _, match := range variablePattern.FindAllStringSubmatch(content, -1) {
		name := match[1]
		if builtinVariables[name] || seen[name] {
			continue
		}
		seen[name] = true
		vars = append(vars, name)
	}
	sort.Strings(vars)
	return vars
}

// GetTemplateContent 返回模板原文（前端预览用）。
func (s *TemplateService) GetTemplateContent(workspacePath string, name string) (string, error) {
	if err := validateTemplateName(name); err != nil {
		return "", err
	}
	root, err := templateWorkspaceRoot(workspacePath)
	if err != nil {
		return "", err
	}
	defer root.Close()
	content, exists, err := sourceReadRoot(root, templatesDirName+"/"+name+".md", sourceMaxFileBytes)
	if err != nil {
		return "", fmt.Errorf("读取模板失败: %w", err)
	}
	if exists {
		return string(content), nil
	}
	// 只有工作区没有同名模板时回退；不可读或不安全的自定义版本不能被悄悄忽略。
	if bundled, ok := bundledTemplateContent(name); ok {
		return bundled, nil
	}
	return "", fmt.Errorf("读取模板失败: 模板 %s 不存在", name)
}

// CreateFromTemplate 用模板创建笔记：渲染占位符 → 经 FileService 落盘。
// targetRelativePath 是相对工作区的目标路径（含 .md），{{title}} 取其去扩展名的文件名。
// 返回创建好的文件节点；目标已存在时由 FileService 报错。
func (s *TemplateService) CreateFromTemplate(
	workspacePath string,
	templateName string,
	targetRelativePath string,
	variables map[string]string,
) (*FileNode, error) {
	raw, err := s.GetTemplateContent(workspacePath, templateName)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(targetRelativePath) == "" {
		return nil, fmt.Errorf("目标路径不能为空")
	}
	targetRelativePath, err = sourceRelativePath(targetRelativePath)
	if err != nil {
		return nil, fmt.Errorf("目标路径不合法: %w", err)
	}
	if !strings.EqualFold(path.Ext(targetRelativePath), ".md") {
		return nil, fmt.Errorf("模板目标必须是 .md 文件")
	}
	if _, _, err := workbenchPath(workspacePath, targetRelativePath); err != nil {
		return nil, err
	}
	title := strings.TrimSuffix(path.Base(targetRelativePath), path.Ext(targetRelativePath))

	now := time.Now()
	values := map[string]string{
		"title":    title,
		"date":     now.Format("2006-01-02"),
		"time":     now.Format("15:04"),
		"datetime": now.Format("2006-01-02 15:04"),
	}
	for k, v := range variables {
		values[k] = v
	}

	content := variablePattern.ReplaceAllStringFunc(templateNoteContent(raw), func(placeholder string) string {
		name := variablePattern.FindStringSubmatch(placeholder)[1]
		if v, ok := values[name]; ok {
			return v
		}
		// 变量没提供值时保留原占位符，用户落盘后还能看到漏了什么
		return placeholder
	})

	if s.files == nil {
		return nil, fmt.Errorf("模板服务未接入文件服务")
	}
	return s.files.CreateFile(workspacePath, targetRelativePath, content)
}

func validateTemplateName(name string) error {
	if name == "" || name != strings.TrimSpace(name) || name == "." || name == ".." ||
		strings.ContainsAny(name, "/\\") || strings.HasSuffix(name, ".") {
		return fmt.Errorf("模板名不合法: %q", name)
	}
	if _, err := sourceRelativePath(name + ".md"); err != nil {
		return fmt.Errorf("模板名不合法: %q", name)
	}
	return nil
}

func templateWorkspaceRoot(workspace string) (*os.Root, error) {
	full, err := sourceWorkspacePath(workspace)
	if err != nil {
		return nil, fmt.Errorf("模板工作区不可用: %w", err)
	}
	return os.OpenRoot(full)
}

func describeTemplate(name, content string, builtin bool) *TemplateInfo {
	fm, _, _ := SplitFrontMatter(content)
	props := ParseFrontMatter(fm)
	info := &TemplateInfo{
		Name: name, Builtin: builtin,
		Category: props["template-category"].Str, Description: props["template-description"].Str,
		Destination: props["template-destination"].Str,
	}
	// An invalid recommendation must never become the chooser's default path.
	if info.Destination != "" {
		clean, err := sourceRelativePath(variablePattern.ReplaceAllString(info.Destination, "value"))
		if err != nil || !strings.EqualFold(path.Ext(clean), ".md") {
			info.Destination = ""
		} else {
			info.Destination = strings.ReplaceAll(info.Destination, "\\", "/")
		}
	}
	info.Variables = extractVariables(templateNoteContent(content) + "\n" + info.Destination)
	return info
}

// Chooser hints live in the editable Markdown template, but are not properties
// of a created note. Preserve all other frontmatter and original line endings.
func templateNoteContent(content string) string {
	if _, _, found := SplitFrontMatter(content); !found {
		return content
	}
	lines := splitWorkbenchLines(content)
	kept := make([]workbenchLine, 0, len(lines))
	inFrontmatter := true
	for index, line := range lines {
		if index > 0 && inFrontmatter {
			if line.text == "---" {
				inFrontmatter = false
			} else if key, _, ok := strings.Cut(line.text, ":"); ok {
				switch key {
				case "template-category", "template-description", "template-destination":
					continue
				}
			}
		}
		kept = append(kept, line)
	}
	return joinWorkbenchLines(kept)
}
