# NoteVault 开发进度与待办任务清单 (TODO)

> **文档版本**：v3.5.0  
> **更新时间**：2026-11-26  
> **当前状态**：核心功能已高度闭环（完成度约 94%~96%），生产构建零报错，90 个测试文件 / 1007 个用例全绿；**CI 五 job 全绿 + Release 流水线跨三平台完整验证**（v3.1.0 已发布 14 个产物：Win 3 / macOS 6 / Linux 5），`golangci-lint` 0 issues、eslint 0 errors；首屏 JS 入口 612KB → 305KB，并新增性能回归门禁。  
> **定位边界**：单机、本地优先的程序员第二大脑与研发工作台。纯 Markdown 存储，无厂商锁定。

---

## 一、明确不做（Scope Out · 边界定案）

以下内容属于**产品设计原则外的明确不实现项**，避免后续开发中被误当作遗漏项处理：

| 领域 | 决策 | 理由与替代方案 |
| :--- | :--- | :--- |
| **移动端（iOS / Android）** | ❌ 明确不做 | 定位为单机桌面端深度差异化工具，避免引入重型移动端云基建，构建模板已移除。 |
| **自动更新 / 版本检查服务器** | ❌ 明确不做 | 零中心化服务端。用户通过 GitHub Release 页面手动下载安装包或绿色包。 |
| **自研云端多端同步** | ❌ 明确不做 | 核心承诺是「纯 Markdown 零锁定」，同步完全交给 Git、Syncthing、坚果云或 iCloud 等现有文件同步工具。 |
| **SQLite + FTS5 引擎重构** | ❌ 明确不做 | 实测内存 BM25 倒排索引在 5,000+ 篇笔记下检索延迟 $<1.2\text{ms}$，坚持零 CGO、零外部数据库的立身之本。 |

---

## 二、核心已完成里程碑（Phase 0 ~ Phase 10 全段交付）

- [x] **研发工作台闭环（Today & Projects）**
  - [x] **今日工作台 (`/today`)**：4 大数字概览指标卡（今日任务、阻塞卡点、面试复习、到期提醒）、工作状态指示。
  - [x] **敏捷任务看板**：任务分类（US 用户故事 / DTS 缺陷单 / 额外任务 / 阻塞项 Blocker），支持勾选与原地编辑。
  - [x] **进展时间线 (Timeline)**：记录工作小步，为日报提供精准事实依据。
  - [x] **智能日报生成器**：一键提取今日完成与卡点，集成 LLM 润色，自动归档至 `DailyReport/`，支持纯净 HTML/邮件格式导出。
  - [x] **每日五题 (Daily SRS)**：艾宾浩斯遗忘曲线算法动态排期，三级反馈（掌握/模糊/不会），弱项重温。
  - [x] **项目组合看板 (`/projects`)**：扫描 `Projects/*`，解析 `project.md`（状态/技术栈/下一步）与 `Tasks.md`（进度计算）。
- [x] **知识管理与沉浸式编辑（Vault & Editor）**
  - [x] **PARA 结构化知识库 (`/vault`)**：Projects, Areas, Resources, Archives + Inbox 五大核心目录树浏览与拖拽移动。
  - [x] **CodeMirror 6 双链编辑器 (`/editor`)**：多标签页切换、1 秒防抖自动保存、粘贴图片自动转存 `assets/`。
  - [x] **双向链接体系**：`[[文档#标题^块ID]]` 解析与实时键入下拉补全（`autocompletion`）、反向链接面板（Backlinks）、H1~H4 文档大纲。
  - [x] **无限白板画布 (`/canvas`)**：兼容 Obsidian Canvas `.canvas` 开放格式，支持文本节点 Markdown 渲染、笔记引用卡片与自由连线。
- [x] **检索与知识智能（Discover & Insights）**
  - [x] **混合检索管线**：纯 Go 内存倒排索引 + BM25 关键词评分 + BGE-M3/Ollama 向量检索（RRF 0.4/0.6 融合），两段式懒加载 Snippet。
  - [x] **知识库 RAG 问答 (`/discover?tab=qna`)**：分块检索（Top-8 块）+ 混合召回 + 重排，答复精准溯源引用文档及章节。
  - [x] **多维表格 Bases (`.nvbase`)**：自研轻量属性查询引擎，支持表格/看板/列表三视图，强类型属性推断与多维条件组过滤。
  - [x] **知识编译流水线 (`CompileService`)**：AI 批量/单篇提炼 `Inbox/` 笔记，自动生成 TLDR、标签与双链建议并归档至 `Compiled/`。
- [x] **底层工程与安全防护**
  - [x] **版本快照时间机器 (`SnapshotService`)**：覆盖保存与删除前强制自动记录增量快照，纯 Go LCS 行级 Diff 对比与安全回滚（附带还原前二次备份）。
  - [x] **三层安全防护**：路径穿越校验（`confineToWorkspace`）、CSP 严格网络外带防护、XSS DOMPurify 过滤、Windows DPAPI 凭据安全存储。
  - [x] **数据信封与迁移 (`Envelope[T]`)**：统一元数据格式，自动向后兼容升级。
  - [x] **异步任务框架 (`TaskService`)** 与 **`fsnotify` 实时文件事件推送**。
  - [x] **外部生态接口**：Web Clipper 本地剪藏服务（`127.0.0.1:27123`）、MCP Server（14 个 stdio 工具：6 只读 + 8 写，供 Claude Code / Cursor / Codex 联动；写工具需 `--enable-write`）。

---

## 三、当前重点待办事项（Pending Tasks）

