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
  Pin,
  Plus,
  RefreshCw,
  Rocket,
  Search,
  Sparkles,
  X,
} from '@lucide/vue'
import type { WorkbenchProject, WorkbenchTask } from '@/api/workbench'
import TaskCard from '@/components/workbench/TaskCard.vue'
import { useWorkbenchStore } from '@/stores/workbench'
import { useWorkspaceStore } from '@/stores/workspace'
import { useToast } from '@/composables/useToast'
import { requestCopilot } from '@/composables/useCopilotRequest'
import {
  collectionDateLabel,
  matchesTaskPeriod,
  normalizedCollectionPath,
  projectStatusLabel,
  projectSummaryContext,
  tasksForProject,
  type TaskPeriod,
} from '@/utils/workbenchCollections'
import '@/styles/workbench-collections.css'
import { requestSourceImport } from '@/composables/useSourceImport'
import CollectionOnboarding from '@/components/workbench/CollectionOnboarding.vue'

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
const showTaskForm = ref(false)
const taskTitle = ref('')
const taskKind = ref('US')
const taskDue = ref('')
const addingTask = ref(false)
const taskError = ref('')

const tabs = [
  { id: 'tasks', icon: ListTodo },
  { id: 'notes', icon: FileText },
  { id: 'architecture', icon: Layers },
  { id: 'technology', icon: Code2 },
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
  void router.push({ path: '/projects', query: { project: project.path } })
}
function showPortfolio() {
  void router.push('/projects')
}
function openDocument(path: string) {
  workspace.openFile(path)
  void router.push({ path: '/editor', query: { file: path } })
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
</script>

<template>
  <div class="collection-page projects-page">
    <div class="collection-shell">
      <header class="collection-hero">
        <div>
          <div class="collection-eyebrow">
            <Rocket :size="15" /> {{ t('projects.eyebrow') }}
          </div>
          <h1>{{ selectedProject?.name || t('projects.title') }}</h1>
          <p class="collection-subtitle">
            {{
              selectedProject
                ? selectedProject.nextStep || t('projects.detailSubtitle')
                : t('projects.subtitle')
            }}
          </p>
        </div>
        <div class="collection-actions">
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
        </div>
      </header>

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

        <nav
          v-if="selectedProject"
          class="collection-tabs"
          :aria-label="t('projects.navAria')"
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
        </nav>

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
          <template v-else-if="detailTab === 'notes' || detailTab === 'architecture'">
            <div class="collection-section-title">
              <h2>{{ detailTab === 'architecture' ? t('projects.architectureTitle') : t('projects.tab.notes') }}</h2>
              <button
                class="collection-button subtle"
                type="button"
                @click="router.push({ path: '/editor', query: { folder: selectedProject.folder } })"
              >
                {{ t('projects.openFolder') }} <ArrowUpRight :size="14" />
              </button>
            </div>
            <div class="collection-panel">
              <div
                v-if="(detailTab === 'architecture' ? architectureNotes : projectNotes).length"
                class="collection-document-list"
              >
                <div
                  v-for="doc in detailTab === 'architecture' ? architectureNotes : projectNotes"
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
                    collectionDateLabel(doc.modifiedAt)
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
                  {{ detailTab === 'architecture' ? t('projects.architectureEmptyTitle') : t('projects.notesEmptyTitle') }}
                </h3>
                <p>
                  {{
                    detailTab === 'architecture'
                      ? t('projects.architectureEmptyDesc')
                      : t('projects.notesEmptyDesc')
                  }}
                </p>
              </div>
            </div>
          </template>
          <template v-else>
            <div class="collection-section-title">
              <h2>
                {{ t('projects.techTitle') }} <span class="collection-tab-count">{{ projectReviewCards.length }}</span>
              </h2>
              <button
                class="collection-button subtle"
                type="button"
                @click="router.push('/learning')"
              >
                {{ t('projects.goBookshelf') }} <ArrowUpRight :size="14" />
              </button>
            </div>
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
              class="collection-panel project-review-cards"
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
            <div class="collection-grid">
              <button
                v-for="book in relatedBooks"
                :key="book.path"
                class="project-related-book collection-panel"
                type="button"
                @click="router.push({ path: '/learning', query: { book: book.path } })"
              >
                <BookOpen :size="25" /><span><strong>{{ book.name }}</strong><small>{{ t('projects.bookMeta', { n: book.noteCount, progress: book.progress }) }}</small></span><ChevronRight :size="16" />
              </button>
            </div>
            <div
              v-if="!relatedBooks.length"
              class="collection-empty collection-panel"
            >
              <BookOpen :size="30" />
              <h3>{{ t('projects.connectTitle') }}</h3>
              <p>{{ t('projects.connectDesc') }}</p>
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
              <span class="project-active-time"><Clock :size="12" />{{ collectionDateLabel(card.project.modifiedAt) }}</span><button
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
  margin-bottom: 23px;
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
