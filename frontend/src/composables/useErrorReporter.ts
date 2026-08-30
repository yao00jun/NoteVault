import type { App } from 'vue'
import { ErrorMonitor as ErrorMonitorService } from '@bindings/github.com/notevault/notevault/index.js'
import type { ErrorReport } from '@bindings/github.com/notevault/notevault/models.js'

let installGuard = false
let reportQueue: ErrorReport[] = []
let lastConfigSync = 0

/**
 * 全局错误捕获安装器
 * 注册 Vue errorHandler 与 window error 事件，捕获未处理错误后调用 ErrorMonitor.ReportError
 *
 * 用法：
 *   import { installErrorReporter } from '@/composables/useErrorReporter'
 *   app.use({ install: installErrorReporter })
 */
export function installErrorReporter(app: App): void {
  if (installGuard) return
  installGuard = true

  // 1) Vue 内部抛出的错误（组件渲染 / setup / 事件）
  app.config.errorHandler = (err, instance, info) => {
    const report = buildReport(err as Error, {
      extra: { info },
      tags: { source: 'vue-component' },
    })
    void safeReport(report)
    // 仍然打印到 console，便于开发者定位
    console.error('[Vue errorHandler]', err, info)
  }

  // 2) 资源加载错误与运行时脚本错误
  window.addEventListener('error', (event) => {
    const target = event.target
    // 资源加载错误（img/script/css）
    if (target && (target as HTMLElement).tagName) {
      const el = target as HTMLElement
      const src = el.getAttribute('src') || el.getAttribute('href') || ''
      void safeReport(
        buildReport(new Error('Resource load error'), {
          message: `Failed to load <${el.tagName.toLowerCase()}>: ${src}`,
          tags: { source: 'resource', tag: el.tagName.toLowerCase() },
          extra: { src },
        }),
      )
      return
    }
    // 普通 JS 错误
    void safeReport(
      buildReport(event.error || new Error(event.message), {
        source: `${event.filename}:${event.lineno}:${event.colno}`,
        tags: { source: 'window-error' },
      }),
    )
  })

  // 3) Promise rejection 未捕获
  window.addEventListener('unhandledrejection', (event) => {
    const reason = event.reason
    const err = reason instanceof Error ? reason : new Error(String(reason))
    void safeReport(
      buildReport(err, {
        tags: { source: 'unhandled-rejection' },
        extra: { reason: String(reason) },
      }),
    )
  })
}

function buildReport(err: Error, opts: Partial<ErrorReport> = {}): ErrorReport {
  return {
    message: opts.message || err.message,
    stack: err.stack,
    level: opts.level || 'error',
    source: opts.source,
    tags: opts.tags,
    extra: opts.extra,
    userAgent: typeof navigator !== 'undefined' ? navigator.userAgent : '',
    timestamp: Date.now(),
  }
}

async function safeReport(report: ErrorReport): Promise<void> {
  try {
    await ErrorMonitorService.ReportError(report)
    // 上报成功后，尝试排空队列
    await flushQueue()
  } catch (e) {
    // 上报失败，入队（最多保留 50 条）
    reportQueue.push(report)
    if (reportQueue.length > 50) reportQueue.shift()
    console.warn('[ErrorReporter] report failed, queued:', e)
  }
}

async function flushQueue(): Promise<void> {
  if (reportQueue.length === 0) return
  const snapshot = reportQueue
  reportQueue = []
  for (const item of snapshot) {
    try {
      await ErrorMonitorService.ReportError(item)
    } catch {
      // 失败再次入队（限制递归）
      if (reportQueue.length < 50) reportQueue.push(item)
    }
  }
}

/**
 * 同步 ErrorMonitor 配置：当用户在设置页改了 sentryDSN/enableLocalLog 后调用
 * （后端 ErrorMonitor.UpdateConfig 通过环境变量 + 调用同步，这里仅触发本地去抖动）
 */
export function syncErrorMonitorConfig(): void {
  const now = Date.now()
  // 5 秒去抖
  if (now - lastConfigSync < 5000) return
  lastConfigSync = now
  // 后端 ErrorMonitor 在 main.go 启动时根据 env 装配；
  // 当用户通过设置页修改 DSN 后，建议在重启应用后才生效（环境变量驱动）。
  // 这里只做提示不阻塞 UI。
}