### 1. 【P1 · 体验完善】本地 Rerank（重排序）开箱即用支持补完 —— ✅ 已完成（2026-11-19）
- **原问题**：只有 Cohere（云端）一个 provider，本地用户在设置页开重排会被 404 静默降级。
- **本轮落地**：
  - [x] 新增 `llamacpp` 与 `tei` 两个本地 provider：前者走 llama-server `--reranking`（与 Cohere 同 schema），后者按 TEI 的 `query + texts → [{index,score}]` 独立构造与解析；请求体构造器与响应解析器被自检和真实检索共用，杜绝「自检假通过、检索真失败」。
  - [x] 设置页补 `llama-server` / `TEI` 两个预设按钮（一键填 Base URL 与模型，本机端点自动清空云端 Key），并补 i18n 与测试。
  - [x] `ProbeRerank` 强化：404 按 provider 给出**启动命令级**提示（`--reranking` / docker run 参数）；200 但零结果判定为不可用（提示加载的是 reranker 而非 chat 模型）；空 Base URL 不再静默落到 openai.com。
- **仍缺**：Ollama 原生 `/api/rerank` 上游未合并，故不提供该 provider（保持原有结论）。

### 2. 【P1 · 性能与交互】AI 知识编译异步化与进度/取消 —— ✅ 已完成（2026-11-19）
- **原问题**：`CompileService` 走同步接口（120s 超时），大文档或慢模型下前端长时间无反馈。
- **本轮落地**：
  - [x] `CompileAll` / `CompileNote` 保留同步语义（测试、MCP、旧调用方不受影响），新增 `StartCompileAll` / `StartCompileNote` 提交到 `TaskService`，逐篇回报进度；`CompileTaskResult(taskID)` 按任务 ID 取回结构化结果。
  - [x] 取消链路打通：`TaskService.Cancel` → ctx 取消 → 在途 LLM 请求同步中断（`summarizeCtx`），终态记为 `cancelled` 而不是 `failed`，被取消的那篇不计入失败明细。
  - [x] `CompileView` 增加进度条（百分比 + 当前文件）、取消按钮；完成/取消/失败分别提示，失败项在列表中就地标红。
- **仍缺 → 已补齐（2026-11-23）**：周报 AI 回顾已复用同一套任务流异步化——`StartWeeklyReport` 提交任务、结果按任务 ID 暂存由 `WeeklyReportTaskResult` 取回，取消时在途 LLM 请求中断、纯统计骨架仍优雅落盘；ReportsView 增加进度条 + 取消按钮，后端未注入队列时自动退回同步路径。任务队列依赖抽象为 `taskSubmitter`（原 `compileTaskQueue` 改名共享，Wails 绑定规避理由不变）。**今日 AI 简报同日跟进异步化（fd5e66a）**：`StartDailyBriefing` / `DailyBriefingTaskResult` 同模式，useBriefing 异步优先 + 同步兜底，TodayView 简报卡片加进度行与取消按钮；与周报不同，简报无降级语义——失败不暂存结果，错误随 task:finished 事件提示。至此三处 AI 长调用（编译/周报/简报）全部走任务队列，前端 lint 告警同日清零（51 → 0，no-v-html 经逐一核查在配置层豁免并写明 sanitizeHtml 契约）。

### 3. 【P2 · 界面布局】今日工作台宽屏空间利用 —— ✅ 已完成（2026-11-19）
- **本轮落地**：
  - [x] 新增 `TodayInsights` 手风琴（主栅格下方）：方案 1 = 迷你月历 + 近 4 周写作热力条（复用 `StatsService.GetWritingActivity`，一次取 35 天）；方案 2 = 结构速览（项目 / 任务 / 阻塞 / 待复习 / 提醒，全部由 workbench store 派生，零额外 IO）。
  - [x] 默认收起、同一时间只展开一块、展开状态记在 `localStorage`；5 个用例覆盖展开/互斥/记忆/数据派生。

### 4. 【P2 · 质量工程】测试 Mock 告警清理与并发调优 —— ✅ 已完成（2026-11-19）
- **本轮落地**：
  - [x] 新增 `src/test/apiMock.ts`（Proxy 兜底，不维护 Service 名单，避免随 Go 新增服务漂移）+ `src/test/setup.ts` 全局默认 mock；`ReportsView` / `TitleBar` 等漏声明 `CredentialService` 的工厂改用 `apiMock({...})`。
  - [x] `vitest.config.ts` 显式固定 `maxWorkers: 8`：**891 用例 / 33.8s**（2 worker 时 70.9s，历史 98s）；全量日志中 `No "xxx" export is defined` 告警从 36 条降到 0。
  - [x] 明确不采用 `isolate: false`（虽可再省约 30s）：跨文件共享模块状态会让失败数随机波动，不可复现的红灯不值得。

### 5. 【P3 · 自动化交付】CI/CD 多平台 Release 流水线 —— ✅ 已完成并经 Actions 首跑验证（2026-11-22，v0.3.0 全平台发布成功）
- **本轮落地**：
  - [x] 新增 `.github/workflows/release.yml`：`v*` tag 触发（支持 workflow_dispatch 重跑），矩阵构建 Windows（exe + NSIS 安装包）、macOS（arm64 / amd64 各一份 dmg）、Linux（tar.gz），并把 MCP 二进制一并产出。
  - [x] 统一产物命名 `notevault-<tag>-<platform>[-installer].<ext>`，`softprops/action-gh-release` 汇总发布并自动生成 release notes；预发布 tag 自动标记 prerelease。
