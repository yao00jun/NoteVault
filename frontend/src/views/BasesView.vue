<script setup lang="ts">
import { ref, computed, onMounted, watch, onBeforeUnmount } from 'vue'
import {
  Table2,
  LayoutGrid,
  List as ListIcon,
  Plus,
  Save,
  Trash2,
  Pencil,
  Filter as FilterIcon,
  X,
  AlertTriangle,
  FileText,
  Sparkles,
  RefreshCw,
  ArrowUp,
  ArrowDown,
} from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { confirmDialog } from '@/composables/useConfirm'
import { useI18n } from 'vue-i18n'
import { useWorkspaceStore } from '@/stores/workspace'
import { toWorkspace, toWorkspaceList } from '@/utils/workspace'
import { BaseService, WorkspaceService } from '@bindings/github.com/notevault/notevault/index.js'
import type {
  BaseDef,
  BaseFilter,
  BaseFilterGroup,
  BaseResult,
  BaseSummary,
  BaseView,
  BuiltinTemplate,
  PropertyMeta,
} from '@bindings/github.com/notevault/notevault/models.js'

const router = useRouter()
const { t } = useI18n()
const workspaceStore = useWorkspaceStore()

const currentWorkspace = computed(() => workspaceStore.currentWorkspace)
const wsPath = computed(() => currentWorkspace.value?.path ?? '')

// 已保存的视图
const bases = ref<BaseSummary[]>([])
const selectedName = ref('')

// 当前正在编辑的定义（工作副本）。
// 编辑器改的是这份副本，磁盘上的定义只在点保存时才变——
// 否则改一个条件就写一次盘，撤销无从下手。
const def = ref<BaseDef | null>(null)
const savedSnapshot = ref('')
const activeViewId = ref('')

// 元信息
const properties = ref<PropertyMeta[]>([])
const operators = ref<string[]>([])
const templates = ref<BuiltinTemplate[]>([])

// 查询结果
const result = ref<BaseResult | null>(null)

const isLoading = ref(false)
const isRunning = ref(false)
const errorMsg = ref('')
const showBuilder = ref(true)

// 不需要输入值的运算符：给它们留一个输入框只会让人以为漏填了
const valuelessOperators = new Set(['empty', 'notEmpty'])

const dirty = computed(() => {
  if (!def.value) return false
  return JSON.stringify(def.value) !== savedSnapshot.value
})

const isSaved = computed(() => bases.value.some((b) => b.name === selectedName.value))

const activeView = computed<BaseView | null>(() => {
  if (!def.value?.views?.length) return null
  return def.value.views.find((v) => v.id === activeViewId.value) ?? def.value.views[0] ?? null
})

const propertyNames = computed(() => properties.value.map((p) => p.name))

/** 用于列选择器：把隐式属性排在自定义属性之后（后端已排好，这里只取名字） */
const columnCandidates = computed(() => properties.value)

function viewIcon(type: string) {
  if (type === 'board') return LayoutGrid
  if (type === 'list') return ListIcon
  return Table2
}

function operatorLabel(op: string): string {
  // 缺翻译时退回原始运算符名，而不是显示空白
  const key = `bases.operators.${op}`
  const label = t(key)
  return label === key ? op : label
}

function viewTypeLabel(type: string): string {
  const key = `bases.viewTypes.${type}`
  const label = t(key)
  return label === key ? type : label
}

async function ensureWorkspace(): Promise<boolean> {
  if (currentWorkspace.value) return true
  try {
    const ws = await WorkspaceService.GetCurrentWorkspace()
    if (ws) {
      workspaceStore.setCurrentWorkspace(toWorkspace(ws))
      return true
    }
  } catch (e) {
    console.error('Failed to get workspace:', e)
  }
  return false
}

async function loadMeta() {
  try {
    const [ops, tpls] = await Promise.all([BaseService.ListOperators(), BaseService.ListTemplates()])
    operators.value = (ops as string[]) || []
    templates.value = ((tpls as BuiltinTemplate[]) || []).filter(Boolean)
  } catch (e) {
    console.error('Failed to load bases meta:', e)
  }
}

async function loadProperties() {
  if (!wsPath.value) return
  try {
    const props = await BaseService.ListProperties(wsPath.value)
    properties.value = ((props as PropertyMeta[]) || []).filter(Boolean)
  } catch (e) {
    console.error('Failed to load properties:', e)
  }
}

