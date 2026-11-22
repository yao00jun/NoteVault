<script setup lang="ts">
import { useViewRoute } from '@/composables/useViewRoute'
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  AlertTriangle,
  ArrowUpRight,
  BookMarked,
  BookOpen,
  CheckCircle2,
  ChevronRight,
  Clock,
  Code2,
  FileText,
  Layers,
  ListTodo,
  PackageOpen,
  Pin,
  Plus,
  Circle,
  Flag,
  RefreshCw,
  Rocket,
  Search,
  Sparkles,
  X,
} from '@lucide/vue'
import type { WorkbenchDeliverable, WorkbenchProject, WorkbenchTask } from '@/api/workbench'
import { FileService } from '@/api'
import TaskCard from '@/components/workbench/TaskCard.vue'
import { useWorkbenchStore } from '@/stores/workbench'
import { useWorkspaceStore } from '@/stores/workspace'
import { useToast } from '@/composables/useToast'
import { requestCopilot } from '@/composables/useCopilotRequest'
import {
  matchesTaskPeriod,
  normalizedCollectionPath,
  projectStatusLabel,
  projectSummaryContext,
  tasksForProject,
  type TaskPeriod,
} from '@/utils/workbenchCollections'
import '@/styles/workbench-collections.css'
import { requestSourceImport } from '@/composables/useSourceImport'
import { formatRelativeTime } from '@/utils/relativeTime'
import CollectionOnboarding from '@/components/workbench/CollectionOnboarding.vue'
import PageHeader from '@/components/common/PageHeader.vue'

const workbench = useWorkbenchStore()
const workspace = useWorkspaceStore()
const router = useRouter()
const route = useViewRoute('/projects')
const toast = useToast()
const { t } = useI18n()
const search = ref('')
const statusFilter = ref('all')
const typeFilter = ref('all')
const periodFilter = ref<TaskPeriod>('all')
const detailTab = ref('tasks')
// 资料页签内的文档筛选：架构总结不再是独立页签，而是资料的一种视图
const docFilter = ref<'all' | 'notes' | 'architecture'>('all')
const showTaskForm = ref(false)
const taskTitle = ref('')
const taskKind = ref('US')
const taskDue = ref('')
const addingTask = ref(false)
const taskError = ref('')

// 三个页签覆盖项目全貌：任务推进、资料（文档/架构/技术栈/复习）、交付物。
const tabs = [
  { id: 'tasks', icon: ListTodo },
  { id: 'notes', icon: FileText },
  { id: 'deliverables', icon: PackageOpen },
]
const selectedProject = computed(() => {
  const query = route.query.project
  return workbench.projects.find((project) =>
    [project.path, project.folder, project.name].includes(String(query ?? ''))
  )
})
const statusOptions = computed(() => [
  ...new Set([
    '进行中',
    '暂停',
    '已完成',
    ...workbench.projects.map((project) => projectStatusLabel(project.status)),
  ]),
])
const activeProjects = computed(
  () =>
    workbench.projects.filter((project) => projectStatusLabel(project.status) === '进行中').length
)
const openTaskCount = computed(
  () =>
    workbench.tasks.filter((task) => !task.completed && Boolean(task.project || task.projectPath))
      .length
)
const todayIds = computed(() => new Set(workbench.todayTasks.map((task) => task.id)))
const selectedTasks = computed(() =>
  selectedProject.value ? tasksForProject(selectedProject.value, workbench.tasks) : []
)

function taskMatches(task: WorkbenchTask) {
  return (
    (typeFilter.value === 'all' || task.type === typeFilter.value) &&
    matchesTaskPeriod(task, periodFilter.value, workbench.today)
  )
}

const displayedTasks = computed(() => selectedTasks.value.filter(taskMatches))
const projectCards = computed(() =>
  workbench.projects
    .map((project) => {
      const tasks = tasksForProject(project, workbench.tasks)
      return {
        project,
        tasks,
        visibleTasks: tasks.filter(taskMatches),
        done: tasks.filter((task) => task.completed).length,
        todayCount: tasks.filter((task) => !task.completed && todayIds.value.has(task.id)).length,
      }
    })
    .filter((card) => {
      const query = search.value.trim().toLocaleLowerCase()
      if (
        statusFilter.value !== 'all' &&
        projectStatusLabel(card.project.status) !== statusFilter.value
      )
        return false
      if (
        query &&
        ![card.project.name, card.project.nextStep, ...card.project.techStack]
          .join(' ')
          .toLocaleLowerCase()
          .includes(query)
      )
        return false
      return (
        (typeFilter.value === 'all' && periodFilter.value === 'all') || card.visibleTasks.length > 0
      )
    })
    .sort((a, b) => b.project.modifiedAt.localeCompare(a.project.modifiedAt))
)

const projectNotes = computed(() => {
  const project = selectedProject.value
  if (!project) return []
  const folder = normalizedCollectionPath(project.folder) + '/'
  const documents = [
    ...project.notes,
    ...workbench.documents.filter((doc) => normalizedCollectionPath(doc.path).startsWith(folder)),
  ]
  return [...new Map(documents.map((doc) => [doc.path, doc])).values()]
    .filter(
      (doc) =>
        doc.path !== project.path && !/(?:^|\/)tasks\.md$/i.test(normalizedCollectionPath(doc.path))
    )
    .sort((a, b) => b.modifiedAt.localeCompare(a.modifiedAt))
})
const architectureNotes = computed(() =>
  projectNotes.value.filter((doc) =>
    /架构|architecture|(?:^|[/\s-])adr(?:[/\s.-]|$)/i.test(doc.title + ' ' + doc.path)
  )
)
const architecturePaths = computed(() => new Set(architectureNotes.value.map((doc) => doc.path)))
const noteOnlyDocuments = computed(() =>
  projectNotes.value.filter((doc) => !architecturePaths.value.has(doc.path))
)
const displayedDocuments = computed(() => {
  if (docFilter.value === 'architecture') return architectureNotes.value
  if (docFilter.value === 'notes') return noteOnlyDocuments.value
  return projectNotes.value
})
const docFilterOptions = computed(() => [
  { id: 'all' as const, count: projectNotes.value.length },
  { id: 'notes' as const, count: noteOnlyDocuments.value.length },
  { id: 'architecture' as const, count: architectureNotes.value.length },
])
const relatedBooks = computed(() => {
  const project = selectedProject.value
  if (!project) return []
  return workbench.books.filter(
    (book) =>
      book.projects.some((name) => [project.name, project.path, project.folder].includes(name)) ||
      project.techStack.some((tech) =>
        (book.name + ' ' + book.folder.split('/').pop())
          .toLocaleLowerCase()
          .includes(tech.toLocaleLowerCase())
      )
  )
})
// 蒸馏卡片的 source 恰为 WorkbenchProject.folder（"Projects/<名>"），据此关联到本项目。
const projectReviewCards = computed(() => {
  const project = selectedProject.value
  if (!project) return []
  return workbench.cards.filter((card) => card.source === project.folder)
})
const projectDueReviewCards = computed(() =>
  projectReviewCards.value.filter((card) => card.due && card.due <= workbench.today)
)

