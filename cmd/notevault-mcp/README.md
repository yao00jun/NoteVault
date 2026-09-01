# NoteVault MCP Server

让外部 AI Agent（Claude Code / Codex / Cursor 等）通过 **Model Context Protocol** 读取你的 NoteVault 笔记库。

- 零第三方依赖：手写 JSON-RPC 2.0 over stdio，不引入任何 MCP SDK。
- 纯 CLI 进程：只依赖 `internal/service`，编译产物是干净的命令行，可被任意 MCP 客户端作为子进程拉起。
- 默认只读：6 个 tool 中 5 个只读；仅 `create_note` 受 `--enable-write` 门控。

## 构建

```bash
# 在项目根目录
go build -o ./notevault-mcp ./cmd/notevault-mcp
```

## 运行

```bash
./notevault-mcp --workspace "E:\path\to\your\vault"
```

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--workspace` | （必填） | 笔记库根目录的**绝对路径**。 |
| `--enable-write` | `false` | 加上后解锁 `create_note`；不加则写操作一律拒绝。 |

> **路径注意（Windows）**：务必传原生绝对路径（如 `E:\WorkSpace\NoteVault`），不要用 Git Bash 风格的 `/e/WorkSpace/NoteVault`——Go 在 Windows 上无法识别后者，会报「not a directory」。

工作区不存在时进程以退出码 `2` 退出；未传 `--workspace` 时以退出码 `2` 退出。

## 客户端配置

### Claude Code（只读，推荐起步）

```bash
claude mcp add notevault -- notevault-mcp --workspace "E:\path\to\your\vault"
```

### Claude Code（允许创建笔记）

```bash
claude mcp add notevault -- notevault-mcp --workspace "E:\path\to\your\vault" --enable-write
```

### 通用 JSON 配置（含写权限）

```json
{
  "mcpServers": {
    "notevault": {
      "command": "notevault-mcp",
      "args": ["--workspace", "E:\\path\\to\\your\\vault", "--enable-write"]
    }
  }
}
```

> 若 `notevault-mcp` 不在 PATH 中，把 `command` 换成二进制的绝对路径。

## 提供的 Tools

| Tool | 只读 | 参数 | 说明 |
| --- | --- | --- | --- |
| `list_notes` | ✅ | `folder?`、`limit?` | 列出所有笔记，每行 `相对路径<TAB>标题`。`folder` 按路径前缀过滤。 |
| `read_note` | ✅ | `path`（必填） | 按相对路径读取笔记全文 Markdown。 |
| `search_notes` | ✅ | `query`（必填）、`limit?` | BM25 全文检索，返回带标题、路径、命中数与摘要的排名结果。 |
| `get_tags` | ✅ | — | 列出全库标签及出现频次，如 `#ai (12)`。 |
| `get_backlinks` | ✅ | `path?` / `title?` | 给定笔记（按 path 或 title），返回所有通过 `[[wikilink]]` 指向它的笔记。 |
| `create_note` | ⚠️ 需 `--enable-write` | `path`（必填）、`content?` | 原子创建新笔记；文件已存在即失败；未开 `--enable-write` 时调用报错。 |

所有 tool 复用的都是 NoteVault 已有的 `internal/service`（File/Search/Tag/Todo/Graph），**无需预建索引**，启动即扫即用。

## 协议说明

- 支持：`initialize`、`tools/list`、`tools/call`、`ping`，以及通知类消息（如 `notifications/initialized`）。
- 错误码遵循 JSON-RPC 2.0：未知方法 `-32601`、未知 tool / 参数错误 `-32602`、解析失败 `-32700`。
- 日志只写 **stderr**；所有协议响应走 **stdout**，不会污染客户端的 JSON 流。
- 任何内部 panic / 错误都被收敛为合法 JSON-RPC 错误响应，进程不会崩溃。