async function loadBases() {
  if (!wsPath.value) return
  try {
    const list = await BaseService.ListBases(wsPath.value)
    bases.value = ((list as BaseSummary[]) || []).filter(Boolean)
  } catch (e) {
    console.error('Failed to list bases:', e)
    errorMsg.value = t('bases.loadFailed', { msg: (e as Error).message })
  }
}

async function init() {
  errorMsg.value = ''
  if (!(await ensureWorkspace())) return
  isLoading.value = true
  try {
    await Promise.all([loadMeta(), loadProperties(), loadBases()])
    // 有已保存视图就打开第一个，否则给一份能跑的空白定义——
    // 落地页是一张空表单是查询工具最大的上手门槛
    if (bases.value.length > 0) {
      await selectBase(bases.value[0].name)
    } else {
      await newBase()
    }
  } finally {
    isLoading.value = false
  }
}

function adopt(next: BaseDef, markSaved: boolean) {
  def.value = next
  savedSnapshot.value = markSaved ? JSON.stringify(next) : ''
  activeViewId.value = next.views?.[0]?.id ?? ''
}

async function selectBase(name: string) {
  errorMsg.value = ''
  try {
    const loaded = await BaseService.LoadBase(wsPath.value, name)
    if (!loaded) return
    selectedName.value = name
    adopt(loaded as BaseDef, true)
    await run()
  } catch (e) {
    console.error('Failed to load base:', e)
    errorMsg.value = t('bases.openFailed', { msg: (e as Error).message })
  }
}

async function newBase() {
  errorMsg.value = ''
  try {
    const tpl = await BaseService.NewBaseTemplate(t('bases.untitled'))
    if (!tpl) return
    selectedName.value = (tpl as BaseDef).name
    adopt(tpl as BaseDef, false)
    await run()
  } catch (e) {
    console.error('Failed to create base template:', e)
    errorMsg.value = t('bases.createFailed', { msg: (e as Error).message })
  }
}

async function useTemplate(tpl: BuiltinTemplate) {
  errorMsg.value = ''
  // 深拷贝：模板是后端常量的副本，直接改会污染同一次会话里的其它模板实例
  const copy = JSON.parse(JSON.stringify(tpl.def)) as BaseDef
  selectedName.value = copy.name
  adopt(copy, false)
  await run()
}

async function save() {
  if (!def.value || !wsPath.value) return
  const name = (def.value.name || '').trim()
  if (!name) {
    errorMsg.value = t('bases.nameRequired')
    return
  }
  errorMsg.value = ''
  try {
    await BaseService.SaveBase(wsPath.value, def.value as any)
    selectedName.value = name
    savedSnapshot.value = JSON.stringify(def.value)
    await loadBases()
  } catch (e) {
    console.error('Failed to save base:', e)
    errorMsg.value = t('bases.saveFailed', { msg: (e as Error).message })
  }
}

async function remove() {
  if (!isSaved.value || !wsPath.value) return
  if (!(await confirmDialog({ message: t('bases.confirmDelete', { name: selectedName.value }), danger: true }))) return
  try {
    await BaseService.DeleteBase(wsPath.value, selectedName.value)
    await loadBases()
    if (bases.value.length > 0) {
      await selectBase(bases.value[0].name)
    } else {
      await newBase()
    }
  } catch (e) {
    console.error('Failed to delete base:', e)
    errorMsg.value = t('bases.deleteFailed', { msg: (e as Error).message })
  }
}

async function rename() {
  if (!isSaved.value || !def.value || !wsPath.value) return
  const next = window.prompt(t('bases.promptNewName'), selectedName.value)
  if (!next || next.trim() === selectedName.value) return
  try {
    await BaseService.RenameBase(wsPath.value, selectedName.value, next.trim())
    await loadBases()
    await selectBase(next.trim())
  } catch (e) {
    console.error('Failed to rename base:', e)
    errorMsg.value = t('bases.renameFailed', { msg: (e as Error).message })
  }
}

// ---------------------------------------------------------------------------
// 查询执行
// ---------------------------------------------------------------------------

