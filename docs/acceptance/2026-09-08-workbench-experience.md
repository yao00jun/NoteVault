# Workbench 体验完善：交付与验收记录

基线：d239a07ef6d822206f7d8e241bb83f78a6627027（已有 Workbench v2）。本次实现统一导航、资料初始化、三套主题和编辑上下文完善。规格见 [体验规范](../design/WORKBENCH-EXPERIENCE-SPEC.md)，实施计划见 [计划](../superpowers/plans/2026-09-08-workbench-experience.md)，操作步骤见 [用户手册](../UserManual.md)。

状态：开发、独立复审、最终验收与 Windows 打包均已通过。源码版本可通过本文件所在的 Git 提交追溯。

## 交付内容

- 应用外壳统一返回、前进、主页与面包屑。文档入口携带完整文件身份，保留中文及百分号路径；历史与缓存按工作区隔离。文档管理、设置及其他二级页面共用导航，返回恢复列表筛选、图谱视口和集合位置。
- 知识库提供可搜索、排序、固定和按五大空间筛选的文档浏览器；旧文档管理入口正确跳转。项目与书架提供空白创建、已有笔记收录和添加资料入口，常用动作与低频工具分层。
- 共享资料导入支持本地文件夹、上传文件和公开网址，使用现有任务进度、取消、重试及结果查看。输出 Markdown 元数据与内容，保留可用附件，识别项目依赖清单中的技术栈。相同内容跳过；更新保护本地编辑；取消保留已完成结果。
- Copilot 接收当前页面 / 文档 / 项目上下文和有界对话历史。明确的来源初始化命令可启动基础导入，普通问答、条件句和未知后缀不会触发写入。导航时作废旧页面的排队请求。
- macOS、WinUI、Islands 使用不同的面板结构、控件、字体与选中状态，并提供主题预览。编辑器面板可调整和记忆宽度；文档独立保留撤销及滚动状态；状态栏反映实际保存与冲突。
- 编辑器保存失败或外部冲突会阻止离开；工作区切换先处理旧草稿。冲突副本保留外部原文，现有标签页的反向链接同步更新路由。修复抽屉文案的翻译键和 Embedding / Rerank 凭据持久化。

## 数据与兼容性

Markdown 继续作为任务、项目、书籍、SRS、日报及来源台账的权威数据，派生索引不替代原文件。保留五大空间脚手架、Web Clipper、AI 悬浮球 / Copilot、双链、图谱、模板、导出、版本与冲突处理。日报仍保存到 Daily/Reports/，SRS 自评仍使用 7 / 3 / 1 天。

sources.md 使用兼容原 v1 的 Markdown 注释台账：导入条目增加 extractionMode 及可选 warning；独立 generated 记录实际生成的元数据 / AI 文件及其哈希。生成记录不授权普通来源更新覆盖元数据。pendingGenerated 仅用于写入中断的恢复，不表示已完成生成。

## 最终验证

| 命令 / 验收 | 结果 |
|---|---|
| CGO_ENABLED=0 go test ./internal/... | PASS，10 个包；service 16.916s |
| cd frontend && pnpm typecheck | PASS，0 错误 |
| cd frontend && pnpm test | PASS，59 套 / 615 项，14.00s |
| cd frontend && pnpm lint | PASS，0 错误；11 条已有警告（6 条 v-html、5 条格式提示） |
| 实际 Wails / WebView2 桌面验收 | PASS，19 条流程；0 个页面异常，包含真实公开网页导入和程序重启 |
| CGO_ENABLED=0 wails3 package ARCH=amd64 | PASS，Windows amd64 production；构建信息确认 CGO_ENABLED=0 |
| 7z t 安装包及内嵌程序 SHA-256 比对 | PASS，Everything is Ok；内嵌 exe 与生产 exe 完全一致 |
| git diff --check | PASS |

原生验收使用隔离的临时笔记库与用户配置，连接实际 Wails 后端；没有替换业务状态或模拟服务。当前环境没有 Browser 插件，沿用本地 Playwright 通过 WebView2 CDP 操作。桌面尺寸为 1440×900 与 1000×720，覆盖三套主题、设置预览和紧凑编辑器。

