import { defineConfig, devices } from '@playwright/test'

/**
 * Playwright E2E 配置
 *
 * NoteVault 是 Wails 桌面应用，正常 E2E 需要打包后的 exe 或 wails3 dev 环境。
 * 本配置面向「Vite 开发服务器冒烟测试」——验证前端 SPA 在浏览器中的基本渲染与路由，
 * 不依赖 Wails Go 后端绑定（调用后端会 404，但页面应照常挂载）。
 *
 * 完整 E2E（含 Go 后端交互）需使用 `wails3 dev` 或打包后运行。
 *
 * ⚠️ 本文件与 e2e/*.spec.ts 必须入库：被 .gitignore 忽略时 CI 的 E2E job 既没有
 * testDir 也没有 config，会退回默认匹配规则把 src/**\/*.test.ts（Vitest 用例）
 * 当作 Playwright 用例执行，最后报 "No tests found"。
 */
export default defineConfig({
  testDir: './e2e',
  // 只认 *.spec.ts：同目录下若混入 Vitest 的 *.test.ts 不会被误执行
  testMatch: '**/*.spec.ts',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: process.env.CI ? [['html'], ['list']] : 'html',
  use: {
    baseURL: 'http://127.0.0.1:9245',
    // 失败时保留 trace（含网络快照/控制台/页面快照），CI 中归档到 artifacts
    trace: 'retain-on-failure',
    // 注入 mock — 模拟 Wails bindings 的最小桩
    // 让前端代码不报错，能够正常渲染
    launchOptions: {
      args: ['--disable-web-security'],
    },
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: {
    // 用 pnpm exec 而不是 npx：npx 可能解析到全局缓存的另一个 vite 版本，
    // 在干净的 CI 环境里跑出来的东西和项目依赖不是一回事。
    command: 'pnpm exec vite --port 9245 --host 127.0.0.1',
    url: 'http://127.0.0.1:9245',
    reuseExistingServer: !process.env.CI,
    // Vite 8 首次启动要预打包依赖，CI 冷缓存下 30s 不够
    timeout: 120000,
  },
})
