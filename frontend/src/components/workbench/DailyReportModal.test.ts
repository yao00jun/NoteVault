// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { useWorkspaceStore } from '@/stores/workspace'
import type { CopilotRequest } from '@/composables/useCopilotRequest'

const api = vi.hoisted(() => ({ read: vi.fn(), save: vi.fn(), snapshot: vi.fn(), copy: vi.fn() }))
vi.mock('@/api', () => ({ WorkbenchService: { ReadDailyReport: api.read, SaveDailyReport: api.save, GetWorkbench: api.snapshot }, ReminderService: { GetAllReminders: vi.fn().mockResolvedValue([]) } }))
import DailyReportModal from './DailyReportModal.vue'
let pinia: ReturnType<typeof createPinia>
let wrapper: ReturnType<typeof mount>
let saved = new Map<string, string>()

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date(2026, 8, 8, 10))
  vi.resetAllMocks()
  localStorage.clear()
  saved = new Map()
  api.read.mockResolvedValue('')
  api.save.mockImplementation(async (workspace: string, date: string, body: string) => {
    const path = `Daily/Reports/${date}-日报.md`
    saved.set(`${workspace}/${path}`, body)
    return path
  })
  api.snapshot.mockResolvedValue({ date: '2026-09-08', tasks: [], projects: [], books: [], cards: [], progress: [], documents: [], radar: { path: '', content: '' }, indexedAt: 'now', warnings: [] })
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: api.copy.mockResolvedValue(undefined) } })
  pinia = createPinia()
  setActivePinia(pinia)
  useWorkspaceStore().setCurrentWorkspace({ id: 'ws', name: '工作区', path: '/vault', createdAt: '', lastOpenedAt: '' })
})
afterEach(() => { wrapper?.unmount(); disposePinia(pinia); vi.useRealTimers() })
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }, { path: '/editor', component: { template: '<div />' } }] })
  wrapper = mount(DailyReportModal, { props: { visible: true }, global: { plugins: [pinia, router], stubs: { Teleport: true } } })
  await flushPromises()
}

describe('daily report archive', () => {
  it('automatically creates a Markdown report and copies plain corporate email text', async () => {
    await render()
    const content = saved.get('/vault/Daily/Reports/2026-09-08-日报.md')
    expect(content).toContain('主题：【日报】2026年9月8日')
    expect(content).toContain('今日完成：')
    await wrapper.get('[data-testid="copy-report"]').trigger('click')
    await flushPromises()
    expect(api.copy).toHaveBeenCalledWith(content)
    expect(wrapper.text()).toContain('已复制')
  })

  it('preserves a saved manual draft on open and autosaves new edits', async () => {
    api.read.mockResolvedValue('主题：已有日报\n\n今日完成：人工补充的关键结论')
    await render()
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toContain('人工补充的关键结论')
    expect(saved.size).toBe(0)
    await wrapper.get('textarea').setValue('主题：人工改写\n\n今日完成：已验收')
    await vi.advanceTimersByTimeAsync(650)
    expect(saved.get('/vault/Daily/Reports/2026-09-08-日报.md')).toBe('主题：人工改写\n\n今日完成：已验收')
  })

  it('sends the current draft to Copilot and explicitly applies polished text to the same archive', async () => {
    let request: CopilotRequest | undefined
    const capture = (event: Event) => { request = (event as CustomEvent<CopilotRequest>).detail }
    window.addEventListener('notevault:copilot-request', capture)
    try {
      await render()
      await wrapper.get('[data-testid="polish-report"]').trigger('click')
      await flushPromises()
      expect(request?.context).toContain('主题：【日报】')
      expect(request?.source).toBe('daily-report')
      request?.onApply?.('主题：【日报】专业汇报\n\n今日完成：已通过验收')
      await flushPromises()
      expect(saved.get('/vault/Daily/Reports/2026-09-08-日报.md')).toContain('专业汇报')
    } finally { window.removeEventListener('notevault:copilot-request', capture) }
  })

  it('retains the draft and does not pretend to close successfully when the archive write fails', async () => {
    api.save.mockRejectedValue(new Error('磁盘写入失败'))
    await render()
    expect(wrapper.get('[role="alert"]').text()).toContain('磁盘写入失败')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toContain('主题：【日报】')
    await wrapper.get('[data-testid="close-report"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('autosaves a reversal made while an earlier draft is still being written', async () => {
    api.read.mockResolvedValue('Draft A')
    await render()
    let finish!: () => void
    api.save.mockImplementationOnce((_workspace: string, _date: string, body: string) => new Promise<string>(resolve => {
      finish = () => {
        saved.set('/vault/Daily/Reports/2026-09-08-日报.md', body)
        resolve('Daily/Reports/2026-09-08-日报.md')
      }
    }))
    await wrapper.get('textarea').setValue('Draft B')
    await vi.advanceTimersByTimeAsync(550)
    await wrapper.get('textarea').setValue('Draft A')
    finish()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(650)
    expect(saved.get('/vault/Daily/Reports/2026-09-08-日报.md')).toBe('Draft A')
    expect(wrapper.text()).toContain('已归档保存')
  })

  it('compares each queued write with the last successfully saved revision', async () => {
    api.read.mockResolvedValue('Draft A')
    await render()
    let disk = 'Draft A'
    let finish!: () => void
    let first = true
    api.save.mockImplementation(async (_workspace: string, _date: string, body: string, expected: string) => {
      if (expected !== disk) throw new Error('日报已在其他位置修改')
      if (first) {
        first = false
        await new Promise<void>(resolve => { finish = resolve })
      }
      disk = body
      return 'Daily/Reports/2026-09-08-日报.md'
    })
    await wrapper.get('textarea').setValue('Draft B')
    await vi.advanceTimersByTimeAsync(550)
    await wrapper.get('textarea').setValue('Draft C')
    await vi.advanceTimersByTimeAsync(550)
    finish?.()
    await flushPromises()
    expect(disk).toBe('Draft C')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('retains local text on an external archive conflict and can reload without losing that draft', async () => {
    let disk = 'Original archive'
    api.read.mockImplementation(async () => disk)
    api.save.mockImplementation(async (_workspace: string, _date: string, body: string, expected: string) => {
      if (disk !== expected) throw new Error('日报已在其他位置修改，请重新读取')
      disk = body
      return 'Daily/Reports/2026-09-08-日报.md'
    })
    await render()
    disk = 'Updated by sync'
    await wrapper.get('textarea').setValue('Local report draft')
    await vi.advanceTimersByTimeAsync(650)
    expect(disk).toBe('Updated by sync')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('Local report draft')
    expect(wrapper.get('[role="alert"]').text()).toContain('其他位置修改')
    await wrapper.get('[data-testid="reload-report"]').trigger('click')
    await flushPromises()
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('Updated by sync')
    const undo = wrapper.findAll('button').find(button => button.text().includes('恢复上一稿'))!
    await undo.trigger('click')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('Local report draft')
  })
})