验收路径覆盖今日指标 / 顺延 / 卡点，知识库筛选 → 文档 → 设置 → 返回 / 前进，保存失败与冲突副本，图谱 / 反向链接往返，项目 / 书籍 / 章节导航，空白创建与无损收录，本地项目及技术栈，重复跳过，HTML 上传，扫描 PDF / 图片附件，实际公开网页导入，Copilot 明确指令，旧 library 跳转，日报归档，真实五题自评，中文 / 百分号文件名，工作区边界与程序重启后重建。

独立全分支审查发现的两项来源导入问题（缺少提取 / 生成记录、扫描 PDF 被跳过 / 图片缺少 OCR 提示）均已修复。唯一一次定向复审结论为两项 ADDRESSED、Spec compliance PASS、Code quality APPROVED，无未解决的重要问题。新增回归覆盖台账兼容、生成中断恢复、作者修改保护、AI 持久化失败及 PDF 文本 / 附件切换；63 项 SourceImport 和 29 项相关 workbench / credential 测试通过，复审另行独立验证了受影响的测试。

## 已知边界

- 每文件 20 MiB、每批 100 MiB / 500 文件；拒绝 ZIP64。扫描、加密、复杂或不能安全解析的 PDF 保留原附件并提示提取 / OCR 限制，不伪造正文；未内置 OCR。
- 公开网页仅导入选定页面。支持固定 HTTP(S) 环境代理或当前 Windows 用户的固定代理；不执行 PAC / WPAD，不支持 SOCKS 或自动使用 Windows 集成代理认证。私网及保留地址不作为来源。
- 仅在已配置代理且操作系统 DNS 全部返回 198.18/15 fake-IP 时，使用受限的 Cloudflare HTTPS DNS 查询公网地址；连接仍固定到验证后的公网 IP，HTTPS 验证原始主机名。
- AI 整理为可选步骤，输出单独标识的 ai-plan.md，既有任务 / 完成度 / 复习状态不会被编造或覆盖。成功和失败分支通过受控接口测试；本次未用个人 API Key 对真实模型做质量评测。
- 桌面验收的唯一已知控制台资源提示为 Wails beta.16 启动时对可选 /wails/custom.js 的 HEAD 探测 404；最终 0 个页面异常。其他操作系统及移动视口未做原生验收。

## 实施决策

| 决策 | 理由及取舍 |
|---|---|
| 使用相邻的 codex/ 独立工作树 | 保持原工作区完整；增加一个临时检出目录 |
| 按独立文件范围分工，集成与发布统一验证 | 降低实现冲突；各部分仍需独立审查和组合验收 |
| 有选择地纳入被忽略的规格、计划及验收文档 | 保存本次用户批准的方案与证据；避免将本地笔记及临时资料纳入仓库 |
| 使用 Playwright 连接实际 WebView2 | 当前没有 Browser 插件；仅使用隔离验收配置，无法代表其他桌面系统 |
| 支持固定代理和受限公网 DNS 验证 | 兼容本机 fake-IP 网络并保留目的地址约束；特定条件下会向 Cloudflare 查询来源域名 |
| 不安全解析的 PDF 保留附件 | 保持有界处理及原文件；复杂资料可能需要外部提取 / OCR |
| 修复原有抽屉翻译和两类凭据键 | 让真实设置和编辑反馈正确工作；仅扩展 embedding.apiKey 与 rerank.apiKey，不开放任意凭据命名空间 |

## 发布产物

Windows amd64 可执行程序与 NSIS 安装包位于 bin/，不纳入源码提交。

| 文件 | 大小（字节） | SHA-256 |
|---|---:|---|
| notevault.exe | 50851840 | F732DB6F34A915C0C2B4AC8FC5CF9865BEBBA221820776D907E5AFF1F3529FE9 |
| notevault-amd64-installer.exe | 17056798 | FBEDEAEC18E3DE429CD8D693EE423FB4A94A4BB6D93622B5C6BFC555CEA11A73 |

## 完整文件清单

共 105 个文件：修改 64 个，新增 41 个。