async function run() {
  if (!def.value || !wsPath.value) return
  isRunning.value = true
  try {
    const res = await BaseService.RunBase(wsPath.value, def.value as any, activeViewId.value)
    result.value = (res as BaseResult) ?? null
  } catch (e) {
    console.error('Failed to run base:', e)
    errorMsg.value = t('bases.runFailed', { msg: (e as Error).message })
    result.value = null
  } finally {
    isRunning.value = false
  }
}

// 改一个条件就重跑一次，但要防抖：拖滑块 / 连续打字时不该打出十几个请求
let runTimer: ReturnType<typeof setTimeout> | null = null
function scheduleRun() {
  if (runTimer) clearTimeout(runTimer)
  runTimer = setTimeout(() => {
    runTimer = null
    void run()
  }, 220)
}
onBeforeUnmount(() => {
  if (runTimer) clearTimeout(runTimer)
})

async function refresh() {
  // 用外部编辑器改了 front matter 时，属性索引有 30s 缓存，手动刷新绕过它
  if (!wsPath.value) return
  await BaseService.InvalidateCache(wsPath.value)
  await Promise.all([loadProperties(), run()])
}

function switchView(id: string) {
  activeViewId.value = id
  void run()
}

// ---------------------------------------------------------------------------
// 条件编辑
// ---------------------------------------------------------------------------

/**
 * 确保当前视图定义带有可编辑的 filters 结构（bindings 类型里
 * conditions / groups 都是可空的，这里统一补齐），返回条件组对象。
 * def 尚未就绪时返回 null，调用方直接放弃本次编辑动作。
 */
function ensureFilters(): BaseFilterGroup | null {
  if (!def.value) return null
  if (!def.value.filters) {
    def.value.filters = { conjunction: 'and', conditions: [], groups: [] } as BaseFilterGroup
  }
  const filters = def.value.filters
  if (!filters.conditions) filters.conditions = []
  return filters
}

function addCondition() {
  const filters = ensureFilters()
  // conditions 在 ensureFilters 里已补齐，这里只为通过可空类型检查
  if (!filters?.conditions) return
  filters.conditions.push({
    property: propertyNames.value[0] ?? 'file.title',
    operator: 'contains',
    value: '',
  } as BaseFilter)
  scheduleRun()
}

function removeCondition(index: number) {
  if (!def.value?.filters?.conditions) return
  def.value.filters.conditions.splice(index, 1)
  scheduleRun()
}

function setConjunction(conj: string) {
  const filters = ensureFilters()
  if (!filters) return
  filters.conjunction = conj
  scheduleRun()
}

function onConditionChange(cond: BaseFilter) {
  // 切到 empty / notEmpty 时把残留的值清掉，避免保存下来一个看不见的脏值
  if (valuelessOperators.has(cond.operator)) cond.value = ''
  scheduleRun()
}

// ---------------------------------------------------------------------------
// 视图配置（排序 / 分组 / 列）
// ---------------------------------------------------------------------------

const sortProperty = computed(() => activeView.value?.sort?.[0]?.property ?? '')
const sortDesc = computed(() => activeView.value?.sort?.[0]?.desc ?? false)

function setSortProperty(prop: string) {
  const v = activeView.value
  if (!v) return
  if (!prop) {
    v.sort = []
  } else {
    v.sort = [{ property: prop, desc: v.sort?.[0]?.desc ?? false }]
  }
  scheduleRun()
}

function toggleSortDir() {
  const v = activeView.value
  if (!v?.sort?.length) return
  v.sort[0].desc = !v.sort[0].desc
  scheduleRun()
}

function setGroupBy(prop: string) {
  const v = activeView.value
  if (!v) return
  v.groupBy = prop
  scheduleRun()
}

function toggleColumn(name: string) {
  const v = activeView.value
  if (!v) return
  if (!v.columns) v.columns = []
  const i = v.columns.indexOf(name)
  if (i >= 0) {
    v.columns.splice(i, 1)
  } else {
    v.columns.push(name)
  }
  scheduleRun()
}

function isColumnOn(name: string): boolean {
  return !!activeView.value?.columns?.includes(name)
}

function openRow(path: string) {
  workspaceStore.openFile(path)
  workspaceStore.setActiveFile(path)
  router.push('/editor')
}

const groupedResult = computed(() => result.value?.groups?.filter(Boolean) ?? [])
const flatRows = computed(() => result.value?.rows?.filter(Boolean) ?? [])
const hasGroups = computed(() => groupedResult.value.length > 0)

