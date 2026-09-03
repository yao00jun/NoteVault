/**
 * Workspace 绑定类型与前端领域类型的桥。
 *
 * 生成的 Wails 绑定（bindings/.../models.ts 的 Workspace interface）与
 * @/types 的 Workspace 结构逐字段一致，但前者来自"每次构建重新生成"的
 * 目录，业务代码不应直接 import（见 src/bindings 的分层说明）。
 * 此前 8+ 处调用点用 `as any` 强转掩盖这一点；收敛为一个显式断言函数后，
 * 两个类型将来若漂移，编译器会在这一处报错而不是静默放过。
 */
import type { Workspace } from '@/types'

type WorkspaceBinding = {
  id: string
  name: string
  path: string
  createdAt: string
  lastOpenedAt: string
}

export function toWorkspace(ws: WorkspaceBinding | null | undefined): Workspace | null {
  return ws ?? null
}

export function toWorkspaceList(list: WorkspaceBinding[] | null | undefined): Workspace[] {
  return list ?? []
}
