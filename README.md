# NoteVault · 本地优先的个人知识库

> 像 Obsidian 一样管理你的 Markdown 知识库：双向链接、知识图谱、标签、日记、待办、提醒、归档、回收站，外加可插拔的 AI 总结。
> 全部数据以**纯 Markdown 文件**存于本地，无锁定、可长期保存。

NoteVault 是一个基于 **Go + Wails v3 + Vue 3** 的桌面应用。打开即是一个「知识库主页（Knowledge Hub）」，把你的笔记、待办、标签、图谱集中在一处，帮你真正搭起一套属于自己的知识体系。

---

## ✨ 核心特性

| 能力 | 说明 |
|---|---|
| **知识库主页 (Knowledge Hub)** | 默认入口。工作区概览、统计卡片（笔记/收藏/标签/待办/高优/已完成）、文档列表（搜索/排序/收藏过滤）、边栏快捷卡片（待办概览/标签云/最近编辑/快捷入口）。 |
| **本地优先** | 所有笔记就是 `.md` 文件，存在你选的工作区目录里。换电脑、换软件都能直接打开。 |
| **双向链接 + 反链** | 用 `[[笔记名]]` 链接其它文档，编辑器底部自动展示「哪些文档链接了我」。 |
| **知识图谱** | `/graph` 力导向可视化所有 `[[链接]]` 关系，高亮孤立节点与未解析链接，点击直达。 |
| **标签体系** | 支持 `#标签` 行内标签与 YAML front matter `tags:` 提取，标签云按使用频次着色。 |
| **待办 / 提醒** | 自动识别 Markdown 中的 `- [ ]` 待办（`!!` 标记高优先级），可设提醒并触发系统通知。 |
| **日记 (Daily Notes)** | 一键创建 `Daily/YYYY-MM-DD.md`，符合 Obsidian 习惯。 |
| **归档 / 回收站** | 软删除到 `.archive` / `.trash`，支持恢复与永久删除，回收站可定时清理。 |
| **AI 总结（可插拔）** | 接入任意 OpenAI 兼容 API（API Key 仅存本地），一键总结当前文档；未配置时友好提示。 |
| **导出** | 单篇导出为 Markdown 或单文件 HTML（内联样式），整个工作区可打包为 `.zip`。 |
| **多工作区** | 侧栏可切换 / 新建工作区，当前工作区持久化。 |

---

## 🚀 快速开始

### 开发模式

```bash
# 1. 安装依赖
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
# 后端 Go 单测
go test ./...

# 前端单测 + 类型检查
cd frontend
pnpm test        # vitest
pnpm typecheck   # vue-tsc（脚本名是 typecheck，中间没有连字符）
pnpm lint        # eslint
```

---

## 🧱 技术架构

```
NoteVault/
├── main.go                 # Wails 应用入口，注册所有 Service
├── *service.go            # Go 后端服务（12 个）
├── frontend/
│   ├── src/
│   │   ├── views/         # 页面（知识库/编辑器/搜索/标签/待办/提醒/归档/回收站/设置/图谱）
│   │   ├── components/    # 布局与编辑器组件
│   │   ├── stores/        # Pinia 状态（工作区/设置/标签页）
│   │   ├── composables/   # 复用逻辑（标签页/提醒通知）
│   │   ├── utils/         # 文本处理、图片粘贴等
│   │   └── router/        # 路由（hash 模式）
│   └── bindings/          # Wails 自动生成的 TS 绑定
├── docs/                  # PRD / HANDOFF / TODO
└── build/                 # 打包与发布配置
```

### 后端服务（Go，12 个）

`AppService` · `WorkspaceService` · `FileService` · `SearchService` · `TagService` · `TodoService` · `ReminderService` · `ArchiveService` · `TrashService` · `GraphService` · `ExportService` · `SummarizeService`

> 所有服务逻辑零外部依赖（仅 Go 标准库），AI 总结通过 HTTP 调用外部 API。

### 前端技术栈

Vue 3 (`<script setup>`) + TypeScript + Vite + Pinia + Vue Router + CodeMirror 6 + marked + lucide-vue-next

---

## 🔑 AI 总结配置

设置页 → **AI 配置**：

- **API Base URL**：OpenAI 兼容接口地址（如 `https://api.openai.com/v1`）
- **API Key**：仅保存在本机 `localStorage`，不上传任何服务器
- **模型**：如 `gpt-4o-mini`、`deepseek-chat`

配置后即可在编辑器工具栏点击「AI 总结」。

---

## 📌 产品愿景

NoteVault 不追求大而全，而是把**「写作 → 关联 → 回顾 → 行动」**形成闭环：

1. **写作**：纯 Markdown，无格式锁定。
2. **关联**：双向链接 + 图谱，让知识自然成网（链接 > 文件夹）。
3. **回顾**：知识库主页、日记、标签云、反链，随时回看。
4. **行动**：内置待办 / 提醒 / 归档 / 回收站，把知识变成行动。
5. **智能**：可插拔 AI 总结，本地 Key，隐私可控。

---

## 📄 许可证

© 2026 NoteVault. 个人知识管理用途。