| 状态 | 文件 |
|---|---|
| 新增 | [docs/acceptance/2026-09-08-workbench-experience.md](../../docs/acceptance/2026-09-08-workbench-experience.md) |
| 新增 | [docs/design/WORKBENCH-EXPERIENCE-SPEC.md](../../docs/design/WORKBENCH-EXPERIENCE-SPEC.md) |
| 新增 | [docs/superpowers/plans/2026-09-08-workbench-experience.md](../../docs/superpowers/plans/2026-09-08-workbench-experience.md) |
| 修改 | [docs/UserManual.md](../../docs/UserManual.md) |
| 修改 | [frontend/bindings/github.com/notevault/notevault/internal/service/credentialservice.ts](../../frontend/bindings/github.com/notevault/notevault/internal/service/credentialservice.ts) |
| 修改 | [frontend/bindings/github.com/notevault/notevault/internal/service/importservice.ts](../../frontend/bindings/github.com/notevault/notevault/internal/service/importservice.ts) |
| 修改 | [frontend/bindings/github.com/notevault/notevault/internal/service/index.ts](../../frontend/bindings/github.com/notevault/notevault/internal/service/index.ts) |
| 修改 | [frontend/bindings/github.com/notevault/notevault/internal/service/models.ts](../../frontend/bindings/github.com/notevault/notevault/internal/service/models.ts) |
| 修改 | [frontend/src/api/index.ts](../../frontend/src/api/index.ts) |
| 新增 | [frontend/src/api/sourceImport.ts](../../frontend/src/api/sourceImport.ts) |
| 修改 | [frontend/src/App.vue](../../frontend/src/App.vue) |
| 修改 | [frontend/src/components/editor/EditorTabBar.vue](../../frontend/src/components/editor/EditorTabBar.vue) |
| 新增 | [frontend/src/components/editor/MarkdownEditor.test.ts](../../frontend/src/components/editor/MarkdownEditor.test.ts) |
| 修改 | [frontend/src/components/editor/MarkdownEditor.vue](../../frontend/src/components/editor/MarkdownEditor.vue) |
| 新增 | [frontend/src/components/import/SourceImportModal.test.ts](../../frontend/src/components/import/SourceImportModal.test.ts) |
| 新增 | [frontend/src/components/import/SourceImportModal.vue](../../frontend/src/components/import/SourceImportModal.vue) |
| 修改 | [frontend/src/components/knowledge/TemplateCreateDialog.vue](../../frontend/src/components/knowledge/TemplateCreateDialog.vue) |
| 新增 | [frontend/src/components/knowledge/VaultActions.vue](../../frontend/src/components/knowledge/VaultActions.vue) |
| 修改 | [frontend/src/components/knowledge/WorkbenchWidgets.vue](../../frontend/src/components/knowledge/WorkbenchWidgets.vue) |
| 修改 | [frontend/src/components/layout/AICopilotDrawer.test.ts](../../frontend/src/components/layout/AICopilotDrawer.test.ts) |
| 修改 | [frontend/src/components/layout/AICopilotDrawer.vue](../../frontend/src/components/layout/AICopilotDrawer.vue) |
| 修改 | [frontend/src/components/layout/CommandPalette.vue](../../frontend/src/components/layout/CommandPalette.vue) |
| 新增 | [frontend/src/components/layout/GlobalNavigation.vue](../../frontend/src/components/layout/GlobalNavigation.vue) |
| 修改 | [frontend/src/components/layout/SideBar.test.ts](../../frontend/src/components/layout/SideBar.test.ts) |
| 修改 | [frontend/src/components/layout/SideBar.vue](../../frontend/src/components/layout/SideBar.vue) |
| 修改 | [frontend/src/components/layout/StatusBar.vue](../../frontend/src/components/layout/StatusBar.vue) |
| 修改 | [frontend/src/components/layout/TitleBar.vue](../../frontend/src/components/layout/TitleBar.vue) |
| 新增 | [frontend/src/components/settings/ThemePreview.vue](../../frontend/src/components/settings/ThemePreview.vue) |
| 新增 | [frontend/src/components/workbench/CollectionOnboarding.vue](../../frontend/src/components/workbench/CollectionOnboarding.vue) |
| 修改 | [frontend/src/composables/useAIChat.test.ts](../../frontend/src/composables/useAIChat.test.ts) |
| 修改 | [frontend/src/composables/useAIChat.ts](../../frontend/src/composables/useAIChat.ts) |
| 修改 | [frontend/src/composables/useDailyNote.ts](../../frontend/src/composables/useDailyNote.ts) |
| 修改 | [frontend/src/composables/useEditorDraft.test.ts](../../frontend/src/composables/useEditorDraft.test.ts) |
| 修改 | [frontend/src/composables/useEditorDraft.ts](../../frontend/src/composables/useEditorDraft.ts) |
| 新增 | [frontend/src/composables/useEditorLayout.ts](../../frontend/src/composables/useEditorLayout.ts) |
| 新增 | [frontend/src/composables/useEditorSession.ts](../../frontend/src/composables/useEditorSession.ts) |
| 新增 | [frontend/src/composables/usePageContext.ts](../../frontend/src/composables/usePageContext.ts) |
| 新增 | [frontend/src/composables/useSourceImport.test.ts](../../frontend/src/composables/useSourceImport.test.ts) |
| 新增 | [frontend/src/composables/useSourceImport.ts](../../frontend/src/composables/useSourceImport.ts) |
| 新增 | [frontend/src/composables/useViewRoute.ts](../../frontend/src/composables/useViewRoute.ts) |
| 修改 | [frontend/src/i18n/locales/en-US.ts](../../frontend/src/i18n/locales/en-US.ts) |
| 修改 | [frontend/src/i18n/locales/zh-CN.ts](../../frontend/src/i18n/locales/zh-CN.ts) |
| 修改 | [frontend/src/router/index.test.ts](../../frontend/src/router/index.test.ts) |
| 修改 | [frontend/src/router/index.ts](../../frontend/src/router/index.ts) |
| 新增 | [frontend/src/stores/navigation.test.ts](../../frontend/src/stores/navigation.test.ts) |
| 新增 | [frontend/src/stores/navigation.ts](../../frontend/src/stores/navigation.ts) |
| 修改 | [frontend/src/styles/themes.css](../../frontend/src/styles/themes.css) |
| 修改 | [frontend/src/styles/variables.css](../../frontend/src/styles/variables.css) |
| 修改 | [frontend/src/styles/workbench-collections.css](../../frontend/src/styles/workbench-collections.css) |
| 新增 | [frontend/src/utils/conversation.test.ts](../../frontend/src/utils/conversation.test.ts) |
| 新增 | [frontend/src/utils/conversation.ts](../../frontend/src/utils/conversation.ts) |
| 新增 | [frontend/src/utils/navigation.ts](../../frontend/src/utils/navigation.ts) |
| 新增 | [frontend/src/utils/sourceIntent.test.ts](../../frontend/src/utils/sourceIntent.test.ts) |
| 新增 | [frontend/src/utils/sourceIntent.ts](../../frontend/src/utils/sourceIntent.ts) |
| 修改 | [frontend/src/utils/workbenchCollections.ts](../../frontend/src/utils/workbenchCollections.ts) |
| 修改 | [frontend/src/views/ArchiveView.vue](../../frontend/src/views/ArchiveView.vue) |
| 修改 | [frontend/src/views/BasesView.vue](../../frontend/src/views/BasesView.vue) |
| 修改 | [frontend/src/views/CanvasView.vue](../../frontend/src/views/CanvasView.vue) |
| 修改 | [frontend/src/views/CompileView.vue](../../frontend/src/views/CompileView.vue) |
| 修改 | [frontend/src/views/DiscoverView.vue](../../frontend/src/views/DiscoverView.vue) |
| 修改 | [frontend/src/views/EditorView.vue](../../frontend/src/views/EditorView.vue) |
| 修改 | [frontend/src/views/GraphView.vue](../../frontend/src/views/GraphView.vue) |
| 修改 | [frontend/src/views/HistoryView.test.ts](../../frontend/src/views/HistoryView.test.ts) |
| 修改 | [frontend/src/views/HistoryView.vue](../../frontend/src/views/HistoryView.vue) |
| 修改 | [frontend/src/views/ImportView.vue](../../frontend/src/views/ImportView.vue) |
| 修改 | [frontend/src/views/InsightsView.vue](../../frontend/src/views/InsightsView.vue) |
| 修改 | [frontend/src/views/KnowledgeVaultView.vue](../../frontend/src/views/KnowledgeVaultView.vue) |
| 修改 | [frontend/src/views/KnowledgeView.vue](../../frontend/src/views/KnowledgeView.vue) |
| 修改 | [frontend/src/views/LearningView.vue](../../frontend/src/views/LearningView.vue) |
| 修改 | [frontend/src/views/PluginView.vue](../../frontend/src/views/PluginView.vue) |
| 修改 | [frontend/src/views/ProjectsView.vue](../../frontend/src/views/ProjectsView.vue) |
| 修改 | [frontend/src/views/ReportsView.vue](../../frontend/src/views/ReportsView.vue) |
| 修改 | [frontend/src/views/ReviewView.vue](../../frontend/src/views/ReviewView.vue) |
| 修改 | [frontend/src/views/SearchView.vue](../../frontend/src/views/SearchView.vue) |
| 修改 | [frontend/src/views/SettingsView.vue](../../frontend/src/views/SettingsView.vue) |
| 修改 | [frontend/src/views/TagsView.vue](../../frontend/src/views/TagsView.vue) |
| 修改 | [frontend/src/views/TodosView.vue](../../frontend/src/views/TodosView.vue) |
| 修改 | [frontend/src/views/TrashView.vue](../../frontend/src/views/TrashView.vue) |
| 新增 | [frontend/src/views/WelcomeView.test.ts](../../frontend/src/views/WelcomeView.test.ts) |
| 修改 | [frontend/src/views/WelcomeView.vue](../../frontend/src/views/WelcomeView.vue) |
| 修改 | [frontend/src/views/workbenchCollections.test.ts](../../frontend/src/views/workbenchCollections.test.ts) |
| 修改 | [go.mod](../../go.mod) |
| 修改 | [go.sum](../../go.sum) |
| 修改 | [internal/service/credentialservice_test.go](../../internal/service/credentialservice_test.go) |
| 修改 | [internal/service/credentialservice.go](../../internal/service/credentialservice.go) |
| 修改 | [internal/service/importservice.go](../../internal/service/importservice.go) |
| 修改 | [internal/service/ports.go](../../internal/service/ports.go) |
| 新增 | [internal/service/sourceimport_extract.go](../../internal/service/sourceimport_extract.go) |
| 新增 | [internal/service/sourceimport_generated.go](../../internal/service/sourceimport_generated.go) |
| 新增 | [internal/service/sourceimport_inputs.go](../../internal/service/sourceimport_inputs.go) |
| 新增 | [internal/service/sourceimport_pdf.go](../../internal/service/sourceimport_pdf.go) |
| 新增 | [internal/service/sourceimport_provenance_test.go](../../internal/service/sourceimport_provenance_test.go) |
| 新增 | [internal/service/sourceimport_proxy_acceptance_test.go](../../internal/service/sourceimport_proxy_acceptance_test.go) |
| 新增 | [internal/service/sourceimport_proxy_other.go](../../internal/service/sourceimport_proxy_other.go) |
| 新增 | [internal/service/sourceimport_proxy_test.go](../../internal/service/sourceimport_proxy_test.go) |
| 新增 | [internal/service/sourceimport_proxy_windows.go](../../internal/service/sourceimport_proxy_windows.go) |
| 新增 | [internal/service/sourceimport_proxy.go](../../internal/service/sourceimport_proxy.go) |
| 新增 | [internal/service/sourceimport_review_test.go](../../internal/service/sourceimport_review_test.go) |
| 新增 | [internal/service/sourceimport_technology_test.go](../../internal/service/sourceimport_technology_test.go) |
| 新增 | [internal/service/sourceimport_technology.go](../../internal/service/sourceimport_technology.go) |
| 新增 | [internal/service/sourceimport_test.go](../../internal/service/sourceimport_test.go) |
| 新增 | [internal/service/sourceimport_write.go](../../internal/service/sourceimport_write.go) |
| 新增 | [internal/service/sourceimport.go](../../internal/service/sourceimport.go) |
| 修改 | [internal/service/workbench_test.go](../../internal/service/workbench_test.go) |
| 修改 | [internal/service/workbench.go](../../internal/service/workbench.go) |
