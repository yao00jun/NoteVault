<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { Tag, FileText, ArrowLeft, Hash, X, AlertCircle } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { useWorkspaceStore } from '@/stores/workspace'
import { useI18n } from 'vue-i18n'
import { TagService, WorkspaceService } from '@bindings/github.com/notevault/notevault/index.js'

interface TagInfo {
  name: string
  count: number
}

interface TagFileInfo {
  path: string
  title: string
}

const router = useRouter()
const { t } = useI18n()
const workspaceStore = useWorkspaceStore()

const tags = ref<TagInfo[]>([])
const selectedTag = ref<string | null>(null)
const tagFiles = ref<TagFileInfo[]>([])
const isLoading = ref(false)
const errorMsg = ref('')
const currentWorkspace = computed(() => workspaceStore.currentWorkspace)

async function ensureWorkspace() {
  if (!currentWorkspace.value) {
    try {
      const ws = await WorkspaceService.GetCurrentWorkspace()
      if (ws) {
        workspaceStore.setCurrentWorkspace(ws as any)
      } else {
        router.push('/')
        return false
      }
    } catch (e) {
      console.error('Failed to get workspace:', e)
      router.push('/')
      return false
    }
  }
  return true
}

async function loadTags() {
  errorMsg.value = ''
  if (!await ensureWorkspace()) return
  isLoading.value = true
  try {
    const data = await TagService.GetAllTags(currentWorkspace.value!.path)
    tags.value = Array.isArray(data) ? data as TagInfo[] : []
  } catch (e) {
    console.error('Failed to load tags:', e)
    errorMsg.value = t('tags.loadTagsFailed', { msg: (e as Error).message })
    tags.value = []
  } finally {
    isLoading.value = false
  }
}

async function selectTag(tagName: string) {
  selectedTag.value = tagName
  errorMsg.value = ''
  if (!await ensureWorkspace()) return
  isLoading.value = true
  try {
    const data = await TagService.GetFilesByTag(currentWorkspace.value!.path, tagName)
    tagFiles.value = Array.isArray(data) ? data as TagFileInfo[] : []
  } catch (e) {
    console.error('Failed to load tag files:', e)
    errorMsg.value = t('tags.loadFilesFailed', { msg: (e as Error).message })
    tagFiles.value = []
  } finally {
    isLoading.value = false
  }
}

function goBack() {
  selectedTag.value = null
  tagFiles.value = []
}

function openFile(file: TagFileInfo) {
  workspaceStore.openFile(file.path)
  workspaceStore.incrementFileTreeVersion()
  router.push('/editor')
}

// 计算标签云的字体大小（基于使用次数）
function getTagFontSize(count: number): string {
  const maxCount = Math.max(...tags.value.map(tag => tag.count), 1)
  const ratio = count / maxCount
  const size = 14 + ratio * 12 // 14px - 26px
  return size + 'px'
}

onMounted(() => {
  loadTags()
})

watch(() => currentWorkspace.value?.id, () => {
  loadTags()
  selectedTag.value = null
  tagFiles.value = []
})

watch(() => workspaceStore.fileTreeVersion, () => {
  if (selectedTag.value) {
    selectTag(selectedTag.value)
  } else {
    loadTags()
  }
})
</script>

