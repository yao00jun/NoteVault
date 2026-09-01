<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { Search, FileText, X, Clock, ArrowRight, AlertCircle } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { useWorkspaceStore } from '@/stores/workspace'
import { useI18n } from 'vue-i18n'
import { SearchService, WorkspaceService } from '@bindings/github.com/notevault/notevault/index.js'
import { cleanSnippet, highlightText } from '@/utils/text'

interface SearchResult {
  path: string
  title: string
  snippet: string
  matchCount: number
  modTime: string
}

interface IndexStats {
  docCount: number
  tokenCount: number
  scanComplete: boolean
  skippedCount: number
}

// 排序模式（P0-4）。
// relevance 是后端 BM25 算出来的顺序，前端不做任何重排；
// recent 才需要在前端按 modTime 重排。
type SortMode = 'relevance' | 'recent'

const router = useRouter()
const { t, tm } = useI18n()
const workspaceStore = useWorkspaceStore()

const searchQuery = ref('')
const results = ref<SearchResult[]>([])
const isSearching = ref(false)
const errorMsg = ref('')
const recentSearches = ref<string[]>([...(tm('search.recentExamples') as string[])])
const sortMode = ref<SortMode>('relevance')
const indexStats = ref<IndexStats | null>(null)

const hasResults = computed(() => results.value.length > 0)
const currentWorkspace = computed(() => workspaceStore.currentWorkspace)

// 后端已按 BM25 相关性排好序，只在「最近修改」模式下才重排
const sortedResults = computed(() => {
  if (sortMode.value === 'recent') {
    return [...results.value].sort(
      (a, b) => new Date(b.modTime).getTime() - new Date(a.modTime).getTime()
    )
  }
  return results.value
})

// 索引覆盖提示（P0-5）：扫描有文件数与单文件体积两道上限，
// 超限部分不会进索引。不提示的话用户会以为「搜不到就是没有」。
const indexWarning = computed(() => {
  const s = indexStats.value
  if (!s) return ''
  if (s.skippedCount > 0 && !s.scanComplete) {
    return t('search.indexPartialBoth', { skipped: s.skippedCount })
  }
  if (s.skippedCount > 0) {
    return t('search.indexSkipped', { skipped: s.skippedCount })
  }
  if (!s.scanComplete) {
    return t('search.indexTruncated')
  }
  return ''
})

function setSortMode(mode: SortMode) {
  sortMode.value = mode
}

// 拉取索引覆盖统计。不阻塞搜索渲染，失败就当没有提示。
async function loadIndexStats(workspacePath: string) {
  try {
    indexStats.value = (await SearchService.GetIndexStats(workspacePath)) as IndexStats
  } catch {
    indexStats.value = null
  }
}

let searchTimer: ReturnType<typeof setTimeout> | null = null
let searchGeneration = 0

async function doSearch() {
  const generation = ++searchGeneration
  const query = searchQuery.value.trim()
  if (!query) {
    results.value = []
    isSearching.value = false
    return
  }

  if (!currentWorkspace.value?.path) {
    try {
      const workspace = await WorkspaceService.GetCurrentWorkspace()
      if (workspace) workspaceStore.setCurrentWorkspace(workspace as any)
    } catch (e) {
      console.warn('Failed to restore current workspace:', e)
    }
  }

  if (!currentWorkspace.value?.path) {
    if (generation !== searchGeneration) return
    results.value = []
    errorMsg.value = t('search.noWorkspaceTitle')
    isSearching.value = false
    return
  }

  errorMsg.value = ''
  isSearching.value = true
  try {
    const data = await SearchService.Search(currentWorkspace.value.path, query)
    if (generation !== searchGeneration) return
    results.value = Array.isArray(data) ? data as SearchResult[] : []
    // 顺带刷新索引覆盖统计（不 await，避免拖慢搜索结果呈现）
    void loadIndexStats(currentWorkspace.value.path)
  } catch (e) {
    if (generation !== searchGeneration) return
    console.error('Search failed:', e)
    errorMsg.value = e instanceof Error ? e.message : String(e)
    results.value = []
  } finally {
    if (generation === searchGeneration) isSearching.value = false
  }
}

// 输入时 debounce 搜索
watch(searchQuery, () => {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    doSearch()
  }, 300)
})

onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer)
  searchGeneration++
})

function clearSearch() {
  searchGeneration++
  if (searchTimer) {
    clearTimeout(searchTimer)
    searchTimer = null
  }
  searchQuery.value = ''
  results.value = []
  errorMsg.value = ''
  isSearching.value = false
}

function useRecentSearch(term: string) {
  searchQuery.value = term
  if (!recentSearches.value.includes(term)) {
    recentSearches.value.unshift(term)
    if (recentSearches.value.length > 10) {
      recentSearches.value.pop()
    }
  }
}

