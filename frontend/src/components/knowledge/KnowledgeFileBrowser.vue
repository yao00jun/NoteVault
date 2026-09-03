<script setup lang="ts">
/**
 * KnowledgeFileBrowser - 知识库主页的文档浏览区
 * （文件夹侧栏 + 分组文档列表 + 搜索/排序工具条）
 *
 * 从 KnowledgeView 抽出（原 626-837 行模板）。纯展示 + emit 交还父组件：
 * 所有状态（文件、文件夹、星标）由父级持有，本组件不直接调用后端。
 * 样式沿用父组件的 .kv-* class——由父组件在非 scoped 块中提供（见 KnowledgeView 样式注释）。
 */
import { useI18n } from 'vue-i18n'
import {
  FileText,
  Search,
  Star,
  StarOff,
  ChevronRight,
  ChevronDown,
  Folder,
  FolderPlus,
  FolderOpen,
  Sparkles,
  FilePlus,
  Library,
} from 'lucide-vue-next'

const { t } = useI18n()

export interface BrowserFile {
  name: string
  path: string
  isDir: boolean
  modTime?: string
}

export interface FolderEntry {
  path: string
  name: string
  depth: number
}

export interface FileGroup {
  path: string
  name: string
  files: BrowserFile[]
}

defineProps<{
  filteredFiles: BrowserFile[]
  groupedFiles: FileGroup[]
  folderDocumentCounts: Record<string, number>
  visibleFolderEntries: FolderEntry[]
  selectedFolder: string
  selectedFolderLabel: string
  expandedFolders: Record<string, boolean>
  isLoading: boolean
  searchKeyword: string
  showOnlyStarred: boolean
  isStarred: (path: string) => boolean
  formatRelativeTime: (modTime?: string) => string
}>()

const emit = defineEmits<{
  (e: 'update:searchKeyword', v: string): void
  (e: 'update:showOnlyStarred', v: boolean): void
  (e: 'update:sortBy', v: string): void
  (e: 'select-folder', path: string): void
  (e: 'toggle-folder', path: string): void
  (e: 'open-file', file: BrowserFile): void
  (e: 'toggle-star', path: string): void
  (e: 'create-new'): void
  (e: 'create-folder'): void
}>()
</script>