<template>
  <div class="tags-view">
    <div class="tags-header">
      <div class="header-left">
        <button
          v-if="selectedTag"
          class="back-btn"
          @click="goBack"
        >
          <ArrowLeft :size="18" />
        </button>
        <h2 class="tags-title">
          <Tag :size="20" />
          {{ selectedTag ? `#${selectedTag}` : t('tags.title') }}
        </h2>
      </div>
      <div
        v-if="!selectedTag"
        class="tags-count"
      >
        {{ t('tags.total', { count: tags.length }) }}
      </div>
      <div
        v-else
        class="tags-count"
      >
        {{ t('tags.filesCount', { count: tagFiles.length }) }}
      </div>
    </div>

    <div class="tags-content">
      <!-- 错误提示 -->
      <div
        v-if="errorMsg"
        class="error-banner"
      >
        <AlertCircle :size="16" />
        <span>{{ errorMsg }}</span>
      </div>
      <!-- 加载中 -->
      <div
        v-if="isLoading"
        class="tags-state"
      >
        <div class="spinner" />
        <p>{{ t('common.loading') }}</p>
      </div>

      <!-- 标签云 -->
      <div
        v-else-if="!selectedTag"
        class="tag-cloud-section"
      >
        <div
          v-if="tags.length === 0"
          class="tags-state"
        >
          <div class="empty-icon">
            🏷️
          </div>
          <h3>{{ t('tags.emptyTitle') }}</h3>
          <p>{{ t('tags.emptyDesc') }}</p>
        </div>
        <div
          v-else
          class="tag-cloud"
        >
          <button
            v-for="tag in tags"
            :key="tag.name"
            class="tag-cloud-item"
            :style="{ fontSize: getTagFontSize(tag.count) }"
            @click="selectTag(tag.name)"
          >
            <Hash
              :size="12"
              class="tag-hash"
            />
            {{ tag.name }}
            <span class="tag-count-badge">{{ tag.count }}</span>
          </button>
        </div>
      </div>

      <!-- 标签下的文件列表 -->
      <div
        v-else
        class="tag-files-section"
      >
        <div
          v-if="tagFiles.length === 0"
          class="tags-state"
        >
          <div class="empty-icon">
            📄
          </div>
          <h3>{{ t('tags.noFilesTitle') }}</h3>
          <p>{{ t('tags.noFilesDesc') }}</p>
        </div>
        <div
          v-else
          class="files-list"
        >
          <div
            v-for="file in tagFiles"
            :key="file.path"
            class="file-item"
            @click="openFile(file)"
          >
            <div class="file-icon">
              <FileText :size="18" />
            </div>
            <div class="file-info">
              <div class="file-title">
                {{ file.title }}
              </div>
              <div class="file-path">
                {{ file.path }}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tags-view {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.error-banner {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  margin-bottom: var(--space-4);
  background: rgba(239,68,68,0.1);
  border: 1px solid #ef4444;
  border-radius: var(--radius-sm);
  color: #ef4444;
  font-size: var(--text-sm);
  max-width: 700px;
  margin-left: auto;
  margin-right: auto;
}

.tags-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-6) var(--space-8);
  border-bottom: 1px solid var(--border);
  background: var(--bg-window);
}

.header-left {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.back-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  transition: background var(--transition-fast), color var(--transition-fast);
}

.back-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.tags-title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xl);
  font-weight: 700;
  margin: 0;
  color: var(--text-primary);
}

.tags-count {
  font-size: var(--text-sm);
  color: var(--text-muted);
}

.tags-content {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-8);
}

.tags-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: var(--space-12) 0;
  color: var(--text-muted);
  gap: var(--space-3);
}

.tags-state h3 {
  font-size: var(--text-lg);
  font-weight: 600;
  color: var(--text-secondary);
  margin: 0;
}

.tags-state p {
  font-size: var(--text-sm);
  margin: 0;
}

.empty-icon {
  font-size: 48px;
  opacity: 0.5;
}

.spinner {
  width: 32px;
  height: 32px;
  border: 3px solid var(--border);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.tag-cloud-section {
  max-width: 800px;
  margin: 0 auto;
}

.tag-cloud {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
  align-items: center;
  justify-content: center;
}

.tag-cloud-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: var(--space-2) var(--space-4);
  border: 1px solid var(--border);
  border-radius: 20px;
  background: var(--bg-card);
  color: var(--text-secondary);
  font-weight: 600;
  cursor: pointer;
  transition: all var(--transition-fast);
}

.tag-cloud-item:hover {
  background: var(--accent-alpha);
  border-color: var(--accent);
  color: var(--accent);
  transform: translateY(-2px);
}

.tag-hash {
  opacity: 0.6;
}

.tag-count-badge {
  font-size: 11px;
  font-weight: 500;
  color: var(--text-muted);
  background: var(--bg-sidebar);
  padding: 1px 6px;
  border-radius: 10px;
}

.tag-cloud-item:hover .tag-count-badge {
  background: var(--accent);
  color: white;
}

.tag-files-section {
  max-width: 700px;
  margin: 0 auto;
}

.files-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.file-item {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg-card);
  cursor: pointer;
  transition: background var(--transition-fast), border-color var(--transition-fast);
}

.file-item:hover {
  background: var(--bg-hover);
  border-color: var(--accent);
}

.file-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border-radius: var(--radius-sm);
  background: var(--bg-sidebar);
  color: var(--accent);
  flex-shrink: 0;
}

.file-info {
  flex: 1;
  min-width: 0;
}

.file-title {
  font-size: var(--text-base);
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 2px;
}

.file-path {
  font-size: var(--text-xs);
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
