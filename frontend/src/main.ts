import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { i18n, setLocale } from './i18n'
import { useSettingsStore } from './stores/settings'
import { installErrorReporter } from './composables/useErrorReporter'
import './styles/variables.css'
import './styles/global.css'

const app = createApp(App)

app.use(createPinia())
app.use(router)
app.use(i18n)

// 安装全局错误捕获器：Vue errorHandler + window.error + unhandledrejection
// 在 Pinia + i18n 之后挂载，确保 ErrorMonitor bindings 可用
app.use({ install: installErrorReporter })

// 同步持久化的语言设置（在 Pinia 就绪后应用）
setLocale(useSettingsStore().settings.language)

app.mount('#app')
