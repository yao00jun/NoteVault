/**
 * Wails 后端服务统一出口 —— API 防腐层（蓝图专项 7）。
 *
 * 业务视图 / 组件 / store 一律从 `@/api` 导入后端服务与模型类型，
 * 禁止裸 import `@bindings/...`（eslint no-restricted-imports 强制）。
 * 上游 Wails Beta 迭代导致绑定签名变更时，只需在本层适配一处。
 */
export * from '@bindings/github.com/notevault/notevault/index.js'
export * from '@bindings/github.com/notevault/notevault/models.js'
export type { RerankProvider } from '@bindings/github.com/notevault/notevault/internal/service/models.js'
export * from './workbench'
