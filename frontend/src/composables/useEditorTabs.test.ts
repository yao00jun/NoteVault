// @vitest-environment jsdom
/**
 * 标签页集合的索引不变式。
 *
 * 「删掉第 i 个标签后 activeTabIndex 怎么变」原先在 closeTab 与「编译后标签跟随」
 * 两处各写了一遍，这类重复最典型的后果是「关掉 A 结果 B 消失」。
 * 这组用例断言的是**用户看到哪个文件**（activeTab.path），不是下标数字——
 * 下标只有在具体场景里才有意义。
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { computed, defineComponent, ref } from 'vue'
import { createMemoryHistory, createRouter, useRoute } from 'vue-router'
import type { CancellablePromise } from '@wailsio/runtime'
import { i18n } from '@/i18n'
import { FileService } from '@/api'
import { useEditorTabs } from './useEditorTabs'

vi.mock('@/api', () => ({ FileService: { ReadFile: vi.fn() } }))
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => () => {}) } }))

enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(FileService.ReadFile).mockImplementation(
    (_ws, path) => Promise.resolve(`# ${path}`) as CancellablePromise<string>,
  )
})

interface Harness {
  api: ReturnType<typeof useEditorTabs>
  saveTab: ReturnType<typeof vi.fn>
  workspace: { openFile: ReturnType<typeof vi.fn>; closeFile: ReturnType<typeof vi.fn>; setActiveFile: ReturnType<typeof vi.fn> }
  router: ReturnType<typeof createRouter>
}

async function mountTabs(): Promise<Harness> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/editor', component: { template: '<div />' } }],
  })
  await router.push('/editor')
  const workspace = { openFile: vi.fn(), closeFile: vi.fn(), setActiveFile: vi.fn() }
  const saveTab = vi.fn(async () => true)
  const root = ref('C:/vault')
  let api!: ReturnType<typeof useEditorTabs>
  mount(defineComponent({
    setup() {
      api = useEditorTabs({
        workspacePath: computed(() => root.value),
        sessionWorkspacePath: 'C:/vault',
        route: useRoute(),
        router,
        workspace,
        applyInProgress: () => false,
        saveTab,
      })
      return () => null
    },
  }), { global: { plugins: [router, i18n] } })
  return { api, saveTab, workspace, router }
}

/** 依序打开若干文件，返回时全部加载完成 */
async function seed(api: Harness['api'], paths: string[]) {
  for (const path of paths) {
    await api.openFileByPath(path)
    await flushPromises()
  }
}

describe('editor tab collection', () => {
  it('keeps one tab per document and only re-activates an existing one', async () => {
    const { api } = await mountTabs()
    await seed(api, ['a.md', 'b.md', 'a.md'])

    expect(api.tabs.value.map(tab => tab.path)).toEqual(['a.md', 'b.md'])
    expect(api.activeTab.value?.path).toBe('a.md')
    // 第二次打开 a 只切标签，不重复读盘
    expect(FileService.ReadFile).toHaveBeenCalledTimes(2)
  })

  it('drops a late read that lost the race to a newer open request', async () => {
    const { api } = await mountTabs()
    const reads: Array<(value: string) => void> = []
    vi.mocked(FileService.ReadFile).mockImplementation(
      () => new Promise<string>(resolve => { reads.push(resolve) }) as CancellablePromise<string>,
    )

    const first = api.openFileByPath('a.md')
    const second = api.openFileByPath('b.md')
    reads[1]!('# B')
    await second
    await flushPromises()
    reads[0]!('# A')
    await first
    await flushPromises()

    // 晚到的 a.md 不能把自己插进标签栏，更不能顶掉当前文件
    expect(api.tabs.value.map(tab => tab.path)).toEqual(['b.md'])
    expect(api.activeTab.value?.path).toBe('b.md')
  })

  it('keeps the same document active when a tab to its left is closed', async () => {
    const { api } = await mountTabs()
    await seed(api, ['a.md', 'b.md', 'c.md'])
    expect(api.activeTab.value?.path).toBe('c.md')

    api.removeTabAt(0)
    expect(api.activeTab.value?.path).toBe('c.md')
    expect(api.activeTabIndex.value).toBe(1)
  })

  it('activates the neighbour when the active tab itself is closed', async () => {
    const { api } = await mountTabs()
    await seed(api, ['a.md', 'b.md', 'c.md'])

    // 关中间：落到下一个
    expect(api.activeTab.value?.path).toBe('c.md')
    api.removeTabAt(2)
    expect(api.activeTab.value?.path).toBe('b.md')
    // 关中间那个（此时活动的是 b）：落到 c 已不存在，应回到 a
    api.removeTabAt(1)
    expect(api.activeTab.value?.path).toBe('a.md')
    api.removeTabAt(0)
    expect(api.activeTab.value).toBeNull()
    expect(api.activeTabIndex.value).toBe(-1)
  })

  it('refuses to close a dirty tab whose save failed', async () => {
    const { api, saveTab } = await mountTabs()
    await seed(api, ['a.md'])
    api.tabs.value[0]!.isDirty = true
    saveTab.mockResolvedValueOnce(false)

    await api.closeTab(0)

    expect(api.tabs.value).toHaveLength(1)
    expect(api.activeTab.value?.path).toBe('a.md')
  })

  it('reuses one tab for differently cased Windows paths', async () => {
    const { api } = await mountTabs()
    await seed(api, ['Projects/Go/Tasks.md'])

    await api.openFileByPath('projects/go/tasks.md')
    await flushPromises()

    expect(api.tabs.value.map(tab => tab.path)).toEqual(['Projects/Go/Tasks.md'])
    expect(FileService.ReadFile).toHaveBeenCalledTimes(1)
  })
})