- **首跑验证（2026-11-22，v0.3.0）**：三处初跑问题已修——① Linux 的 GTK4 依赖安装必须在 `go install wails3` 之前（编译 CLI 本身就要这些头文件）；② windows-latest 已不预装 NSIS，且 choco 的 nsis 包不自动建 shim，需把 `C:\Program Files (x86)\NSIS` 挂进 `GITHUB_PATH`；③ macos-14 / macos-13 runner label 均不可用（前者触发即被取消、后者永不排队），双架构改跑 macos-latest——arm64 原生 + amd64 走 Apple clang 交叉（MCP 二进制零 CGO，直接交叉编译）。终跑 4 构建 + 1 发布全绿，10 个产物齐全挂上 Release。
- **仍缺 → 已补齐（2026-11-23，v0.3.0 13 产物验证通过）**：Linux deb/rpm/AppImage 进流水线——deb/rpm 走 wails3 内置 nfpm（纯 Go，零 rpmbuild/dpkg 工具链），AppImage 设 `APPIMAGE_EXTRACT_AND_RUN=1`（runner 无 FUSE）。顺带修掉 nfpm.yaml 模板残留（包名 `notevault.exe` → `notevault`、maintainer 改静态值——Actions 不保证 `GIT_COMMITTER_*` 环境变量、license 对齐仓库实际的 Apache-2.0）；version 保留本地兜底默认值、CI 按 tag sed 覆写；`GOARCH` 需显式导出（nfpm.yaml 的 `arch: ${GOARCH}` 靠环境变量展开，Actions 默认不导出）；AppImage 实际输出名为 `<name>-<arch>.AppImage`（wails3 generate appimage 约定）。
- **版本号（已补齐）**：`core.AppVersion` 由 tag 经 `-ldflags -X` 注入，`windows:generate:syso` 用 `scripts/render-windows-info.sh` 渲染临时 info.json，故 exe 文件属性与二进制自报版本都跟随 tag；`build/config.yml` 的 version 仅作默认值，与 `core.DefaultAppVersion` 保持一致即可。
- **终轮补完（2026-11-23，f4b61a5，14 产物验证通过）**：
  - [x] **修复 dmg 无法启动的真 bug**：`Info.plist` 的 `CFBundleExecutable` 是模板残留 `notevault.exe`，.app 包内二进制实际为 `notevault`——此前发布的 dmg 装上后应用起不来（已随重跑替换为修复版）。
  - [x] `CFBundleVersion` / `CFBundleShortVersionString` 由 CI 按 tag 覆写（dmg 内应用版本与 Release 一致）；顺带清掉 GetInfoString/copyright 模板残留。
  - [x] Linux aur 二进制包（archlinux 格式，`pacman -U` 可直接安装），产物 14 个。
  - 踩坑：`sed -i` 在 macOS BSD 版会把 `-i` 后第一个参数当备份后缀吞掉（本地 GNU sed 验证通过但 CI 必挂）——版本注入改为「sed 输出临时文件 + mv」的跨平台写法。

### 6. 【P0 · 工程】CI 红灯修复与版本号治理 —— ✅ 已完成（2026-11-19）
- **原问题**：`main` 分支自 2026-09 起每次 push 都全红（4 个 job 无一通过），红灯被当噪声忽略，等于没有门禁。
- **本轮落地**：
  - [x] Go job：`//go:embed all:frontend/dist` 在干净 checkout 下无 dist 目录 → `go vet` 必挂。补一步空占位（`go:embed all:` 连空目录都不收）。
  - [x] Golangci-lint：`v2.6` 用 go1.25 编译，加载 go1.27 模块直接 `exit 3`（**配置从未真正生效**）→ 升 `v2.15.0`；随后暴露的 61 项历史告警全部清零（见下条）。
  - [x] Frontend/E2E：`pnpm install --frozen-lockfile` 报 `packages field missing or empty`。根因是 `frontend/pnpm-workspace.yaml` 只放 `overrides`（pnpm 10+ 才允许），CI 却用 pnpm 9 → 升 pnpm 12 并对齐本地；另修 `vitest` specifier 与锁文件不一致（`^5.0.0` vs `^5.0.3`）。
- **lint 清零口径**：errcheck 逐处显式处理（`_ = x.Close()`）；gosec 的 G301/G302/G115/G122 按「权限约定/值域由上游保证/威胁模型」在配置里排除并写明理由，G103/G204/G401/G505/G703 逐处 `nolint` 并标注原因；staticcheck 的 QF/ST 建议全部按建议改写，仅专有名词开头的错误串保留 `nolint`。

### 7. 【P1 · 工作台愿景】AI 转型工作流落地（里程碑结构化 + 交付物类型分组）—— ✅ 已完成（2026-11-23）
- **背景**：用户处境 = 白天项目开发 + 领导要求三个月内转 AI（需要补 Python/Java/SQL 等语言基础与 AI 应用知识，路径为依赖链）。经讨论定案：书架为主、路径只是参考文档；章节级粒度；里程碑走 frontmatter 结构化。
- **本轮落地（库骨架 + 代码各半）**：
  - [x] **库骨架（当前工作区）**：`Projects/AI转型/`（project.md 带 milestones frontmatter + Tasks.md 阶段任务）、`Learning/AI转型路径-13周.md`（四轨并行 × 章节级双链 × #est 估时 × 里程碑 × 「本期不做」，总预算 78h/规划 58h）、`Learning/Python基础/` 与 `Learning/AI应用开发/` 两本骨架书（15 章，每章带「本章目标」清单）。已有书（Java/SQL/面试宝典）真链直挂；注意双链目标是**文档名**（[[第1章 环境与工具]]），不是 [[书名#章节]]。
  - [x] **里程碑结构化**：`WorkbenchProject.Milestones`——project.md frontmatter `milestones:` 列表（`- [x] 名称 #due/2026-12-20`，勾选/日期可省略）解析投影；项目页头部显示条带（完成绿 / 逾期红 / 无日期按序展示）。
  - [x] **交付物类型分组**：交付物面板新增「按阶段 / 按类型」切换；类型走 frontmatter `type` 命名约定，新增 `ba-requirement`（BA 需求文档）与 `design-doc`（开发设计文档）标签。
  - [x] 测试：Go +1（里程碑四态解析）、前端 +2（类型分组切换、里程碑条带三态）；门禁全绿（898 用例）。
