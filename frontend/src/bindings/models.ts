// Wails 绑定聚合层（models）
//
// 与同目录 index.ts 同理：wails3 按 Go 包生成绑定，models 分散在
// internal/core 与 internal/service 两个包里，且 bindings 目录每次构建都会被清空重生成。
// 这里把它们聚合成一个扁平出口，业务代码继续用
//   import type { TodoItem } from '@bindings/github.com/notevault/notevault/models.js'
//
// 两个包的 models 之间无重名（core 4 个 + service 16 个，共 20 个）。
// 若将来出现重名，`export type` 会直接报重复导出错误——届时需要显式改名再导出。

export type {
  ErrorMonitorConfig,
  ErrorReport,
  PluginInfo,
  PluginManifest,
} from "@bindings/github.com/notevault/notevault/internal/core/models.js";

export type {
  ArchivedFile,
  FileNode,
  GraphData,
  GraphEdge,
  GraphNode,
  ImportOptions,
  ImportResult,
  QnACitation,
  QnAResponse,
  Reminder,
  SearchResult,
  TagFileInfo,
  TagInfo,
  TodoItem,
  TrashedFile,
  Workspace,
} from "@bindings/github.com/notevault/notevault/internal/service/models.js";