onMounted(init)
watch(() => currentWorkspace.value?.id, init)
</script>

<template>
  <div class="bases-view">
    <!-- 未选工作区 -->
    <div
      v-if="!currentWorkspace"
      class="empty-state"
      data-testid="bases-no-workspace"
    >
      <Table2 :size="48" />
      <h3>{{ t('bases.noWorkspaceTitle') }}</h3>
      <p>{{ t('bases.noWorkspaceDesc') }}</p>
    </div>

    <template v-else>
      <!-- 左侧：视图列表 + 模板 -->
      <aside class="bases-rail">
        <div class="rail-header">
          <span class="rail-title">{{ t('bases.savedTitle') }}</span>
          <button
            class="icon-btn"
            :title="t('bases.new')"
            data-testid="bases-new"
            @click="newBase"
          >
            <Plus :size="14" />
          </button>
        </div>

        <div class="rail-list">
          <button
            v-for="b in bases"
            :key="b.name"
            class="rail-item"
            :class="{ active: b.name === selectedName }"
            data-testid="bases-item"
            @click="selectBase(b.name)"
          >
            <span class="rail-item-name">{{ b.name }}</span>
            <span class="rail-item-meta">
              {{ t('bases.filterCount', { count: b.filterCount }) }}
            </span>
          </button>
          <p
            v-if="bases.length === 0"
            class="rail-empty"
            data-testid="bases-rail-empty"
          >
            {{ t('bases.savedEmpty') }}
          </p>
        </div>

        <div class="rail-header templates">
          <Sparkles :size="12" />
          <span class="rail-title">{{ t('bases.templatesTitle') }}</span>
        </div>
        <div class="rail-list">
          <button
            v-for="tpl in templates"
            :key="tpl.id"
            class="rail-item template"
            data-testid="bases-template"
            :title="tpl.description"
            @click="useTemplate(tpl)"
          >
            <span class="rail-item-name">{{ tpl.title }}</span>
            <span class="rail-item-meta">{{ tpl.description }}</span>
          </button>
        </div>
      </aside>

      <!-- 右侧：主区 -->
      <section class="bases-main">
        <header class="main-header">
          <input
            v-if="def"
            v-model="def.name"
            class="name-input"
            data-testid="bases-name"
            :placeholder="t('bases.namePlaceholder')"
          >
          <div class="header-actions">
            <button
              class="ghost-btn"
              :title="t('bases.refresh')"
              data-testid="bases-refresh"
              @click="refresh"
            >
              <RefreshCw
                :size="14"
                :class="{ spinning: isRunning }"
              />
            </button>
            <button
              class="ghost-btn"
              :disabled="!isSaved"
              :title="t('bases.rename')"
              data-testid="bases-rename"
              @click="rename"
            >
              <Pencil :size="14" />
            </button>
            <button
              class="ghost-btn danger"
              :disabled="!isSaved"
              :title="t('bases.delete')"
              data-testid="bases-delete"
              @click="remove"
            >
              <Trash2 :size="14" />
            </button>
            <button
              class="primary-btn"
              data-testid="bases-save"
              @click="save"
            >
              <Save :size="14" />
              <span>{{ dirty ? t('bases.saveDirty') : t('bases.save') }}</span>
            </button>
          </div>
        </header>

        <!-- 视图切换 -->
        <div
          v-if="def?.views?.length"
          class="view-tabs"
        >
          <button
            v-for="v in def.views"
            :key="v.id"
            class="view-tab"
            :class="{ active: v.id === activeView?.id }"
            data-testid="bases-view-tab"
            @click="switchView(v.id)"
          >
            <component
              :is="viewIcon(v.type)"
              :size="14"
            />
            <span>{{ v.name || viewTypeLabel(v.type) }}</span>
          </button>
          <button
            class="view-tab toggle-builder"
            data-testid="bases-toggle-builder"
            @click="showBuilder = !showBuilder"
          >
            <FilterIcon :size="14" />
            <span>{{ showBuilder ? t('bases.hideBuilder') : t('bases.showBuilder') }}</span>
          </button>
        </div>

        <!-- 查询构建器 -->
        <div
          v-if="showBuilder && def"
          class="builder"
          data-testid="bases-builder"
        >
          <div class="builder-row">
            <label class="builder-label">{{ t('bases.folderScope') }}</label>
            <input
              v-model="def.folder"
              class="text-input narrow"
              data-testid="bases-folder"
              :placeholder="t('bases.folderPlaceholder')"
              @change="scheduleRun"
            >
            <label class="builder-label">{{ t('bases.match') }}</label>
            <div class="seg">
              <button
                class="seg-btn"
                :class="{ active: def.filters?.conjunction !== 'or' }"
                data-testid="bases-conj-and"
                @click="setConjunction('and')"
              >
                {{ t('bases.matchAll') }}
              </button>
              <button
                class="seg-btn"
                :class="{ active: def.filters?.conjunction === 'or' }"
                data-testid="bases-conj-or"
                @click="setConjunction('or')"
              >
                {{ t('bases.matchAny') }}
              </button>
            </div>
          </div>

          <div class="conditions">
            <div
              v-for="(cond, i) in def.filters?.conditions ?? []"
              :key="i"
              class="condition"
              data-testid="bases-condition"
            >
              <select
                v-model="cond.property"
                class="select"
                data-testid="bases-cond-property"
                @change="onConditionChange(cond)"
              >
                <option
                  v-for="p in properties"
                  :key="p.name"
                  :value="p.name"
                >
                  {{ p.name }}{{ p.implicit ? '' : ` (${p.count})` }}
                </option>
              </select>
              <select
                v-model="cond.operator"
                class="select"
                data-testid="bases-cond-operator"
                @change="onConditionChange(cond)"
              >
                <option
                  v-for="op in operators"
                  :key="op"
                  :value="op"
                >
                  {{ operatorLabel(op) }}
                </option>
              </select>
              <input
                v-if="!valuelessOperators.has(cond.operator)"
                v-model="cond.value"
                class="text-input"
                data-testid="bases-cond-value"
                :placeholder="
                  cond.operator === 'in' || cond.operator === 'notIn'
                    ? t('bases.valueListPlaceholder')
                    : t('bases.valuePlaceholder')
                "
                @input="scheduleRun"
              >
              <span
                v-else
                class="value-na"
              >—</span>
              <button
                class="icon-btn"
                :title="t('bases.removeCondition')"
                data-testid="bases-remove-condition"
                @click="removeCondition(i)"
              >
                <X :size="14" />
              </button>
            </div>

            <button
              class="add-condition"
              data-testid="bases-add-condition"
              @click="addCondition"
            >
              <Plus :size="14" />
              <span>{{ t('bases.addCondition') }}</span>
            </button>
          </div>

          <div
            v-if="activeView"
            class="builder-row"
          >
            <label class="builder-label">{{ t('bases.sortBy') }}</label>
            <select
              class="select"
              data-testid="bases-sort-property"
              :value="sortProperty"
              @change="setSortProperty(($event.target as HTMLSelectElement).value)"
            >
              <option value="">
                {{ t('bases.sortNone') }}
              </option>
              <option
                v-for="p in properties"
                :key="p.name"
                :value="p.name"
              >
                {{ p.name }}
              </option>
            </select>
            <button
              class="ghost-btn"
              :disabled="!sortProperty"
              :title="sortDesc ? t('bases.sortDesc') : t('bases.sortAsc')"
              data-testid="bases-sort-dir"
              @click="toggleSortDir"
            >
              <component
                :is="sortDesc ? ArrowDown : ArrowUp"
                :size="14"
              />
            </button>

            <label class="builder-label">{{ t('bases.groupBy') }}</label>
            <select
              class="select"
              data-testid="bases-group-property"
              :value="activeView.groupBy ?? ''"
              @change="setGroupBy(($event.target as HTMLSelectElement).value)"
            >
              <option value="">
                {{ t('bases.groupNone') }}
              </option>
              <option
                v-for="p in properties"
                :key="p.name"
                :value="p.name"
              >
                {{ p.name }}
              </option>
            </select>
          </div>

          <div
            v-if="activeView"
            class="columns-picker"
          >
            <span class="builder-label">{{ t('bases.columns') }}</span>
            <button
              v-for="p in columnCandidates"
              :key="p.name"
              class="chip"
              :class="{ on: isColumnOn(p.name) }"
              data-testid="bases-column-chip"
              @click="toggleColumn(p.name)"
            >
              {{ p.name }}
            </button>
          </div>
        </div>

        <!-- 告警：属性名打错 / 正则写坏都在这里冒出来，而不是弹窗打断 -->
        <div
          v-if="result?.warnings?.length"
          class="warnings"
          data-testid="bases-warnings"
        >
          <AlertTriangle :size="14" />
          <ul>
            <li
              v-for="(w, i) in result.warnings"
              :key="i"
            >
              {{ w }}
            </li>
          </ul>
        </div>

        <div
          v-if="errorMsg"
          class="error-bar"
          data-testid="bases-error"
        >
          {{ errorMsg }}
        </div>

        <!-- 结果统计 -->
        <div
          v-if="result"
          class="result-meta"
          data-testid="bases-meta"
        >
          <span>{{ t('bases.resultCount', { returned: result.returned, total: result.total }) }}</span>
          <span class="dim">{{ t('bases.scanned', { count: result.scanned }) }}</span>
          <span
            v-if="result.truncated"
            class="truncated"
            data-testid="bases-truncated"
          >
            {{ t('bases.truncated') }}
          </span>
        </div>

        <!-- 结果区 -->
        <div class="result-area">
          <div
            v-if="isLoading"
            class="empty-state small"
          >
            {{ t('common.loading') }}
          </div>

          <div
            v-else-if="result && result.returned === 0"
            class="empty-state small"
            data-testid="bases-empty"
          >
            <FileText :size="36" />
            <h3>{{ t('bases.emptyTitle') }}</h3>
            <p>{{ t('bases.emptyDesc') }}</p>
          </div>

          <!-- 看板 / 分组 -->
          <div
            v-else-if="activeView?.type === 'board' && hasGroups"
            class="board"
            data-testid="bases-board"
          >
            <div
              v-for="g in groupedResult"
              :key="g!.key"
              class="board-column"
              data-testid="bases-board-column"
            >
              <div class="board-head">
                <span class="board-label">{{ g!.label }}</span>
                <span class="board-count">{{ g!.count }}</span>
              </div>
              <button
                v-for="row in g!.rows?.filter(Boolean) ?? []"
                :key="row!.path"
                class="card"
                data-testid="bases-board-card"
                @click="openRow(row!.path)"
              >
                <span class="card-title">{{ row!.title }}</span>
                <span
                  v-for="cell in row!.cells"
                  :key="cell.property"
                  class="card-cell"
                >
                  <template v-if="cell.kind === 'list' && cell.list?.length">
                    <span
                      v-for="item in cell.list"
                      :key="item"
                      class="tag-chip"
                    >{{ item }}</span>
                  </template>
                  <template v-else-if="cell.display">
                    {{ cell.display }}
                  </template>
                </span>
              </button>
            </div>
          </div>

          <!-- 列表 -->
          <div
            v-else-if="activeView?.type === 'list'"
            class="list"
            data-testid="bases-list"
          >
            <button
              v-for="row in flatRows"
              :key="row!.path"
              class="list-row"
              data-testid="bases-list-row"
              @click="openRow(row!.path)"
            >
              <span class="list-title">{{ row!.title }}</span>
              <span class="list-cells">
                <span
                  v-for="cell in row!.cells"
                  :key="cell.property"
                  class="list-cell"
                >
                  <template v-if="cell.kind === 'list' && cell.list?.length">
                    <span
                      v-for="item in cell.list"
                      :key="item"
                      class="tag-chip"
                    >{{ item }}</span>
                  </template>
                  <template v-else>{{ cell.display }}</template>
                </span>
              </span>
            </button>
          </div>

          <!-- 表格（默认） -->
          <div
            v-else
            class="table-wrap"
            data-testid="bases-table"
          >
            <table class="table">
              <thead>
                <tr>
                  <th
                    v-for="col in result?.columns ?? []"
                    :key="col"
                    :class="{ sorted: col === sortProperty }"
                    data-testid="bases-th"
                    @click="setSortProperty(col === sortProperty && sortDesc ? '' : col)"
                  >
                    {{ col }}
                    <component
                      v-if="col === sortProperty"
                      :is="sortDesc ? ArrowDown : ArrowUp"
                      :size="12"
                    />
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="row in flatRows"
                  :key="row!.path"
                  data-testid="bases-tr"
                  @click="openRow(row!.path)"
                >
                  <td
                    v-for="cell in row!.cells"
                    :key="cell.property"
                    :class="{ numeric: cell.kind === 'number', dim: cell.empty }"
                  >
                    <template v-if="cell.kind === 'list' && cell.list?.length">
                      <span
                        v-for="item in cell.list"
                        :key="item"
                        class="tag-chip"
                      >{{ item }}</span>
                    </template>
                    <template v-else-if="cell.kind === 'bool'">
                      {{ cell.bool ? '✓' : '·' }}
                    </template>
                    <template v-else>
                      {{ cell.display }}
                    </template>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.bases-view {
  display: flex;
  height: 100%;
  overflow: hidden;
  background: var(--bg-primary);
}

