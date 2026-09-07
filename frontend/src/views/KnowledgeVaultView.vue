<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  Archive,
  ArrowDown,
  ArrowUpRight,
  BookOpen,
  Brain,
  ChevronRight,
  FileText,
  GitGraph,
  History,
  Import,
  LayoutDashboard,
  Library,
  ListTodo,
  Pin,
  Puzzle,
  RefreshCw,
  Search,
  Settings,
  Sparkles,
  Table2,
  Tags,
  Trash2,
  Wrench,
} from '@lucide/vue'
import { useWorkbenchStore } from '@/stores/workbench'
import { useWorkspaceStore } from '@/stores/workspace'
import { SPACE_DEFS, type SpaceKey } from '@/composables/useWorkbenchSpaces'
import { useToast } from '@/composables/useToast'
import { collectionDateLabel, normalizedCollectionPath } from '@/utils/workbenchCollections'
import '@/styles/workbench-collections.css'

const workbench = useWorkbenchStore()
const workspace = useWorkspaceStore()
const router = useRouter()
const toast = useToast()
const search = ref('')
const spaceFilter = ref('all')
const pinnedOnly = ref(false)
const sortOrder = ref('recent')
const visibleLimit = ref(40)
const spaceCopy: Record<SpaceKey, { label: string; description: string }> = {
  learning: { label: '学习', description: '把理解连成体系' },
  projects: { label: '项目', description: '让经验回到实践' },
  resources: { label: '资料收藏', description: '留住有价值的参考' },
  inbox: { label: '收集箱', description: '为灵感留一个入口' },
  daily: { label: '日记与日报', description: '看见每天的积累' },
}
const spaces = computed(() =>
  SPACE_DEFS.map((space) => ({
    ...space,
    ...spaceCopy[space.key],
    count: workbench.documents.filter((doc) =>
      normalizedCollectionPath(doc.path).startsWith(space.dir + '/')
    ).length,
  }))
)
const insights = [
  {
    id: 'graph',
    title: '知识图谱',
    description: '沿着双向链接，发现笔记之间的联系。',
    icon: GitGraph,
  },
  {
    id: 'compile',
    title: '知识编译',
    description: '把收集箱的碎片，整理为可复用的知识。',
    icon: Sparkles,
  },
  {
    id: 'bases',
    title: 'Bases 数据视图',
    description: '按属性筛选、分组，从不同角度看资料。',
    icon: Table2,
  },
]
const tools = [
  {
    label: '标签树',
    description: '从主题发现知识',
    icon: Tags,
    route: '/discover?tab=views&view=tags',
  },
  { label: '语义问答', description: '围绕笔记提问', icon: Brain, route: '/discover?tab=qna' },
  { label: '回顾与提醒', description: '统计、任务与提醒', icon: ListTodo, route: '/review' },
  { label: '归档', description: '已经告一段落的资料', icon: Archive, route: '/archive' },
  {
    label: '历史版本',
    description: '找回笔记的旧版本',
    icon: History,
    route: '/review?tab=versions',
  },
  { label: '文档管理', description: '新建、模板与导出', icon: Library, route: '/library' },
  { label: '导入资料', description: '汇入已有的知识', icon: Import, route: '/import' },
  { label: '白板', description: '展开思路与关系', icon: LayoutDashboard, route: '/canvas' },
  { label: '插件', description: '扩展你的工作方式', icon: Puzzle, route: '/plugins' },
  { label: '回收站', description: '恢复最近删除的资料', icon: Trash2, route: '/trash' },
  { label: '设置', description: '剪藏、外观与偏好', icon: Settings, route: '/settings' },
]
const pinnedDocumentCount = computed(
  () => workbench.documents.filter((doc) => workspace.isPinned(doc.path)).length
)
const filteredDocuments = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  return workbench.documents
    .filter(
      (doc) =>
        (spaceFilter.value === 'all' ||
          normalizedCollectionPath(doc.path).startsWith(spaceFilter.value + '/')) &&
        (!pinnedOnly.value || workspace.isPinned(doc.path)) &&
        (!query ||
          (doc.title + ' ' + normalizedCollectionPath(doc.path))
            .toLocaleLowerCase()
            .includes(query))
    )
    .sort((a, b) =>
      sortOrder.value === 'title'
        ? a.title.localeCompare(b.title, 'zh-CN', { numeric: true })
        : b.modifiedAt.localeCompare(a.modifiedAt)
    )
})
const displayedDocuments = computed(() => filteredDocuments.value.slice(0, visibleLimit.value))