function selectProject(project: WorkbenchProject) {
  detailTab.value = 'tasks'
  docFilter.value = 'all'
  void router.push({ path: '/projects', query: { project: project.path } })
}
function showPortfolio() {
  void router.push('/projects')
}
function openDocument(path: string) {
  workspace.openFile(path)
  void router.push({ path: '/editor', query: { file: path } })
}

const deliverableStages = ['draft', 'review', 'final', 'other'] as const
const deliverableStageFilter = ref<'all' | (typeof deliverableStages)[number]>('all')
// 交付物分组维度：默认按阶段（初稿→成稿→发布），可切换按类型（BA 需求/设计文档/…）
const deliverableViewMode = ref<'stage' | 'type'>('stage')

function normalizeDeliverableStage(stage: string): string {
  const value = stage.trim().toLowerCase()
  if (['draft', '初稿', '草稿', 'wip'].includes(value)) return 'draft'
  if (['review', '修改稿', '评审', '待审'].includes(value)) return 'review'
  if (['final', '最终版', '完成', 'published', '已发布'].includes(value)) return 'final'
  return 'other'
}

function deliverableTypeLabel(type: string): string {
  const value = (type || 'other').trim().toLowerCase()
  const key = `projects.deliverableType.${value}`
  const label = t(key)
  return label === key ? value : label
}

// 里程碑逾期判定：本地时区的当天日期（YYYY-MM-DD 字典序即时间序）
function isOverdue(date: string): boolean {
  if (!date) return false
  const now = new Date()
  const mm = String(now.getMonth() + 1).padStart(2, '0')
  const dd = String(now.getDate()).padStart(2, '0')
  return date < `${now.getFullYear()}-${mm}-${dd}`
}

// 交付物按阶段归类展示，空阶段不渲染，保持面板紧凑。
const deliverableGroups = computed(() => {
  const project = selectedProject.value
  if (!project) return []
  return deliverableStages
    .map((stage) => ({
      key: 'stage-' + stage,
      stage,
      label: t(`projects.deliverableStage.${stage}`),
      items: (project.deliverables ?? []).filter(
        (item: WorkbenchDeliverable) => normalizeDeliverableStage(item.stage) === stage
      ),
    }))
    .filter((group) => group.items.length > 0)
})
// 按类型分组：BA 需求文档、开发设计文档等 frontmatter type 约定
const deliverableTypeGroups = computed(() => {
  const project = selectedProject.value
  if (!project) return []
  const byType = new Map<string, WorkbenchDeliverable[]>()
  for (const item of project.deliverables ?? []) {
    const type = (item.type || 'other').trim().toLowerCase() || 'other'
    const list = byType.get(type)
    if (list) list.push(item)
    else byType.set(type, [item])
  }
  return [...byType.entries()]
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([type, items]) => ({ key: 'type-' + type, label: deliverableTypeLabel(type), items }))
})
// 阶段带：草稿 → 成稿 → 发布，标出每个阶段已有多少产出，点击只看该阶段。
const deliverableStageSteps = computed(() => {
  const counts = new Map<string, number>()
  for (const item of selectedProject.value?.deliverables ?? []) {
    const stage = normalizeDeliverableStage(item.stage)
    counts.set(stage, (counts.get(stage) ?? 0) + 1)
  }
  return deliverableStages
    .filter((stage) => stage !== 'other')
    .map((stage, index) => ({
      stage,
      index: index + 1,
      label: t(`projects.deliverableStage.${stage}`),
      count: counts.get(stage) ?? 0,
    }))
})
const visibleDeliverableGroups = computed(() => {
  if (deliverableViewMode.value === 'type') {
    return deliverableTypeGroups.value
  }
  return deliverableStageFilter.value === 'all'
    ? deliverableGroups.value
    : deliverableGroups.value.filter((group) => group.stage === deliverableStageFilter.value)
})
watch(
  () => selectedProject.value?.path,
  () => {
    deliverableStageFilter.value = 'all'
  }
)

