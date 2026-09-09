<script setup lang="ts">
import { useViewRoute } from '@/composables/useViewRoute'
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  ArrowUpRight,
  BookOpen,
  Bot,
  Braces,
  Brain,
  CheckCircle2,
  ChevronRight,
  Coffee,
  Database,
  FileText,
  GraduationCap,
  Pin,
  Radar,
  RefreshCw,
  Search,
  Sparkles,
  Star,
  Terminal,
} from '@lucide/vue'
import type { InterviewCard, WorkbenchBook } from '@/api/workbench'
import { AppService } from '@/api'
import InterviewReview from '@/components/workbench/InterviewReview.vue'
import { useWorkbenchStore } from '@/stores/workbench'
import { useWorkspaceStore } from '@/stores/workspace'
import { useToast } from '@/composables/useToast'
import { requestCopilot } from '@/composables/useCopilotRequest'
import { collectionDateLabel, parseTechnologyRadar } from '@/utils/workbenchCollections'
import '@/styles/workbench-collections.css'
import { requestSourceImport } from '@/composables/useSourceImport'
import CollectionOnboarding from '@/components/workbench/CollectionOnboarding.vue'

const workbench = useWorkbenchStore()
const workspace = useWorkspaceStore()
const route = useViewRoute('/learning')
const router = useRouter()
const toast = useToast()
const bookSearch = ref('')
const bookStatus = ref('all')
const tabs = [
  { id: 'books', label: '技术书架', icon: BookOpen },
  { id: 'review', label: '每日复习', icon: Brain },
  { id: 'weak', label: '核心薄弱点', icon: Star },
  { id: 'radar', label: '技术雷达', icon: Radar },
]
const activeTab = computed(() =>
  tabs.some((tab) => tab.id === route.query.tab) ? String(route.query.tab) : 'books'
)
const selectedBook = computed(() =>
  activeTab.value === 'books'
    ? workbench.books.find((book) =>
        [book.path, book.folder, book.name].includes(String(route.query.book ?? ''))
      )
    : undefined
)
const weakCards = computed(() => workbench.cards.filter((card) => card.weak))
const noteCount = computed(() => workbench.books.reduce((sum, book) => sum + book.noteCount, 0))
const reviewTarget = computed(() => Math.min(5, workbench.reviewTarget))
const reviewedCount = computed(() => Math.min(reviewTarget.value, workbench.reviewedToday))
const reviewComplete = computed(
  () => reviewTarget.value > 0 && reviewedCount.value >= reviewTarget.value
)
const radarGroups = computed(() => parseTechnologyRadar(workbench.radar.content))
const radarDocumentExists = computed(() =>
  Boolean(workbench.radar.path) &&
  (Boolean(workbench.radar.content) || workbench.documents.some((doc) => doc.path === workbench.radar.path))
)
const radarEntryCount = computed(() =>
  radarGroups.value.reduce((sum, group) => sum + group.entries.length, 0)
)
const filteredBooks = computed(() =>
  workbench.books.filter(
    (book) =>
      (bookStatus.value === 'all' || bookState(book) === bookStatus.value) &&
      [book.name, ...book.projects]
        .join(' ')
        .toLocaleLowerCase()
        .includes(bookSearch.value.trim().toLocaleLowerCase())
  )
)
const chapters = computed(() =>
  [...(selectedBook.value?.chapters ?? [])].sort((a, b) =>
    a.path.localeCompare(b.path, 'zh-CN', { numeric: true })
  )
)
const attachments = computed(() => selectedBook.value?.attachments ?? [])
const hasPDFs = computed(() => attachments.value.some(attachment => /\.pdf$/i.test(attachment.path)))

async function openAttachment(path: string) {
  const workspacePath = workspace.currentWorkspace?.path
  if (!workspacePath) return
  try { await AppService.OpenWorkspaceAttachment(workspacePath, path) }
  catch (error) { toast.error(`无法打开原始资料：${error instanceof Error ? error.message : String(error)}`) }
}

function extractBookPDFs() {
  const book = selectedBook.value
  if (!book) return
  requestSourceImport({ kind: 'book', sourceType: 'attachments', name: book.name, source: book.folder, targetFolder: book.folder, autoStart: true })
}
const weakGroups = computed(() => {
  const groups = new Map<string, InterviewCard[]>()
  for (const card of weakCards.value)
    groups.set(card.filePath, [...(groups.get(card.filePath) ?? []), card])
  return [...groups].map(([path, cards]) => ({
    path,
    title: path.split('/').pop()?.replace(/\.md$/i, '') || path,
    cards,
  }))
})