function openDocument(path: string) {
  workspace.openFile(path)
  void router.push({ path: '/editor', query: { file: path } })
}
function togglePin(path: string, title: string) {
  if (!workspace.togglePin(path, title)) toast.warning('固定区最多容纳 8 项，请先取消一个固定项。')
}
function resetFilters() {
  search.value = ''
  spaceFilter.value = 'all'
  pinnedOnly.value = false
}
watch([search, spaceFilter, pinnedOnly, sortOrder], () => {
  visibleLimit.value = 40
})
watch(() => workspace.currentWorkspace?.path, resetFilters)
</script>

<template>
  <div class="collection-page vault-page">
    <div class="collection-shell">
      <header class="collection-hero">
        <div>
          <div class="collection-eyebrow">
            <Library :size="15" /> KNOWLEDGE / 知识库
          </div>
          <h1>值得留下的，都在这里。</h1>
          <p class="collection-subtitle">
            从五个空间出发，检索、串联并沉淀你的知识资产。
          </p>
        </div>
        <div class="collection-actions">
          <button
            class="collection-icon-button"
            aria-label="刷新知识库"
            type="button"
            :disabled="workbench.loading"
            @click="workbench.refresh()"
          >
            <RefreshCw
              :size="15"
              :class="{ 'collection-spin': workbench.loading }"
            />
          </button>
          <button
            class="collection-button"
            type="button"
            @click="router.push('/library')"
          >
            <FileText :size="14" /> 管理文档
          </button>
        </div>
      </header>
      <div
        v-if="workbench.error"
        class="collection-notice error"
        role="alert"
      >
        {{ workbench.error
        }}<button
          class="collection-button subtle"
          type="button"
          @click="workbench.refresh()"
        >
          重试
        </button>
      </div>
      <div
        v-if="!workspace.currentWorkspace"
        class="collection-empty collection-panel"
      >
        <Library :size="34" />
        <h2>先打开一个工作区</h2>
        <p>笔记、资料和灵感，会在这里连成你的知识库。</p>
        <button
          class="collection-button primary"
          type="button"
          @click="router.push('/')"
        >
          选择工作区
        </button>
      </div>
      <template v-else>
        <section aria-labelledby="vault-spaces-heading">
          <div class="collection-section-title">
            <h2 id="vault-spaces-heading">
              知识空间
            </h2>
            <span>{{ workbench.documents.length }} 篇文档</span>
          </div>
          <div class="vault-space-grid">
            <button
              v-for="space in spaces"
              :key="space.key"
              class="vault-space"
              :data-space="space.dir"
              data-testid="vault-space"
              type="button"
              @click="router.push({ path: '/editor', query: { folder: space.dir } })"
            >
              <span class="vault-space-top"><span class="vault-space-icon"><component
                :is="space.icon"
                :size="20"
              /></span><ArrowUpRight :size="14" /></span>
              <strong>{{ space.label }}</strong><small>{{ space.description }}</small><span class="vault-space-count">{{ space.count }} <small>篇文档</small></span>
            </button>
          </div>
        </section>

        <section
          class="vault-insights"
          aria-labelledby="vault-insights-heading"
        >
          <div class="collection-section-title">
            <h2 id="vault-insights-heading">
              洞察与提炼
            </h2>
            <span>让积累产生新的联系</span>
          </div>
          <div class="vault-insight-grid">
            <button
              v-for="insight in insights"
              :key="insight.id"
              class="vault-insight"
              :data-testid="'vault-insight-' + insight.id"
              type="button"
              @click="router.push({ path: '/insights', query: { tab: insight.id } })"
            >
              <component
                :is="insight.icon"
                :size="22"
              /><span><strong>{{ insight.title }}</strong><small>{{ insight.description }}</small></span><ChevronRight :size="15" />
            </button>
          </div>
        </section>

        <section
          class="vault-documents"
          aria-labelledby="vault-documents-heading"
        >
          <div class="collection-section-title">
            <h2 id="vault-documents-heading">
              {{ pinnedOnly ? '固定文档' : '文档索引' }}
              <span class="collection-tab-count">{{ filteredDocuments.length }}</span>
            </h2>
            <button
              class="collection-button subtle"
              type="button"
              @click="router.push('/discover?tab=search')"
            >
              全文检索 <ArrowUpRight :size="13" />
            </button>
          </div>
          <div class="vault-document-toolbar">
            <label class="collection-search"><Search :size="15" /><input
              v-model="search"
              data-testid="vault-search"
              type="search"
              aria-label="搜索文档名称或路径"
              placeholder="搜索文档名称或路径…"
            ></label>
            <label class="vault-inline-filter"><span class="vault-visually-hidden">文档空间</span><select
              v-model="spaceFilter"
              aria-label="文档空间"
            >
              <option value="all">全部空间</option>
              <option
                v-for="space in spaces"
                :key="space.key"
                :value="space.dir"
              >
                {{ space.label }}
              </option>
            </select></label>
            <button
              class="collection-button"
              :class="{ 'vault-pin-active': pinnedOnly }"
              :aria-pressed="pinnedOnly"
              type="button"
              @click="pinnedOnly = !pinnedOnly"
            >
              <Pin :size="13" /> 固定 {{ pinnedDocumentCount }}
            </button>
            <label class="vault-inline-filter"><span class="vault-visually-hidden">文档排序</span><select
              v-model="sortOrder"
              aria-label="文档排序"
            >
              <option value="recent">最近更新</option>
              <option value="title">文档名称</option>
            </select></label>
          </div>
          <div class="collection-panel vault-document-panel">
            <div
              v-if="workbench.loading && !workbench.documents.length"
              class="vault-loading"
              role="status"
            >
              <RefreshCw
                :size="17"
                class="collection-spin"
              /> 正在整理文档索引…
            </div>
            <div
              v-else-if="displayedDocuments.length"
              class="collection-document-list"
            >
              <div
                v-for="doc in displayedDocuments"
                :key="doc.path"
                class="collection-document-row"
                data-testid="vault-document"
              >
                <button
                  class="collection-document-open"
                  type="button"
                  @click="openDocument(doc.path)"
                >
                  <FileText :size="19" /><span class="collection-document-copy"><strong>{{ doc.title }}</strong><small>{{ doc.path }}</small></span>
                </button>
                <span class="collection-document-date">{{
                  collectionDateLabel(doc.modifiedAt)
                }}</span>
                <button
                  class="collection-icon-button"
                  :class="{ pinned: workspace.isPinned(doc.path) }"
                  data-testid="document-pin"
                  :aria-label="(workspace.isPinned(doc.path) ? '取消固定 ' : '固定 ') + doc.title"
                  :aria-pressed="workspace.isPinned(doc.path)"
                  type="button"
                  @click="togglePin(doc.path, doc.title)"
                >
                  <Pin :size="14" />
                </button>
              </div>
            </div>
            <div
              v-else
              class="collection-empty"
            >
              <BookOpen :size="32" />
              <h3>
                {{ workbench.documents.length ? '没有找到符合条件的文档' : '从一篇笔记开始积累' }}
              </h3>
              <p>
                {{
                  pinnedOnly
                    ? '给常用文档点一下固定，它也会出现在侧栏。'
                    : workbench.documents.length
                      ? '试试其他关键词，或切换到全部空间。'
                      : '新建笔记、导入资料或剪藏文章，它们都会汇集到对应的空间。'
                }}
              </p>
              <button
                v-if="workbench.documents.length"
                class="collection-button"
                type="button"
                @click="resetFilters"
              >
                清除筛选
              </button><button
                v-else
                class="collection-button"
                type="button"
                @click="router.push('/library')"
              >
                管理文档 <ArrowUpRight :size="13" />
              </button>
            </div>
            <button
              v-if="filteredDocuments.length > displayedDocuments.length"
              class="vault-show-more"
              type="button"
              @click="visibleLimit += 40"
            >
              再显示 {{ Math.min(40, filteredDocuments.length - displayedDocuments.length) }} 篇
              <ArrowDown :size="14" />
            </button>
          </div>
        </section>

        <details class="vault-tools">
          <summary>
            <Wrench :size="15" /><span>更多工具</span><small>标签、归档与历史版本</small><ChevronRight :size="15" />
          </summary>
          <nav
            class="vault-tool-grid"
            aria-label="知识库工具"
          >
            <button
              v-for="tool in tools"
              :key="tool.label"
              class="vault-tool"
              type="button"
              @click="router.push(tool.route)"
            >
              <component
                :is="tool.icon"
                :size="17"
              /><span><strong>{{ tool.label }}</strong><small>{{ tool.description }}</small></span><ArrowUpRight :size="12" />
            </button>
          </nav>
        </details>
      </template>
    </div>
  </div>
