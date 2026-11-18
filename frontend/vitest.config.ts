import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
      // 绑定聚合层（必须与 vite.config.ts 保持一致）：
      // vitest 优先读本文件、不继承 vite.config.ts，漏配就会报
      // "Cannot find package '@bindings/...'".
      // wails3 生成的是 internal/ 下的嵌套绑定结构，业务代码只面向下面两个稳定路径，
      // 由 src/bindings 聚合转出（详见 src/bindings/index.ts 的说明）。
      // 精确规则必须排在下面的 @bindings 通配之前（命中即止）。
      '@bindings/github.com/notevault/notevault/index.js': fileURLToPath(
        new URL('./src/bindings/index.ts', import.meta.url),
      ),
      '@bindings/github.com/notevault/notevault/models.js': fileURLToPath(
        new URL('./src/bindings/models.ts', import.meta.url),
      ),
      // 测试环境手动映射 @bindings → bindings 目录（跳过 wails 插件）
      '@bindings': fileURLToPath(new URL('./bindings', import.meta.url)),
    },
  },
  test: {
    // 默认 node 环境，组件测试用 // @vitest-environment jsdom 注解
    environment: 'node',
    include: ['src/**/*.test.ts', 'e2e/**/*.test.mjs'],
    // 全局兜底 mock（@/api 全桩），消除漏 mock 导致的退出阶段 Unhandled Error。
    setupFiles: ['./src/test/setup.ts'],
    // 并发上限显式固定，不跟随机器核数：
    // 本机默认并发（60+ worker）曾出现 worker 启动超时、整份测试文件被静默跳过，
    // 表现为「用例数变少但仍然全绿」。宁可用受控的并发换稳定的计数。
    // 实测 8 worker：80 文件 / 891 用例，33.8s（2 worker 时 70.9s）。
    // 没有采用 vitest 建议的 isolate:false：跨文件共享模块状态会让失败数随机波动，
    // 省下的启动开销不值得换来不可复现的红灯。
    maxWorkers: 8,
    minWorkers: 1,
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json', 'html'],
      include: ['src/**/*.ts', 'src/**/*.vue'],
      exclude: ['src/**/*.test.ts', 'src/**/*.spec.ts', 'e2e/**', 'bindings/**'],
    },
  },
})