- **后续候选**（未排期）：Outlook 摄取一期（用户已确认桌面版 Outlook → COM 只读方案可行；一期先做今日会议条带 + 会议纪要模板 + 日报融合）、学习卡点 AI 拆解闭环（路径文档用顺后再上）、里程碑进今天页与周报。

### 8. 【P1 · 日报周报】工作周报生成器 + 日报里程碑融合 —— ✅ 已完成（2026-11-23）
- **背景**：用户反馈「每天写日报、每周写周报，好烦恼，不知道怎么写」。盘点后发现日报生成器已有（主题/今日完成/进行中/阻塞/明日计划，任务+进展自动拼装），真正的缺口是**工作周报完全没有**（知识周报是另一回事）。
- **本轮落地**：
  - [x] **工作周报生成器**：`buildWeeklyReport`——本周完成（completedAt 或进展记录落周内）→ 本周推进（周内有动作的进行中任务）→ 风险与求助（阻塞任务 + **逾期里程碑**）→ 里程碑进展（本周到期/已完成）→ 下周计划（下周窗口到期）。`isoWeekWindow` 用周四锚定年/周号，跨年边界正确（2027-01-01 → 2026-W53）。
  - [x] **后端读写**：`ReadWeeklyReport` / `SaveWeeklyReport`（`Daily/Reports/<week>-周报.md`，ISO 周标识 `2026-W47` 正则校验防穿越，expectedContent 并发保护与日报同款）。
  - [x] **WeeklyReportModal**：周切换（‹ › 上/下一周，周一写上周报的场景）+ 自动拼装/重新汇总 + 手动改写 + 显式归档保存 + 一键复制纯文本 + AI 润色（copilot 流，提示词约束六段结构与「不虚构」）。入口在报表页横幅。
  - [x] **日报融合里程碑**：`buildDailyReport` 新增可选里程碑入参——近 7 天内到期或已逾期的里程碑自动追加「近期里程碑」提醒段（无相关项不加段，报告不膨胀）。
  - [x] 测试：后端 +1（周报读写往返/非法周标识/并发冲突）、utils +3（ISO 窗口跨年、周报拼装、日报里程碑段）、组件 +3（生成归档/已归档稿保留/周切换重载）；门禁全绿（904 用例）。
- **测试踩坑记录**：jsdom 下 Teleport 必须 stub，否则测试整体挂起（非超时假象）；周报组件测试镜像 DailyReportModal.test 结构（真实 store + GetWorkbench mock + fake timers）最稳。
- **仍缺**：会议数据源（Outlook 摄取后，日报/周报可自动附「本周会议」段）；里程碑进今天页。

### 9. 【P1 · Outlook 摄取】日历会议只读接入（一期）—— ✅ 已完成（2026-11-23）
- **范围边界**：只「摄取」不「管理」——收发/回复/归档留在 Outlook；数据源 = 本机桌面版 Outlook COM（用户确认桌面版已装），邮件摄取二期再议。
- **本轮落地**：
  - [x] **OutlookService 只读日历**：Windows 走 COM（go-ole：`Outlook.Application → Session → GetDefaultFolder(9)`，`Items.Restrict` 按时间窗过滤，VT_DATE 经 `GetVariantDate` 转本地时间）；provider 接口化（`meetingProvider`），非 Windows 明确报错；测试注入桩。
  - [x] **20 秒超时护栏**：Outlook 有未处理完的首启对话框/配置向导时 COM 会无限阻塞（本机实测踩坑）——provider 外层 goroutine + select 超时，前端永远在有限时间内得到降级结果（超时后 COM goroutine 残留但数量有界）。
  - [x] **TodayView 今日会议条带**：时间/主题/地点 + 每条「纪要」按钮——一键创建 `Daily/Meetings/<date>-<主题>.md`（frontmatter 时间/地点/组织者，正文结论/待办/讨论骨架），已存在则直接打开；数据源不可用条带整体隐藏。
  - [x] **日报/周报会议段**：`buildDailyReport`「今日会议」（HH:mm-HH:mm 主题（地点））、`buildWeeklyReport`「本周会议」（带日期，超 15 条折叠尾注）；`fetchMeetingsSafe` 失败静默降级为空段，绝不阻断报告生成。
  - [x] 测试：后端 5 个（排序/边界校验/错误传播/空结果非 nil/文件名清洗）、utils 3 个、TodayView 2 个（条带渲染+创建纪要/数据源失败隐藏）；门禁全绿（909 用例）。
