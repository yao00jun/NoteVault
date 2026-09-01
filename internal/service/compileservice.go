// Package service 内的 CompileService 实现「知识编译流水线」（P1-5）。
//
// 设计取舍：
//   - 约定 Inbox 目录（Raw 素材）与 Compiled 目录；一键编译 = AI 生成 TL;DR + 标签 + 建议双链，
//     再把笔记从 Inbox 移动到 Compiled。
//   - 每一步都先建一份手动快照（复用 P1-2 的 SnapshotService），保证可整体撤销。
//   - AI 调用通过 CompileAI 接口抽象，便于测试时注入桩实现；生产默认走 SummarizeService。
//   - frontmatter 合并采用「保留原文、仅替换/追加受管字段」的策略，绝不丢弃用户已有的其他 frontmatter 键。
package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CompileOutput 是「知识编译」的结构化产出。
type CompileOutput struct {
	// TLDR 一句话摘要，调用方约束模型控制在 200 字以内。
	TLDR string
	// Tags AI 建议的标签（已去重，不含前导 #）。
	Tags []string
	// SuggestedLinks AI 建议的双链目标，形如 "笔记标题"（不含 [[ ]]）。
	SuggestedLinks []string
}

// CompileAI 是编译流程对 AI 的依赖抽象。
// summarizeCompileClient 是其生产默认实现，复用 SummarizeService.Summarize。
// 测试时可注入桩实现。
type CompileAI interface {
	Compile(apiKey, baseURL, model, title, body string) (*CompileOutput, error)
}

// summarizeCompileClient 把 SummarizeService 适配成 CompileAI：
// 用结构化提示词调 Summarize，再把自由文本解析成 CompileOutput。
// 刻意不往 SummarizeService 上挂新的导出方法——否则会触发端口契约测试，
// 还要同步 ports.go 与前端 TS；编译专属的提示词/解析逻辑留在 CompileService 内更内聚。
type summarizeCompileClient struct {
	svc *SummarizeService
}

// NewSummarizeCompileAI 用 SummarizeService 构造一个生产用的 CompileAI。
func NewSummarizeCompileAI(svc *SummarizeService) CompileAI {
	return &summarizeCompileClient{svc: svc}
}

func (c *summarizeCompileClient) Compile(apiKey, baseURL, model, title, body string) (*CompileOutput, error) {
	content := body
	if strings.TrimSpace(content) == "" {
		content = title
	}
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("没有可编译的内容")
	}

	userPrompt := "请编译下面这篇笔记，按严格格式输出，不要输出任何额外说明。\n" +
		"第一行：TLDR: 后接一句话摘要，不超过200字。\n" +
		"第二行：TAGS: 后接 3 到 5 个标签，用英文逗号分隔，不要带 # 号。\n" +
		"第三行：LINKS: 后接 2 到 4 个建议双链目标，使用 [[标题]] 格式，用英文逗号分隔；" +
		"只能建议与本文主题相关、可能已存在于知识库中的笔记标题。\n\n" +
		"标题：\n" + title + "\n\n正文：\n" + content

	raw, err := c.svc.Summarize(apiKey, baseURL, model, userPrompt)
	if err != nil {
		return nil, err
	}
	return parseCompileOutput(raw)
}

// parseCompileOutput 把模型返回的 TLDR:/TAGS:/LINKS: 三段文本解析成结构化结果。
// 对格式不严格：缺失某段时返回零值而非报错，保证编译流程不中断。
func parseCompileOutput(raw string) (*CompileOutput, error) {
	out := &CompileOutput{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "TLDR:"):
			out.TLDR = strings.TrimSpace(strings.TrimPrefix(line, "TLDR:"))
		case strings.HasPrefix(line, "TAGS:"):
			out.Tags = splitCompileCSV(strings.TrimPrefix(line, "TAGS:"))
		case strings.HasPrefix(line, "LINKS:"):
			rawLinks := splitCompileCSV(strings.TrimPrefix(line, "LINKS:"))
			for _, l := range rawLinks {
				l = strings.TrimSpace(l)
				l = strings.TrimPrefix(l, "[[")
				l = strings.TrimSuffix(l, "]]")
				l = strings.TrimSpace(l)
				if l != "" {
					out.SuggestedLinks = append(out.SuggestedLinks, l)
				}
			}
		}
	}
	return out, nil
}

func splitCompileCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// CompileService 知识编译流水线。
type CompileService struct {
	fileSvc *FileService
	snapSvc *SnapshotService
	ai      CompileAI

	inboxDir    string
	compiledDir string
}

// NewCompileService 构造编译服务。
//   - inboxDir / compiledDir：相对于工作区根目录的目录名（默认 "Inbox" / "Compiled"）。
//   - ai：AI 调用实现，生产环境传 *SummarizeService 即可。
func NewCompileService(fileSvc *FileService, snapSvc *SnapshotService, ai CompileAI, inboxDir, compiledDir string) *CompileService {
	if inboxDir == "" {
		inboxDir = "Inbox"
	}
	if compiledDir == "" {
		compiledDir = "Compiled"
	}
	return &CompileService{
		fileSvc:     fileSvc,
		snapSvc:     snapSvc,
		ai:          ai,
		inboxDir:    filepath.ToSlash(inboxDir),
		compiledDir: filepath.ToSlash(compiledDir),
	}
}

// CompileResult 单篇笔记的编译结果。
type CompileResult struct {
	// Source 原始（Inbox 内）相对路径。
	Source string
	// Dest 编译后（Compiled 内）相对路径。
	Dest string
	// SnapshotID 编译前建立的快照 ID，用于整体撤销。
	SnapshotID string
	// Output AI 产出的结构化内容。
	Output *CompileOutput
}

// ListInbox 列出 Inbox 目录下的全部 Markdown 笔记（相对路径，含 Inbox 前缀）。
// 目录不存在时返回空切片而非错误。
func (s *CompileService) ListInbox(workspacePath string) ([]string, error) {
	root := filepath.Join(workspacePath, s.inboxDir)
	var out []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil // 目录不存在，视为空
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(p), ".md") {
			return nil
		}
		rel, rerr := filepath.Rel(workspacePath, p)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CompileNote 编译单篇笔记：先建快照，再调 AI，最后把更新后的笔记移动到 Compiled。
// relativePath 必须位于 Inbox 目录内，否则报错（避免误编译库内其他笔记）。
func (s *CompileService) CompileNote(workspacePath, relativePath, apiKey, baseURL, model string) (*CompileResult, error) {
	rel := filepath.ToSlash(relativePath)
	if !strings.HasPrefix(rel, s.inboxDir+"/") {
		return nil, fmt.Errorf("编译目标 %q 不在 Inbox 目录 %q 内", rel, s.inboxDir)
	}

	content, err := s.fileSvc.ReadFile(workspacePath, rel)
	if err != nil {
		return nil, err
	}

	// 撤销入口：编译前为原文建一份手动快照。
	snap, err := s.snapSvc.CreateManualSnapshot(workspacePath, rel)
	if err != nil {
		return nil, fmt.Errorf("建立撤销快照失败：%w", err)
	}

	fm, body, found := SplitFrontMatter(content)
	title := compileTitle(fm, body, filepath.Base(rel))

	out, err := s.ai.Compile(apiKey, baseURL, model, title, body)
	if err != nil {
		return nil, fmt.Errorf("AI 编译失败：%w", err)
	}

	compiledAt := time.Now().UTC().Format(time.RFC3339)
	var newFM string
	if found {
		newFM = mergeCompileMeta(fm, out, compiledAt)
	} else {
		newFM = buildFreshFrontMatter(out, compiledAt)
	}

	newBody := body
	if len(out.SuggestedLinks) > 0 {
		newBody = appendRelatedSection(body, out.SuggestedLinks)
	}
	newContent := "---\n" + newFM + "---\n" + newBody

	dest := filepath.ToSlash(filepath.Join(s.compiledDir, filepath.Base(rel)))
	if _, err := s.fileSvc.CreateFile(workspacePath, dest, newContent); err != nil {
		return nil, fmt.Errorf("写入编译结果失败（原文件未改动）：%w", err)
	}
	// 移动：删除 Inbox 原文。若删除失败则回滚已写入的 Compiled 副本。
	if err := s.fileSvc.DeleteFile(workspacePath, rel); err != nil {
		_ = s.fileSvc.DeleteFile(workspacePath, dest)
		return nil, fmt.Errorf("移动编译结果失败（已回滚）：%w", err)
	}

	return &CompileResult{
		Source:     rel,
		Dest:       dest,
		SnapshotID: snap.ID,
		Output:     out,
	}, nil
}