function bookState(book: WorkbenchBook) {
  if (['planned', 'unread', 'not-started', '待学习', '待读', '未开始'].includes(book.status.toLocaleLowerCase())) return '待读'
  return ['completed', 'done', '已沉淀', '已完成'].includes(book.status.toLocaleLowerCase())
    ? '已沉淀'
    : book.status || '在学'
}
function bookProgress(book: WorkbenchBook) {
  return Math.min(100, Math.max(0, Math.round(book.progress || 0)))
}
function bookIcon(book: WorkbenchBook) {
  const name = book.name + ' ' + book.folder
  if (/java/i.test(name)) return Coffee
  if (/\bgo\b|golang/i.test(name)) return Terminal
  if (/mysql|sql|数据库/i.test(name)) return Database
  if (/vue|react|前端|javascript|typescript/i.test(name)) return Braces
  if (/\bai\b|agent|人工智能/i.test(name)) return Bot
  return BookOpen
}
function switchTab(id: string) {
  void router.replace({ path: '/learning', query: { tab: id } })
}
function selectBook(book: WorkbenchBook) {
  void router.push({ path: '/learning', query: { book: book.path } })
}
function openDocument(path: string) {
  workspace.openFile(path)
  void router.push({ path: '/editor', query: { file: path } })
}
function togglePin(path: string, title: string) {
  if (!workspace.togglePin(path, title)) toast.warning('固定区最多容纳 8 项，请先取消一个固定项。')
}
function explainCard(card: InterviewCard) {
  requestCopilot({
    source: 'learning',
    workspacePath: workspace.currentWorkspace?.path,
    prompt:
      '请用一个贴近工程实践的例子解释这道题的底层原理，指出容易混淆的地方，再给我一道追问题检验理解。',
    context: [
      '# 面试题：' + card.question,
      '来源：' + card.filePath,
      '## 已有答案',
      card.answer,
      '自评：' + card.level + '；连续不会次数：' + card.failures,
    ].join('\n'),
  })
}
function planBookStudy() {
  const book = selectedBook.value
  if (!book || !chapters.value.length) return
  requestCopilot({
    source: 'learning',
    workspacePath: workspace.currentWorkspace?.path,
    prompt: '请根据这个技术分册的章节和关联项目，整理下一次学习的重点与可执行的实践建议。',
    context: [
      '# 技术分册：' + book.name,
      '研读进度：' + bookProgress(book) + '%',
      '关联项目：' + book.projects.join('、'),
      '## 章节',
      ...chapters.value.map((chapter) => '- ' + chapter.title + ' (' + chapter.path + ')'),
    ].join('\n'),
  })
}
watch(
  () => workspace.currentWorkspace?.path,
  () => {
    bookSearch.value = ''
    bookStatus.value = 'all'
  }
)
</script>

