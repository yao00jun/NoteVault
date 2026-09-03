<script setup lang="ts">
/**
 * ReportsView - 写作报表中心（B 期）
 *
 * 三个板块，数据全部自动派生：
 *  1. 汇总卡片：近 7 天 / 近 30 天改动、活跃天数、最长连续（StatsService）
 *  2. GitHub 风格写作热力图（StatsService.GetWritingActivity，纯 CSS grid 渲染）
 *  3. 知识资产 Top10：被 [[双向链接]] 最多的笔记（GraphService 边统计）
 * 不引入图表库，保持本地优先的轻量体积。
 */
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { BarChart3, Flame, Link2, CalendarRange, Activity } from 'lucide-vue-next'
import { StatsService, GraphService } from '@bindings/github.com/notevault/notevault/index.js'
import { useWorkspaceStore } from '@/stores/workspace'

const { t } = useI18n()
const router = useRouter()
const workspaceStore = useWorkspaceStore()

interface DayActivity {
  date: string
  edited: number
}
interface WritingActivity {
  days: DayActivity[]
  activeDays: number
  weekEdited: number
  monthEdited: number
  longestStreak: number
}
interface TopLinked {
  path: string
  title: string
  links: number
}

const HEATMAP_DAYS = 91 // 13 周 × 7 天，与 GitHub 首页卡片同规格

const activity = ref<WritingActivity | null>(null)
const topLinked = ref<TopLinked[]>([])
const loading = ref(false)
const errorMsg = ref('')