- **测试根因记录**：jsdom 下组件渲染抛错被 Vue 吞成 warn，表现为「元素缺失」而非报错——本次为测试数据字段大小写与绑定口径不一致（绑定是小写 json tag，测试数据误用大写字段 → `meeting.start.slice` 抛 TypeError → 渲染回滚）。定位手段：v-if 硬编码 true 分辨「渲染抛错」vs「数据未到」。
- **真机验证状态**：本机（开发机）COM 错误路径已实测（未启动 Outlook → 32s 后报「服务器运行失败」，超时护栏生效）；完整正向链路待用户在装有已配置 Outlook 的机器上验证——首次使用需启动桌面版 Outlook。
- **lint 盲区教训**：带 build tag 的平台文件会逃过本地另一平台的 lint（Windows 本地永远看不见 `!windows` 文件的告警，首跑 CI 才在 Linux 上暴露 ST1005）——平台文件里的错误串风格要与既有 nolint 口径对齐。**进阶**：语法错误同样会逃过本地编译（`!windows` 文件不参与 Windows 构建）——本地用 `GOOS=linux go build/vet ./internal/service/` 做跨平台检查（service 包零 wails 依赖，可独立编译；全仓 Linux 构建需 CGO 走 docker 路径）。
- **二期候选**：邮件摄取（旗标/未读 → Inbox 蒸馏流）、会议纪要自动带参会人列表、里程碑进今天页。

### 10. 【P1 · 里程碑闭环 + Outlook 二期】里程碑条带进今天页 + 旗标邮件摄取 —— ✅ 已完成（2026-11-23）
- **本轮落地**：
  - [x] **里程碑条带进今天页**：7 天内到期或已逾期的里程碑按日期排序展示（逾期标红、带归属项目），已完成/无日期/远期不进条带——数据由项目页同一份 frontmatter 投影派生，零额外存储。
  - [x] **旗标邮件摄取（Outlook 二期）**：`ListFlaggedEmails` 读收件箱 `olFlagMarked`（用户显式标记的待办信号，信号强度高于未读故未读不进一期），按接收时间倒序取前 N（默认 10/上限 50），正文压缩空行截 2000 字符作摘要；超时包装泛型化（会议/邮件共用）。
  - [x] **TodayView「待处理邮件」条带**：一键收录为 `Inbox/邮件/<接收日期>-<主题>.md`（frontmatter 带 source: outlook / from / received，正文要点 + 待办骨架）——进 Inbox 即可被知识编译蒸馏；收录后移出条带；**只读不动 Outlook 旗标状态**（清旗标留在 Outlook 内完成）。
  - [x] 测试：后端 +2（limit 钳制/错误传播与非 nil）、TodayView +3（里程碑三态筛选/邮件收录移除/数据源失败隐藏）；门禁全绿（912 用例）。
- **踩坑记录**：绑定字段大小写口径（小写 json tag）在邮件模板上重犯两次——`email.ReceivedTime` / `item.EntryID` 大写引用会静默产出 undefined（渲染抛错被 Vue 吞成 warn、filter 永不相等），批量替换时必须把 **item/item 侧** 的属性一并覆盖。
- **绑定漂移门禁立功**：重生成绑定后手工挑文件提交漏掉了 index.ts（新增类型导出），CI 的「regenerate + git diff --exit-code」门禁按设计拦截——**重生成后应用 git status 全量核对，不要手工挑文件**。
- **仍缺 → 已补齐（2026-11-23）**：①里程碑条带行改为可点按钮，点击直达所属项目页（`/projects?project=<folder>/project.md`）；②会议纪要 frontmatter 自动带 `attendees: [名单]`——COM 遍历 RequiredAttendees + OptionalAttendees 两个 Recipients 集合取 Name，空列表省略该行、单字段异常降级不阻断；③**里程碑一键完成**（`SetProjectMilestoneDone`：frontmatter 块内定位含里程碑名的列表行切换 `[ ]`↔`[x]`，正文同名标题不误伤、引号包裹的 YAML 行同样处理，写走 workbench 变更管线）——今天页行尾 ✓ 按钮 → mutation → store.refresh → 条带即时移出，不用跳项目页手改文档。
- **测试坑**：mock 的 store 状态（reactive 对象）在组件闭包里持有旧引用——模拟 refresh 时必须**原地修改** `harness.state.projects = [...]`，整体替换 `harness.state = reactive({...})` 不会触发组件更新。

### 11. 【P2 · Outlook 收尾】邮件全文按需摄取 + 参会人进纪要正文 —— ✅ 已完成（2026-11-23）
- **本轮落地**：
  - [x] `GetEmailBody`：COM `Namespace.GetItemFromID` 按 EntryID 定位邮件读**全文**（不截断），超时包装 + 非 Windows 桩 + 空 entryID 校验；邮件已删/移走返回明确错误。
  - [x] 收录邮件时先拉全文（`fetchEmailBodySafe` 失败降级为列表带回的 2000 字摘要），Inbox 笔记「要点」段用全文替代摘要——蒸馏流输入更完整。
  - [x] 会议纪要正文新增「参会人」名单行（frontmatter 的 attendees 之外补人读视图）。
  - [x] 测试：后端 +1（entryID 校验/透传/错误传播）、utils 断言扩展（全文替代摘要、参会人正文行）；门禁全绿（913 用例）。
- **二期剩余**：里程碑行跳转的锚点定位（跳到 frontmatter 对应行）。

### 11.5 【P1 · 学习闭环】学习卡点 AI 拆解——看不懂 X → AI 拆前置 → Inbox —— ✅ 已完成（2026-11-23）
- **闭环**：路径卡片「记录卡点」（概念 + 上下文）→ ReportService.StartDecomposeBlocker 异步任务调 LLM（严格 PRE: 名称|理由|时长 格式拆 3-5 个前置）→ 前置清单渲染 → 一键保存 Inbox/卡点/<date>-<概念>.md（前置为勾选清单）→ 知识编译蒸馏 / 学完勾掉。AI 失败/未配置降级为 Message 不阻断。
- **测试**：后端 6 个（解析/降级/生命周期/空 entryID 传播）、前端 2 个（提交渲染保存/空概念告警）；门禁全绿（918 用例）。

