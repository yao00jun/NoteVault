package service

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// WorkspacePreset 是「岗位工作台」的初始化模板：为某类工作补齐资料分类、
// 一个可重命名的示例项目（含上下文、任务、交付物目录与复盘骨架）。
//
// 设计约束：所有写入幂等，已有文件一律不覆盖；不创建空项目之外的任何
// 用户数据，示例项目可随时重命名或删除。
type WorkspacePreset struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Highlights  []string `json:"highlights"`
}

type workspacePresetSpec struct {
	WorkspacePreset
	project   string
	context   string
	tasks     string
	resources []string
}

const presetProjectDoc = `---
name: %s
status: 进行中
phase: 规划
coreQuestion: %s
background: %s
nextStep: %s
techStack: []
tags: [项目]
---

# %s

## 目标与验收标准

这个项目最终要达成什么？如何确认已经完成？

## 范围

这次做什么、不做什么。

## 核心问题

%s

## 背景信息

%s

## 交付物

把初稿、修改稿和最终版放进 ` + "`Deliverables/`" + `，用「交付物」模板标注类型与阶段。
项目页会按初稿 / 修改稿 / 最终版归类，AI 也能据此继续推进。

## 里程碑

| 里程碑 | 验收结果 | 计划日期 |
| --- | --- | --- |
|  |  |  |

## 当前进展与风险

## 下一步

%s

## 资料与决策

## 复盘

项目结束后用「复盘」模板创建 [[Projects/%s/复盘.md|复盘]]，沉淀结果与改进。
`

const presetTasksDoc = `# %s · 任务

> 在下面填写实际任务。任务支持截止日期、优先级和进度记录，Today 会自动汇总。

## 交付任务

- [ ]

## 复盘与改进

## 额外事项

## 完成记录

<!-- 任务写法参考（替换为自己的实际内容后再使用）：
- [ ] [US] 交付事项 #due/2026-09-30 #priority/high
- [ ] [DTS] 技术改进
-->
`

var workspacePresetSpecs = []workspacePresetSpec{
	{
		WorkspacePreset: WorkspacePreset{
			ID: "content", Name: "内容创作",
			Description: "围绕选题、素材、初稿与发布版本组织内容生产。",
			Highlights:  []string{"Resources/选题素材", "Resources/对标账号", "交付物按初稿/修改稿/最终版归档"},
		},
		project:   "内容创作-示例项目",
		resources: []string{"Resources/选题素材", "Resources/对标账号"},
		context: fmt.Sprintf(presetProjectDoc,
			"内容创作-示例项目",
			"这个选题解决读者的什么问题？",
			"把目标读者、发布渠道和已有素材写在这里，AI 会基于它推进下一步。",
			"收集 3 篇对标文章，形成选题判断",
			"内容创作-示例项目",
			"这个选题解决读者的什么问题？读者看完能带走什么？",
			"目标读者、发布渠道、历史数据和已有素材。",
			"收集 3 篇对标文章，形成选题判断。",
			"内容创作-示例项目",
		),
		tasks: fmt.Sprintf(presetTasksDoc, "内容创作-示例项目"),
	},
	{
		WorkspacePreset: WorkspacePreset{
			ID: "product", Name: "产品验证",
			Description: "用问题、访谈和验证结论推进一个产品想法。",
			Highlights:  []string{"Resources/竞品", "Resources/用户访谈", "交付物按验证阶段归档"},
		},
		project:   "产品验证-示例项目",
		resources: []string{"Resources/竞品", "Resources/用户访谈"},
		context: fmt.Sprintf(presetProjectDoc,
			"产品验证-示例项目",
			"哪一类用户的哪个问题最值得先验证？",
			"写下目标用户、当前替代方案和已有证据，AI 会基于它设计验证路径。",
			"访谈 5 位目标用户，记录真实痛点",
			"产品验证-示例项目",
			"哪一类用户的哪个问题最值得先验证？现有方案为什么不够好？",
			"目标用户、当前替代方案、已有访谈和竞品信息。",
			"访谈 5 位目标用户，记录真实痛点。",
			"产品验证-示例项目",
		),
		tasks: fmt.Sprintf(presetTasksDoc, "产品验证-示例项目"),
	},
	{
		WorkspacePreset: WorkspacePreset{
			ID: "research", Name: "投研分析",
			Description: "围绕研报、数据、观点与结论组织一次投研分析。",
			Highlights:  []string{"Resources/研报", "Resources/数据底稿", "交付物按分析阶段归档"},
		},
		project:   "投研分析-示例项目",
		resources: []string{"Resources/研报", "Resources/数据底稿"},
		context: fmt.Sprintf(presetProjectDoc,
			"投研分析-示例项目",
			"这次分析要回答的核心投资问题是什么？",
			"写明标的、时间范围和已有假设，AI 会基于它整理证据与反证。",
			"收集近三年财报与行业数据，列出关键假设",
			"投研分析-示例项目",
			"这次分析要回答的核心投资问题是什么？哪些假设最可能被证伪？",
			"标的、行业、时间范围、已有数据与关键假设。",
			"收集近三年财报与行业数据，列出关键假设。",
			"投研分析-示例项目",
		),
		tasks: fmt.Sprintf(presetTasksDoc, "投研分析-示例项目"),
	},
}