async function load() {
  const ws = workspaceStore.currentWorkspace
  if (!ws?.path) return
  loading.value = true
  errorMsg.value = ''
  try {
    const [act, graph] = await Promise.all([
      StatsService.GetWritingActivity(ws.path, HEATMAP_DAYS) as Promise<WritingActivity | null>,
      GraphService.GetGraph(ws.path) as Promise<{ nodes: Array<{ id: string; title: string; path: string; resolved: boolean }>; edges: Array<{ source: string; target: string }> } | null>,
    ])
    activity.value = act

    // 入度统计：一条边指向某笔记就记一次被链接
    const inDegree = new Map<string, number>()
    for (const e of graph?.edges ?? []) {
      inDegree.set(e.target, (inDegree.get(e.target) ?? 0) + 1)
    }
    const titles = new Map<string, string>()
    for (const n of graph?.nodes ?? []) {
      if (n.resolved) titles.set(n.id, n.title)
    }
    topLinked.value = [...inDegree.entries()]
      .filter(([id]) => titles.has(id))
      .map(([id, links]) => ({ path: id, title: titles.get(id) ?? id, links }))
      .sort((a, b) => b.links - a.links)
      .slice(0, 10)
  } catch (e) {
    errorMsg.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(() => workspaceStore.currentWorkspace?.id, load)
watch(() => workspaceStore.fileTreeVersion, load)

// 热力图按周分列：前导补齐使第一列从周一开始
const heatmapColumns = computed(() => {
  const days = activity.value?.days ?? []
  if (!days.length) return []
  const firstDow = (new Date(days[0].date + 'T00:00:00').getDay() + 6) % 7 // 0=周一
  const padded: Array<DayActivity | null> = [...Array<DayActivity | null>(firstDow).fill(null), ...days]
  const cols: Array<Array<DayActivity | null>> = []
  for (let i = 0; i < padded.length; i += 7) {
    cols.push(padded.slice(i, i + 7))
  }
  return cols
})

// 色阶：0 / 1 / 2 / 3-4 / 5+ 五档
function level(edited: number): number {
  if (edited <= 0) return 0
  if (edited === 1) return 1
  if (edited === 2) return 2
  if (edited <= 4) return 3
  return 4
}

function openFile(path: string) {
  workspaceStore.openFile(path)
  workspaceStore.incrementFileTreeVersion()
  router.push('/editor')
}
</script>

<template>
  <div class="reports-view">
    <header class="rp-banner">
      <div class="rp-banner-icon">
        <BarChart3 :size="24" />
      </div>
      <div>
        <h1 class="rp-title">
          {{ t('reports.title') }}
        </h1>
        <p class="rp-sub">
          {{ t('reports.subtitle') }}
        </p>
      </div>
    </header>

    <div
      v-if="errorMsg"
      class="rp-error"
    >
      ⚠️ {{ errorMsg }}
    </div>
    <div
      v-else-if="loading && !activity"
      class="rp-empty"
    >
      {{ t('common.loading') }}
    </div>

    <template v-if="activity">
      <!-- 汇总卡片 -->
      <section class="rp-cards">
        <div class="rp-card">
          <CalendarRange
            class="rp-card-icon week"
            :size="18"
          />
          <div>
            <div class="rp-card-value">
              {{ activity.weekEdited }}
            </div>
            <div class="rp-card-label">
              {{ t('reports.week') }}
            </div>
          </div>
        </div>
        <div class="rp-card">
          <Activity
            class="rp-card-icon month"
            :size="18"
          />
          <div>
            <div class="rp-card-value">
              {{ activity.monthEdited }}
            </div>
            <div class="rp-card-label">
              {{ t('reports.month') }}
            </div>
          </div>
        </div>
        <div class="rp-card">
          <BarChart3
            class="rp-card-icon active"
            :size="18"
          />
          <div>
            <div class="rp-card-value">
              {{ activity.activeDays }}
            </div>
            <div class="rp-card-label">
              {{ t('reports.activeDays') }}
            </div>
          </div>
        </div>
        <div class="rp-card">
          <Flame
            class="rp-card-icon streak"
            :size="18"
          />
          <div>
            <div class="rp-card-value">
              {{ activity.longestStreak }}
            </div>
            <div class="rp-card-label">
              {{ t('reports.longestStreak') }}
            </div>
          </div>
        </div>
      </section>

      <!-- 写作热力图 -->
      <section class="rp-section">
        <h2 class="rp-section-title">
          {{ t('reports.heatmap') }}
        </h2>
        <div
          v-if="!heatmapColumns.length"
          class="rp-empty"
        >
          {{ t('reports.noData') }}
        </div>
        <div
          v-else
          class="rp-heatmap-wrap"
        >
          <div
            class="rp-heatmap"
            role="img"
            :aria-label="t('reports.heatmap')"
          >
            <div
              v-for="(col, ci) in heatmapColumns"
              :key="ci"
              class="rp-heat-col"
            >
              <template
                v-for="(cell, ri) in col"
                :key="ri"
              >
                <div
                  v-if="cell"
                  class="rp-heat-cell"
                  :class="`lv-${level(cell.edited)}`"
                  :title="`${cell.date} · ${cell.edited}`"
                />
                <div
                  v-else
                  class="rp-heat-cell empty"
                />
              </template>
            </div>
          </div>
          <div class="rp-heat-legend">
            <span>{{ t('reports.less') }}</span>
            <span
              v-for="lv in 5"
              :key="lv"
              class="rp-heat-cell"
              :class="`lv-${lv - 1}`"
            />
            <span>{{ t('reports.more') }}</span>
          </div>
        </div>
      </section>

      <!-- 知识资产 Top10 -->
      <section class="rp-section">
        <h2 class="rp-section-title">
          <Link2 :size="15" />
          {{ t('reports.topLinked') }}
        </h2>
        <div
          v-if="!topLinked.length"
          class="rp-empty"
        >
          {{ t('reports.emptyGraph') }}
        </div>
        <ol
          v-else
          class="rp-toplist"
        >
          <li
            v-for="(item, idx) in topLinked"
            :key="item.path"
          >
            <button
              class="rp-topitem"
              @click="openFile(item.path)"
            >
              <span class="rp-toprank">{{ idx + 1 }}</span>
              <span
                class="rp-toptitle"
                :title="item.path"
              >{{ item.title }}</span>
              <span class="rp-toplinks">{{ item.links }} {{ t('reports.links') }}</span>
            </button>
          </li>
        </ol>
      </section>
    </template>
  </div>
</template>

<style scoped>
.reports-view {
  flex: 1;
  overflow-y: auto;
  background: var(--bg-content);
  padding: var(--space-6) var(--space-8);
}

.rp-banner {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin-bottom: var(--space-5);
}
.rp-banner-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  border-radius: var(--radius-md);
  background: rgba(59, 130, 246, 0.12);
  color: #3b82f6;
}
.rp-title {
  font-size: var(--text-2xl);
  color: var(--text-primary);
}
.rp-sub {
  font-size: var(--text-sm);
  color: var(--text-secondary);
}

.rp-error,
.rp-empty {
  padding: var(--space-4);
  border-radius: var(--radius-md);
  background: var(--bg-card);
  border: 1px solid var(--border);
  color: var(--text-secondary);
  font-size: var(--text-sm);
  margin-bottom: var(--space-4);
}
.rp-error {
  color: var(--error);
}

.rp-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: var(--space-3);
  margin-bottom: var(--space-6);
}
.rp-card {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-4);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
}
.rp-card-icon {
  flex-shrink: 0;
}
.rp-card-icon.week { color: #3b82f6; }
.rp-card-icon.month { color: #a855f7; }
.rp-card-icon.active { color: #22c55e; }
.rp-card-icon.streak { color: #f97316; }
.rp-card-value {
  font-size: var(--text-xl);
  font-weight: 700;
  color: var(--text-primary);
  line-height: 1.1;
}
.rp-card-label {
  font-size: var(--text-xs);
  color: var(--text-muted);
}

.rp-section {
  margin-bottom: var(--space-6);
}
.rp-section-title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-lg);
  color: var(--text-primary);
  margin-bottom: var(--space-3);
}

.rp-heatmap-wrap {
  padding: var(--space-4);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  overflow-x: auto;
}
.rp-heatmap {
  display: flex;
  gap: 3px;
  width: max-content;
}
.rp-heat-col {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.rp-heat-cell {
  width: 12px;
  height: 12px;
  border-radius: 2px;
  background: var(--bg-hover);
}
.rp-heat-cell.empty {
  background: transparent;
}
.rp-heat-cell.lv-0 { background: var(--bg-hover); }
.rp-heat-cell.lv-1 { background: #9be29b; }
.rp-heat-cell.lv-2 { background: #4fc26a; }
.rp-heat-cell.lv-3 { background: #2f9e44; }
.rp-heat-cell.lv-4 { background: #1d6f34; }

.rp-heat-legend {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-top: var(--space-3);
  font-size: var(--text-xs);
  color: var(--text-muted);
}

.rp-toplist {
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: 0;
  margin: 0;
}
.rp-topitem {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  width: 100%;
  padding: var(--space-2) var(--space-3);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  text-align: left;
  transition: border-color var(--transition-fast);
}
.rp-topitem:hover {
  border-color: var(--border-accent);
}
.rp-toprank {
  width: 22px;
  height: 22px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--bg-active);
  color: var(--text-secondary);
  font-size: var(--text-xs);
  font-weight: 700;
  flex-shrink: 0;
}
.rp-toptitle {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-primary);
}
.rp-toplinks {
  font-size: var(--text-xs);
  color: var(--text-muted);
  flex-shrink: 0;
}
</style>