### 12. 【P1 · 学习路径】路径进度投影——勾选状态 → 完成度/分轨/节奏 —— ✅ 已完成（2026-11-23）
- **定位**：兑现「路径只是参考文档」的承诺——文档是唯一数据源（勾选直接在文档里改），应用只做投影不回写。
- **本轮落地**：
  - [x] **RoadmapService.GetRoadmapProgress**：扫描 Learning **顶层**带 `kind: roadmap` frontmatter 的 .md（书架 book.md 与子文件夹不受影响），按 `## 分节` 统计勾选与估时（`#est/Nh`），无勾选分节（总预算/本期不做）不输出为轨；总体 = 各轨聚合（done/total/percent/总估时）。
  - [x] **LearningView「路径进度」页签**：每份路径文档一张卡——标题/目标/截止/每周预算 + 总进度条 + 每轨 done/total + 估时对照；挂载即处于该页签时立即加载（watch immediate）；数据源为空给创建引导（指向 AI转型路径-13周.md 模板）、报错内联展示。
  - [x] 测试：后端 3 个（同构夹具：分轨统计/估时汇总/百分比/非轨分节跳过/子文件夹与 book.md 不受影响；空目录与非目录；非 roadmap 跳过）、LearningView 3 个（投影渲染/空态引导/错误内联）；门禁全绿（916 用例）。
- **二期候选**：与书架章节勾选的双向联动；里程碑行跳转的锚点定位。
- **下一步建议（已落地）**：`RoadmapTrack.Next` / `RoadmapProgress.NextStep`——分节内/跨轨第一个未完成项（勾选标记剥除，保留双链与估时），路径卡片进度条下方显示「下一步」徽章 + 章节双链 + 预估时长；全部完成时省略。

### 13. 【P0 · 性能工程】基准测试体系 + 大库压测 + CI 门禁 + 首屏包体 —— ✅ 已完成（2026-11-26）

- **背景**：性能结论此前只有零散的「5,000 篇 <1.2ms」这类点状验证，没有基准、没有回归门禁；
  首屏入口 chunk 612KB，其中约 60% 是从未被首屏使用的 CodeMirror。
- **基准体系**：`internal/service/perf_bench_test.go` 覆盖分词（gse 真实路径 + bigram 兜底）、
  建索引 100/1000/5000 三档、摘要落盘/重载、BM25 内核与 Search 全链路、混合检索
  （确定性向量桩，不依赖 Ollama）、快照 diff 与回滚；固定种子语料，各档互为前缀。
  基线 `benchmarks/baseline.txt`（83 个基准，Windows/7500F，`-count=3` 取最小）。
- **大库压测**：`perf_stress_test.go`（`NV_STRESS_TEST=1`，2 万篇／44MB）。关键数据：
  当前版本受 `maxScanFiles=5000` 约束，实际索引 5000 篇（`scanComplete=false`，
  已由状态栏暴露，不是静默截断）：建索引 5.2s、峰值分配 352MB、常驻 151MB、
  摘要 4.97MB、冷启动 503ms；推算 2 万篇全量：建索引 24s、峰值 978MB、常驻 499MB、
  摘要 20.3MB、冷启动 1.12s。**复杂度线性，无超线性退化**。
- **发现的优化点**（已写入 benchmarks/README.md）：
  ① 建索引 144KB/篇、8.5M 次分配 → 预分配与缓冲复用可直接砍大头；
  ② 摘要加载 5000 篇 205ms/136MB，Gob 同步阻塞，可并行/分片；
  ③ 全链路 Search 冷路径 P50≈380ms，而 BM25 内核亚微秒~2.1ms，差额几乎全在
  「每次 walk 全库 + 即时 snippet」——watcher 活跃时被 `skipRefreshable` 短路，
  但启动后首批查询与 watcher 积压时会付这笔钱；
  ④ 混合检索 1s/次 = `b.N=1` 的冷调用假象（含向量库惰性初始化，71.7K allocs）；
  CI 上 `b.N≈20` 的稳态是 56ms / 51.2K allocs。看稳态要用 `-benchtime=10x`。
- **三个候选已落地**（提交 `e73d396`，都先分段测量再动手，两条结论与原假设不同）：
  ① 摘要加载 205ms → **40ms**（5.1x）。分段计时证明「Gob 解码慢」是错的：
     解码只占 16ms（8%），81% 是解码后建 `tokenFreq` map 与倒排表。
     倒排表改为**按文档分片**（`inverted []map`，文档按 relPath CRC 归属一片），
     各 worker 独占一片、零合并；「并行构建 + 串行合并」实测仍要 67ms（合并即瓶颈）。
     代价是内存 +2.7%（每个 token 在多个分片各有一份 postings 头部）。
  ② 全链路 Search 371ms → **116ms**（3.2x）。冷路径实测构成：walk 320ms / 打分 0.5ms /
     snippet 30ms。**放弃了 TTL 扫描缓存**——「写完立刻搜」是常规操作，缓存窗口会让
     新内容搜不到；改为 `WalkDir` 收集路径 + **并行 stat**（各 worker 写局部 map 后合并），
     语义不变。无变更 refresh 320ms → 88ms。
  ③ 建索引 B/op 722MB → **661MB**（−8.4%），5.62s → 4.63s。`freqOf` 的 map hint 从
     「词次」改为「词次/2」省 40MB；`tokenizeIndex` 去掉整篇冗余 `strings.ToLower`
     （`segmentRuns` 内部已逐 rune 小写，long-zh 分词 238µs → 161µs）。
     分配量能降，**耗时由 gse 支配**：合并相邻中文段成单次 `CutAll`（token 序列
     1000/1000 一致）只快 6%，说明成本随长度线性增长、不是调用次数问题，未采用。