/* 左栏 */
.bases-rail {
  width: 220px;
  flex-shrink: 0;
  border-right: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  background: var(--bg-sidebar);
}

.rail-header {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  padding: var(--space-3) var(--space-3) var(--space-1);
}

.rail-header.templates {
  margin-top: var(--space-3);
  border-top: 1px solid var(--border);
  padding-top: var(--space-3);
  color: var(--text-muted);
}

.rail-title {
  flex: 1;
  font-size: var(--text-xs);
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.rail-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 0 var(--space-2);
}

.rail-item {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  padding: var(--space-2);
  border-radius: var(--radius-sm);
  text-align: left;
  color: var(--text-secondary);
  transition: background var(--transition-fast), color var(--transition-fast);
}

.rail-item:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.rail-item.active {
  background: var(--accent-alpha, rgba(0, 122, 255, 0.1));
  color: var(--accent);
  font-weight: 600;
}

.rail-item-name {
  font-size: var(--text-sm);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}

.rail-item-meta {
  font-size: var(--text-xs);
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}

.rail-empty {
  padding: var(--space-2);
  font-size: var(--text-xs);
  color: var(--text-muted);
}

/* 主区 */
.bases-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.main-header {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-3);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.name-input {
  flex: 1;
  min-width: 0;
  background: transparent;
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  padding: var(--space-1) var(--space-2);
  font-size: var(--text-lg);
  font-weight: 600;
  color: var(--text-primary);
}

