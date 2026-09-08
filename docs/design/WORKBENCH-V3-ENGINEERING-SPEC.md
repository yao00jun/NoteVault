# NoteVault 工作台 v3 进阶工程规格书：去日记化、项目驱动与实战知识沉淀 (Workbench v3 Spec)

> 编写日期：2026-09-09
> 目标执行者：Codex Desktop / zcode / 全栈研发团队
> 状态：**设计定稿，待施工 (Ready for Implementation)**
> 核心定位：**破除传统笔记软件“日记本”思维，回归工程师“项目交付 + 实战沉淀”肌肉记忆**
> 底线保证：**纯 Markdown 文件存储（单一真理源）、零 CGO、无专有数据库锁定、现有功能与门禁 100% 保持全绿**

---

## 一、 为什么发起 v3 优化？三大核心重构原则

### 1.1 原则一：去日记化，以「工作日志 / 日报」替代「今日日记」
* **痛点**：传统知识库的“日记流 (Daily Notes)”让程序员极不适应。任务与日期绑定导致跨天碎片化；且系统内“日记一套、日报一套”造成重复劳动。
* **重构**：彻底摘除“日记”心智。侧边栏与工作台的 `今日日记` 全面收拢为 **`工作日志 / 每日汇报 (WorkLog)`**。今日勾选的任务切片与随手记进展，就是唯一的自然工作流水；下班一键生成的【邮件书面日报】，就是这一天的唯一归档。

### 1.2 原则二：任务认祖归宗，生于项目、归于项目
* **痛点**：待办如果写在日记里，随着日期翻篇，两周后想复盘某个项目做了哪些 US/DTS，需要翻多篇日记。
* **重构**：**任务天然属于【项目】**。所有待办（US/DTS/临时）落盘在对应项目的 `Projects/<项目名>/Tasks.md` 中。
* **工作台透视机制**：【今日工作台】本身不拥有任务，它只是一个**“今日透视镜”**，自动聚合当天分配给各个项目的任务；在今日勾选完成或记录进展，直接回写进项目的 Markdown，项目生命周期与研发进度条完全可追溯。

### 1.3 原则三：打通“实战 ➔ 资产”管道（知识库不再需要专门整理）
* **痛点**：平时研发忙，根本没时间单独坐在“知识库”页面写长篇大论，知识库往往荒废。
* **重构**：引入 **`⚡ 沉淀为知识 (Distill to Knowledge)`** 机制。
  - 在【项目】中写完技术方案、排错复盘或架构设计后，点击一键沉淀；
  - 自动向【技术书架 (Learning)】归档专题笔记，或向【面试宝典】生成带 `<!-- srs: ... -->` 的面试题卡；
  - 自动注入“实战项目 ↔ 理论题库”的双向链接。知识库在日常做项目中顺手滚雪球积累！

---

## 二、 界面与导航层改造规格

### 2.1 侧边栏重塑 (`frontend/src/components/layout/SideBar.vue`)
1. **动作区 (Action Zone)**：
   - 原 `📅 今日日记` 按钮文案与图标调整：
     - 图标：`Clock3` 或 `FileCheck2`；
     - 文案：`每日工作日志` / `工作汇报`；
     - 点击行为：直接打开今日的工作日志/日报文件 `Daily/Reports/YYYY-MM-DD-日报.md`（若未生成则提示并打开日报生成器），不再创建空洞的流水日记。
   - 保留 `+ 新建`（支持新建文档、项目、技术分册、资料导入）与 `📊 生成日报`（带今日完成计数角标）。
2. **工作流区与固定区**：
   - 保持 4 大核心入口平展：
     - `⚡ 今日 · 指挥中心`（`/today`）
     - `🚀 项目 · 组合看板`（`/projects`）
     - `📘 学习 · 技术书架`（`/learning`）
     - `📚 知识库 · 资产沉淀`（`/vault`）
   - 保持 Pinned 固定区与状态指示点（`● 本地索引实时`）。