- **回归锁**：`searchindex_shard_test.go` 验证「默认分片」与「强制单分片」在同一语料上
  stats 与查询排序逐位相等；增量新增+删除+摘要往返后分片归属仍正确
  （防 posting 写错分片导致幽灵命中或 tokenCount 虚高）。
- **CI 门禁**：`scripts/benchcheck` + `Performance Benchmarks` job。耗时取 -count=3 最小值、
  3x 阈值（只拦算法级退化）；allocs 1.5x、B/op 2x；解析不到基准行视为失败。工具自带 9 个单测。
  **基线按平台分文件**（首跑发现的坑）：`baseline.txt` 本机 Windows / `baseline-linux.txt` CI。
  跨平台比值实测 0.02x~2.16x 且双向（HybridSearch 偏快、SnapshotDiff 与 bm25/5000 偏慢），
  混用等于阈值永远打不到；`goos:` 不一致时内存指标降级为「只报告」。
- **首屏包体**：新增 `plugins/activeEditor.ts`（只 `import type`，运行时零 CM 依赖），
  把活动编辑器注册表与选区读写从 `editorBridge` 拆出；三条首屏链路（App.vue 快捷键兜底、
  AI 抽屉、pluginRuntime store）改为引用轻量层、重操作按需加载。
  **入口 612KB → 305KB**，CM 只随编辑器路由加载。加打包边界回归锁（外壳静态引入
  editorBridge/@codemirror 即失败，做过变异测试）。顺带移除未使用的 mermaid
  （112 个传递依赖）与指向不存在文件的 npm script。
- **测试补盲**：BasesView / CanvasView 此前零直接测试，补 7 个文件 73 用例
  （渲染/视图切换/条件构建与防抖/保存删除；列表/节点连线/缩放/编辑/拖拽/新建/保存）。
- **修正**：`e2e/exe-smoke.sh` 的 `NOTEvault_DEBUG_PORT` 拼写错误——程序读
  `NOTEVAULT_DEBUG_PORT`，该路径从未被真正验证过却一直显示 PASS。

### 14. 【P2 · 可维护性】巨型组件拆分 —— ✅ 三档全部完成（2026-11-26）

- **先量了构成，结论和「行数」不一致**：
  | 文件 | 总行 | script | template | style | 真实问题 |
  | :--- | ---: | ---: | ---: | ---: | :--- |
  | `SettingsView.vue` | 2106 | 233 | 1142 | 731 | 模板+样式堆叠（逻辑早已抽进 4 个 composable） |
  | `EditorView.vue` | 1952 | 1019 | 379 | 555 | **逻辑过载**：87 个顶层定义、43 个函数、21 个 ref/computed、8 个 watch |
  | `i18n/locales/*.ts` | 2410×2 | — | — | — | 纯数据，按域拆文件收益低（构建期合并，运行时无感） |
- **第一轮（已完成）**：`SettingsView` 的「集成」分组（错误监控 / MCP / Web Clipper，正好是
  `sectionGroups` 里 integration 组的三个 children）抽成 `components/settings/IntegrationSettings.vue`。
  2106 → 1768 行，diff 仅 8 增 346 删。
- **两个隐性成本（这轮实测出来的）**：
  ① **scoped 样式不会跟着模板走**。子组件内元素拿不到父组件的 scope id，`.setting-item` /
  `.toggle-switch` 这类分节原语必掉样式。解法是 `settings-section.css` 作唯一定义点，
  父子各自 `<style scoped src>` 引入（编译两次、各套各的 scope id，产物里能验证）。
  ② **锚点 id 是隐式契约**。左侧导航用 `document.getElementById('settings-section-<key>')`
  定位，key 必须与 `sectionGroups.children` 逐字一致，子组件根元素上必须保留。
- **验证方式**：19 个 SettingsView 用例 + 全量 90 文件 1007 用例；typecheck/lint/构建；
  另用 Playwright 对比 computed style——父视图渲染的 `.setting-item` 与子组件渲染的逐项相等
  （background/border/radius/padding/display），开关仍是滑轨样式（截图人工复核三个区块）。
- **风险分级（下一轮的判断依据）**：
  - **低风险**：`EditorView` 里的纯函数（`slugifyHeading`、`cssEscape`、`parseWikiLinkFile`
    + `WIKI_LINK_RE`、`countMarkdownNotes`）→ 移到 `utils/`，无响应式依赖，可直接单测。
  - **中风险**：`EditorView` 的 wiki 链接/锚点导航簇（`pendingAnchor`、`handleWikiLinkClick`、
    `resolvePendingAnchor`、`tryScrollToPendingAnchor` + 滚动定位）→ 抽 composable；
    `EditorView.navigation.test.ts`（15 用例，`shallowMount` 全组件）能兜住。
  - **高风险**：标签页生命周期（`openFileByPath` / `closeTab` / 保存与冲突状态 / 路由同步）。
    这些状态被 template 直接引用 61 处，抽 composable 后要返回一大袋 state，
    「谁是真源」容易走样，且现有 9 个 fileOperations 用例覆盖不到冲突与并发保存。
    动之前应先补测试。
  - **不建议做**：`i18n` 语言包按域拆分——纯数据、构建期合并，唯一收益是 diff 更小。
