<!--
  SidebarOutline.vue：侧栏文档大纲（VSCode Outline 式）
  - 大纲数据由 EditorView 广播（notevault:outline-changed，marked.lexer 解析、
    行号与 jumpToLine 同源），随编辑实时更新，避免侧栏重复解析出旧磁盘内容
  - 点击标题派发 notevault:outline-jump（detail.line），由编辑器平滑滚动到对应段落
  - 未打开文档/无标题时显示轻量空态
-->
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useWorkspaceStore } from '@/stores/workspace'

defineOptions({ name: 'SidebarOutline' })

interface OutlineItem { level: number; text: string; line: number }

const emit = defineEmits<{ count: [count: number] }>()

const workspaceStore = useWorkspaceStore()
const route = useRoute()
const router = useRouter()

const items = ref<OutlineItem[]>([])

function handleOutlineChanged(event: Event) {
  const detail = (event as CustomEvent<{ file?: string; items?: OutlineItem[] }>).detail
  if (!detail) return
  // 只接受当前活跃文档的大纲，忽略编辑器里仍挂着的其他标签页广播
  if (!detail.file || detail.file !== workspaceStore.activeFile) return
  items.value = detail.items ?? []
}

onMounted(() => window.addEventListener('notevault:outline-changed', handleOutlineChanged))
onBeforeUnmount(() => {
  window.removeEventListener('notevault:outline-changed', handleOutlineChanged)
  emit('count', 0)
})

watch(items, value => emit('count', value.length))

const hasDocument = computed(() => !!workspaceStore.activeFile)
const isEmpty = computed(() => items.value.length === 0)

function indentStyle(level: number) {
  return { paddingLeft: `${Math.min(level - 1, 4) * 10 + 6}px` }
}

function jump(item: OutlineItem) {
  // 不在编辑器页时先带文件跳回去；随后再点一次即可定位（保持行为可预期）
  if (route.path !== '/editor') {
    router.push({ path: '/editor', query: { file: workspaceStore.activeFile || '' } })
    return
  }
  window.dispatchEvent(new CustomEvent('notevault:outline-jump', { detail: { line: item.line } }))
}
</script>

<template>
  <div
    class="sidebar-outline"
    data-testid="sidebar-outline"
  >
    <p
      v-if="!hasDocument"
      class="outline-empty"
    >
      打开文档查看大纲
    </p>
    <p
      v-else-if="isEmpty"
      class="outline-empty"
    >
      暂无标题大纲
    </p>
    <div
      v-else
      class="outline-list"
    >
      <button
        v-for="(item, index) in items"
        :key="`${item.line}-${index}`"
        class="outline-item"
        :style="indentStyle(item.level)"
        :title="`H${item.level} · 第 ${item.line + 1} 行`"
        @click="jump(item)"
      >
        <span
          class="outline-level"
          :class="`outline-level-${item.level}`"
        >H{{ item.level }}</span>
        <span class="outline-text">{{ item.text }}</span>
      </button>
    </div>
  </div>
</template>

<style scoped>
.sidebar-outline { padding: 2px 2px 4px; }
.outline-empty {
  margin: 2px 0 4px;
  color: var(--text-muted);
  font-size: 11px;
  line-height: 1.7;
}
.outline-list { display: flex; flex-direction: column; gap: 1px; }
.outline-item {
  display: flex;
  align-items: center;
  gap: 5px;
  min-width: 0;
  max-width: 100%;
  height: 24px;
  padding-right: 6px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary);
  font-size: 11px;
  text-align: left;
  cursor: pointer;
  transition: background var(--transition-fast), color var(--transition-fast);
}
.outline-item:hover { background: var(--bg-hover); color: var(--text-primary); }
.outline-level {
  flex-shrink: 0;
  font-size: 9px;
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}
.outline-level-1 { color: var(--accent); font-weight: 600; }
.outline-text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