### 2.2 今日指挥中心升级 (`frontend/src/views/TodayView.vue`)
1. **顶部操作条**：
   - 移除原有的 `今日日记` 独立按钮，保留 `生成/复制日报` 主按钮，并提供 `查看今日日志` 次要按钮；
2. **新增任务交互强化**：
   - “新建待办”表单中，项目下拉框默认选中用户当前正在进行的高频项目（避免创建无主孤儿任务）；若确实为通用杂务，归入项目通用组；
   - 任务创建后，明确保存到 `Projects/<项目名>/Tasks.md`；
3. **进展时间线 (Progress Timeline)**：
   - 点击任一任务卡片的“随手记一笔”，进展不仅附着在该任务下，而且实时反映在下方的时间线流中；下班点击生成日报时，自动汇总为邮件日报的完成/卡点要点。

---

## 三、 核心新特性：实战 ➔ 知识「⚡ 一键沉淀」管道

### 3.1 触发入口 (`EditorView.vue` / `MarkdownEditor.vue`)
* 当编辑器当前打开的文件位于 `Projects/` 目录下（即属于项目文档或排查复盘时）：
* 在编辑器顶部右侧操作栏（或浮动工具栏）显式渲染一个高亮微光按钮：
  ```html
  <button class="distill-btn" @click="openDistillModal">
    <Zap :size="14" />
    <span>沉淀为知识</span>
  </button>
  ```

### 3.2 沉淀弹窗组件 (`frontend/src/components/workbench/DistillKnowledgeModal.vue`)
点击按钮后弹出轻量模态框，提供两种沉淀模式：

#### 模式 A：沉淀为技术书架笔记 (Book Chapter)
* **目标分册**：下拉选择 `Learning/` 下现有的技术分册（如 `Learning/Go/`、`Learning/Java/`、`Learning/MySQL/`）或输入新建一个分册；
* **笔记标题**：默认带入当前文章标题（可微调，如 `Kafka消息积压与协程泄漏排查复盘.md`）；
* **关联机制**：
  - 在生成的目标笔记顶部注入标准元数据与反向双链：
    ```markdown
    ---
    type: tech-note
    sourceProject: "[[Projects/物流中台/project.md|物流中台]]"
    distilledAt: 2026-09-09
    tags: ["Go", "并发", "排错复盘"]
    ---
    > 📌 **实战案例来源**：[[Projects/物流中台/Kafka排查记录.md|物流中台项目实战]]

    (此处为沉淀的技术要点与复盘正文...)
    ```
  - 原项目文章末尾自动追加一条小徽章：
    `> 💡 **技术资产沉淀**：已沉淀至 [[Learning/Go/Kafka消息积压.md|Go进阶 - Kafka消息积压]]`。

#### 模式 B：沉淀为面试宝典题卡 (Interview Spaced Repetition Card)
* **适用场景**：将项目踩坑的核心考点转化为面试题；
* **字段配置**：
  - **面试考点题面**：如 `在高并发消费场景下，Kafka 消息堆积与协程泄漏如何定位与预防？`（可点击 AI 自动从当前笔记提炼）；
  - **核心解答提要**：自动提取或用户微调核心 1/2/3 结论；
  - **所属面试专题**：选择 `Learning/面试宝典/` 下的分类（如 `Go面试真题.md`、`架构与中间件.md`）；
* **落盘规范**：
  - 采用合规纯文本注入到对应题库文件末尾：
    ```markdown
    ### [Q{{ID}}] 在高并发消费场景下，Kafka 消息堆积与协程泄漏如何定位与预防？
    <!-- srs: {"level":"掌握","interval":7,"due":"2026-09-16","reps":1,"source":"Projects/物流中台"} -->

    #### 核心要点：
    1. 现象与定位：pprof 抓取 Goroutine 堆栈，排查无缓冲 Channel 读写阻塞；
    2. 解决思路：限制 WorkerPool 并发上限，消费超时注入 context.WithTimeout；
    3. 实战案例佐证：详见项目复盘 [[Projects/物流中台/Kafka排查记录.md|物流中台案例]]。
    ```
  - 此题卡立刻生效，进入面试宝典的 SRS 算法轮转体系中！