// CompileAll 编译 Inbox 内全部笔记。
// 单篇失败不影响其他篇；返回每篇结果或错误，由调用方决定如何处理。
func (s *CompileService) CompileAll(workspacePath, apiKey, baseURL, model string) ([]*CompileResult, []error) {
	notes, err := s.ListInbox(workspacePath)
	if err != nil {
		return nil, []error{err}
	}
	var results []*CompileResult
	var errs []error
	for _, n := range notes {
		r, e := s.CompileNote(workspacePath, n, apiKey, baseURL, model)
		if e != nil {
			errs = append(errs, fmt.Errorf("%s: %w", n, e))
			continue
		}
		results = append(results, r)
	}
	return results, errs
}

// ---- frontmatter 合并辅助 ----

// mergeCompileMeta 保留原 frontmatter 文本，仅替换/追加受管字段
// （tldr / tags / compiled / compiled_at），丢弃用户原有同名键。
func mergeCompileMeta(fm string, out *CompileOutput, compiledAt string) string {
	var b strings.Builder
	inTagsBlock := false
	for _, line := range strings.Split(fm, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if inTagsBlock {
			// 消费 block-array 形式的 tags 续行（- item）
			if strings.HasPrefix(trimmed, "- ") || trimmed == "" {
				continue
			}
			inTagsBlock = false
		}
		switch {
		case strings.HasPrefix(lower, "tldr:"),
			strings.HasPrefix(lower, "compiled:"),
			strings.HasPrefix(lower, "compiled_at:"):
			continue
		case strings.HasPrefix(lower, "tags:"):
			if !strings.Contains(trimmed, "[") {
				inTagsBlock = true // 多行 block-array，后续续行一并丢弃
			}
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("tldr: " + yamlQuote(out.TLDR) + "\n")
	b.WriteString("tags: [" + strings.Join(out.Tags, ", ") + "]\n")
	b.WriteString("compiled: true\n")
	b.WriteString("compiled_at: " + compiledAt + "\n")
	return strings.TrimRight(b.String(), "\n")
}

// buildFreshFrontMatter 为原本没有 frontmatter 的笔记新建一块。
func buildFreshFrontMatter(out *CompileOutput, compiledAt string) string {
	var b strings.Builder
	b.WriteString("tldr: " + yamlQuote(out.TLDR) + "\n")
	b.WriteString("tags: [" + strings.Join(out.Tags, ", ") + "]\n")
	b.WriteString("compiled: true\n")
	b.WriteString("compiled_at: " + compiledAt + "\n")
	return strings.TrimRight(b.String(), "\n")
}

// appendRelatedSection 在正文末尾追加「相关笔记（编译建议）」小节。
func appendRelatedSection(body string, links []string) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n\n## 相关笔记（编译建议）\n\n")
	for _, l := range links {
		b.WriteString("- [[" + l + "]]\n")
	}
	return b.String()
}

// compileTitle 从 frontmatter / 正文 H1 / 文件名推导标题。
func compileTitle(fm, body, baseName string) string {
	if fm != "" {
		props := ParseFrontMatter(fm)
		if t, ok := props["title"]; ok && t.Kind == KindString && t.Str != "" {
			return t.Str
		}
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return strings.TrimSuffix(baseName, filepath.Ext(baseName))
}

// yamlQuote 给可能含特殊字符的字符串加双引号（YAML 安全）。
func yamlQuote(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, ":#\"'") || strings.HasPrefix(s, " ") || strings.HasPrefix(s, "-") {
		escaped := strings.ReplaceAll(s, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return s
}
