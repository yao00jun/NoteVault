package service

import (
	"fmt"
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
}

// TemplateService 提供模板列表与从模板创建笔记。
type TemplateService struct {
	files *FileService
}

func NewTemplateService(files *FileService) *TemplateService {
	return &TemplateService{files: files}
}

// ListTemplates 列出工作区全部模板（按名称排序）。
// Templates/ 目录不存在时返回空列表——这是全新工作区的正常状态，不是错误。
func (s *TemplateService) ListTemplates(workspacePath string) ([]*TemplateInfo, error) {
	dir := filepath.Join(workspacePath, templatesDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取模板目录失败: %w", err)
	}

	templates := make([]*TemplateInfo, 0)
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
		templates = append(templates, info)
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
	if err != nil {
		return "", fmt.Errorf("读取模板失败: %w", err)
	}
	return string(content), nil
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
	tplPath, err := s.templatePath(workspacePath, templateName)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(tplPath)
	if err != nil {
		return nil, fmt.Errorf("读取模板失败: %w", err)
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