---

## 四、 后端服务支持 (`internal/service/workbench_distill.go`)

在 Go 后端扩展一个轻量、纯 Markdown 写入的沉淀接口：
1. **API 契约**：
   ```go
   type DistillRequest struct {
       SourceFile   string `json:"sourceFile"`   // 相对工作区路径，如 "Projects/A/debug.md"
       TargetMode   string `json:"targetMode"`   // "book" | "interview"
       TargetFolder string `json:"targetFolder"` // 如 "Learning/Go" 或 "Learning/面试宝典"
       TargetTitle  string `json:"targetTitle"`  // 标题
       Question     string `json:"question"`     // 仅 interview 模式
       Answer       string `json:"answer"`       // 仅 interview 模式
       Summary      string `json:"summary"`      // 提炼正文
   }

   func (s *TodoService) DistillKnowledge(workspacePath string, req DistillRequest) error
   ```
2. **安全与纯文件原则**：
   - 路径统一经过 `confineToWorkspace`，杜绝任何路径穿越；
   - 纯 Go 文件读写，更新或追加 Markdown，不触碰任何专有二进制库；
   - 更新后自动触发前端 `workspaceStore.incrementFileTreeVersion()` 与 `workbenchStore.refresh()`，看板即时响应。

---

## 五、 实施 Checklist 与质量门禁

请全栈研发助手（Codex / zcode）严格按部就班推进：

### Step 1: 侧栏与心智去日记化 (`SideBar.vue` + `TodayView.vue`)
- [ ] 将侧栏动作区的 `今日日记` 升级为 `每日工作日志 / 汇报`；
- [ ] `TodayView.vue` 移除多余的日记独立按钮，统一为 `查看工作日志` 与 `生成日报`；
- [ ] 确保新建任务默认附着在所选项目下，落盘在 `Projects/<项目名>/Tasks.md`。

### Step 2: 后端知识沉淀服务 (`internal/service/workbench_distill.go` + 测试)
- [ ] 实现 `DistillKnowledge` 方法，支持将项目文档沉淀为技术分册笔记或面试题卡；
- [ ] 编写单测 `workbench_distill_test.go`，验证双链注入与 SRS 注释合法性。

### Step 3: 前端沉淀交互组件 (`DistillKnowledgeModal.vue` + `EditorView.vue`)
- [ ] 开发 `DistillKnowledgeModal.vue` 弹窗（支持模式切换、分册选择、AI 提炼草稿）；
- [ ] 在 `EditorView.vue` 顶部操作栏中，当打开 `Projects/` 文件时动态渲染 `⚡ 沉淀为知识` 按钮；
- [ ] 编写前端组件测试。

### Step 4: 全套质量门禁自测
- [ ] 运行 `go test ./internal/...` 确保后端全绿；
- [ ] 运行 `cd frontend && pnpm typecheck` 确保 TypeScript 0 error；
- [ ] 运行 `cd frontend && pnpm test` 确保 59+ 套测试 100% 全部通过；
- [ ] 运行 `cd frontend && pnpm lint` 确保 0 error。

---

## 六、 预期终极体验提升

| 维度 | 改造前 | 改造后 (v3 终极态) |
|---|---|---|
| **日常记录心智** | 面对空白日记本不知写啥，日记日报两张皮。 | **无日记负担**！今天勾选的任务与进展就是日志，下班一键出日报归档。 |
| **任务与项目关系** | 任务漂浮在日期里，项目只是个静态文件夹。 | **任务归属项目**！工作台只是今天透镜，做完任务项目自动涨进度。 |
| **知识库如何维护** | 平时工作忙，空想整理知识库，最后荒废。 | **实战随手沉淀**！项目做完点一下 `⚡ 沉淀`，自动变成技术书和面试题。 |
| **面试与能力跃迁** | 背抽象八股文，面试容易忘。 | 面试题直接带着真实项目实操案例，理论实战双向穿透！ |