async function createRetrospective(): Promise<void> {
  const project = selectedProject.value
  const workspacePath = workspace.currentWorkspace?.path
  if (!project || !workspacePath) return
  const path = `${project.folder}/复盘.md`
  try {
    const content = `---\ntags: [复盘]\n---\n\n# ${project.name} · 复盘\n\n## 结果数据\n\n## 做对了什么\n\n## 做错了什么\n\n## 下次改进\n\n## 可复用资产\n`
    await FileService.CreateFile(workspacePath, path, content)
    await workbench.refresh()
    openDocument(path)
  } catch (error) {
    toast.error((error as Error).message)
  }
}
function togglePin(path: string, title: string) {
  if (!workspace.togglePin(path, title)) toast.warning(t('common.pinLimit'))
}
function openBlocker(task: WorkbenchTask) {
  const project = workbench.projects.find((item) => tasksForProject(item, [task]).length > 0)
  if (project) selectProject(project)
  else openDocument(task.filePath)
}
function summarizeProject() {
  const project = selectedProject.value
  if (!project) return
  requestCopilot({
    source: 'project',
    workspacePath: workspace.currentWorkspace?.path,
    prompt: t('projects.summaryPrompt'),
    context: projectSummaryContext(project, selectedTasks.value, projectNotes.value, workbench.today),
  })
}
function resetTaskForm() {
  showTaskForm.value = false
  taskTitle.value = ''
  taskKind.value = 'US'
  taskDue.value = ''
  taskError.value = ''
  addingTask.value = false
}
async function addTask() {
  const project = selectedProject.value
  if (!project || !taskTitle.value.trim() || addingTask.value) return
  const workspacePath = workspace.currentWorkspace?.path
  addingTask.value = true
  taskError.value = ''
  try {
    await workbench.addTask(project.folder, taskTitle.value.trim(), taskKind.value, taskDue.value)
    if (
      workspace.currentWorkspace?.path === workspacePath &&
      selectedProject.value?.path === project.path
    )
      resetTaskForm()
  } catch (error) {
    if (workspace.currentWorkspace?.path === workspacePath && selectedProject.value?.path === project.path)
      taskError.value = String(error instanceof Error ? error.message : error)
  } finally {
    if (workspace.currentWorkspace?.path === workspacePath && selectedProject.value?.path === project.path)
      addingTask.value = false
  }
}
watch([() => workspace.currentWorkspace?.path, () => selectedProject.value?.path], resetTaskForm)
// 切换项目时资料筛选回到「全部」，避免沿用上一个项目的架构视图造成空列表错觉
watch(
  () => selectedProject.value?.path,
  () => {
    docFilter.value = 'all'
  }
)
</script>

