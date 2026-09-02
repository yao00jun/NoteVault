// Wails 绑定聚合层（models）
//
// 与同目录 index.ts 同理：wails3 按 Go 包生成绑定，models 分散在
// internal/core 与 internal/service 两个包里，且 bindings 目录每次构建都会被清空重生成。
// 这里把它们聚合成一个扁平出口，业务代码继续用
//   import type { TodoItem } from '@bindings/github.com/notevault/notevault/models.js'
//
// 两个包的 models 之间无重名（core 4 个 + service 37 个，共 41 个）。
// 若将来出现重名，`export type` 会直接报重复导出错误——届时需要显式改名再导出。
//
// 维护约定：**新增 Go model 必须同步补进下面的列表**。漏补不会在 Go 侧报错，
// 而是等到前端 import 时才炸出 "Module has no exported member"（P1-6 实际踩过）。
// 核对方式：比对本文件与 bindings/.../internal/service/models.ts 的导出清单。

export type {
  ErrorMonitorConfig,
  ErrorReport,
  PluginInfo,
  PluginManifest,
} from "@bindings/github.com/notevault/notevault/internal/core/models.js";

// PropKind 是 Go 侧的枚举（enum），不是纯类型：
// 前端要拿它的值做类型化渲染（数字右对齐、标签渲染成 chip），
// 所以这里必须用值导出，`export type` 会把运行时成员擦掉。
export { PropKind } from "@bindings/github.com/notevault/notevault/internal/service/models.js";

export type {
  ArchivedFile,
  BaseCell,
  CompileAllResult,
  CompileErrorItem,
  CompileOutput,
  CompileResult,
  BaseDef,
  BaseFilter,
  BaseFilterGroup,
  BaseGroup,
  BaseResult,
  BaseRow,
  BaseSort,
  BaseSummary,
  BaseView,
  BuiltinTemplate,
  DiffOp,
  FileNode,
  GitStatus,
  GraphData,
  GraphEdge,
  GraphNode,
  ImportOptions,
  ImportResult,
  LLMEndpointPreset,
  LLMProbeResult,
  PropertyMeta,
  QnACitation,
  QnAResponse,
  Reminder,
  SearchIndexStats,
  SearchResult,
  Snapshot,
  SnapshotDiff,
  SnapshotFileSummary,
  SnapshotRestoreResult,
  SnapshotStats,
  TagFileInfo,
  TagInfo,
  TemplateInfo,
  TodoItem,
  TrashedFile,
  Workspace,
} from "@bindings/github.com/notevault/notevault/internal/service/models.js";