func workspacePresetByID(id string) (workspacePresetSpec, bool) {
	normalized := strings.TrimSpace(strings.ToLower(id))
	for _, spec := range workspacePresetSpecs {
		if spec.ID == normalized {
			return spec, true
		}
	}
	return workspacePresetSpec{}, false
}

// ListWorkspacePresets 返回可用的岗位工作台预设，按 ID 稳定排序。
func (s *WorkspaceService) ListWorkspacePresets() []WorkspacePreset {
	out := make([]WorkspacePreset, 0, len(workspacePresetSpecs))
	for _, spec := range workspacePresetSpecs {
		preset := spec.WorkspacePreset
		preset.Highlights = append([]string{}, spec.Highlights...)
		out = append(out, preset)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ApplyWorkspacePreset 在指定工作区补齐预设目录与示例项目。
// 幂等：重复应用不会覆盖或重复创建；已有同名项目时保持原样。
func (s *WorkspaceService) ApplyWorkspacePreset(workspacePath, presetID string) error {
	spec, ok := workspacePresetByID(presetID)
	if !ok {
		return fmt.Errorf("未知的工作台预设: %s", presetID)
	}
	if strings.TrimSpace(workspacePath) == "" {
		return fmt.Errorf("请先打开工作区")
	}
	full, err := sourceWorkspacePath(workspacePath)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(full)
	if err != nil {
		return err
	}
	defer root.Close()

	dirs := make([]string, 0, len(spec.resources)+1)
	dirs = append(dirs, spec.resources...)
	dirs = append(dirs, "Projects/"+spec.project+"/Deliverables")

	var errs []error
	for _, dir := range dirs {
		if err := sourceCheckPath(root, dir); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := root.MkdirAll(dir, 0750); err != nil {
			errs = append(errs, fmt.Errorf("创建目录 %s 失败: %w", dir, err))
		}
	}

	files := map[string]string{
		"Projects/" + spec.project + "/project.md": spec.context,
		"Projects/" + spec.project + "/Tasks.md":   spec.tasks,
	}
	ordered := make([]string, 0, len(files))
	for relative := range files {
		ordered = append(ordered, relative)
	}
	sort.Strings(ordered)
	for _, relative := range ordered {
		// currentDefault=true 表示允许在文件缺失时创建；已存在的文件不会被覆盖。
		if err := ensureScaffoldFile(root, relative, files[relative], true); err != nil {
			errs = append(errs, fmt.Errorf("初始化 %s 失败: %w", relative, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("应用预设「%s」时 %d 项失败: %w", spec.Name, len(errs), errs[0])
	}
	return nil
}