<template>
  <div class="collection-page projects-page">
    <div class="collection-shell">
      <PageHeader
        :title="selectedProject?.name || t('projects.title')"
        :subtitle="
          selectedProject ? t('projects.detailSubtitle') : t('projects.subtitle')
        "
        :eyebrow="t('projects.eyebrow')"
        :icon="Rocket"
        :dense="Boolean(selectedProject)"
        :tabs-aria-label="t('projects.navAria')"
      >
        <template #actions>
          <template v-if="workspace.hasWorkspace">
            <button
              class="collection-button primary"
              data-testid="create-collection"
              @click="requestSourceImport({ kind: 'project', sourceType: 'empty' })"
            >
              {{ t('projects.createProject') }}
            </button>
            <button
              class="collection-button"
              data-testid="import-collection"
              @click="requestSourceImport({ kind: 'project', sourceType: 'folder', targetFolder: selectedProject?.folder, name: selectedProject?.name })"
            >
              {{ selectedProject ? t('projects.addMaterials') : t('projects.createFromSources') }}
            </button>
          </template>
          <template v-if="selectedProject">
            <button
              class="collection-icon-button"
              :class="{ pinned: workspace.isPinned(selectedProject.path) }"
              type="button"
              :aria-label="workspace.isPinned(selectedProject.path) ? t('projects.unpinProject') : t('projects.pinProject')"
              :aria-pressed="workspace.isPinned(selectedProject.path)"
              @click="togglePin(selectedProject.path, selectedProject.name)"
            >
              <Pin :size="16" />
            </button>
            <button
              class="collection-button"
              data-testid="project-open-document"
              type="button"
              @click="openDocument(selectedProject.path)"
            >
              <FileText :size="15" /> {{ t('projects.projectDoc') }}
            </button>
            <button
              class="collection-button primary"
              data-testid="project-ai-summary"
              type="button"
              @click="summarizeProject"
            >
              <Sparkles :size="15" /> {{ t('projects.aiSummary') }}
            </button>
          </template>
          <button
            v-else
            class="collection-button"
            type="button"
            :disabled="workbench.loading"
            @click="workbench.refresh()"
          >
            <RefreshCw
              :size="14"
              :class="{ 'collection-spin': workbench.loading }"
            /> {{ t('projects.refresh') }}
          </button>
        </template>

        <template
          v-if="selectedProject"
          #meta
        >
          <div
            class="project-context"
            data-testid="project-context"
          >
            <div class="project-context-head">
              <span
                class="collection-status"
                :data-status="projectStatusLabel(selectedProject.status)"
              >{{ projectStatusLabel(selectedProject.status) }}</span>
              <span
                v-if="selectedProject.phase"
                class="project-context-phase"
              >{{ t('projects.phaseLabel') }} · {{ selectedProject.phase }}</span>
              <span
                v-if="selectedProject.retrospective"
                class="project-context-tag"
              >{{ t('projects.retroDone') }}</span>
              <span class="project-context-spacer" />
              <button
                v-if="selectedProject.retrospective"
                class="collection-button subtle"
                type="button"
                data-testid="project-open-retro"
                @click="openDocument(selectedProject.retrospective || '')"
              >
                <FileText :size="14" /> {{ t('projects.retroOpen') }}
              </button>
              <button
                v-else
                class="collection-button subtle"
                type="button"
                data-testid="project-create-retro"
                @click="createRetrospective"
              >
                <Plus :size="14" /> {{ t('projects.retroCreate') }}
              </button>
            </div>
            <p class="project-context-line">
              <strong>{{ t('projects.nextStepLabel') }}</strong>{{ selectedProject.nextStep || t('projects.nextStepEmpty') }}
            </p>
            <ul
              v-if="selectedProject.milestones?.length"
              class="project-milestones"
              :aria-label="t('projects.milestonesLabel')"
              data-testid="project-milestones"
            >
              <li
                v-for="(milestone, index) in selectedProject.milestones"
                :key="index"
                :class="{ done: milestone.done, overdue: !milestone.done && isOverdue(milestone.date) }"
              >
                <CheckCircle2
                  v-if="milestone.done"
                  :size="13"
                />
                <Circle
                  v-else
                  :size="13"
                />
                <span class="pm-name">{{ milestone.name }}</span>
                <span
                  v-if="milestone.date"
                  class="pm-date"
                >{{ milestone.date }}</span>
              </li>
            </ul>
            <p
              v-if="selectedProject.coreQuestion"
              class="project-context-line"
            >
              <strong>{{ t('projects.coreQuestionLabel') }}</strong>{{ selectedProject.coreQuestion }}
            </p>
            <details
              v-if="selectedProject.background"
              class="project-context-more"
            >
              <summary>{{ t('projects.backgroundLabel') }}</summary>
              <p>{{ selectedProject.background }}</p>
            </details>
          </div>
        </template>

        <template
          v-if="selectedProject"
          #tabs
        >
          <button
            v-for="tab in tabs"
            :key="tab.id"
            class="collection-tab"
            :class="{ active: detailTab === tab.id }"
            :data-testid="'project-tab-' + tab.id"
            :aria-current="detailTab === tab.id ? 'page' : undefined"
            type="button"
            @click="detailTab = tab.id"
          >
            <component
              :is="tab.icon"
              :size="15"
            />{{ t('projects.tab.' + tab.id) }}
          </button>
        </template>
      </PageHeader>

      <div
        v-if="workbench.error"
        class="collection-notice error"
        role="alert"
      >
        <AlertTriangle :size="17" /><span>{{ workbench.error }}</span>
        <button
          class="collection-button subtle"
          type="button"
          @click="workbench.refresh()"
        >
          {{ t('common.retry') }}
        </button>
      </div>
      <div
        v-if="!workspace.currentWorkspace"
        class="collection-empty collection-panel"
      >
        <Rocket :size="34" />
        <h2>{{ t('common.noWorkspaceTitle') }}</h2>
        <p>{{ t('projects.noWorkspaceDesc') }}</p>
        <button
          class="collection-button primary"
          type="button"
          @click="router.push('/')"
        >
          {{ t('sidebar.selectWorkspace') }}
        </button>
      </div>
      <template v-else>
        <CollectionOnboarding
          v-if="!selectedProject"
          kind="project"
        />
        <div
          v-if="!selectedProject"
          class="collection-metrics"
        >
          <div class="collection-metric">
            <strong>{{ workbench.projects.length }}</strong>{{ t('projects.metricProjects') }}
          </div>
          <div class="collection-metric">
            <strong>{{ activeProjects }}</strong>{{ t('projects.metricActive') }}
          </div>
          <div class="collection-metric">
            <strong>{{ openTaskCount }}</strong>{{ t('projects.metricTasks') }}
          </div>
          <div
            class="collection-metric"
            :class="{ attention: workbench.blockers.length }"
          >
            <strong>{{ workbench.blockers.length }}</strong>{{ t('projects.metricBlockers') }}
          </div>
        </div>
        <section
          v-if="workbench.blockers.length"
          class="project-blockers"
          data-testid="global-blockers"
          aria-labelledby="project-blockers-title"
        >
          <div class="project-blockers-heading">
            <AlertTriangle :size="18" />
            <h2 id="project-blockers-title">
              {{ t('projects.globalBlockers') }}
            </h2>
            <span>{{ t('projects.blockerCount', { n: workbench.blockers.length }) }}</span>
          </div>
          <button
            v-for="task in workbench.blockers"
            :key="task.id"
            class="project-blocker"
            type="button"
            @click="openBlocker(task)"
          >
            <span
              class="collection-type"
              :data-type="task.type"
            >{{
              task.type === 'todo' ? t('projects.typeTodo') : task.type
            }}</span>
            <span class="project-blocker-copy"><strong>{{ task.project || t('projects.noProject') }} · {{ task.title }}</strong><small>{{ task.blocker || t('projects.noBlockerDesc') }}</small></span>
            <ChevronRight :size="16" />
          </button>
        </section>

        <div
          v-if="!selectedProject || detailTab === 'tasks'"
          class="collection-filters"
        >
          <label
            v-if="!selectedProject"
            class="collection-search"
          ><Search :size="16" /><input
            v-model="search"
            :aria-label="t('projects.searchAria')"
            :placeholder="t('projects.searchPlaceholder')"
            type="search"
          ></label>
          <label
            v-if="!selectedProject"
            class="collection-filter"
          >{{ t('projects.statusFilter') }}<select
            v-model="statusFilter"
            data-testid="project-status-filter"
          >
            <option value="all">{{ t('projects.allStatuses') }}</option>
            <option
              v-for="status in statusOptions"
              :key="status"
              :value="status"
            >
              {{ status }}
            </option>
          </select></label>
          <label class="collection-filter">{{ t('projects.typeFilter') }}<select
            v-model="typeFilter"
            data-testid="project-type-filter"
          >
            <option value="all">{{ t('projects.allTasks') }}</option>
            <option value="US">{{ t('projects.typeUS') }}</option>
            <option value="DTS">{{ t('projects.typeDTS') }}</option>
            <option value="额外">{{ t('projects.typeExtra') }}</option>
            <option value="todo">{{ t('projects.typePlain') }}</option>
          </select></label>
          <label class="collection-filter">{{ t('projects.periodFilter') }}<select
            v-model="periodFilter"
            data-testid="project-period-filter"
          >
            <option value="all">{{ t('projects.periodAll') }}</option>
            <option value="today">{{ t('projects.periodToday') }}</option>
            <option value="week">{{ t('projects.periodWeek') }}</option>
            <option value="overdue">{{ t('projects.periodOverdue') }}</option>
          </select></label>
        </div>

        <div
          v-if="workbench.loading && !workbench.projects.length"
          class="collection-grid"
          role="status"
          :aria-label="t('projects.loadingAria')"
        >
          <div
            v-for="n in 3"
            :key="n"
            class="collection-skeleton"
          />
        </div>
        <section
          v-else-if="selectedProject"
          data-testid="project-detail-content"
        >
          <template v-if="detailTab === 'tasks'">
            <div class="collection-section-title">
              <h2>
                {{ t('projects.tab.tasks') }} <span class="collection-tab-count">{{ displayedTasks.length }}</span>
              </h2>
              <button
                class="collection-button"
                type="button"
                @click="showTaskForm = !showTaskForm"
              >
                <Plus :size="14" /> {{ t('projects.addTask') }}
              </button>
            </div>
            <form
              v-if="showTaskForm"
              class="project-task-form collection-panel"
              @submit.prevent="addTask"
            >
              <label class="collection-field project-task-title">{{ t('projects.taskContent') }}<input
                v-model="taskTitle"
                :placeholder="t('projects.taskPlaceholder')"
                required
                maxlength="500"
                autofocus
              ></label>
              <label class="collection-field">{{ t('projects.kindLabel') }}<select v-model="taskKind">
                <option value="US">{{ t('projects.typeUS') }}</option>
                <option value="DTS">{{ t('projects.typeDTS') }}</option>
                <option value="额外">{{ t('projects.typeExtra') }}</option>
                <option value="todo">{{ t('projects.typePlain') }}</option>
              </select></label>
              <label class="collection-field">{{ t('projects.dueLabel') }}<input
                v-model="taskDue"
                type="date"
              ></label>
              <div class="collection-actions">
                <button
                  class="collection-button primary"
                  type="submit"
                  :disabled="addingTask || workbench.busy || !taskTitle.trim()"
                >
                  {{ addingTask ? t('projects.adding') : t('projects.add') }}
                </button><button
                  class="collection-icon-button"
                  :aria-label="t('projects.cancelAddAria')"
                  type="button"
                  @click="resetTaskForm"
                >
                  <X :size="15" />
                </button>
              </div>
              <p
                v-if="taskError"
                class="project-form-error"
                role="alert"
              >
                {{ taskError }}
              </p>
            </form>
            <div
              v-if="displayedTasks.length"
              class="project-task-list"
            >
              <TaskCard
                v-for="task in displayedTasks"
                :key="task.id"
                :task="task"
              />
            </div>
            <div
              v-else
              class="collection-empty collection-panel"
            >
              <CheckCircle2 :size="30" />
              <h3>{{ selectedTasks.length ? t('projects.tasksEmptyTitle') : t('projects.tasksStartTitle') }}</h3>
              <p>
                {{
                  selectedTasks.length
                    ? t('projects.tasksEmptyDesc')
                    : t('projects.tasksStartDesc')
                }}
              </p>
            </div>
          </template>
          <template v-else-if="detailTab === 'notes'">
            <div class="collection-section-title">
              <h2>
                {{ t('projects.tab.notes') }} <span class="collection-tab-count">{{ projectNotes.length }}</span>
              </h2>
              <div
                class="project-doc-filter"
                role="group"
                :aria-label="t('projects.docFilterAria')"
              >
                <button
                  v-for="option in docFilterOptions"
                  :key="option.id"
                  :class="{ active: docFilter === option.id }"
                  :data-testid="'project-doc-filter-' + option.id"
                  :aria-pressed="docFilter === option.id"
                  type="button"
                  @click="docFilter = option.id"
                >
                  {{ t('projects.docFilter.' + option.id) }}<span class="collection-tab-count">{{ option.count }}</span>
                </button>
              </div>
              <button
                class="collection-button subtle project-open-folder"
                type="button"
                @click="router.push({ path: '/editor', query: { folder: selectedProject.folder } })"
              >
                {{ t('projects.openFolder') }} <ArrowUpRight :size="14" />
              </button>
            </div>
            <div class="collection-panel">
              <div
                v-if="displayedDocuments.length"
                class="collection-document-list"
              >
                <div
                  v-for="doc in displayedDocuments"
                  :key="doc.path"
                  class="collection-document-row"
                >
                  <button
                    class="collection-document-open"
                    type="button"
                    @click="openDocument(doc.path)"
                  >
                    <FileText :size="19" /><span class="collection-document-copy"><strong>{{ doc.title }}</strong><small>{{ doc.path }}</small></span>
                  </button>
                  <span class="collection-document-date">{{
                    formatRelativeTime(doc.modifiedAt)
                  }}</span>
                  <button
                    class="collection-icon-button"
                    :class="{ pinned: workspace.isPinned(doc.path) }"
                    :aria-label="workspace.isPinned(doc.path) ? t('projects.unpinAria', { name: doc.title }) : t('projects.pinAria', { name: doc.title })"
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
                <Layers :size="30" />
                <h3>
                  {{ docFilter === 'architecture' ? t('projects.architectureEmptyTitle') : t('projects.notesEmptyTitle') }}
                </h3>
                <p>
                  {{
                    docFilter === 'architecture'
                      ? t('projects.architectureEmptyDesc')
                      : t('projects.notesEmptyDesc')
                  }}
                </p>
              </div>
            </div>

            <!-- 技术栈 / 复习卡 / 关联书籍：都属于项目的「资料」，不再单独占一个页签 -->
            <section
              class="collection-panel project-tech"
              data-testid="project-technology"
            >
              <div class="collection-tags project-technology-tags">
                <span
                  v-for="tech in selectedProject.techStack"
                  :key="tech"
                  class="collection-tag"
                ><Code2 :size="13" />{{ tech }}</span><span
                  v-if="!selectedProject.techStack.length"
                  class="collection-subtitle"
                >{{ t('projects.noTechStack') }}</span>
              </div>
              <div
                v-if="projectReviewCards.length"
                class="project-review-cards"
                data-testid="project-review-cards"
              >
                <div class="project-review-cards-head">
                  <strong><BookMarked :size="15" />{{ t('projects.reviewCardsTitle') }}</strong>
                  <span class="project-review-cards-meta">{{ t('projects.reviewCardsMeta', { total: projectReviewCards.length, due: projectDueReviewCards.length }) }}</span>
                  <button
                    class="collection-button subtle"
                    type="button"
                    data-testid="project-review-cards-go"
                    @click="router.push({ path: '/learning', query: { tab: 'review' } })"
                  >
                    {{ t('projects.reviewCardsGo') }} <ArrowUpRight :size="14" />
                  </button>
                </div>
                <ul class="project-review-cards-list">
                  <li
                    v-for="card in projectReviewCards.slice(0, 3)"
                    :key="card.id"
                  >
                    <span class="project-review-question">{{ card.question }}</span>
                    <span
                      v-if="card.due && card.due <= workbench.today"
                      class="project-review-due"
                    >{{ t('projects.reviewDue') }}</span>
                  </li>
                </ul>
                <small
                  v-if="projectReviewCards.length > 3"
                  class="project-review-cards-more"
                >{{ t('projects.reviewCardsMore', { count: projectReviewCards.length - 3 }) }}</small>
              </div>
              <div
                v-if="relatedBooks.length"
                class="project-related-books"
              >
                <button
                  v-for="book in relatedBooks"
                  :key="book.path"
                  class="collection-button project-related-book"
                  type="button"
                  @click="router.push({ path: '/learning', query: { book: book.path } })"
                >
                  <BookOpen :size="15" />{{ book.name }}<small>{{ t('projects.bookMeta', { n: book.noteCount, progress: book.progress }) }}</small>
                </button>
              </div>
              <p
                v-else
                class="project-tech-empty"
              >
                {{ t('projects.connectDesc') }}
              </p>
            </section>
          </template>
          <template v-else-if="detailTab === 'deliverables'">
            <div class="collection-section-title">
              <h2>
                {{ t('projects.tab.deliverables') }} <span class="collection-tab-count">{{ (selectedProject.deliverables ?? []).length }}</span>
              </h2>
              <button
                class="collection-button subtle"
                type="button"
                @click="router.push({ path: '/editor', query: { folder: selectedProject.folder + '/Deliverables' } })"
              >
                {{ t('projects.openDeliverablesFolder') }} <ArrowUpRight :size="14" />
              </button>
            </div>
            <div class="deliverable-view-toggle">
              <button
                class="collection-button subtle"
                :class="{ active: deliverableViewMode === 'stage' }"
                type="button"
                data-testid="deliverable-view-stage"
                @click="deliverableViewMode = 'stage'"
              >
                <Layers :size="14" /> {{ t('projects.groupbyStage') }}
              </button>
              <button
                class="collection-button subtle"
                :class="{ active: deliverableViewMode === 'type' }"
                type="button"
                data-testid="deliverable-view-type"
                @click="deliverableViewMode = 'type'"
              >
                <FileText :size="14" /> {{ t('projects.groupbyType') }}
              </button>
            </div>
            <ol
              v-if="deliverableViewMode === 'stage'"
              class="project-stage-band"
              :aria-label="t('projects.stageBandAria')"
            >
              <li
                v-for="step in deliverableStageSteps"
                :key="step.stage"
              >
                <button
                  class="project-stage"
                  :class="{
                    reached: step.count > 0,
                    active: deliverableStageFilter === step.stage,
                  }"
                  :data-testid="'deliverable-stage-' + step.stage"
                  :aria-pressed="deliverableStageFilter === step.stage"
                  :disabled="step.count === 0"
                  type="button"
                  @click="
                    deliverableStageFilter =
                      deliverableStageFilter === step.stage ? 'all' : step.stage
                  "
                >
                  <span class="project-stage-index">{{ step.index }}</span>
                  <span class="project-stage-copy">
                    <strong>{{ step.label }}</strong>
                    <small>{{ t('projects.stageCount', { n: step.count }) }}</small>
                  </span>
                </button>
                <ChevronRight
                  v-if="step.index < deliverableStageSteps.length"
                  :size="15"
                  class="project-stage-arrow"
                />
              </li>
            </ol>
            <button
              v-if="deliverableViewMode === 'stage' && deliverableStageFilter !== 'all'"
              class="collection-button subtle project-stage-reset"
              type="button"
              data-testid="deliverable-stage-reset"
              @click="deliverableStageFilter = 'all'"
            >
              {{ t('projects.stageAll') }}
            </button>
            <div
              v-if="visibleDeliverableGroups.length"
              class="collection-panel project-deliverables"
              data-testid="project-deliverables"
            >
              <div
                v-for="group in visibleDeliverableGroups"
                :key="group.key"
                class="project-deliverable-group"
              >
                <h3 class="project-deliverable-group-head">
                  {{ group.label }} <span class="collection-tab-count">{{ group.items.length }}</span>
                </h3>
                <div class="collection-document-list">
                  <div
                    v-for="item in group.items"
                    :key="item.path"
                    class="collection-document-row"
                  >
                    <button
                      class="collection-document-open"
                      type="button"
                      @click="openDocument(item.path)"
                    >
                      <PackageOpen :size="19" /><span class="collection-document-copy"><strong>{{ item.title }}</strong><small>{{ deliverableTypeLabel(item.type) }}<template v-if="item.version"> · {{ item.version }}</template></small></span>
                    </button>
                    <span class="collection-document-date">{{ formatRelativeTime(item.modifiedAt) }}</span>
                  </div>
                </div>
              </div>
            </div>
            <div
              v-else
              class="collection-empty collection-panel"
            >
              <PackageOpen :size="30" />
              <h3>{{ t('projects.deliverablesEmptyTitle') }}</h3>
              <p>{{ t('projects.deliverablesEmptyDesc') }}</p>
            </div>
          </template>
        </section>
        <div
          v-else-if="projectCards.length"
          class="collection-grid"
        >
          <article
            v-for="card in projectCards"
            :key="card.project.path"
            class="collection-card project-card"
            data-testid="project-card"
          >
            <div class="collection-card-top">
              <button
                class="collection-card-heading"
                type="button"
                @click="selectProject(card.project)"
              >
                <span class="collection-card-icon"><Rocket :size="20" /></span><span><h2>{{ card.project.name }}</h2>
                  <span
                    class="collection-status"
                    :data-status="projectStatusLabel(card.project.status)"
                  >{{ projectStatusLabel(card.project.status) }}</span></span>
              </button>
              <button
                class="collection-icon-button"
                :class="{ pinned: workspace.isPinned(card.project.path) }"
                :aria-label="
                  workspace.isPinned(card.project.path)
                    ? t('projects.unpinAria', { name: card.project.name })
                    : t('projects.pinAria', { name: card.project.name })
                "
                :aria-pressed="workspace.isPinned(card.project.path)"
                type="button"
                @click="togglePin(card.project.path, card.project.name)"
              >
                <Pin :size="14" />
              </button>
            </div>
            <div class="collection-card-body">
              <p class="project-next-step">
                <span>{{ t('projects.nextStepLabel') }}</span>{{ card.project.nextStep || t('projects.nextStepEmpty') }}
              </p>
              <div class="collection-tags">
                <span
                  v-for="tech in card.project.techStack"
                  :key="tech"
                  class="collection-tag"
                >{{
                  tech
                }}</span>
              </div>
              <div class="project-card-counts">
                <span><ListTodo :size="13" />{{ t('projects.doneCount', { done: card.done, total: card.tasks.length }) }}</span><span>{{ t('projects.todayCount', { n: card.todayCount }) }}</span>
              </div>
              <div class="project-preview-list">
                <button
                  v-for="task in card.visibleTasks.slice(0, 3)"
                  :key="task.id"
                  class="project-task-preview"
                  :class="{ completed: task.completed, blocked: !task.completed && task.blocker }"
                  type="button"
                  @click="selectProject(card.project)"
                >
                  <span
                    class="collection-type"
                    :data-type="task.type"
                  >{{
                    task.type === 'todo' ? t('projects.typeTodo') : task.type
                  }}</span><span>{{ task.title }}</span><CheckCircle2
                    v-if="task.completed"
                    :size="13"
                  /><AlertTriangle
                    v-else-if="task.blocker"
                    :size="13"
                  />
                </button>
                <span
                  v-if="!card.visibleTasks.length"
                  class="project-no-tasks"
                >{{ t('projects.noTasks') }}</span>
              </div>
            </div>
            <div class="collection-card-footer">
              <span class="project-active-time"><Clock :size="12" />{{ formatRelativeTime(card.project.modifiedAt) }}</span><button
                type="button"
                @click="selectProject(card.project)"
              >
                {{ t('projects.viewProject') }} <ChevronRight :size="14" />
              </button>
            </div>
          </article>
        </div>
        <div
          v-else
          class="collection-empty collection-panel"
        >
          <Rocket :size="34" />
          <h2>{{ workbench.projects.length ? t('projects.notFoundTitle') : t('projects.emptyTitle') }}</h2>
          <p>
            {{
              workbench.projects.length
                ? t('projects.notFoundDesc')
                : t('projects.emptyDesc')
            }}
          </p>
          <button
            v-if="!workbench.projects.length"
            class="collection-button"
            type="button"
            @click="requestSourceImport({ kind: 'project', sourceType: 'empty' })"
          >
            {{ t('projects.createFirst') }} <Plus :size="14" />
          </button>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.project-blockers {
  margin: 0 0 26px;
  padding: 17px 20px 9px;
  border: 1px solid color-mix(in srgb, var(--error) 25%, var(--border));
  border-radius: var(--radius-lg);
  background: color-mix(in srgb, var(--error) 4%, var(--bg-content));
}
.project-blockers-heading {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--error);
  margin-bottom: 8px;
}
.project-blockers-heading h2 {
  font-size: 14px;
  font-weight: 600;
}
.project-blockers-heading > span {
  margin-left: auto;
  color: var(--text-secondary);
  font-size: 12px;
}
.project-blocker {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 11px 0;
  border: 0;
  border-top: 1px solid color-mix(in srgb, var(--error) 9%, transparent);
  background: none;
  color: var(--text-primary);
  text-align: left;
}
.project-blocker:hover .project-blocker-copy strong {
  color: var(--error);
}
.project-blocker-copy {
  flex: 1;
  min-width: 0;
}
.project-blocker-copy strong {
  display: block;
  font-size: 13px;
  font-weight: 500;
  line-height: 1.6;
  overflow-wrap: anywhere;
}
.project-blocker-copy small {
  display: block;
  margin-top: 3px;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.7;
  overflow-wrap: anywhere;
}
.project-next-step {
  font-size: 13px;
  line-height: 1.8;
  margin-bottom: 13px !important;
  min-height: 46px;
  overflow-wrap: anywhere;
}
.project-next-step > span {
  display: block;
  font-size: 11px;
  color: var(--text-secondary);
  margin-bottom: 2px;
}
.project-card {
  transition:
    border-color var(--transition-base),
    box-shadow var(--transition-base);
}
.project-card:hover {
  border-color: var(--border-accent);
  box-shadow: var(--shadow-sm);
}
.project-card-counts {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  font-size: 11px;
  color: var(--text-secondary);
  margin: 18px 0 11px;
}
.project-card-counts > span,
.project-active-time {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.project-preview-list {
  display: flex;
  flex-direction: column;
  gap: 7px;
  min-height: 65px;
}
.project-task-preview {
  display: flex;
  align-items: center;
  gap: 7px;
  text-align: left;
  padding: 7px 8px;
  background: color-mix(in srgb, var(--bg-hover) 75%, transparent);
  border: 0;
  border-radius: 6px;
  color: var(--text-primary);
  font-size: 12px !important;
}
.project-task-preview > span:nth-child(2) {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.project-task-preview.completed > span:nth-child(2) {
  text-decoration: line-through;
  color: var(--text-secondary);
}
.project-task-preview.completed > svg {
  color: var(--success);
}
.project-task-preview.blocked {
  background: color-mix(in srgb, var(--error) 6%, var(--bg-content));
}
.project-task-preview.blocked > svg {
  color: var(--error);
}
.project-no-tasks {
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.8;
  padding: 9px 0;
}
.project-task-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.project-task-form {
  display: flex;
  align-items: end;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 18px;
}
.project-task-title {
  flex: 1;
  min-width: 190px;
}
.project-form-error {
  flex-basis: 100%;
  color: var(--error);
  font-size: 12px;
}
.project-technology-tags {
  margin-bottom: 0;
}
.project-technology-tags .collection-tag {
  padding: 7px 11px;
  font-size: 13px;
}
.project-related-book {
  display: flex;
  align-items: center;
  gap: 14px;
  color: var(--text-primary);
  text-align: left;
}
.project-related-book > svg:first-child {
  color: var(--accent);
}
.project-related-book > span {
  flex: 1;
}
.project-related-book strong {
  display: block;
  font-size: 15px;
  font-weight: 600;
}
.project-related-book small {
  display: block;
  margin-top: 7px;
  color: var(--text-secondary);
  font-size: 12px;
}
.project-related-book:hover {
  border-color: var(--border-accent);
}
.project-review-cards {
  display: grid;
  gap: 10px;
  margin-bottom: 14px;
}
.project-review-cards-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.project-review-cards-head strong {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--text-primary);
}
.project-review-cards-meta {
  color: var(--text-muted);
  font-size: 11px;
}
.project-review-cards-head .collection-button {
  margin-left: auto;
}
.project-review-cards-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 7px;
}
.project-review-cards-list li {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
  font-size: 12px;
}
.project-review-question {
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}
.project-review-due {
  flex-shrink: 0;
  color: #c96963;
  font-size: 11px;
}
.project-review-cards-more {
  color: var(--text-muted);
  font-size: 11px;
}
.project-context {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.project-context-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.project-context-phase {
  color: var(--text-secondary);
  font-size: 12px;
}
.project-context-spacer {
  flex: 1 1 auto;
}
.project-context-line {
  margin: 0;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.6;
  overflow-wrap: anywhere;
}
.project-milestones {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.project-milestones li {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 10px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--bg-card);
  font-size: var(--text-xs);
  color: var(--text-secondary);
}
.project-milestones li.done {
  color: var(--success, #22c55e);
  border-color: color-mix(in srgb, var(--success, #22c55e) 40%, var(--border));
}
.project-milestones li.overdue {
  color: var(--danger, #ef4444);
  border-color: color-mix(in srgb, var(--danger, #ef4444) 40%, var(--border));
}
.project-milestones .pm-date {
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}
.deliverable-view-toggle {
  display: flex;
  gap: 8px;
}
.deliverable-view-toggle .collection-button.active {
  color: var(--accent);
  border-color: var(--accent);
}
.project-context-line strong {
  margin-right: 8px;
  color: var(--text-primary);
  font-weight: 500;
}
/* 背景默认折叠，避免详情页顶部被长文占满 */
.project-context-more {
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.7;
}
.project-context-more summary {
  cursor: pointer;
  color: var(--text-muted);
  width: fit-content;
  padding: 1px 0;
}
.project-context-more p {
  margin: 5px 0 0;
  padding-left: 10px;
  border-left: 2px solid var(--border);
  overflow-wrap: anywhere;
}
/* 资料页签内的文档筛选（全部 / 笔记 / 架构） */
.project-doc-filter {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 2px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface-panel);
  margin-right: auto;
}
.project-doc-filter button {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: 0;
  border-radius: 6px;
  padding: 4px 10px;
  background: transparent;
  color: var(--text-secondary);
  font-size: 12px;
}
.project-doc-filter button:hover {
  color: var(--text-primary);
  background: var(--bg-hover);
}
.project-doc-filter button.active {
  background: var(--accent-alpha);
  color: var(--accent);
  font-weight: 600;
}
.project-open-folder {
  margin-left: auto;
}
/* 资料页签底部：技术栈 / 复习卡 / 关联书籍 */
.project-tech {
  display: grid;
  gap: 14px;
  margin-top: 14px;
}
.project-tech .project-review-cards {
  margin-bottom: 0;
  padding: 0;
  border: 0;
  background: none;
}
.project-related-books {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.project-related-book {
  gap: 8px;
  max-width: 100%;
}
.project-related-book small {
  color: var(--text-muted);
  font-size: 11px;
  white-space: nowrap;
}
.project-tech-empty {
  margin: 0;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.7;
}
/* 交付物阶段带：草稿 → 成稿 → 发布 */
.project-stage-band {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin: 0 0 12px;
  padding: 0;
  list-style: none;
}
.project-stage-band li {
  display: flex;
  align-items: center;
  gap: 6px;
}
.project-stage {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  padding: 6px 12px 6px 8px;
  border: 1px dashed var(--border);
  border-radius: 10px;
  background: transparent;
  color: var(--text-secondary);
  text-align: left;
  transition:
    background var(--transition-fast),
    border-color var(--transition-fast);
}
.project-stage.reached {
  border-style: solid;
  background: var(--surface-panel);
  color: var(--text-primary);
}
.project-stage.active {
  border-color: var(--accent);
  background: var(--accent-alpha);
  color: var(--accent);
}
.project-stage:disabled {
  opacity: 0.5;
}
.project-stage-index {
  display: grid;
  place-items: center;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: var(--bg-hover);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.project-stage.reached .project-stage-index {
  background: var(--accent);
  color: var(--accent-contrast);
}
.project-stage-copy strong {
  display: block;
  font-size: 12.5px;
  font-weight: 600;
}
.project-stage-copy small {
  display: block;
  color: var(--text-muted);
  font-size: 11px;
}
.project-stage-arrow {
  color: var(--text-muted);
}
.project-stage-reset {
  margin-bottom: 10px;
}
.project-deliverable-group + .project-deliverable-group {
  margin-top: 16px;
}
.project-deliverable-group-head {
  margin: 0 0 8px;
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 500;
}
@media (max-width: 560px) {
  .project-blockers {
    padding-left: 14px;
    padding-right: 14px;
  }
  .project-task-form > .collection-field {
    flex: 1 1 100%;
  }
}
</style>