.name-input:hover,
.name-input:focus {
  border-color: var(--border);
  background: var(--bg-card);
  outline: none;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex-shrink: 0;
}

.icon-btn,
.ghost-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--space-1) var(--space-2);
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  transition: background var(--transition-fast), color var(--transition-fast);
}

.icon-btn:hover,
.ghost-btn:hover:not(:disabled) {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.ghost-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.ghost-btn.danger:hover:not(:disabled) {
  color: var(--danger, #ef4444);
}

.primary-btn {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  padding: var(--space-1) var(--space-3);
  border-radius: var(--radius-sm);
  background: var(--accent);
  color: var(--text-inverse);
  font-size: var(--text-sm);
  font-weight: 500;
}

.primary-btn:hover {
  background: var(--accent-hover);
}

.spinning {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

/* 视图切换 */
.view-tabs {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  padding: var(--space-2) var(--space-3) 0;
  flex-shrink: 0;
}

.view-tab {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  padding: var(--space-1) var(--space-3);
  border-radius: var(--radius-sm) var(--radius-sm) 0 0;
  font-size: var(--text-sm);
  color: var(--text-muted);
  border-bottom: 2px solid transparent;
}

.view-tab:hover {
  color: var(--text-primary);
}

.view-tab.active {
  color: var(--accent);
  border-bottom-color: var(--accent);
  font-weight: 600;
}

.view-tab.toggle-builder {
  margin-left: auto;
}

/* 构建器 */
.builder {
  padding: var(--space-3);
  border-bottom: 1px solid var(--border);
  background: var(--bg-card);
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  flex-shrink: 0;
}

.builder-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.builder-label {
  font-size: var(--text-xs);
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.select,
.text-input {
  padding: var(--space-1) var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--text-sm);
  min-width: 0;
}

.text-input {
  flex: 1;
}

.text-input.narrow {
  flex: 0 1 180px;
}

.seg {
  display: flex;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  overflow: hidden;
}

.seg-btn {
  padding: var(--space-1) var(--space-3);
  font-size: var(--text-xs);
  color: var(--text-muted);
}

.seg-btn.active {
  background: var(--accent);
  color: var(--text-inverse);
}

.conditions {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.condition {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.value-na {
  flex: 1;
  color: var(--text-muted);
  font-size: var(--text-sm);
}

.add-condition {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  align-self: flex-start;
  padding: var(--space-1) var(--space-2);
  border-radius: var(--radius-sm);
  color: var(--accent);
  font-size: var(--text-sm);
}

.add-condition:hover {
  background: var(--accent-alpha, rgba(0, 122, 255, 0.1));
}

.columns-picker {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex-wrap: wrap;
}

.chip {
  padding: 2px var(--space-2);
  border: 1px solid var(--border);
  border-radius: 999px;
  font-size: var(--text-xs);
  color: var(--text-muted);
}

.chip.on {
  background: var(--accent);
  border-color: var(--accent);
  color: var(--text-inverse);
}

/* 告警 / 错误 */
.warnings {
  display: flex;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  background: var(--warning-bg, rgba(245, 158, 11, 0.12));
  color: var(--warning, #f59e0b);
  font-size: var(--text-xs);
  flex-shrink: 0;
}

.warnings ul {
  margin: 0;
  padding-left: var(--space-3);
}

.error-bar {
  padding: var(--space-2) var(--space-3);
  background: var(--danger-bg, rgba(239, 68, 68, 0.12));
  color: var(--danger, #ef4444);
  font-size: var(--text-sm);
  flex-shrink: 0;
}

.result-meta {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-3);
  font-size: var(--text-xs);
  color: var(--text-secondary);
  flex-shrink: 0;
}

.result-meta .dim {
  color: var(--text-muted);
}

.result-meta .truncated {
  color: var(--warning, #f59e0b);
  font-weight: 600;
}

/* 结果区 */
.result-area {
  flex: 1;
  overflow: auto;
  padding: 0 var(--space-3) var(--space-3);
}

.table-wrap {
  overflow-x: auto;
}

.table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--text-sm);
}

.table th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--bg-card);
  text-align: left;
  padding: var(--space-2);
  font-size: var(--text-xs);
  font-weight: 600;
  color: var(--text-muted);
  border-bottom: 1px solid var(--border);
  cursor: pointer;
  white-space: nowrap;
}

.table th.sorted {
  color: var(--accent);
}

.table td {
  padding: var(--space-2);
  border-bottom: 1px solid var(--border);
  color: var(--text-secondary);
  vertical-align: top;
}

.table td.numeric {
  text-align: right;
  font-variant-numeric: tabular-nums;
}

.table td.dim {
  color: var(--text-muted);
}

.table tbody tr:hover {
  background: var(--bg-hover);
  cursor: pointer;
}

.tag-chip {
  display: inline-block;
  padding: 1px var(--space-2);
  margin: 0 2px 2px 0;
  border-radius: 999px;
  background: var(--accent-alpha, rgba(0, 122, 255, 0.1));
  color: var(--accent);
  font-size: var(--text-xs);
}

/* 看板 */
.board {
  display: flex;
  gap: var(--space-3);
  align-items: flex-start;
  overflow-x: auto;
  padding-top: var(--space-2);
}

.board-column {
  flex: 0 0 240px;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-2);
}

.board-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: var(--text-xs);
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.board-count {
  color: var(--accent);
}

.card {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  padding: var(--space-2);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  border: 1px solid var(--border);
  text-align: left;
  width: 100%;
}

.card:hover {
  border-color: var(--border-accent, var(--accent));
}

.card-title {
  font-size: var(--text-sm);
  font-weight: 500;
  color: var(--text-primary);
}

.card-cell {
  font-size: var(--text-xs);
  color: var(--text-muted);
}

/* 列表 */
.list {
  display: flex;
  flex-direction: column;
  padding-top: var(--space-2);
}

.list-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  padding: var(--space-2);
  border-bottom: 1px solid var(--border);
  text-align: left;
}

.list-row:hover {
  background: var(--bg-hover);
}

.list-title {
  font-size: var(--text-sm);
  color: var(--text-primary);
}

.list-cells {
  display: flex;
  gap: var(--space-2);
  font-size: var(--text-xs);
  color: var(--text-muted);
  flex-shrink: 0;
}

/* 空状态 */
.empty-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  color: var(--text-muted);
  text-align: center;
}

.empty-state.small {
  padding: var(--space-6) 0;
}

.empty-state h3 {
  margin: 0;
  font-size: var(--text-base);
  color: var(--text-secondary);
}

.empty-state p {
  margin: 0;
  font-size: var(--text-sm);
}
</style>
