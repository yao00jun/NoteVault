# Workbench v3 验收记录

验收日期：2026-09-09。实现分支：`codex/workbench-v3`，起点：`d7cd23c`。

## 已交付行为

- 侧栏「每日工作日志」与今日页「查看今日日志」统一读取 `Daily/Reports/YYYY-MM-DD-日报.md`。尚未归档时提示并打开现有日报生成器，保留历史日记阅读能力。
- 今日新任务默认选中近期活跃的进行中项目，明确展示 `Projects/<项目>/Tasks.md` 保存位置。通用事务也写入项目，首次使用时创建必要的 `project.md`。进展只写在原任务下，由同一 Markdown 派生时间线和日报。
- 项目 Markdown 的标签栏与编辑工具栏均提供「沉淀为知识」。可选择已有或新技术分册、编辑摘要，或选择面试专题、编辑题目与解答。
- 技术笔记保存标准 frontmatter、来源链接，并按需创建 `book.md`；面试题追加标准 `<!-- srs: ... -->`，初始掌握、7 天后复习、次数 1。两种模式均在来源项目文章追加知识链接。
- 重试不会重复生成内容或重置已复习题卡。同名独立章节、非法路径、保留文件名、符号链接、破坏 Markdown/SRS 结构的输入均受保护；失败保留原文并返回错误。
- 沉淀前保存受影响的编辑草稿，等待在途保存，操作后重载文件。Windows 大小写别名同样受到保护；同时发生的编辑保留为冲突，不覆盖知识链接或题卡。
- 面试解答中的标题嵌套在题卡内，LF/CRLF 的反引号和波浪号代码围栏保持原样。完整工作区路径的双向链接可正确跳转，保留原有短文件名链接。
- AI 提炼复用现有配置与问答服务，先展示结构化草稿，由用户点击「采用草稿」后应用，最后明确保存。错误或等待期间的手写内容不会自动丢失。

## 质量门禁

| 命令 | 结果 |
|---|---|
| `CGO_ENABLED=0 go test ./internal/...` | 10 个包全部通过；另一次 `-count=1` 全量运行同样通过 |
| `cd frontend && pnpm typecheck` | 退出码 0，0 errors |
| `cd frontend && pnpm test` | 65 套、684 项全部通过（原基线 59 套、615 项） |
| `cd frontend && pnpm lint` | 退出码 0，0 errors；10 条原有 warnings，无新增 warning |
| `wails3 generate bindings ./... -clean=true -ts -i -names` | 成功生成；28 services / 133 methods |
| `cd frontend && pnpm build` | 生产构建成功；保留原有大 chunk 提示 |
| `CGO_ENABLED=0 go build -tags production -trimpath -buildvcs=false ...` | Windows amd64 生产二进制构建成功，构建信息确认零 CGO |
| `git diff --check` | 通过 |

未添加存储引擎或依赖。Web Clipper、脚手架、原有 PARA 空间、AI Floating Orb、Copilot 与既有测试保留。

## 真实桌面验收

使用真实 Wails/WebView2，通过 Playwright CDP 操作独立配置和临时工作区，检查实际 Markdown 字节；未修改用户知识库。13 条流程通过：

1. 活跃项目默认选择、任务保存位置和任务创建。
2. 进展回写任务、时间线更新、完成日期保留。
3. 通用事务项目自动初始化。
4. 缺失工作日志触发日报生成，包含任务进展和卡点。
5. 已有日志进入正确文件，全局返回回到原页面。
6. 带未保存修改的项目文档沉淀为新书，后续保存保留来源徽章。
7. 学习模块发现新书，章节与项目原文双向跳转。
8. 章节重名错误保留来源与目标原文。
9. Windows CRLF 文档默认解答保存成功；`Go.md` / `go.md` 合并到同一专题并保留编辑内容与代码示例。
10. 题卡的初始 7 天状态正确；将测试题标记到期后可在复习界面自评，评分写回 Markdown。
11. macOS、WinUI、Islands Dark 三种主题在 1440×960 与 1000×720 下无横向溢出，保存按钮可访问，Escape 关闭正常。
12. 全局 AI 浮动球打开 Copilot。
13. 应用重启后从 Markdown 恢复新书、日志、题卡复习状态和来源链接。

桌面流程无页面异常。故意触发的重名拒绝返回 422；启动时保留既有可选资源 404 日志。

AI 提炼的请求、预览、应用、错误与异步编辑保护已通过组件测试；提供方响应使用测试替身，本次未调用真实远程模型。

## 修改和新增文件

### 工作日志和项目任务

- `frontend/src/components/layout/SideBar.vue`
- `frontend/src/components/layout/SideBar.test.ts`
- `frontend/src/views/TodayView.vue`
- `frontend/src/views/TodayView.test.ts`
- `frontend/src/components/workbench/TaskCard.vue`
- `frontend/src/composables/useWorkLog.ts`（新增）
- `frontend/src/composables/useWorkLog.test.ts`（新增）
- `frontend/src/utils/defaultTaskProject.ts`（新增）
- `frontend/src/utils/defaultTaskProject.test.ts`（新增）
- `internal/service/workbench_mutations.go`
- `internal/service/workbench_mutation_test.go`

### 知识沉淀后端和服务契约

- `internal/service/workbench_distill.go`（新增）
- `internal/service/workbench_distill_test.go`（新增；22 个测试及表驱动场景）
- `internal/service/workbench_markdown.go`
- `internal/service/ports.go`
- `frontend/src/api/workbench.ts`
- `frontend/src/stores/workbench.ts`
- `frontend/src/stores/workbench.test.ts`
- `frontend/bindings/github.com/notevault/notevault/internal/service/index.ts`
- `frontend/bindings/github.com/notevault/notevault/internal/service/models.ts`
- `frontend/bindings/github.com/notevault/notevault/internal/service/todoservice.ts`

### 沉淀界面、编辑器和链接

- `frontend/src/components/workbench/DistillKnowledgeModal.vue`（新增）
- `frontend/src/components/workbench/DistillKnowledgeModal.test.ts`（新增）
- `frontend/src/utils/distillKnowledge.ts`（新增）
- `frontend/src/utils/distillKnowledge.test.ts`（新增）
- `frontend/src/views/EditorView.vue`
- `frontend/src/components/editor/EditorTabBar.vue`
- `frontend/src/components/editor/EditorTabBar.test.ts`（新增）
- `frontend/src/components/editor/EditorToolbar.vue`
- `frontend/src/components/editor/MarkdownEditor.vue`
- `frontend/src/components/editor/MarkdownEditor.test.ts`
- `frontend/src/composables/useEditorDraft.ts`
- `frontend/src/composables/useEditorDraft.test.ts`
- `frontend/src/utils/wikiLinkFiles.ts`（新增）
- `frontend/src/utils/wikiLinkFiles.test.ts`（新增）

### 规格与验收资料

- `docs/design/WORKBENCH-V3-ENGINEERING-SPEC.md`（将用户提供的权威规格纳入版本管理）
- `docs/superpowers/plans/2026-09-09-workbench-v3.md`
- `docs/design/WORKBENCH-V3-ACCEPTANCE.md`