<template>
  <section class="kv-section kv-section-main">
    <div class="kv-section-header">
      <h2>
        <FileText :size="16" />
        <span>{{ t('knowledge.myDocs') }}</span>
        <span class="kv-section-count">{{ filteredFiles.length }}</span>
      </h2>
      <div class="kv-section-tools">
        <div class="kv-search">
          <Search :size="14" />
          <input
            :value="searchKeyword"
            :placeholder="t('knowledge.searchPlaceholder')"
            @input="emit('update:searchKeyword', ($event.target as HTMLInputElement).value)"
          >
        </div>
        <select
          class="kv-sort"
          @change="emit('update:sortBy', ($event.target as HTMLSelectElement).value)"
        >
          <option value="modified">
            {{ t('knowledge.sortRecent') }}
          </option>
          <option value="name">
            {{ t('knowledge.sortName') }}
          </option>
          <option value="created">
            {{ t('knowledge.sortCreated') }}
          </option>
        </select>
        <button
          class="kv-icon-toggle"
          :class="{ active: showOnlyStarred }"
          :title="t('knowledge.onlyStarred')"
          @click="emit('update:showOnlyStarred', !showOnlyStarred)"
        >
          <Star :size="14" />
        </button>
      </div>
    </div>

    <div
      v-if="isLoading"
      class="kv-loading"
    >
      <div class="kv-spinner" />
      {{ t('common.loading') }}
    </div>

    <div
      v-else-if="filteredFiles.length === 0"
      class="kv-empty"
    >
      <Sparkles :size="32" />
      <h3>{{ searchKeyword ? t('knowledge.noMatchTitle') : t('knowledge.emptyTitle') }}</h3>
      <p v-if="!searchKeyword">
        {{ t('knowledge.emptyDescCreate') }}
      </p>
      <p v-else>
        {{ t('knowledge.emptyDescSearch') }}
      </p>
      <button
        v-if="!searchKeyword"
        class="kv-btn-primary"
        @click="emit('create-new')"
      >
        <FilePlus :size="14" />
        <span>{{ t('knowledge.newDoc') }}</span>
      </button>
    </div>

    <div
      v-else
      class="kv-library-layout"
    >
      <aside class="kv-folder-nav">
        <div class="kv-folder-nav-header">
          <span>{{ t('knowledge.folderNav') }}</span>
          <button
            class="kv-folder-action"
            :title="t('knowledge.newFolder')"
            @click="emit('create-folder')"
          >
            <FolderPlus :size="14" />
          </button>
        </div>
        <button
          class="kv-folder-item"
          :class="{ active: selectedFolder === '' }"
          @click="emit('select-folder', '')"
        >
          <Library :size="14" />
          <span class="kv-folder-name">{{ t('knowledge.allDocs') }}</span>
          <span class="kv-folder-count">{{ folderDocumentCounts[''] || 0 }}</span>
        </button>
        <div class="kv-folder-list">
          <div
            v-for="folder in visibleFolderEntries"
            :key="folder.path"
            class="kv-folder-row"
          >
            <button
              class="kv-folder-toggle"
              :title="expandedFolders[folder.path] === false ? t('knowledge.expandFolder') : t('knowledge.collapseFolder')"
              @click.stop="emit('toggle-folder', folder.path)"
            >
              <ChevronRight
                v-if="expandedFolders[folder.path] === false"
                :size="12"
              />
              <ChevronDown
                v-else
                :size="12"
              />
            </button>
            <button
              class="kv-folder-item"
              data-testid="folder-item"
              :class="{ active: selectedFolder === folder.path }"
              :style="{ paddingLeft: `${folder.depth * 12 + 4}px` }"
              @click="emit('select-folder', folder.path)"
            >
              <FolderOpen
                v-if="selectedFolder === folder.path"
                :size="14"
              />
              <Folder
                v-else
                :size="14"
              />
              <span class="kv-folder-name">{{ folder.name }}</span>
              <span class="kv-folder-count">{{ folderDocumentCounts[folder.path] || 0 }}</span>
            </button>
          </div>
        </div>
      </aside>

      <div class="kv-document-pane">
        <div class="kv-document-context">
          <div>
            <FolderOpen :size="15" />
            <strong>{{ selectedFolderLabel }}</strong>
            <span>{{ filteredFiles.length }} {{ t('knowledge.stats.docs') }}</span>
          </div>
          <button
            class="kv-context-new"
            :title="t('knowledge.newDocInFolder')"
            @click="emit('create-new')"
          >
            <FilePlus :size="14" />
            <span>{{ t('knowledge.newDocInFolder') }}</span>
          </button>
        </div>
        <div
          v-for="group in groupedFiles"
          :key="group.path || 'root'"
          class="kv-doc-group"
        >
          <button
            v-if="group.path !== selectedFolder"
            class="kv-doc-group-header"
            @click="emit('select-folder', group.path)"
          >
            <FolderOpen :size="14" />
            <span>{{ group.name }}</span>
            <span class="kv-folder-count">{{ group.files.length }}</span>
          </button>
          <div class="kv-doc-list">
            <div
              v-for="file in group.files"
              :key="file.path"
              class="kv-doc-item"
              data-testid="document-item"
              @click="emit('open-file', file)"
            >
              <FileText
                :size="16"
                class="kv-doc-icon"
              />
              <div class="kv-doc-body">
                <div class="kv-doc-title">
                  {{ file.name.replace(/\.(md|markdown)$/, '') }}
                </div>
                <div class="kv-doc-meta">
                  <span class="kv-doc-path">{{ file.path }}</span>
                  <span class="kv-doc-time">{{ formatRelativeTime(file.modTime) }}</span>
                </div>
              </div>
              <button
                class="kv-doc-star"
                :class="{ active: isStarred(file.path) }"
                :title="isStarred(file.path) ? t('knowledge.unstar') : t('knowledge.star')"
                @click.stop="emit('toggle-star', file.path)"
              >
                <Star
                  v-if="isStarred(file.path)"
                  :size="14"
                />
                <StarOff
                  v-else
                  :size="14"
                />
              </button>
              <ChevronRight
                :size="14"
                class="kv-doc-arrow"
              />
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
