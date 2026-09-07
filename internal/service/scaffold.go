package service

// WorkspaceScaffold —— 工作区脚手架：幂等初始化约定目录与引导文件（2026-09-07 用户拍板）。
//
// 背景：目录约定（Learning/Projects/Resources/Inbox/Daily/Templates/assets）散落在
// 各服务的隐式字符串里，而 CreateWorkspace 只建根目录——新工作区四张卡片全 0、
// 编译页「Inbox 为空」、模板列表为空，新用户不知道目录该长什么样。
//
// 铁律（对应架构红线「数据操作可撤销」）：
//   - 目录用 MkdirAll（本身幂等）；
//   - 文件【只在不存在时】写入——绝不覆盖用户已有同名文件，绝不删除/改写任何现存内容；
//   - 因此跑多少遍结果都一样，老工作区打开时补齐也零风险。
//
// 失败语义：脚手架失败不阻断工作区创建/切换（退化为旧行为：目录惰性创建），
// 只记日志由下次打开重试。

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// scaffoldDirs 脚手架目录清单（相对工作区根）。
// 与 useWorkbenchSpaces.SPACE_DEFS、compileservice、assetgc、templateservice
// 的约定目录一一对应；新增约定目录时两处同步。
var scaffoldDirs = []string{
	"Learning",
	"Projects",
	"Resources",
	"Inbox",
	"Daily",
	"Templates",
	"assets",
}

// scaffoldFiles 引导文件：内容为中文（与内置 Bases 模板同风格）。
// map 的 key 是相对路径，value 是文件全文；文件已存在则原样跳过。
var scaffoldFiles = map[string]string{
	"开始使用.md": `# 欢迎使用 NoteVault 👋

这是一个本地优先的个人知识库：所有内容都是普通 Markdown 文件，
可以用任何编辑器打开，也方便用 Git / 网盘备份同步。

## 五个知识空间

| 目录 | 用途 |
| --- | --- |
| Learning/ | 按专题归类的长效知识 |
| Projects/ | 进行中项目的交付物 |
| Resources/ | 网络收集的学习资料 |
| Inbox/ | 待整理的原始素材（剪藏也会落在这里） |
| Daily/ | 每日记录：工作台点「日记」自动创建 Today 笔记 |

## 三个立刻能试的功能

1. **双链**：输入 [[开始使用]] 引用本页，保存后左侧「反向链接」会出现引用它的笔记。
2. **待办**：在任何笔记里写一行 "- [ ] 试试待办面板"，工作台「今日待办」会自动聚合。
3. **知识编译**：把网页剪藏或粗糙笔记丢进 Inbox/，到「洞察提炼 → 知识编译」一键生成
   摘要、标签与双链建议，成稿移入 Compiled/。

> 模板在 Templates/ 里；新建文档时可选择套用。本文件可以随时删除或改写。
`,
	"Inbox/使用引导.md": `# Inbox 使用引导

这里存放**待整理的原始素材**：

- 浏览器剪藏（Web Clipper 扩展 / ` + "`POST /api/clip`" + ` 落盘到这里）
- 随手记的灵感、摘录、临时笔记

**下一步**：素材够了就到「洞察提炼 → 知识编译」一键编译——
AI 会生成摘要、标签与双链建议，并把成稿移入 Compiled/。

整理完的资料想长期留存，移到 Resources/（资料收藏）或 Learning/（专题知识）。
本文件可随时删除。
`,
	"Templates/日记模板.md": `# {{title}}

## 今天做了什么

- 

## 学到什么

- 

## 明天注意

- 
`,
	"Templates/会议记录.md": `# 会议：{{title}}

- **时间**：{{date}} {{time}}
- **参会**：

## 议题与结论

- 

## 行动项

- [ ] 
`,
	"Templates/读书笔记.md": `# {{title}}

- **作者**：
- **状态**：reading
- **评分**：

## 核心观点

- 

## 摘录

> 

## 我的想法

- 
`,
}

