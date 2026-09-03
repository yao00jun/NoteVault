import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import wails from "@wailsio/runtime/plugins/vite";
import { fileURLToPath, URL } from "node:url";

// https://vitejs.dev/config/
export default defineConfig({
  // Vite 8 默认用 lightningcss 做 CSS 转换与压缩。lightningcss 不认识 Vue scoped
  // 的 `:deep()` 组合子，会在 [lightningcss minify] 阶段报 "deep is not recognized"。
  // 用 postcss 做转换可保证 Vue 的 scoped 插件先在 postcss 阶段把 `:deep()` 改写成
  // `[data-v-xxx] p` 这类普通选择器，lightningcss 压缩时就再也碰不到 `:deep`。
  // 注意：真正的告警根因是 MarkdownPreview.vue 里有一处「嵌套 :deep()」(`:deep(.nv-embed-body :deep(p))`)，
  // 该写法 Vue 无法解析、会原样透传，已被修正为 `:deep(.nv-embed-body p)`。
  css: {
    transformer: "postcss",
  },
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  plugins: [vue(), wails("./bindings")],
  resolve: {
    alias: {
      // 绑定聚合层：wails3 按 Go 包生成绑定，输出是 internal/ 下的嵌套结构，
      // 且每次构建都会清空 bindings 重生成（-clean 默认为 true）。
      // 业务代码只面向下面这两个稳定路径，由 src/bindings 聚合转出
      // （详见 src/bindings/index.ts 的说明）。
      //
      // 精确规则必须排在下面的 @bindings 通配之前（命中即止）。
      // ⚠️ vitest 读的是 vitest.config.ts、不继承本文件的 alias，
      // 改动这里必须同步改 vitest.config.ts，否则测试会报
      // "Cannot find package '@bindings/...'".
      "@bindings/github.com/notevault/notevault/index.js": fileURLToPath(
        new URL("./src/bindings/index.ts", import.meta.url),
      ),
      "@bindings/github.com/notevault/notevault/models.js": fileURLToPath(
        new URL("./src/bindings/models.ts", import.meta.url),
      ),
      "@": fileURLToPath(new URL("./src", import.meta.url)),
      // 兼容非 wails3 dev 环境（纯 vite 启动/E2E 测试时）也能解析 @bindings
      "@bindings": fileURLToPath(new URL("./bindings", import.meta.url)),
    },
  },
});
