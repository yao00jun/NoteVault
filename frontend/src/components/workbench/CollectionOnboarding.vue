<script setup lang="ts">
import { computed } from 'vue'
import { FolderPlus } from '@lucide/vue'
import { useWorkbenchStore } from '@/stores/workbench'
import { requestSourceImport } from '@/composables/useSourceImport'

const props = defineProps<{ kind: 'project' | 'book' }>()
const workbench = useWorkbenchStore()
const space = computed(() => props.kind === 'book' ? 'Learning' : 'Projects')
const folders = computed(() => {
  const registered = new Set((props.kind === 'book' ? workbench.books : workbench.projects).map(item => item.folder))
  const found = new Set<string>()
  for (const document of workbench.documents) {
    const parts = document.path.replace(/\\/g, '/').split('/')
    const folder = parts.slice(0, 2).join('/')
    if (parts.length >= 3 && parts[0] === space.value && parts[1] !== '面试宝典' && !registered.has(folder)) found.add(folder)
  }
  return [...found].sort((a, b) => a.localeCompare(b, 'zh-CN'))
})
function adopt(folder: string) {
  requestSourceImport({ kind: props.kind, sourceType: 'adopt', source: folder, name: folder.split('/').pop(), targetFolder: folder, autoStart: true })
}
</script>

<template>
  <section
    v-if="folders.length"
    class="collection-onboarding collection-panel"
    data-testid="unregistered-collections"
  >
    <div><FolderPlus :size="18" /><strong>发现 {{ folders.length }} 个已有目录</strong><span>接入{{ kind === 'book' ? '书架' : '项目看板' }}，保留现有笔记</span></div>
    <div class="onboarding-folders">
      <button
        v-for="folder in folders"
        :key="folder"
        class="collection-button"
        @click="adopt(folder)"
      >
        {{ folder.split('/').pop() }} <span>接入{{ kind === 'book' ? '书架' : '看板' }}</span>
      </button>
    </div>
  </section>
</template>

<style scoped>
.collection-onboarding { margin: 20px 0; padding: 16px 18px; }
.collection-onboarding > div:first-child { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; font-size: 13px; }
.collection-onboarding strong { font-weight: 600; }
.collection-onboarding span { font-size: 12px; color: var(--text-secondary); }
.onboarding-folders { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 12px; }
</style>