function openResult(result: SearchResult) {
  // 保存到最近搜索
  if (searchQuery.value.trim() && !recentSearches.value.includes(searchQuery.value.trim())) {
    recentSearches.value.unshift(searchQuery.value.trim())
    if (recentSearches.value.length > 10) {
      recentSearches.value.pop()
    }
  }
  // 跳转到编辑器并打开文件
  workspaceStore.openFile(result.path)
  workspaceStore.incrementFileTreeVersion()
  router.push('/editor')
}

/** 计算标题所在文件夹路径（用于上下文提示） */
function getFolder(path: string): string {
  if (!path) return ''
  const idx = path.lastIndexOf('/')
  if (idx === -1) return ''
  return path.substring(0, idx)
}
</script>

<template>
  <div class="search-view">
    <div class="search-header">
      <h2 class="search-title">
        {{ t('search.title') }}
      </h2>
      <div class="search-input-wrapper">
        <Search
          :size="18"
          class="search-input-icon"
        />
        <input
          v-model="searchQuery"
          type="text"
          data-testid="search-input"
          class="search-input"
          :placeholder="t('search.placeholder')"
          autofocus
        >
        <button
          v-if="searchQuery"
          class="search-clear"
          @click="clearSearch"
        >
          <X :size="16" />
        </button>
      </div>
    </div>

    <div class="search-content">
      <div
        v-if="errorMsg"
        class="error-banner"
        data-testid="search-error"
      >
        <AlertCircle :size="16" />
        <span>{{ errorMsg }}</span>
      </div>
      <!-- 搜索中 -->
      <div
        v-else-if="isSearching"
        class="search-state"
      >
        <div class="spinner" />
        <p>{{ t('search.searching') }}</p>
      </div>

      <!-- 最近搜索 -->
      <div
        v-else-if="!searchQuery && recentSearches.length > 0"
        class="recent-searches"
      >
        <h3 class="section-title">
          <Clock :size="16" />
          {{ t('search.recentTitle') }}
        </h3>
        <div class="recent-list">
          <button
            v-for="term in recentSearches"
            :key="term"
            class="recent-item"
            @click="useRecentSearch(term)"
          >
            <Search :size="14" />
            <span>{{ term }}</span>
          </button>
        </div>
      </div>

      <!-- 无结果 -->
      <div
        v-else-if="searchQuery && !hasResults && !isSearching"
        class="search-state"
      >
        <div class="empty-icon">
          🔍
        </div>
        <h3>{{ t('search.noResultTitle') }}</h3>
        <p>{{ t('search.noResultDesc') }}</p>
      </div>

      <!-- 搜索结果 -->
      <div
        v-else-if="hasResults"
        class="search-results"
      >
        <div class="results-header">
          <span>{{ t('search.matchCount', { count: results.length }) }}</span>
          <div
            class="sort-switch"
            role="group"
            :aria-label="t('search.sortBy')"
          >
            <button
              type="button"
              class="sort-btn"
              :class="{ active: sortMode === 'relevance' }"
              data-testid="sort-relevance"
              @click="setSortMode('relevance')"
            >
              {{ t('search.sortRelevance') }}
            </button>
            <button
              type="button"
              class="sort-btn"
              :class="{ active: sortMode === 'recent' }"
              data-testid="sort-recent"
              @click="setSortMode('recent')"
            >
              {{ t('search.sortRecent') }}
            </button>
          </div>
        </div>
        <div
          v-if="indexWarning"
          class="index-warning"
          data-testid="index-warning"
        >
          <AlertCircle :size="14" />
          <span>{{ indexWarning }}</span>
        </div>
        <div class="results-list">
          <div
            v-for="result in sortedResults"
            :key="result.path"
            class="result-item"
            data-testid="search-result"
            @click="openResult(result)"
          >
            <div class="result-icon">
              <FileText :size="18" />
            </div>
            <div class="result-content">
              <div class="result-title">
                <span v-html="highlightText(result.title || '', searchQuery)" />
                <span
                  v-if="getFolder(result.path)"
                  class="result-folder"
                >📁 {{ getFolder(result.path) }}</span>
              </div>
              <div
                class="result-snippet"
                v-html="cleanSnippet(result.snippet, searchQuery)"
              />
            </div>
            <div class="result-meta">
              <span class="match-count">{{ t('search.matchCountInFile', { count: result.matchCount }) }}</span>
              <ArrowRight
                :size="16"
                class="result-arrow"
              />
            </div>
          </div>
        </div>
      </div>

      <!-- 空状态 -->
      <div
        v-else
        class="search-state"
      >
        <div class="empty-icon">
          📝
        </div>
        <h3>{{ t('search.emptyTitle') }}</h3>
        <p>{{ t('search.emptyDesc') }}</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.search-view {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.search-header {
  padding: var(--space-6) var(--space-8);
  border-bottom: 1px solid var(--border);
  background: var(--bg-window);
}

.search-title {
  font-size: var(--text-xl);
  font-weight: 700;
  margin: 0 0 var(--space-4) 0;
  color: var(--text-primary);
}

.search-input-wrapper {
  position: relative;
  display: flex;
  align-items: center;
}

.search-input-icon {
  position: absolute;
  left: var(--space-3);
  color: var(--text-muted);
}

.search-input {
  width: 100%;
  height: 42px;
  padding: 0 var(--space-10) 0 var(--space-10);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg-input);
  color: var(--text-primary);
  font-size: var(--text-base);
  outline: none;
  transition: border-color var(--transition-fast), box-shadow var(--transition-fast);
}

