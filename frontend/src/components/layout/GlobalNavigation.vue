<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, ArrowRight, ChevronRight } from '@lucide/vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { useNavigationStore } from '@/stores/navigation'
import { usePageContext } from '@/composables/usePageContext'
import { sectionFallback } from '@/utils/navigation'

const router = useRouter()
const route = useRoute()
const workspace = useWorkspaceStore()
const navigation = useNavigationStore()
const { context, breadcrumbs } = usePageContext()
const backAvailable = computed(() => navigation.canGoBack || sectionFallback(route, workspace.hasWorkspace, context.value.section) !== route.fullPath)
</script>

<template>
  <nav
    class="global-navigation"
    aria-label="全局导航"
  >
    <div class="navigation-controls">
      <button
        class="icon-btn"
        data-testid="global-back"
        aria-label="返回上一页"
        title="返回上一页 (Alt+←)"
        :disabled="!backAvailable"
        @click="navigation.back(router)"
      >
        <ArrowLeft :size="16" />
      </button>
      <button
        class="icon-btn"
        data-testid="global-forward"
        aria-label="前进"
        title="前进 (Alt+→)"
        :disabled="!navigation.canGoForward"
        @click="navigation.forward(router)"
      >
        <ArrowRight :size="16" />
      </button>
    </div>
    <ol
      v-if="workspace.hasWorkspace"
      class="navigation-breadcrumbs"
      aria-label="当前位置"
    >
      <li
        v-for="(crumb, index) in breadcrumbs"
        :key="index"
      >
        <ChevronRight
          v-if="index"
          :size="12"
          aria-hidden="true"
        />
        <button
          v-if="crumb.to"
          :title="crumb.label"
          @click="router.push(crumb.to)"
        >
          {{ crumb.label }}
        </button>
        <span
          v-else
          aria-current="page"
          :title="crumb.label"
        >{{ crumb.label }}</span>
      </li>
    </ol>
  </nav>
</template>

<style scoped>
.global-navigation { display: flex; align-items: center; flex: 1; min-width: 0; gap: 10px; --wails-draggable: no-drag; }
.navigation-controls { display: flex; gap: 2px; flex-shrink: 0; }
.navigation-controls button { display: grid; place-items: center; width: 30px; height: 28px; border: 0; border-radius: var(--control-radius, 6px); background: transparent; color: var(--text-primary); }
.navigation-controls button:hover:not(:disabled) { background: var(--bg-hover); }
.navigation-controls button:disabled { opacity: .3; cursor: default; }
.navigation-breadcrumbs { display: flex; gap: 4px; min-width: 0; align-items: center; padding: 0; margin: 0; list-style: none; font-size: 12px; }
.navigation-breadcrumbs li { display: flex; align-items: center; gap: 4px; min-width: 0; }
.navigation-breadcrumbs li:last-child { flex: 0 1 auto; }
.navigation-breadcrumbs button, .navigation-breadcrumbs span { color: var(--text-secondary); background: none; border: 0; padding: 4px; border-radius: var(--radius-sm); min-width: 0; max-width: 200px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font: inherit; }
.navigation-breadcrumbs button:hover { background: var(--bg-hover); color: var(--text-primary); }
.navigation-breadcrumbs span[aria-current] { color: var(--text-primary); }
button:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
@media (max-width: 900px) { .navigation-breadcrumbs li:not(:last-child) { display: none; } }
@media (max-width: 650px) { .navigation-breadcrumbs { display: none; } }
</style>
