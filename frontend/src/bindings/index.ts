// Wails 绑定聚合层（services）
//
// 为什么需要这一层：
//   wails3 的绑定是按 Go 包生成的。项目的服务都在 internal/ 子包里，
//   所以 build/Taskfile.yml 必须用 `./...` 扫描（否则子包服务不会被采集），
//   而多包扫描的输出是嵌套结构：
//     frontend/bindings/github.com/notevault/notevault/internal/{app,plugin,infra/monitor,service}/
//   且 `-clean` 默认为 true——每次构建都会清空整个 bindings 目录重新生成。
//
// 因此业务代码不能直接 import bindings 下的路径：一是会耦合这个易变的包结构，
// 二是 bindings 目录里任何手工文件都活不过一次构建。
//
// 这里把所有服务聚合成一个扁平出口，业务代码继续用
//   import { FileService } from '@bindings/github.com/notevault/notevault/index.js'
// 由 tsconfig / vite 的 alias 指到本文件（见 vite.config.ts 与 tsconfig.json）。
// 新增 Go 服务时，在这里补一行导出即可。

export { AppService } from "@bindings/github.com/notevault/notevault/internal/app/index.js";

export { ErrorMonitor } from "@bindings/github.com/notevault/notevault/internal/infra/monitor/index.js";

export { PluginService } from "@bindings/github.com/notevault/notevault/internal/plugin/index.js";

export {
  ArchiveService,
  BaseService,
  CompileService,
  CredentialService,
  ExportService,
  FileService,
  GitService,
  GraphService,
  ImportService,
  LLMConfigService,
  QnAService,
  ReminderService,
  ReportService,
  SearchService,
  SnapshotService,
  StatsService,
  SummarizeService,
  TagService,
  TemplateService,
  TodoService,
  TrashService,
  WorkspaceService,
} from "@bindings/github.com/notevault/notevault/internal/service/index.js";
