/**
 * 统一的 `@/api` 测试替身工厂。
 *
 * 为什么需要它：`vi.mock('@/api', factory)` 是**整模块替换**——工厂里没列出的导出，
 * 在被访问时会抛 “No "XxxService" export is defined on the "@/api" mock”。
 * 而 settings store 在初始化时会碰 CredentialService / ClipperService，
 * 于是用例虽然全绿，退出阶段还是会刷一串 Unhandled Error。
 *
 * 用法：
 * ```ts
 * vi.mock('@/api', () => apiMock({ CompileService: { ListInbox: vi.fn() } }))
 * ```
 * 未显式声明的 Service 会自动得到一个「任意方法都返回 Promise<undefined>」的桩，
 * 需要断言的 Service 仍应显式传入真实的 vi.fn()。
 *
 * 之所以用 Proxy 而不是维护一份 Service 名单：名单会随 Go 侧新增服务而漂移，
 * 漂移的那一刻就重新长出告警——这正是本文件要解决的问题。
 */
type ServiceStub = Record<string, (...args: unknown[]) => Promise<undefined>>

function serviceStub(): ServiceStub {
  return new Proxy({} as ServiceStub, {
    get: (target, prop) => {
      // 让 vitest / 断言库探测模块时拿到稳定的东西：
      // then 必须为 undefined，否则它会被当成 thenable 而 await 住。
      if (prop === 'then') return undefined
      if (typeof prop === 'symbol') return undefined
      if (!(prop in target)) {
        target[prop] = () => Promise.resolve(undefined)
      }
      return target[prop]
    },
  })
}

export function apiMock(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return new Proxy(overrides, {
    get: (target, prop) => {
      if (prop === 'then') return undefined
      if (typeof prop === 'symbol') return undefined
      if (prop in target) return target[prop]
      const stub = serviceStub()
      target[prop as string] = stub
      return stub
    },
  })
}
