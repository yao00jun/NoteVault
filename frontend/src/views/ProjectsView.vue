<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  AlertTriangle,
  ArrowLeft,
  ArrowUpRight,
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

const workbench = useWorkbenchStore()
const workspace = useWorkspaceStore()
const router = useRouter()
const route = useRoute()
const toast = useToast()
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
  { id: 'tasks', label: '任务清单', icon: ListTodo },
  { id: 'notes', label: '项目笔记', icon: FileText },
  { id: 'architecture', label: '架构总结', icon: Layers },
  { id: 'technology', label: '关联技术', icon: Code2 },
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
  if (!workspace.togglePin(path, title)) toast.warning('固定区最多容纳 8 项，请先取消一个固定项。')
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
    prompt:
      '请提炼这个项目近一周的研发进展、当前卡点与下一步行动。以提供的记录为依据，区分已完成和计划中的工作。',
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
      <button
        v-if="selectedProject"
        class="collection-back"
        type="button"
        @click="showPortfolio"
      >
        <ArrowLeft :size="14" /> 全部项目
      </button>
      <header class="collection-hero">
        <div>
          <div class="collection-eyebrow">
            <Rocket :size="15" /> PROJECTS / 项目
          </div>
          <h1>{{ selectedProject?.name || '每个项目，都有下一步。' }}</h1>
          <p class="collection-subtitle">
            {{
              selectedProject
                ? selectedProject.nextStep || '打开项目文档，记下下一步行动。'
                : '把并行工作放在一起，让进展、卡点与下一步清晰可见。'
            }}
          </p>
        </div>
        <div class="collection-actions">
          <template v-if="selectedProject">
            <button
              class="collection-icon-button"
              :class="{ pinned: workspace.isPinned(selectedProject.path) }"
              type="button"
              :aria-label="workspace.isPinned(selectedProject.path) ? '取消固定项目' : '固定项目'"
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
              <FileText :size="15" /> 项目文档
            </button>
            <button
              class="collection-button primary"
              data-testid="project-ai-summary"
              type="button"
              @click="summarizeProject"
            >
              <Sparkles :size="15" /> 提炼本周进展
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
            /> 刷新
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
          重试
        </button>
      </div>
      <div
        v-if="!workspace.currentWorkspace"
        class="collection-empty collection-panel"
      >
        <Rocket :size="34" />
        <h2>先打开一个工作区</h2>
        <p>项目、任务和研发笔记会在这里汇合。</p>
        <button
          class="collection-button primary"
          type="button"
          @click="router.push('/')"
        >
          选择工作区
        </button>
      </div>
      <template v-else>
        <div
          v-if="!selectedProject"
          class="collection-metrics"
        >
          <div class="collection-metric">
            <strong>{{ workbench.projects.length }}</strong> 个项目
          </div>
          <div class="collection-metric">
            <strong>{{ activeProjects }}</strong> 个进行中
          </div>
          <div class="collection-metric">
            <strong>{{ openTaskCount }}</strong> 项待办
          </div>
          <div
            class="collection-metric"
            :class="{ attention: workbench.blockers.length }"
          >
            <strong>{{ workbench.blockers.length }}</strong> 个卡点
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
              全局卡点
            </h2>
            <span>{{ workbench.blockers.length }} 项待协调</span>
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
              task.type === 'todo' ? '待办' : task.type
            }}</span>
            <span class="project-blocker-copy"><strong>{{ task.project || '未归属项目' }} · {{ task.title }}</strong><small>{{ task.blocker || '这项任务已标记为阻塞，需要协调处理。' }}</small></span>
            <ChevronRight :size="16" />
          </button>
        </section>

        <nav
          v-if="selectedProject"
          class="collection-tabs"
          aria-label="项目详情"
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
            />{{ tab.label }}
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
            aria-label="搜索项目"
            placeholder="搜索项目或技术栈…"
            type="search"
          ></label>
          <label
            v-if="!selectedProject"
            class="collection-filter"
          >项目状态<select
            v-model="statusFilter"
            data-testid="project-status-filter"
          >
            <option value="all">全部状态</option>
            <option
              v-for="status in statusOptions"
              :key="status"
              :value="status"
            >
              {{ status }}
            </option>
          </select></label>
          <label class="collection-filter">任务类型<select
            v-model="typeFilter"
            data-testid="project-type-filter"
          >
            <option value="all">全部任务</option>
            <option value="US">US · 用户故事</option>
            <option value="DTS">DTS · 问题单</option>
            <option value="额外">额外 · 临时任务</option>
            <option value="todo">普通待办</option>
          </select></label>
          <label class="collection-filter">时间范围<select
            v-model="periodFilter"
            data-testid="project-period-filter"
          >
            <option value="all">全部时间</option>
            <option value="today">今日</option>
            <option value="week">本周</option>
            <option value="overdue">已逾期</option>
          </select></label>
        </div>

        <div
          v-if="workbench.loading && !workbench.projects.length"
          class="collection-grid"
          role="status"
          aria-label="正在读取项目"
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
                任务清单 <span class="collection-tab-count">{{ displayedTasks.length }}</span>
              </h2>
              <button
                class="collection-button"
                type="button"
                @click="showTaskForm = !showTaskForm"
              >
                <Plus :size="14" /> 添加任务
              </button>
            </div>
            <form
              v-if="showTaskForm"
              class="project-task-form collection-panel"
              @submit.prevent="addTask"
            >
              <label class="collection-field project-task-title">任务内容<input
                v-model="taskTitle"
                placeholder="下一件要完成的事"
                required
                maxlength="500"
                autofocus
              ></label>
              <label class="collection-field">类型<select v-model="taskKind">
                <option value="US">US · 用户故事</option>
                <option value="DTS">DTS · 问题单</option>
                <option value="额外">额外 · 临时任务</option>
                <option value="todo">普通待办</option>
              </select></label>
              <label class="collection-field">截止日期<input
                v-model="taskDue"
                type="date"
              ></label>
              <div class="collection-actions">
                <button
                  class="collection-button primary"
                  type="submit"
                  :disabled="addingTask || workbench.busy || !taskTitle.trim()"
                >
                  {{ addingTask ? '添加中…' : '添加' }}
                </button><button
                  class="collection-icon-button"
                  aria-label="取消添加任务"
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
              <h3>{{ selectedTasks.length ? '这个筛选下没有任务' : '下一步，从一个任务开始' }}</h3>
              <p>
                {{
                  selectedTasks.length
                    ? '调整任务类型或时间范围，查看其他工作。'
                    : '添加用户故事、问题单或临时任务，让项目开始向前走。'
                }}
              </p>
            </div>
          </template>
          <template v-else-if="detailTab === 'notes' || detailTab === 'architecture'">
            <div class="collection-section-title">
              <h2>{{ detailTab === 'architecture' ? '架构与设计决策' : '项目笔记' }}</h2>
              <button
                class="collection-button subtle"
                type="button"
                @click="router.push({ path: '/editor', query: { folder: selectedProject.folder } })"
              >
                打开项目目录 <ArrowUpRight :size="14" />
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
                <Layers :size="30" />
                <h3>
                  {{ detailTab === 'architecture' ? '留下架构取舍的理由' : '记录让经验可复用' }}
                </h3>
                <p>
                  {{
                    detailTab === 'architecture'
                      ? '项目目录中的架构、Architecture 或 ADR 笔记会汇集在这里。'
                      : '在项目目录添加联调记录、排查过程或复盘笔记，即可在这里继续阅读。'
                  }}
                </p>
              </div>
            </div>
          </template>
          <template v-else>
            <div class="collection-section-title">
              <h2>正在使用的技术</h2>
              <button
                class="collection-button subtle"
                type="button"
                @click="router.push('/learning')"
              >
                前往技术书架 <ArrowUpRight :size="14" />
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
              >还没有记录项目技术栈。</span>
            </div>
            <div class="collection-grid">
              <button
                v-for="book in relatedBooks"
                :key="book.path"
                class="project-related-book collection-panel"
                type="button"
                @click="router.push({ path: '/learning', query: { book: book.path } })"
              >
                <BookOpen :size="25" /><span><strong>{{ book.name }}</strong><small>{{ book.noteCount }} 篇笔记 · {{ book.progress }}% 研读进度</small></span><ChevronRight :size="16" />
              </button>
            </div>
            <div
              v-if="!relatedBooks.length"
              class="collection-empty collection-panel"
            >
              <BookOpen :size="30" />
              <h3>连接实践与学习</h3>
              <p>技术分册关联这个项目后，项目里的技术问题和学习笔记就能在这里相遇。</p>
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
                  (workspace.isPinned(card.project.path) ? '取消固定 ' : '固定 ') +
                    card.project.name
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
                <span>下一步</span>{{ card.project.nextStep || '打开项目文档，记录下一步行动' }}
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
                <span><ListTodo :size="13" />{{ card.done }}/{{ card.tasks.length }} 已完成</span><span>今日待办 {{ card.todayCount }}</span>
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
                    task.type === 'todo' ? '待办' : task.type
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
                >暂无任务，先记下下一步。</span>
              </div>
            </div>
            <div class="collection-card-footer">
              <span class="project-active-time"><Clock :size="12" />{{ collectionDateLabel(card.project.modifiedAt) }}</span><button
                type="button"
                @click="selectProject(card.project)"
              >
                查看项目 <ChevronRight :size="14" />
              </button>
            </div>
          </article>
        </div>
        <div
          v-else
          class="collection-empty collection-panel"
        >
          <Rocket :size="34" />
          <h2>{{ workbench.projects.length ? '没有符合筛选的项目' : '让项目从想法走向完成' }}</h2>
          <p>
            {{
              workbench.projects.length
                ? '试着调整状态、任务类型或时间范围。'
                : '在项目空间建立项目文档，记录状态、下一步与技术栈，这里会自动整理为项目看板。'
            }}
          </p>
          <button
            v-if="!workbench.projects.length"
            class="collection-button"
            type="button"
            @click="router.push({ path: '/editor', query: { folder: 'Projects' } })"
          >
            打开项目空间 <ArrowUpRight :size="14" />
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
