import type { PluginStartInfo, PluginTransport } from './types'
import { createWorkerSource } from './workerSource'

type Listener = (event: { data?: unknown }) => void

function addListener(
  target: Map<string, Listener[]>,
  type: string,
  listener: Listener,
): void {
  const list = target.get(type) ?? []
  list.push(listener)
  target.set(type, list)
}

function removeListener(
  target: Map<string, Listener[]>,
  type: string,
  listener: Listener,
): void {
  const list = target.get(type)
  if (!list) return
  const index = list.indexOf(listener)
  if (index >= 0) list.splice(index, 1)
}

/**
 * createMainThreadTransport 在主进程（WebView）上下文里执行插件，
 * 是 full-trust 插件的执行通道，与 Worker 沙箱通道平级。
 *
 * 与 Worker 通道的本质区别：
 *   - 插件代码通过 new Function 执行，其作用域链是全局作用域，
 *     因此能访问 document / fetch / localStorage 等一切浏览器能力——
 *     这正是「完全信任」模式的意义，也是它必须逐插件显式授权的原因；
 *   - 不再注入沙箱禁用逻辑（trusted=true）。外联改由 CSP 的
 *     connect-src / img-src 兜底（见 internal/security/csp.go）；
 *   - 与宿主仍走同一套消息协议，所以 PluginRuntimeHost 不需要区分插件跑在哪。
 *
 * 代价：插件崩溃或死循环会直接影响宿主，Worker 通道则可以随时 terminate。
 * 因此这个通道只允许对「manifest 声明 trust=full 且用户已显式授权」的插件开放。
 */
export function createMainThreadTransport(info: PluginStartInfo): PluginTransport {
  // 两侧各持一份监听器：Worker 里 postMessage 是跨线程的，插件不会收到自己的消息，
  // 这里必须模拟同样的隔离，否则插件会收到自己发出的每一条消息（死循环风险）。
  const hostListeners = new Map<string, Listener[]>() // 宿主注册，收插件发出的消息
  const pluginListeners = new Map<string, Listener[]>() // 插件注册，收宿主发出的消息
  let terminated = false

  const dispatch = (target: Map<string, Listener[]>, data: unknown): void => {
    // 异步派发，与 Worker 的消息语义保持一致；
    // 同步派发会让插件的同步递归调用直接把主线程栈打爆
    queueMicrotask(() => {
      if (terminated) return
      for (const listener of [...(target.get('message') ?? [])]) listener({ data })
    })
  }

  // 注入给插件代码的 scope：只暴露通信必需的成员。
  // 注意这并不隔离全局——插件函数体的作用域链仍是全局，照样能拿 document / fetch。
  const scope = {
    addEventListener(type: string, listener: Listener): void {
      addListener(pluginListeners, type, listener)
    },
    postMessage(data: unknown): void {
      dispatch(hostListeners, data)
    },
    removeEventListener(type: string, listener: Listener): void {
      removeListener(pluginListeners, type, listener)
    },
  }

  try {
    const bootstrap = new Function('self', createWorkerSource(info.id, info.permissions, true))
    bootstrap(scope)
  } catch (error) {
    // 运行时引导自身出错（生成的代码有问题）：直接上报，
    // 让宿主把插件标记为失败，而不是让异常冒泡打断整个加载流程
    dispatch(hostListeners, {
      error: error instanceof Error ? error.message : String(error),
      id: info.id,
      pluginId: info.id,
      type: 'plugin:error',
    })
  }

  return {
    addEventListener(type, listener) {
      addListener(hostListeners, type, listener)
    },
    postMessage(data) {
      dispatch(pluginListeners, data)
    },
    removeEventListener(type, listener) {
      removeListener(hostListeners, type, listener)
    },
    terminate() {
      terminated = true
      hostListeners.clear()
      pluginListeners.clear()
    },
  }
}