.search-input:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-alpha);
}

.search-clear {
  position: absolute;
  right: var(--space-3);
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  transition: background var(--transition-fast), color var(--transition-fast);
}

.search-clear:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.search-content {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-6) var(--space-8);
}

.error-banner {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  max-width: 720px;
  margin: 0 auto var(--space-4);
  padding: var(--space-2) var(--space-3);
  border: 1px solid #ef4444;
  border-radius: var(--radius-sm);
  background: rgba(239, 68, 68, .1);
  color: #ef4444;
  font-size: var(--text-sm);
}

.search-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: var(--space-12) 0;
  color: var(--text-muted);
  gap: var(--space-3);
}

.search-state h3 {
  font-size: var(--text-lg);
  font-weight: 600;
  color: var(--text-secondary);
  margin: 0;
}

.search-state p {
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

.recent-searches {
  max-width: 600px;
}

.section-title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--text-secondary);
  margin: 0 0 var(--space-3) 0;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.recent-list {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}

.recent-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: var(--text-sm);
  transition: background var(--transition-fast), border-color var(--transition-fast);
}

.recent-item:hover {
  background: var(--bg-hover);
  border-color: var(--accent);
  color: var(--text-primary);
}

.results-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
  font-size: var(--text-sm);
  color: var(--text-muted);
  margin-bottom: var(--space-3);
}

.sort-switch {
  display: flex;
  gap: 2px;
  padding: 2px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
}

.sort-btn {
  padding: var(--space-1) var(--space-3);
  border: none;
  border-radius: calc(var(--radius-md) - 2px);
  background: transparent;
  color: var(--text-muted);
  font-size: var(--text-xs);
  cursor: pointer;
  transition: background var(--transition-fast), color var(--transition-fast);
}

.sort-btn:hover {
  color: var(--text-primary);
}

.sort-btn.active {
  background: var(--bg-card);
  color: var(--text-primary);
  font-weight: 500;
}

.index-warning {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin-bottom: var(--space-3);
  padding: var(--space-2) var(--space-3);
  border: 1px solid var(--warning-border, var(--border));
  border-radius: var(--radius-md);
  background: var(--warning-bg, var(--bg-secondary));
  color: var(--text-secondary);
  font-size: var(--text-xs);
  line-height: 1.5;
}

.results-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.result-item {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  padding: var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg-card);
  cursor: pointer;
  transition: background var(--transition-fast), border-color var(--transition-fast);
}

.result-item:hover {
  background: var(--bg-hover);
  border-color: var(--accent);
}

.result-icon {
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

.result-content {
  flex: 1;
  min-width: 0;
}

.result-title {
  display: flex;
  align-items: baseline;
  gap: var(--space-2);
  margin-bottom: var(--space-1);
  flex-wrap: wrap;
}

.result-title :deep(span:first-child) {
  font-size: var(--text-base);
  font-weight: 600;
  color: var(--text-primary);
}

.result-folder {
  font-size: var(--text-xs);
  color: var(--text-muted);
  background: var(--bg-sidebar);
  padding: 1px 8px;
  border-radius: 10px;
}

.result-snippet {
  font-size: var(--text-sm);
  color: var(--text-secondary);
  line-height: 1.5;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.result-meta {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: var(--space-2);
  flex-shrink: 0;
}

.match-count {
  font-size: var(--text-xs);
  color: var(--accent);
  background: var(--accent-alpha);
  padding: 2px 8px;
  border-radius: 10px;
}

.result-arrow {
  color: var(--text-muted);
  opacity: 0;
  transition: opacity var(--transition-fast), transform var(--transition-fast);
}

.result-item:hover .result-arrow {
  opacity: 1;
  transform: translateX(2px);
}

:deep(.search-highlight) {
  background: var(--accent-alpha);
  color: var(--accent);
  padding: 0 2px;
  border-radius: 2px;
}
</style>
