# NoteVault · 本地优先的个人知识库

> 像 Obsidian 一样管理你的 Markdown 知识库：双向链接、知识图谱、标签、日记、待办、提醒、归档、回收站、Canvas 白板，外加可插拔的 AI 能力（总结 / 问答 / 语义检索）。
> 全部数据以**纯 Markdown 文件**存于本地，无锁定、可长期保存。

NoteVault 是一个基于 **Go + Wails v3 + Vue 3** 的桌面应用。打开即是一个「知识库主页（Knowledge Hub）」，把你的笔记、待办、标签、图谱集中在一处，帮你真正搭起一套属于自己的知识体系。

---

## ✨ 核心特性

| 能力 | 说明 |
|---|---|
| **知识库主页 (Knowledge Hub)** | 默认入口。工作区概览、统计卡片、文档列表（搜索/排序/收藏过滤）、边栏快捷卡片（待办概览/标签云/最近编辑/快捷入口）。 |
| **本地优先** | 所有笔记就是 `.md` 文件，存在你选的工作区目录里。换电脑、换软件都能直接打开。 |
| **双向链接 + 反链** | `[[笔记名]]`、`[[笔记#标题]]`、`[[笔记^块ID]]`、嵌入 `![[...]]`，编辑器底部自动展示「哪些文档链接了我」，图谱与预览共用同一套 Obsidian 口径解析。 |
| **知识图谱** | 力导向可视化所有链接关系，高亮孤立节点与未解析链接，点击直达。 |
| **语义检索** | 本地向量检索（BGE-M3 嵌入）+ BM25 关键词检索混合召回，支持可选 Rerank 重排；配合全文检索实现「词面不重叠也能命中」。 |
| **AI 问答 (QnA)** | 基于你的笔记库做检索增强问答（RAG），答案可溯源到具体笔记。 |
| **AI 总结（可插拔）** | 接入任意 OpenAI 兼容 API（API Key 仅存本地），一键总结当前文档。 |
| **Canvas 白板** | Markdown 驱动的白板视图，卡片自由布局与连线。 |
| **插件系统** | 可插拔插件架构（JS 插件 + 权限声明 + 信任管理），插件可向编辑器注入工具栏按钮。 |
| **版本历史** | 内置 Git 快照与历史对比（diff），笔记改动可追溯、可回滚。 |
| **待办 / 提醒** | 自动识别 Markdown 中的 `- [ ]` 待办（`!!` 标记高优先级），可设提醒并触发系统通知。 |
| **日记 / 模板** | 一键创建 `Daily/YYYY-MM-DD.md`，支持自定义笔记模板。 |
| **报告 / 回顾** | 批量笔记总结与定期回顾。 |
| **归档 / 回收站** | 软删除到 `.archive` / `.trash`，支持恢复与永久删除，回收站可定时清理。 |
| **导入 / 导出** | 单篇导出为 Markdown 或单文件 HTML（内联样式），工作区可打包 `.zip`，支持外部笔记导入。 |
| **多工作区** | 侧栏可切换 / 新建工作区，当前工作区持久化，支持文件变更实时监听。 |

---

## 🚫 范围边界（明确不做）

NoteVault 是**单机、本地优先的桌面知识库**。以下三项是产品范围外的**定案**，不是待办：

| 项目 | 说明 |
|---|---|
| **移动端** | 不做 iOS / Android。桌面单端做差异化，不跟 Obsidian 正面竞争 |
| **自动更新 / 版本检查** | 不做。无发布服务器，不引入更新通道，用户手动下载 Release |
| **多端同步** | 不做自研同步（WebDAV 等）。只内置 Git 能力，同步交给 Git / Syncthing / 网盘——与「纯 Markdown、无锁定」的定位一致 |

---

## 🚀 快速开始

> 📖 **用户手册**：安装、上手与全部功能说明见 [docs/UserManual.md](docs/UserManual.md)

### 环境要求