<template>
  <div class="collection-page learning-page">
    <div class="collection-shell">
      <header class="collection-hero">
        <div>
          <div class="collection-eyebrow">
            <GraduationCap :size="16" /> LEARNING / 学习
          </div>
          <h1>{{ selectedBook?.name || '技术书架' }}</h1>
          <p class="collection-subtitle">
            {{
              selectedBook
                ? '把章节里的理解，带回项目里的实践。'
                : '按章节积累知识，用短时复习把理解留得更久。'
            }}
          </p>
        </div>
        <div class="collection-actions">
          <template v-if="workspace.hasWorkspace">
            <button
              class="collection-button primary"
              data-testid="create-collection"
              @click="requestSourceImport({ kind: 'book', sourceType: 'empty' })"
            >
              新建技术分册
            </button>
            <button
              class="collection-button"
              data-testid="import-collection"
              @click="requestSourceImport({ kind: 'book', sourceType: 'folder', targetFolder: selectedBook?.folder, name: selectedBook?.name })"
            >
              {{ selectedBook ? '添加资料' : '从资料创建' }}
            </button>
          </template>
          <template v-if="selectedBook">
            <button
              class="collection-icon-button"
              :class="{ pinned: workspace.isPinned(selectedBook.path) }"
              :aria-label="workspace.isPinned(selectedBook.path) ? '取消固定分册' : '固定分册'"
              :aria-pressed="workspace.isPinned(selectedBook.path)"
              type="button"
              @click="togglePin(selectedBook.path, selectedBook.name)"
            >
              <Pin :size="16" />
            </button>
            <button
              class="collection-button"
              type="button"
              @click="openDocument(selectedBook.path)"
            >
              <FileText :size="14" /> 分册文档
            </button>
            <button
              class="collection-button primary"
              type="button"
              data-testid="book-plan-study"
              :disabled="!chapters.length"
              :title="chapters.length ? '梳理章节学习重点' : '先提取正文或添加章节笔记'"
              @click="planBookStudy"
            >
              <Sparkles :size="14" /> 梳理学习重点
            </button>
          </template>
          <template v-else>
            <button
              class="collection-icon-button"
              aria-label="刷新学习资料"
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
              @click="router.push({ path: '/editor', query: { folder: 'Learning' } })"
            >
              打开学习空间 <ArrowUpRight :size="14" />
            </button>
          </template>
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
        <BookOpen :size="34" />
        <h2>先打开一个工作区</h2>
        <p>技术分册与面试题卡会在这里准备好。</p>
        <button
          class="collection-button primary"
          type="button"
          @click="router.push('/')"
        >
          选择工作区
        </button>
      </div>
      <template v-else>
        <CollectionOnboarding
          v-if="!selectedBook && activeTab === 'books'"
          kind="book"
        />
        <section
          v-if="selectedBook"
          class="learning-book-detail"
          data-testid="book-detail"
        >
          <aside class="collection-panel learning-book-overview">
            <div class="learning-detail-symbol">
              <component
                :is="bookIcon(selectedBook)"
                :size="36"
              />
            </div>
            <span class="collection-status">{{ bookState(selectedBook) }}</span>
            <h2>{{ selectedBook.name }}</h2>
            <div class="learning-progress-label">
              <span>研读进度</span><strong>{{ bookProgress(selectedBook) }}%</strong>
            </div>
            <div
              class="learning-progress"
              role="progressbar"
              :aria-label="selectedBook.name + '研读进度'"
              :aria-valuenow="bookProgress(selectedBook)"
              aria-valuemin="0"
              aria-valuemax="100"
            >
              <span :style="{ width: bookProgress(selectedBook) + '%' }" />
            </div>
            <div class="learning-book-numbers">
              <span><strong>{{ selectedBook.noteCount }}</strong> 篇笔记</span><span><strong>{{ selectedBook.reviewCount }}</strong> 题待复习</span>
            </div>
            <div class="learning-related-projects">
              <h3>关联项目</h3>
              <button
                v-for="name in selectedBook.projects"
                :key="name"
                data-testid="book-related-project"
                type="button"
                @click="router.push({ path: '/projects', query: { project: name } })"
              >
                {{ name }}<ArrowUpRight :size="13" />
              </button>
              <p v-if="!selectedBook.projects.length">
                还没有关联项目
              </p>
            </div>
            <button
              class="collection-button learning-full-width"
              type="button"
              @click="switchTab('review')"
            >
              <Brain :size="15" /> 开始今日复习
            </button>
          </aside>
          <div class="learning-book-content">
            <section class="collection-panel learning-chapters">
              <div class="collection-section-title">
                <h2>
                  章节与笔记 <span class="collection-tab-count">{{ chapters.length }}</span>
                </h2>
                <button
                  class="collection-button subtle"
                  type="button"
                  @click="router.push({ path: '/editor', query: { folder: selectedBook.folder } })"
                >
                  打开目录 <ArrowUpRight :size="13" />
                </button>
              </div>
              <div
                v-if="chapters.length"
                class="collection-document-list"
              >
                <div
                  v-for="(chapter, index) in chapters"
                  :key="chapter.path"
                  class="collection-document-row"
                >
                  <button
                    class="collection-document-open"
                    type="button"
                    @click="openDocument(chapter.path)"
                  >
                    <span class="learning-chapter-number">{{
                      String(index + 1).padStart(2, '0')
                    }}</span><span class="collection-document-copy"><strong>{{ chapter.title }}</strong><small>{{ chapter.path }}</small></span>
                  </button>
                  <span class="collection-document-date">{{
                    collectionDateLabel(chapter.modifiedAt)
                  }}</span>
                  <button
                    class="collection-icon-button"
                    :class="{ pinned: workspace.isPinned(chapter.path) }"
                    :aria-label="
                      (workspace.isPinned(chapter.path) ? '取消固定 ' : '固定 ') + chapter.title
                    "
                    :aria-pressed="workspace.isPinned(chapter.path)"
                    type="button"
                    @click="togglePin(chapter.path, chapter.title)"
                  >
                    <Pin :size="14" />
                  </button>
                </div>
              </div>
              <div
                v-else
                class="collection-empty"
              >
                <FileText :size="30" />
                <h3>{{ attachments.length ? `已保存 ${attachments.length} 份原始资料` : '从第一篇章节笔记开始' }}</h3>
                <p>{{ attachments.length ? '尚未生成章节正文。可在下方打开原文件，或提取 PDF 正文。' : '分册目录中的笔记会按章节顺序列在这里。' }}</p>
              </div>
            </section>
            <section
              v-if="attachments.length || selectedBook.studyPlanPath || selectedBook.sourceRecordsPath"
              class="collection-panel learning-sources"
              data-testid="book-sources"
            >
              <div class="collection-section-title">
                <h2>原始资料 <span class="collection-tab-count">{{ attachments.length }}</span></h2>
                <button
                  v-if="hasPDFs"
                  class="collection-button"
                  type="button"
                  data-testid="book-extract-pdfs"
                  @click="extractBookPDFs"
                >
                  <RefreshCw :size="14" />提取 PDF 正文
                </button>
              </div>
              <div
                v-if="attachments.length"
                class="learning-source-list"
              >
                <div
                  v-for="attachment in attachments"
                  :key="attachment.path"
                  class="learning-source-row"
                >
                  <FileText :size="20" />
                  <div class="learning-source-copy">
                    <strong>{{ attachment.name }}</strong><small>{{ attachment.notePath ? '已有可读正文' : '原文件已保存 · 尚未提取正文' }} · {{ Math.max(1, Math.round(attachment.size / 1024)) }} KB</small>
                  </div>
                  <div class="learning-source-actions">
                    <button
                      v-if="attachment.notePath"
                      class="collection-button subtle"
                      type="button"
                      data-testid="book-source-note"
                      @click="openDocument(attachment.notePath)"
                    >
                      阅读正文
                    </button>
                    <button
                      class="collection-button subtle"
                      type="button"
                      data-testid="book-open-attachment"
                      @click="openAttachment(attachment.path)"
                    >
                      打开原文件<ArrowUpRight :size="13" />
                    </button>
                  </div>
                </div>
              </div>
              <div class="learning-source-links">
                <button
                  v-if="selectedBook.studyPlanPath"
                  class="collection-button subtle"
                  type="button"
                  data-testid="book-study-plan"
                  @click="openDocument(selectedBook.studyPlanPath)"
                >
                  <Sparkles :size="14" />AI 整理建议（待确认）
                </button>
                <button
                  v-if="selectedBook.sourceRecordsPath"
                  class="collection-button subtle"
                  type="button"
                  data-testid="book-source-records"
                  @click="openDocument(selectedBook.sourceRecordsPath)"
                >
                  查看导入记录
                </button>
              </div>
            </section>
          </div>
        </section>
        <template v-else>
          <div class="collection-metrics">
            <div class="collection-metric">
              <strong>{{ workbench.books.length }}</strong> 个技术分册
            </div>
            <div class="collection-metric">
              <strong>{{ noteCount }}</strong> 篇笔记
            </div>
            <div class="collection-metric">
              <strong>{{ reviewedCount }}/{{ reviewTarget }}</strong> 今日复习
            </div>
            <div class="collection-metric">
              <strong>{{ weakCards.length }}</strong> 个薄弱点
            </div>
          </div>
          <nav
            class="collection-tabs"
            aria-label="学习工作区"
          >
            <button
              v-for="tab in tabs"
              :key="tab.id"
              class="collection-tab"
              :class="{ active: activeTab === tab.id }"
              :data-testid="'learning-tab-' + tab.id"
              :aria-current="activeTab === tab.id ? 'page' : undefined"
              type="button"
              @click="switchTab(tab.id)"
            >
              <component
                :is="tab.icon"
                :size="15"
              />{{ tab.label
              }}<span
                v-if="tab.id === 'weak' && weakCards.length"
                class="collection-tab-count"
              >{{
                weakCards.length
              }}</span><CheckCircle2
                v-if="tab.id === 'review' && reviewComplete"
                :size="13"
              />
            </button>
          </nav>
          <template v-if="activeTab === 'books'">
            <div
              v-if="reviewTarget || workbench.reviewQueue.length"
              class="learning-review-callout"
            >
              <span class="learning-review-symbol"><Brain :size="23" /></span>
              <div>
                <h2>{{ reviewComplete ? '今天的复习已完成' : '给记忆留十分钟' }}</h2>
                <p>
                  {{
                    reviewComplete
                      ? '让知识慢慢变成直觉，明天继续。'
                      : '今日最多 5 道题，先回想，再核对答案。'
                  }}
                </p>
              </div>
              <button
                class="collection-button"
                type="button"
                @click="switchTab('review')"
              >
                {{ reviewComplete ? '查看复习' : '开始复习' }}<ChevronRight :size="14" />
              </button>
            </div>
            <div class="collection-filters">
              <label class="collection-search"><Search :size="15" /><input
                v-model="bookSearch"
                type="search"
                aria-label="搜索技术分册"
                placeholder="搜索分册或关联项目…"
              ></label>
              <label class="collection-filter">研读状态<select v-model="bookStatus">
                <option value="all">全部分册</option>
                <option value="待读">待读</option>
                <option value="在学">在学</option>
                <option value="已沉淀">已沉淀</option>
              </select></label>
            </div>
            <div
              v-if="workbench.loading && !workbench.books.length"
              class="collection-grid"
              role="status"
              aria-label="正在读取技术分册"
            >
              <div
                v-for="n in 3"
                :key="n"
                class="collection-skeleton"
              />
            </div>
            <div
              v-else-if="filteredBooks.length"
              class="collection-grid"
            >
              <article
                v-for="(book, index) in filteredBooks"
                :key="book.path"
                class="collection-card learning-book-card"
                :data-tone="index % 4"
              >
                <div class="learning-book-cover">
                  <button
                    class="learning-book-title"
                    type="button"
                    @click="selectBook(book)"
                  >
                    <component
                      :is="bookIcon(book)"
                      :size="29"
                    /><span><small>技术分册 / {{ String(index + 1).padStart(2, '0') }}</small>
                      <h2>{{ book.name }}</h2></span>
                  </button>
                  <button
                    class="collection-icon-button"
                    :class="{ pinned: workspace.isPinned(book.path) }"
                    :aria-label="
                      (workspace.isPinned(book.path) ? '取消固定 ' : '固定 ') + book.name
                    "
                    :aria-pressed="workspace.isPinned(book.path)"
                    type="button"
                    @click="togglePin(book.path, book.name)"
                  >
                    <Pin :size="14" />
                  </button>
                </div>
                <div class="collection-card-body">
                  <div class="learning-progress-label">
                    <span>{{ bookState(book) }}</span><strong>{{ bookProgress(book) }}%</strong>
                  </div>
                  <div
                    class="learning-progress"
                    role="progressbar"
                    :aria-label="book.name + '研读进度'"
                    :aria-valuenow="bookProgress(book)"
                    aria-valuemin="0"
                    aria-valuemax="100"
                  >
                    <span :style="{ width: bookProgress(book) + '%' }" />
                  </div>
                  <div class="learning-book-numbers">
                    <span><FileText :size="12" />{{ book.noteCount }} 篇笔记</span><span><Brain :size="12" />{{ book.reviewCount }} 题待复习</span>
                  </div>
                  <div class="collection-tags learning-book-project-tags">
                    <button
                      v-for="name in book.projects"
                      :key="name"
                      class="collection-tag"
                      type="button"
                      @click="router.push({ path: '/projects', query: { project: name } })"
                    >
                      {{ name }}<ArrowUpRight :size="11" />
                    </button><span
                      v-if="!book.projects.length"
                      class="learning-unlinked"
                    >尚未关联项目</span>
                  </div>
                </div>
                <div class="collection-card-footer">
                  <span>{{ book.chapters.length }} 个章节</span><button
                    type="button"
                    @click="selectBook(book)"
                  >
                    继续阅读 <ChevronRight :size="14" />
                  </button>
                </div>
              </article>
            </div>
            <div
              v-else
              class="collection-empty collection-panel"
            >
              <BookOpen :size="34" />
              <h2>{{ workbench.books.length ? '没有找到这个分册' : '为下一项技术留一个位置' }}</h2>
              <p>
                {{
                  workbench.books.length
                    ? '换一个名称或研读状态试试。'
                    : '在学习空间建立技术分册，章节笔记、关联项目和研读进度会整理在这里。'
                }}
              </p>
              <button
                v-if="!workbench.books.length"
                class="collection-button primary"
                @click="requestSourceImport({ kind: 'book', sourceType: 'folder' })"
              >
                从资料建立第一本书
              </button>
            </div>
          </template>
          <section
            v-else-if="activeTab === 'review'"
            class="learning-review-section"
          >
            <div class="collection-section-title">
              <h2><Brain :size="18" /> 今天，巩固一点</h2>
              <span>{{ reviewedCount }}/{{ reviewTarget }} 已复习</span>
            </div>
            <p class="learning-section-intro">
              先用自己的话回想答案，再按实际掌握程度自评。掌握后 7 天、模糊后 3 天、不会则明天再见。
            </p>
            <InterviewReview :limit="5" />
          </section>
          <section
            v-else-if="activeTab === 'weak'"
            data-testid="weak-card-list"
          >
            <div class="collection-section-title">
              <h2><Star :size="17" /> 核心薄弱点专题</h2>
              <span>{{ weakCards.length }} 道题值得再深入一点</span>
            </div>
            <p class="learning-section-intro">
              连续两次评为「不会」的题目会汇集在这里。回到原始笔记，或借助追问把关键概念弄清楚。
            </p>
            <section
              v-for="group in weakGroups"
              :key="group.path"
              class="learning-weak-group"
            >
              <h3>
                <FileText :size="14" />{{ group.title }}<span>{{ group.cards.length }}</span>
              </h3>
              <article
                v-for="card in group.cards"
                :key="card.id"
                class="learning-weak-card collection-panel"
              >
                <div class="learning-weak-title">
                  <Star :size="16" />
                  <h3>{{ card.question }}</h3>
                  <span>连续 {{ card.failures }} 次不会</span>
                </div>
                <details class="learning-weak-answer">
                  <summary>回看答案</summary>
                  <div>{{ card.answer || '这道题还没有记录答案，打开原始笔记补充。' }}</div>
                </details>
                <div class="learning-weak-footer">
                  <span>下次复习 {{ card.due || '待安排' }}</span>
                  <div class="collection-actions">
                    <button
                      class="collection-button subtle"
                      type="button"
                      @click="openDocument(card.filePath)"
                    >
                      打开笔记 <ArrowUpRight :size="13" />
                    </button><button
                      class="collection-button"
                      type="button"
                      @click="explainCard(card)"
                    >
                      <Sparkles :size="14" /> 解释与追问
                    </button>
                  </div>
                </div>
              </article>
            </section>
            <div
              v-if="!weakCards.length"
              class="collection-empty collection-panel"
            >
              <CheckCircle2 :size="32" />
              <h3>暂时没有需要特别关注的题目</h3>
              <p>保持诚实自评，复习会自动把精力带回最需要的地方。</p>
              <button
                class="collection-button"
                type="button"
                @click="switchTab('review')"
              >
                继续今日复习 <ChevronRight :size="14" />
              </button>
            </div>
          </section>
          <section
            v-else
            data-testid="technology-radar"
          >
            <div class="collection-section-title">
              <h2><Radar :size="18" /> 技术雷达</h2>
              <button
                class="collection-button"
                type="button"
                @click="
                  radarDocumentExists
                    ? openDocument(workbench.radar.path)
                    : router.push({ path: '/editor', query: { folder: 'Learning' } })
                "
              >
                <FileText :size="14" />{{ radarDocumentExists ? '编辑决策笔记' : '打开学习空间' }}
              </button>
            </div>
            <p class="learning-section-intro">
              从评估到采用，把每一次技术选择的理由留下来。
            </p>
            <div class="learning-radar-layout">
              <figure
                class="learning-radar-map"
                aria-label="四环技术雷达，从内向外为采用推荐、尝试中、评估中、放弃废弃"
              >
                <div class="learning-radar-circle radar-hold">
                  <span>放弃废弃</span>
                </div>
                <div class="learning-radar-circle radar-assess">
                  <span>评估中</span>
                </div>
                <div class="learning-radar-circle radar-trial">
                  <span>尝试中</span>
                </div>
                <div class="learning-radar-circle radar-adopt">
                  <span>采用推荐</span>
                </div>
                <div class="learning-radar-center">
                  <strong>{{ radarEntryCount }}</strong><small>项决策</small>
                </div>
              </figure>
              <div class="learning-radar-groups">
                <section
                  v-for="group in radarGroups"
                  :key="group.id"
                  class="learning-radar-category collection-panel"
                  :data-category="group.id"
                  data-testid="radar-category"
                >
                  <div class="learning-radar-heading">
                    <span class="learning-radar-dot" />
                    <h3>{{ group.label }}</h3>
                    <span>{{ group.entries.length }}</span>
                  </div>
                  <p>{{ group.description }}</p>
                  <ul v-if="group.entries.length">
                    <li
                      v-for="(entry, index) in group.entries"
                      :key="index"
                    >
                      <strong>{{ entry.name }}</strong><span>{{ entry.reason || '尚未记录决策理由' }}</span>
                    </li>
                  </ul>
                  <div
                    v-else
                    class="learning-radar-empty"
                  >
                    还没有记录
                  </div>
                </section>
              </div>
            </div>
            <p
              v-if="!workbench.radar.content.trim()"
              class="learning-radar-hint"
            >
              在技术雷达笔记中，以「评估中」「尝试中」「采用推荐」「放弃废弃」分节记录技术与选择理由。
            </p>
          </section>
        </template>
      </template>
    </div>
  </div>
