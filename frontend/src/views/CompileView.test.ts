// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { useWorkspaceStore } from '@/stores/workspace'
import { useSettingsStore } from '@/stores/settings'
import CompileView from './CompileView.vue'

const harness = vi.hoisted(() => ({
  listInbox: vi.fn(),
  startAll: vi.fn(),
  startOne: vi.fn(),
  taskResult: vi.fn(),
  compileAll: vi.fn(),
  compileNote: vi.fn(),
  cancel: vi.fn(),
  handlers: {} as Record<string, (ev?: unknown) => void>,
}))

vi.mock('@/api', () => ({
  CompileService: {
    ListInbox: harness.listInbox,
    StartCompileAll: harness.startAll,
    StartCompileNote: harness.startOne,
    CompileTaskResult: harness.taskResult,
    CompileAll: harness.compileAll,
    CompileNote: harness.compileNote,
  },
  TaskService: { Cancel: harness.cancel },
  // settings store 的 ai 配置 watcher 会把它推给 Web Clipper，
  // 缺这个导出会在用例结束后抛 Unhandled Error（即使断言全过）。
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
  CredentialService: {
    GetCredential: vi.fn(async () => ''),
    SaveCredential: vi.fn(async () => undefined),
    DeleteCredential: vi.fn(async () => undefined),
  },
}))

// 动态 import 的 runtime 被静态 mock 接管：Events.On 把回调登记到 harness，
// 用例里手动投递 task:progress / task:finished 事件。
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (ev?: unknown) => void) => {
      harness.handlers[name] = cb
      return () => {}
    },
  },
}))

enableAutoUnmount(afterEach)

const doneResult = {
  Results: [
    { Source: 'Inbox/a.md', Dest: 'Compiled/a.md', SnapshotID: 's1', Output: { TLDR: 't', Tags: [], SuggestedLinks: [] } },
  ],
  Errors: [{ Path: 'Inbox/b.md', Error: '模型超时' }],
}

async function renderCompile() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const workspace = useWorkspaceStore()
  workspace.setCurrentWorkspace({ id: 'a', name: 'A', path: '/ws', createdAt: '', lastOpenedAt: '' })
  const settings = useSettingsStore()
  Object.assign(settings.settings.ai, {
    baseURL: 'http://127.0.0.1:11434/v1',
    model: 'qwen2.5:7b',
    apiKey: '',
    protocol: 'openai-chat',
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/compile', component: CompileView },
      { path: '/settings', component: { template: '<div />' } },
      { path: '/history', component: { template: '<div />' } },
    ],
  })
  await router.push('/compile')
  await router.isReady()
  const wrapper = mount(CompileView, { global: { plugins: [pinia, router, i18n] } })
  await flushPromises()
  return { wrapper }
}

beforeEach(() => {
  localStorage.clear()
  ;(i18n.global.locale as any).value = 'zh-CN'
  harness.handlers = {}
  harness.listInbox.mockReset().mockResolvedValue(['Inbox/a.md', 'Inbox/b.md'])
  harness.startAll.mockReset().mockResolvedValue('task-1')
  harness.startOne.mockReset().mockResolvedValue('task-2')
  harness.taskResult.mockReset().mockResolvedValue(doneResult)
  harness.compileAll.mockReset().mockResolvedValue(doneResult)
  harness.compileNote.mockReset()
  harness.cancel.mockReset().mockResolvedValue(true)
})

describe('CompileView · 异步编译任务（P1-2）', () => {
  it('编译全部走任务队列：显示进度、完成后展示结果与失败明细', async () => {
    const { wrapper } = await renderCompile()
    await wrapper.get('.compile-all-btn').trigger('click')
    await flushPromises()

    expect(harness.startAll).toHaveBeenCalledOnce()
    const bar = wrapper.get('[data-testid="compile-progress"]')
    expect(bar.text()).toContain('准备中')

    harness.handlers['task:progress']?.({ data: { id: 'task-1', percent: 50, message: '已完成 1/2 篇' } })
    await flushPromises()
    expect(wrapper.get('[data-testid="compile-progress"]').text()).toContain('50%')
    expect(wrapper.get('[data-testid="compile-progress"]').text()).toContain('已完成 1/2 篇')

    // 完成后面板收尾：进度条消失、结果按任务 ID 取回、失败项就地标红
    harness.listInbox.mockResolvedValue(['Inbox/b.md'])
    harness.handlers['task:finished']?.({ data: { id: 'task-1', status: 'succeeded' } })
    await flushPromises()

    expect(wrapper.find('[data-testid="compile-progress"]').exists()).toBe(false)
    expect(harness.taskResult).toHaveBeenCalledWith('task-1')
    const panel = wrapper.get('.result-panel')
    expect(panel.text()).toContain('失败 1 篇')
    const failed = wrapper.get('.note-item.error')
    expect(failed.text()).toContain('模型超时')
  })

  it('忽略其它任务的进度事件，避免被并发导入任务串扰', async () => {
    const { wrapper } = await renderCompile()
    await wrapper.get('.compile-all-btn').trigger('click')
    await flushPromises()

    harness.handlers['task:progress']?.({ data: { id: 'import-9', percent: 90, message: '导入中' } })
    await flushPromises()

    const bar = wrapper.get('[data-testid="compile-progress"]')
    expect(bar.text()).not.toContain('90%')
    expect(bar.text()).not.toContain('导入中')
  })

  it('取消按钮调用 TaskService.Cancel 并进入取消中状态', async () => {
    harness.cancel.mockReturnValue(new Promise(() => {})) // 挂起：观察取消中态
    const { wrapper } = await renderCompile()
    await wrapper.get('.compile-all-btn').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="compile-cancel"]').trigger('click')
    await flushPromises()

    expect(harness.cancel).toHaveBeenCalledWith('task-1')
    expect(wrapper.get('[data-testid="compile-cancel"]').attributes('disabled')).toBeDefined()
  })

  it('后端未接任务队列（返回空 ID）时退回同步 CompileAll', async () => {
    harness.startAll.mockResolvedValue('')
    const { wrapper } = await renderCompile()
    await wrapper.get('.compile-all-btn').trigger('click')
    await flushPromises()

    expect(harness.compileAll).toHaveBeenCalledOnce()
    expect(wrapper.find('[data-testid="compile-progress"]').exists()).toBe(false)
    expect(wrapper.get('.result-panel').text()).toContain('失败 1 篇')
  })

  it('单篇编译走 StartCompileNote 并对该篇就地标记失败', async () => {
    harness.taskResult.mockResolvedValue({
      Results: [],
      Errors: [{ Path: 'Inbox/a.md', Error: '鉴权失败' }],
    })
    const { wrapper } = await renderCompile()
    await wrapper.findAll('.compile-one-btn')[0].trigger('click')
    await flushPromises()

    expect(harness.startOne).toHaveBeenCalledOnce()
    harness.handlers['task:finished']?.({ data: { id: 'task-2', status: 'failed', error: '鉴权失败' } })
    await flushPromises()

    const item = wrapper.findAll('.note-item')[0]
    expect(item.classes()).toContain('error')
    expect(item.text()).toContain('鉴权失败')
  })
})
