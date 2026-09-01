import js from '@eslint/js'
import tseslint from 'typescript-eslint'
import pluginVue from 'eslint-plugin-vue'

export default [
  {
    ignores: ['dist', 'bindings', 'node_modules', 'bin', 'coverage', '*.config.js', '*.config.ts'],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/recommended'],
  {
    files: ['**/*.vue'],
    languageOptions: {
      parserOptions: {
        parser: tseslint.parser,
      },
    },
  },
  {
    // 插件样例依赖 /// <reference> 拿到 notevault.d.ts 的类型提示。
    // 不能改成 import：插件是纯 JS，在 Worker 里通过 new Function 执行，
    // import 既解析不了模块，还会破坏调用方的源码偏移。
    files: ['plugins-samples/**/*.js'],
    rules: {
      '@typescript-eslint/triple-slash-reference': 'off',
    },
  },
  {
    rules: {
      // 现有代码大量使用 any（Wails 绑定返回值为 any），关闭以避免噪音
      '@typescript-eslint/no-explicit-any': 'off',
      '@typescript-eslint/no-unused-vars': 'off',
      '@typescript-eslint/no-non-null-assertion': 'off',
      // 单文件组件命名风格放宽
      'vue/multi-word-component-names': 'off',
      'vue/no-unused-vars': 'off',
      // TS 项目关闭 no-undef（由 tsc 负责）
      'no-undef': 'off',
      'no-unused-vars': 'off',
    },
  },
]