- **第二轮（低风险档，提交 `bdf43f3`）**：把 `slugifyHeading` / `cssEscape` /
  `WIKI_LINK_RE` / `parseWikiLinkTarget` / `parseWikiLinkFile` 收敛到
  `utils/markdownText.ts`。真正的收益不是行数，而是**消掉两份逐字相同的副本**：
  前两个函数在 EditorView 与 MarkdownPreview 各有一份，它们决定
  「点 `[[note#标题]]` 能不能滚到那个标题」——补一处忘另一处就是「点了没反应」，
  而两份各自自洽、测试很难发现。`parseWikiLinkFile` 现在委托给
  `parseWikiLinkTarget().file`，出链与点击跳转从此不可能出现口径差异。
  新增 17 个用例（CSS 未定义时的兜底分支、别名内含 `#`/`^` 的先剥顺序、
  块 ID 取最后一个 `^`、`matchAll` 不共享 `lastIndex`）。
  EditorView −36 行、MarkdownPreview −82 行；入口包体不变（305.20 kB，
  utils 只被两个懒加载视图引用）。
- **第三轮（补测试 → 中/高两档一起做，2026-11-26）**
  - **先补的三类测试（14 个用例）**：
    - `EditorView.anchorTiming.test.ts`（6）：用**手动帧泵**（stub `requestAnimationFrame`）
      而不是等 `setTimeout`——锚点的正确性取决于「第几帧能看到目标」，真实计时器只能测出
      「最终跳到了」，测不出「第一帧没渲染出来时会不会重试」。锁住的契约：第一帧未命中→
      第二帧重试；两帧都未命中→警告一次并**清空 pending**（否则下次切标签会被粘回去）；
      目标标签已打开时同步跳（不等 watcher）；`data-heading-text` 优先、`textContent`
      兜底时剥掉注入的 `¶`；块引用走 `[data-block-id]`。
    - `EditorView.conflict.test.ts`（5）：视图层横幅与三个出口的接线（丢弃重载 /
      另存副本 / 行级对比），并断言冲突期间**一律不写盘**；干净标签收到外部变更不弹横幅。
      此前只有 `useEditorDraft` 的状态机单测，按钮接线断了测试也发现不了。
    - `useEditorDraft.test.ts` +3：跨文档并发保存时 `isSaving` 不提前归零；
      写入中关闭标签 → 结果丢弃且不留下 `saveErrors`；写入中切工作区 → 不把旧会话的失败
      记到新工作区头上。
  - **中风险档**：`useEditorAnchorNavigation.ts`（`pendingAnchor` / 两帧重试 / 清空规则 /
    wiki-link 点击 / `?anchor=` 引用 / 大纲滚动）。时序契约从视图里搬进 composable，
    由上面 6 个用例锁定。
  - **高风险档**：`useEditorTabs.ts`（tabs / activeTabIndex / 打开 / 切换 / 关闭 / 路由同步 /
    延迟打开队列）。真正的收益不是行数，而是**索引不变式只剩一份实现**：
    「删掉第 i 个标签后 activeTabIndex 怎么变」原先在 `closeTab` 与「编译后标签跟随」
    两处各写一遍，漏改一处就是关错标签；现在统一走 `removeTabAt()`。
    新增 `useEditorTabs.test.ts`（6）：同一文档只占一个标签、晚到的读盘结果被丢弃
    （版本号守卫）、关左侧标签后当前文档不变、关当前标签落到邻居、保存失败拒绝关闭、
    Windows 大小写别名复用同一标签。
  - **踩到的坑（值得记）**：标签层与草稿层互相依赖，用「延迟句柄」破环时一开始写成了
    普通 `let`——`watch(applyInProgress)` 建立时读到 `undefined` 之后再也不会被追踪，
    rename 结束时的排队打开永远不执行（4 个 fileOperations 用例里立刻挂了 2 个）。
    改成 `shallowRef` 后恢复。**跨 composable 的延迟依赖必须是响应式的**，否则 watch 静默失效。
  - 结果：`EditorView.vue` 1952 → 1663 行（−289），单测 91 文件 / 1024 用例 →
    **94 文件 / 1044 用例**；typecheck / lint / 生产构建干净，入口包体仍 305.20 kB。

---

## 四、验证基线（2026-11-19）

| 门禁 | 命令 | 结果 |
| :--- | :--- | :--- |
| Go 编译/静态检查 | `go vet ./...` | 通过（含干净 checkout + 空 dist 占位） |
| Go 单测 | `go test ./internal/... ./cmd/...` | 全包 ok |
| Go lint | `golangci-lint run`（v2.15.0） | **0 issues** |
| 性能基准 | `go test ./internal/service/ -run '^$' -bench ... -count=3` | 83 个基准，基线已入库 |
| 大库压测 | `NV_STRESS_TEST=1 go test ... -run TestStressIndexLargeVault` | 2 万篇 7 分钟内跑完 |
| 前端单测 | `pnpm test` | **90 文件 / 1007 用例 / 28.0s**（2026-11-26） |
| 类型检查 | `pnpm typecheck` | 干净 |
| 前端 lint | `pnpm lint` | 0 errors（51 warnings 不阻断） |
| 打包 | `wails3 task package` | exe 50.3M / mcp 37.1M / installer 17.0M |
| 版本注入 | `wails3 task windows:build VERSION=v0.2.0` | exe 属性与 `core.AppVersion` 均为 0.2.0 |
