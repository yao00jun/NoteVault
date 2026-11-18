/**
 * 全局测试初始化（vitest setupFiles）。
 *
 * 职责只有一件事：给 `@/api` 装一个**全桩兜底**。
 *
 * 起因：`vi.mock('@/api', factory)` 是整模块替换，工厂里漏掉的 Service 在访问时
 * 会抛 “No "XxxService" export is defined on the "@/api" mock”。而 settings store
 * 在初始化阶段会碰 CredentialService（恢复凭据）、ClipperService（推送 AI 配置），
 * 于是用例全绿、退出阶段却刷一串 Unhandled Error —— 既污染日志，也掩盖真正的失败。
 *
 * 这里注册的默认 mock 作用于所有用例；需要具体行为的用例仍应自己
 * `vi.mock('@/api', () => apiMock({ ... }))` 覆盖（后注册者生效）。
 * 之所以不在这里写死 Service 名单：名单会随 Go 侧新增服务漂移，漂移即回归。
 */
import { vi } from 'vitest'
import { apiMock } from './apiMock'

vi.mock('@/api', () => apiMock())
