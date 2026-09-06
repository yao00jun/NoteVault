package service

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
	Name      string   `json:"name"`      // 模板名（文件名去扩展名），即模板 ID
	Variables []string `json:"variables"` // 需要用户填写的自定义变量（已排除内置）
	Builtin   bool     `json:"builtin"`   // 是否为应用内置模板（工作区同名模板可覆盖）
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
		info := &TemplateInfo{Name: name, Variables: []string{}, Builtin: true}
		if content, err := fs.ReadFile(bundledTemplateFS, "templates/"+entry.Name()); err == nil {
			info.Variables = extractVariables(string(content))
		}
		out = append(out, info)
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

	dir := filepath.Join(workspacePath, templatesDirName)
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取模板目录失败: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		info := &TemplateInfo{Name: name, Variables: []string{}}
		if content, err := os.ReadFile(filepath.Join(dir, entry.Name())); err == nil {
			info.Variables = extractVariables(string(content))
		}
		// 读失败不影响列出：只是拿不到变量清单，创建时仍可用
		info.Builtin = false
		merged[name] = info // 工作区模板覆盖同名内置
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
	path, err := s.templatePath(workspacePath, name)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(path)
	if err == nil {
		return string(content), nil
	}
	// 工作区读不到（不存在或不可读）时回退内置版本：
	// Windows 上目录不存在报 ERROR_PATH_NOT_FOUND，勿依赖 os.IsNotExist 细分
	if bundled, ok := bundledTemplateContent(name); ok {
		return bundled, nil
	}
	return "", fmt.Errorf("读取模板失败: %w", err)
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
	title := strings.TrimSuffix(filepath.Base(targetRelativePath), ".md")

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

	content := variablePattern.ReplaceAllStringFunc(string(raw), func(placeholder string) string {
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

// templatePath 校验模板名并返回其绝对路径。
// 模板名必须是纯文件名（不含路径分隔符），防止用 ../../ 越过 Templates/。
func (s *TemplateService) templatePath(workspacePath string, name string) (string, error) {
	if name == "" || name != filepath.Base(name) || strings.Contains(name, "\\") {
		return "", fmt.Errorf("模板名不合法: %q", name)
	}
	return filepath.Join(workspacePath, templatesDirName, name+".md"), nil
}
