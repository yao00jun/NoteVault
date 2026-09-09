package service

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
)

// Initialization adds useful structure and guides. Only exact known application
// defaults may be upgraded; user-authored documents are never replaced.
var scaffoldDirs = []string{
	"Learning", "Learning/面试宝典", "Projects", "Resources", "Inbox",
	"Daily", "Daily/Reports", "Templates", "assets",
}

var scaffoldFiles = map[string]string{
	"开始使用.md": `# 欢迎使用 NoteVault

所有内容都是普通 Markdown 文件，可以直接编辑、备份和迁移。

## 从工作到知识

1. **Today**：查看实际待办、截止日期和阻塞，记录任务进展。
2. **Projects**：为正在推进的工作建立项目，维护目标、任务和交付记录。
3. **Learning**：将项目经验整理为技术笔记、专题知识或面试卡片。
4. **Reports**：在 Daily/Reports 中保留工作日报、周报和阶段总结。

## 五个知识空间

| 目录 | 用途 |
| --- | --- |
| Projects/ | 每个项目一个目录，project.md 记录概览，Tasks.md 维护任务 |
| Learning/ | 技术笔记和专题知识；面试卡片放入 Learning/面试宝典/ |
| Resources/ | 可重复引用的外部资料 |
| Inbox/ | 网页剪藏、待整理的原始素材 |
| Daily/Reports/ | 工作报告与复盘 |

## 创建第一份实际内容

在「从模板新建」中按用途选择项目概览、项目任务、技术设计、排障记录、
技术笔记、面试卡片、会议记录、工作报告或读书笔记。
选择器会提供推荐路径；从某个文件夹新建时优先放入所选文件夹。

模板保存在 Templates/，可以直接修改，也可以放入自己的 Markdown 模板。
文件顶部的 template-category、template-description、template-destination
分别设置用途、说明和推荐路径；这些提示不会写入创建的笔记。

## 持续整理

- 用双链关联项目记录、技术笔记和原始资料。
- 从 Inbox 收集素材，在「洞察提炼 → 知识编译」中整理摘要、标签与关联。
- 将值得复用的项目经验沉淀到 Learning，再将真实问题整理成面试卡片。

此引导页可自由编辑或删除。应用不会自动删除已有的日记、报告或自定义模板。
`,
	"Inbox/使用引导.md": `# Inbox 使用引导

这里接收网页剪藏、摘录、随手记录和其他待整理素材。

## 整理去向

- 正在推进的工作：关联或整理到 Projects 对应项目。
- 已理解、值得复用的知识：沉淀到 Learning。
- 需要保留的外部资料：归档到 Resources。
- 工作成果与进展：汇总到 Daily/Reports 的报告。

「洞察提炼 → 知识编译」可以协助生成摘要、标签和双链建议，
编译后的文档保存在 Compiled，再根据用途归类。

Web Clipper 仍将剪藏内容保存在这里。本文件可自由修改或删除。
`,
}

func workspaceScaffoldFiles() map[string]string {
	files := make(map[string]string, len(scaffoldFiles))
	for name, content := range scaffoldFiles {
		files[name] = content
	}
	// The bundled Markdown is the only definition of the current default templates.
	for _, template := range bundledTemplates() {
		if content, ok := bundledTemplateContent(template.Name); ok {
			files[templatesDirName+"/"+template.Name+".md"] = content
		}
	}
	return files
}

// Only the two historical topic folders are migrated. Newly-created root folders,
// build artifacts, application metadata and all other names stay where users put them.
func migrateLegacyFolders(root *os.Root) []error {
	var errs []error
	for _, name := range []string{"Java", "SQL"} {
		info, err := root.Lstat(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("检查 %s 失败: %w", name, err))
			continue
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		target := "Learning/" + name
		if err := sourceCheckPath(root, target); err != nil {
			errs = append(errs, err)
			continue
		}
		if _, err := root.Lstat(target); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("检查 %s 失败: %w", target, err))
			continue
		}
		if err := root.Rename(name, target); err != nil {
			errs = append(errs, fmt.Errorf("迁移目录 %s 失败: %w", name, err))
			continue
		}
		log.Printf("[scaffold] 历史主题目录 %s 已迁入 %s", name, target)
	}
	return errs
}

// EnsureWorkspaceScaffold is idempotent. Individual failures do not prevent safe
// independent entries from being initialized; callers can log the combined error.
func EnsureWorkspaceScaffold(workspacePath string) error {
	if strings.TrimSpace(workspacePath) == "" {
		return fmt.Errorf("scaffold: 工作区路径为空")
	}
	full, err := sourceWorkspacePath(workspacePath)
	if err != nil {
		return fmt.Errorf("scaffold: %w", err)
	}
	root, err := os.OpenRoot(full)
	if err != nil {
		return err
	}
	defer root.Close()
	var errs []error
	for _, dir := range scaffoldDirs {
		if err := sourceCheckPath(root, dir); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := root.MkdirAll(dir, 0750); err != nil {
			errs = append(errs, fmt.Errorf("创建目录 %s 失败: %w", dir, err))
		}
	}

	files := workspaceScaffoldFiles()
	// Add retired default paths so exact old copies can be archived, never reseeded.
	paths := make(map[string]bool, len(files)+len(legacyScaffoldDigests))
	for relative := range files {
		paths[relative] = true
	}
	for relative := range legacyScaffoldDigests {
		paths[relative] = true
	}
	ordered := make([]string, 0, len(paths))
	for relative := range paths {
		ordered = append(ordered, relative)
	}
	sort.Strings(ordered)
	for _, relative := range ordered {
		content, currentDefault := files[relative]
		if err := ensureScaffoldFile(root, relative, content, currentDefault); err != nil {
			errs = append(errs, fmt.Errorf("初始化 %s 失败: %w", relative, err))
		}
	}
	errs = append(errs, migrateLegacyFolders(root)...)
	if len(errs) > 0 {
		return fmt.Errorf("scaffold: %d 项初始化失败: %w", len(errs), errs[0])
	}
	return nil
}