</template>

<style scoped>
.learning-review-callout {
  display: flex;
  align-items: center;
  gap: 15px;
  padding: 18px 21px;
  border: 1px solid var(--border);
  background: color-mix(in srgb, var(--accent) 4%, var(--bg-card));
  border-radius: var(--radius-lg);
}
.learning-review-symbol {
  display: flex;
  color: var(--accent);
}
.learning-review-callout > div {
  flex: 1;
  min-width: 0;
}
.learning-review-callout h2 {
  font-size: 14px;
  font-weight: 600;
}
.learning-review-callout p {
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.8;
  margin-top: 4px;
}
.learning-book-card {
  --book-accent: var(--accent);
}
.learning-book-card[data-tone='1'] {
  --book-accent: #8b72c1;
}
.learning-book-card[data-tone='2'] {
  --book-accent: #3d967f;
}
.learning-book-card[data-tone='3'] {
  --book-accent: #b47a44;
}
.learning-book-cover {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 23px 19px 24px 24px;
  border-left: 5px solid color-mix(in srgb, var(--book-accent) 70%, var(--bg-card));
  background: color-mix(in srgb, var(--book-accent) 8%, var(--bg-card));
  min-height: 92px;
}
.learning-book-title {
  display: flex;
  align-items: center;
  gap: 15px;
  padding: 0;
  border: 0;
  background: none;
  color: var(--book-accent);
  text-align: left;
  min-width: 0;
}
.learning-book-title > span {
  min-width: 0;
}
.learning-book-title small {
  font-size: 10px;
  letter-spacing: 1px;
  color: var(--text-secondary);
}
.learning-book-title h2 {
  margin-top: 8px;
  font-size: 20px;
  font-weight: 650;
  line-height: 1.4;
  overflow-wrap: anywhere;
  color: var(--text-primary);
}
.learning-book-title:hover h2 {
  color: var(--book-accent);
}
.learning-book-cover .collection-icon-button {
  background: transparent;
  border-color: transparent;
}
.learning-progress-label {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 2px 0 10px;
}
.learning-progress-label strong {
  color: var(--text-primary);
  font-size: 12px;
  font-weight: 600;
}
.learning-progress {
  height: 5px;
  overflow: hidden;
  background: var(--bg-hover);
  border-radius: 5px;
}
.learning-progress > span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--book-accent, var(--accent));
}
.learning-book-numbers {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  font-size: 11px;
  color: var(--text-secondary);
  margin-top: 17px;
}
.learning-book-numbers > span {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.learning-book-numbers strong {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
}
.learning-book-project-tags {
  margin-top: 15px;
  min-height: 24px;
}
.learning-book-project-tags button {
  border: 0;
}
.learning-book-project-tags button:hover {
  color: var(--accent);
}
.learning-unlinked {
  font-size: 11px;
  color: var(--text-secondary);
  line-height: 24px;
}
.learning-book-detail {
  display: grid;
  grid-template-columns: 265px minmax(0, 1fr);
  align-items: start;
  gap: 22px;
}
.learning-book-overview h2 {
  font-size: 19px;
  font-weight: 600;
  margin: 10px 0 26px;
  overflow-wrap: anywhere;
}
.learning-detail-symbol {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 72px;
  height: 83px;
  margin-bottom: 22px;
  background: var(--accent-alpha);
  color: var(--accent);
  border-radius: var(--radius-md);
  border-left: 4px solid var(--accent);
}
.learning-related-projects {
  border-top: 1px solid var(--border-light);
  margin: 23px 0;
  padding-top: 18px;
}
.learning-related-projects h3 {
  font-size: 12px;
  font-weight: 500;
  color: var(--text-secondary);
  margin-bottom: 10px;
}
.learning-related-projects button {
  display: flex;
  width: 100%;
  justify-content: space-between;
  align-items: center;
  border: 0;
  background: none;
  padding: 7px 0;
  text-align: left;
  color: var(--accent);
  font-size: 13px;
}
.learning-related-projects p {
  color: var(--text-secondary);
  font-size: 12px;
}
.learning-full-width {
  width: 100%;
}
.learning-chapters {
  min-width: 0;
}
.learning-book-content { display: grid; gap: 18px; min-width: 0; }
.learning-source-list { max-height: 340px; overflow-y: auto; }
.learning-source-row { display: flex; align-items: center; gap: 12px; padding: 14px 0; border-bottom: 1px solid var(--border-light); }
.learning-source-row > svg { flex-shrink: 0; color: var(--accent); }
.learning-source-copy { flex: 1; min-width: 0; }
.learning-source-copy strong { display: block; font-size: 13px; overflow-wrap: anywhere; }
.learning-source-copy small { display: block; margin-top: 6px; color: var(--text-secondary); font-size: 11px; }
.learning-source-actions, .learning-source-links { display: flex; gap: 8px; flex-wrap: wrap; }
.learning-source-actions { justify-content: flex-end; flex-shrink: 0; }
.learning-source-links { margin-top: 14px; }
@media (max-width: 1050px) {
  .learning-source-row { flex-wrap: wrap; }
  .learning-source-actions { margin-left: auto; }
}
.learning-chapter-number {
  color: var(--text-secondary);
  font-size: 12px;
  font-family: var(--font-mono);
  min-width: 22px;
}
.learning-section-intro {
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.9;
  margin: -5px 0 24px !important;
  max-width: 800px;
}
.learning-review-section {
  max-width: 860px;
}
.learning-weak-group {
  margin-top: 24px;
}
.learning-weak-group > h3 {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 500;
  margin-bottom: 12px;
}
.learning-weak-group > h3 span {
  font-size: 11px;
}
.learning-weak-card {
  margin-bottom: 13px;
}
.learning-weak-title {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}
.learning-weak-title > svg {
  color: var(--warning);
  flex-shrink: 0;
  margin-top: 3px;
}
.learning-weak-title h3 {
  font-size: 15px;
  font-weight: 600;
  flex: 1;
  line-height: 1.6;
}
.learning-weak-title > span {
  font-size: 11px;
  color: var(--text-secondary);
  white-space: nowrap;
  line-height: 2;
}
.learning-weak-answer {
  margin: 16px 0;
  font-size: 13px;
}
.learning-weak-answer summary {
  color: var(--accent);
  cursor: pointer;
  width: fit-content;
}
.learning-weak-answer > div {
  white-space: pre-wrap;
  line-height: 1.9;
  padding: 15px;
  margin-top: 12px;
  border-radius: var(--radius-sm);
  background: var(--bg-hover);
  overflow-wrap: anywhere;
}
.learning-weak-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.learning-weak-footer > span {
  font-size: 11px;
  color: var(--text-secondary);
}
.learning-radar-layout {
  display: grid;
  grid-template-columns: 255px minmax(0, 1fr);
  align-items: start;
  gap: 26px;
}
.learning-radar-map {
  position: relative;
  width: 255px;
  height: 255px;
  margin: 18px 0;
}
.learning-radar-circle {
  position: absolute;
  border: 1px solid var(--border);
  border-radius: 50%;
  inset: 0;
  background: color-mix(in srgb, var(--text-secondary) 2%, var(--bg-content));
}
.learning-radar-circle > span {
  position: absolute;
  top: 8px;
  left: 50%;
  transform: translateX(-50%);
  white-space: nowrap;
  font-size: 10px;
  color: var(--text-secondary);
}
.radar-assess {
  inset: 26px;
  background: color-mix(in srgb, var(--warning) 5%, var(--bg-content));
  border-color: color-mix(in srgb, var(--warning) 30%, var(--border));
}
.radar-trial {
  inset: 52px;
  background: color-mix(in srgb, var(--accent) 6%, var(--bg-content));
  border-color: color-mix(in srgb, var(--accent) 30%, var(--border));
}
.radar-adopt {
  inset: 78px;
  background: color-mix(in srgb, var(--success) 8%, var(--bg-content));
  border-color: color-mix(in srgb, var(--success) 40%, var(--border));
}
.learning-radar-center {
  position: absolute;
  inset: 108px 85px 89px;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.learning-radar-center strong {
  font-size: 24px;
  font-weight: 600;
  line-height: 1;
  color: var(--text-primary);
}
.learning-radar-center small {
  font-size: 10px;
  color: var(--text-secondary);
  margin-top: 5px;
}
.learning-radar-groups {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}
.learning-radar-category {
  --radar-color: var(--warning);
  padding: 18px;
}
.learning-radar-category[data-category='trial'] {
  --radar-color: var(--accent);
}
.learning-radar-category[data-category='adopt'] {
  --radar-color: var(--success);
}
.learning-radar-category[data-category='hold'] {
  --radar-color: var(--text-secondary);
}
.learning-radar-heading {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-bottom: 6px;
}
.learning-radar-heading h3 {
  font-size: 13px;
  font-weight: 600;
  flex: 1;
}
.learning-radar-heading > span:last-child {
  font-size: 11px;
  color: var(--text-secondary);
}
.learning-radar-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--radar-color);
}
.learning-radar-category > p {
  color: var(--text-secondary);
  font-size: 11px;
  line-height: 1.6;
}
.learning-radar-category ul {
  list-style: none;
  margin: 13px 0 0;
  padding: 0;
}
.learning-radar-category li {
  padding-top: 11px;
  margin-top: 11px;
  border-top: 1px solid var(--border-light);
}
.learning-radar-category li strong {
  display: block;
  font-size: 13px;
  font-weight: 500;
  overflow-wrap: anywhere;
}
.learning-radar-category li span {
  display: block;
  color: var(--text-secondary);
  font-size: 11px;
  line-height: 1.8;
  margin-top: 5px;
  overflow-wrap: anywhere;
}
.learning-radar-empty {
  color: var(--text-secondary);
  font-size: 12px;
  padding: 20px 0 8px;
}
.learning-radar-hint {
  margin-top: 24px !important;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.8;
}
@media (max-width: 1000px) {
  .learning-book-detail {
    grid-template-columns: 1fr;
  }
  .learning-book-overview {
    max-width: none;
  }
  .learning-radar-layout {
    grid-template-columns: 1fr;
  }
  .learning-radar-map {
    margin: 0 auto 12px;
  }
}
@media (max-width: 560px) {
  .learning-review-callout {
    flex-wrap: wrap;
    padding: 16px;
  }
  .learning-review-callout > button {
    margin-left: 38px;
  }
  .learning-radar-groups {
    grid-template-columns: 1fr;
  }
  .learning-weak-title {
    flex-wrap: wrap;
  }
  .learning-weak-title > span {
    flex-basis: 100%;
    padding-left: 26px;
  }
}
</style>