- Go 1.25+
- Node.js 22+ 与 pnpm 9+
- [Wails v3 CLI](https://wails.io/)

### 开发模式

```bash
# 1. 安装前端依赖
cd frontend && pnpm install

# 2. 启动前端开发服务器（热重载）
pnpm dev

# 3. 另开终端启动 Wails 应用
wails3 dev
```

### 生产构建

```bash
# 一键构建（含前端打包 + Go 编译）
wails3 build

# 产物：bin/notevault.exe
```

### 运行测试

```bash
# 后端 Go 单测 + vet
go vet ./...
go test ./...

# 前端单测 + 类型检查
cd frontend
pnpm test        # vitest
pnpm typecheck   # vue-tsc
pnpm lint        # eslint
```

CI（GitHub Actions）覆盖：`go vet` / `go test` / `golangci-lint` / 前端 vitest + vue-tsc + 构建 / Playwright E2E。

---

## 🧱 技术架构

```
NoteVault/
├── main.go                     # Wails 应用入口，注册所有 Service
├── internal/
│   ├── app/                    # 应用装配：服务容器、依赖注入、文件变更/任务事件
│   ├── core/                   # 领域核心：模型、错误、版本
│   ├── platform/               # 平台层：凭据加密存储、进程锁（Windows 适配）
│   ├── service/                # 业务服务（38 个）：文件/检索/图谱/待办/提醒/向量/AI 等
│   ├── plugin/                 # 插件系统：加载、权限、信任、内置插件
│   ├── mcp/                    # MCP 协议实现（JSON-RPC 2.0 over stdio）
│   ├── infra/                  # 基础设施：文件监控、schema、fs 工具
│   └── security/               # CSP 等安全策略
├── cmd/notevault-mcp/          # MCP Server 独立二进制（供 Claude Code / Cursor 等接入）
├── frontend/                   # Vue 3 + TypeScript 前端
│   ├── src/views/              # 页面（知识库/编辑器/搜索/QnA/Canvas/图谱/标签/待办/提醒/插件/历史/报告/设置等）
│   ├── src/components/         # 布局与编辑器组件（CodeMirror 6）
│   ├── src/stores/             # Pinia 状态
│   └── bindings/               # Wails 自动生成的 TS 绑定
├── build/                      # Wails 构建模板：图标、各平台打包配置（NSIS/WiX）
├── scripts/                    # build.bat / dev.bat 快捷脚本
└── e2e/                        # exe 级启动冒烟测试
```

### 后端服务（Go）

按分层架构组织：`core`（领域模型）← `service`（业务逻辑，零 UI 依赖）← `app`（装配）← `platform`（平台能力）。核心服务包括：

- **文件与工作区**：`FileService` · `WorkspaceService` · `WorkspaceWatcher` · `TemplateService` · `GitService`（版本快照）· `SnapshotService`
- **检索与知识**：`SearchService`（全文/混合检索）· `SearchIndex` · `VectorStore` · `EmbeddingClient` · `RerankClient` · `GraphService`（双链/图谱）· `TagService` · `PropertyIndex`
- **AI 能力**：`SummarizeService` · `QnAService`（RAG 问答）· `LLMClient` · `LlmEndpoint`
- **任务与回顾**：`TodoService` · `ReminderService` · `ReportService` · `ReviewService` · `CompileService`
- **内容管理**：`ArchiveService` · `TrashService` · `ExportService` · `ImportService` · `StatsService`
- **插件与扩展**：`PluginService`（JS 插件加载 / 权限 / 信任）· `MCP Server`

### 前端技术栈

Vue 3 (`<script setup>`) + TypeScript + Vite + Pinia + Vue Router + CodeMirror 6 + marked + lucide-vue-next

---

## 🔌 MCP Server（外部 AI 接入）

NoteVault 附带独立的 MCP Server，让 Claude Code / Codex / Cursor 等 AI Agent 直接读写你的笔记库：

```bash
# 构建
go build -o ./notevault-mcp ./cmd/notevault-mcp

# 运行（默认只读；--enable-write 解锁 create_note）
./notevault-mcp --workspace "E:\path\to\your\vault"
```

零第三方依赖（手写 JSON-RPC 2.0 over stdio），详见 [`cmd/notevault-mcp/README.md`](cmd/notevault-mcp/README.md)。

---

## 🔑 AI 总结配置

设置页 → **AI 配置**：

- **API Base URL**：OpenAI 兼容接口地址（如 `https://api.openai.com/v1`）
- **API Key**：仅保存在本机（凭据经平台加密存储），不上传任何服务器
- **模型**：如 `gpt-4o-mini`、`deepseek-chat`

配置后即可在编辑器工具栏点击「AI 总结」。

---

## 📌 产品愿景

NoteVault 不追求大而全，而是把**「写作 → 关联 → 检索 → 回顾 → 行动」**形成闭环：

1. **写作**：纯 Markdown，无格式锁定。
2. **关联**：双向链接 + Canvas + 图谱，让知识自然成网（链接 > 文件夹）。
3. **检索**：全文 + 语义双通道，「忘词也能找回那篇笔记」。
4. **回顾**：知识库主页、日记、报告、版本历史，随时回看。
5. **行动**：内置待办 / 提醒 / 归档 / 回收站，把知识变成行动。
6. **智能**：可插拔 AI 与插件系统，本地优先，隐私可控。

---

## 📄 许可证

[Apache License 2.0](LICENSE)