</template>

<style scoped>
.vault-space-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 12px;
}
.vault-space {
  --space-color: var(--accent);
  text-align: left;
  min-width: 0;
  padding: 18px 17px 17px;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--bg-card);
  color: var(--text-primary);
  transition:
    background var(--transition-fast),
    border-color var(--transition-fast);
}
.vault-space[data-space='Projects'] {
  --space-color: #8b72c1;
}
.vault-space[data-space='Resources'] {
  --space-color: #3d967f;
}
.vault-space[data-space='Inbox'] {
  --space-color: #b47a44;
}
.vault-space[data-space='Daily'] {
  --space-color: #6c89b2;
}
.vault-space:hover {
  background: color-mix(in srgb, var(--space-color) 4%, var(--bg-card));
  border-color: color-mix(in srgb, var(--space-color) 45%, var(--border));
}
.vault-space-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 18px;
  color: var(--text-secondary);
}
.vault-space-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  background: color-mix(in srgb, var(--space-color) 10%, var(--bg-card));
  color: var(--space-color);
  border-radius: var(--radius-sm);
}
.vault-space > strong {
  display: block;
  font-size: 14px;
  font-weight: 600;
  margin-bottom: 7px;
}
.vault-space > small {
  display: block;
  color: var(--text-secondary);
  font-size: 11px;
  line-height: 1.7;
}
.vault-space-count {
  display: block;
  margin-top: 19px;
  color: var(--text-primary);
  font-size: 19px;
  font-weight: 600;
}
.vault-space-count small {
  color: var(--text-secondary);
  font-size: 11px;
  font-weight: 400;
  margin-left: 3px;
}
.vault-insights {
  margin-top: 32px;
}
.vault-insight-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}
.vault-insight {
  display: flex;
  align-items: center;
  gap: 14px;
  min-width: 0;
  text-align: left;
  padding: 20px 17px;
  color: var(--text-primary);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
}
.vault-insight > svg:first-child {
  color: var(--accent);
}
.vault-insight > svg:last-child {
  color: var(--text-secondary);
}
.vault-insight > span {
  flex: 1;
  min-width: 0;
}
.vault-insight strong {
  display: block;
  font-size: 13px;
  font-weight: 600;
}
.vault-insight small {
  display: block;
  font-size: 11px;
  color: var(--text-secondary);
  line-height: 1.8;
  margin-top: 6px;
}
.vault-insight:hover {
  border-color: var(--border-accent);
}
.vault-documents {
  margin-top: 32px;
}
.vault-document-toolbar {
  display: flex;
  align-items: center;
  gap: 9px;
  flex-wrap: wrap;
  margin-bottom: 13px;
}
.vault-document-toolbar .collection-search {
  min-width: 200px;
}
.vault-inline-filter select {
  height: 37px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-secondary);
  border-radius: var(--radius-sm);
  padding: 8px 9px;
  font-size: 12px;
}
.vault-pin-active {
  color: var(--accent);
  background: var(--accent-alpha);
  border-color: var(--border-accent);
}
.vault-document-panel {
  padding: 5px 20px;
}
.vault-loading {
  display: flex;
  gap: 10px;
  align-items: center;
  justify-content: center;
  min-height: 170px;
  color: var(--text-secondary);
  font-size: 13px;
}
.vault-show-more {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 16px 0;
  border: 0;
  background: none;
  color: var(--accent);
  font-size: 12px;
}
.vault-tools {
  margin-top: 32px;
  padding-top: 20px;
  border-top: 1px solid var(--border-light);
}
.vault-tools summary {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  list-style: none;
  color: var(--text-secondary);
  font-size: 13px;
}
.vault-tools summary::-webkit-details-marker {
  display: none;
}
.vault-tools summary > small {
  flex: 1;
  font-size: 11px;
  padding-left: 5px;
}
.vault-tools[open] summary > svg:last-child {
  transform: rotate(90deg);
}
.vault-tool-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 7px 20px;
  margin-top: 19px;
}
.vault-tool {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 13px 10px;
  color: var(--text-secondary);
  text-align: left;
  border: 0;
  background: transparent;
  border-radius: var(--radius-sm);
}
.vault-tool > span {
  flex: 1;
}
.vault-tool strong {
  display: block;
  color: var(--text-primary);
  font-size: 12px;
  font-weight: 500;
}
.vault-tool small {
  display: block;
  font-size: 10px;
  margin-top: 5px;
}
.vault-tool:hover {
  background: var(--bg-hover);
  color: var(--accent);
}
.vault-visually-hidden {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
@media (max-width: 1100px) {
  .vault-space-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
  .vault-insight-grid {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 650px) {
  .vault-space-grid,
  .vault-tool-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .vault-document-toolbar .collection-search {
    flex-basis: 100%;
  }
  .vault-document-panel {
    padding: 5px 14px;
  }
}
</style>