// legacyMigrationReserved 遗留目录迁移的保留名：脚手架约定目录 + 编译产物目录。
// 命中保留名（或点前缀系统目录）的根目录条目一律原地不动。
var legacyMigrationReserved = func() map[string]bool {
	m := map[string]bool{"Compiled": true}
	for _, d := range scaffoldDirs {
		m[d] = true
	}
	return m
}()

// migrateLegacyFolders 把根目录下游离的主题目录收进 Learning/（2026-09-07 用户指令：
// 「Java SQL 怎么还在外面？说好的整理文件夹呢？」）。
//
// 规则（与脚手架铁律一致，零破坏）：
//   - 只处理【目录】，文件一律不动；跳过点前缀目录（.trash/.notevault/.archive）
//     与保留名（scaffoldDirs + Compiled）；
//   - 目标 Learning/<名> 已存在时跳过——绝不合并、绝不覆盖；
//   - 同卷 os.Rename 原子完成；移动后根目录不再有该目录，天然幂等。
func migrateLegacyFolders(workspacePath string) []error {
	entries, err := os.ReadDir(workspacePath)
	if err != nil {
		return []error{fmt.Errorf("读工作区根目录失败: %w", err)}
	}
	var errs []error
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, ".") || legacyMigrationReserved[name] {
			continue
		}
		srcPath, err := confineToWorkspace(workspacePath, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("路径校验 %s 失败: %w", name, err))
			continue
		}
		targetRel := "Learning/" + name
		targetPath, err := confineToWorkspace(workspacePath, targetRel)
		if err != nil {
			errs = append(errs, fmt.Errorf("路径校验 %s 失败: %w", targetRel, err))
			continue
		}
		if _, err := os.Stat(targetPath); err == nil {
			continue // 目标已存在：保留原样，不合并不覆盖
		} else if !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("检查 %s 失败: %w", targetRel, err))
			continue
		}
		if err := os.Rename(srcPath, targetPath); err != nil {
			errs = append(errs, fmt.Errorf("迁移目录 %s 失败: %w", name, err))
			continue
		}
		log.Printf("[scaffold] 根目录主题目录 %s 已迁入 %s", name, targetRel)
	}
	return errs
}

// EnsureWorkspaceScaffold 幂等初始化工作区约定目录与引导文件。
// 返回值只反映"是否有致命失败"；单条失败不阻断其余条目（继续初始化别的），
// 最后汇总为 error 供调用方记日志。
func EnsureWorkspaceScaffold(workspacePath string) error {
	if workspacePath == "" {
		return fmt.Errorf("scaffold: 工作区路径为空")
	}

	var errs []error

	// 1. 目录：MkdirAll 幂等；路径统一走 confineToWorkspace（路径限定规范）
	for _, dir := range scaffoldDirs {
		dirPath, err := confineToWorkspace(workspacePath, dir)
		if err != nil {
			errs = append(errs, fmt.Errorf("路径校验 %s 失败: %w", dir, err))
			continue
		}
		if err := os.MkdirAll(dirPath, 0o750); err != nil {
			errs = append(errs, fmt.Errorf("创建目录 %s 失败: %w", dir, err))
		}
	}

	// 2. 引导文件：只在不存在时写入，绝不覆盖用户内容
	for rel, content := range scaffoldFiles {
		full, err := confineToWorkspace(workspacePath, rel)
		if err != nil {
			errs = append(errs, fmt.Errorf("路径校验 %s 失败: %w", rel, err))
			continue
		}
		if _, err := os.Stat(full); err == nil {
			continue // 已存在（无论是谁写的）——用户内容，不碰
		} else if !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("检查文件 %s 失败: %w", rel, err))
			continue
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			errs = append(errs, fmt.Errorf("写入 %s 失败: %w", rel, err))
		}
	}

	// 3. 遗留目录迁移：根目录游离的主题目录（如 Java、SQL）收进 Learning/
	errs = append(errs, migrateLegacyFolders(workspacePath)...)

	if len(errs) > 0 {
		return fmt.Errorf("scaffold: %d 项初始化失败: %w", len(errs), errs[0])
	}
	return nil
}
