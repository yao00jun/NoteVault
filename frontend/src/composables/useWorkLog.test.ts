// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { WorkbenchService } from '@/api/workbench'
import { useWorkspaceStore } from '@/stores/workspace'
import { resetToasts, useToast } from './useToast'
import { useWorkLog } from './useWorkLog'

vi.mock('@/api/workbench', () => ({ WorkbenchService: { ReadDailyReport: vi.fn() } }))

const piniaInstances: Pinia[] = []
const workspaceA = { id: 'a', name: 'A', path: '/a', createdAt: '', lastOpenedAt: '' }
const workspaceB = { id: 'b', name: 'B', path: '/b', createdAt: '', lastOpenedAt: '' }

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((accept, fail) => { resolve = accept; reject = fail })
  return { promise, resolve, reject }
}

async function renderWorkLog(hasWorkspace = true) {
  const pinia = createPinia()
  piniaInstances.push(pinia)
  setActivePinia(pinia)
  const workspace = useWorkspaceStore()
  if (hasWorkspace) workspace.setCurrentWorkspace({ ...workspaceA })
  const router = createRouter({ history: createMemoryHistory(), routes: ['today', 'projects', 'editor'].map(name => ({ path: `/${name}`, component: { template: '<div />' } })) })
  await router.push('/projects')
  let openTodayWorkLog!: () => Promise<boolean>
  const wrapper = mount({
    setup() { ({ openTodayWorkLog } = useWorkLog()); return {} },
    template: '<div />',
  }, { global: { plugins: [pinia, router] } })
  return { wrapper, workspace, router, openTodayWorkLog }
}

enableAutoUnmount(afterEach)
beforeEach(() => {
  localStorage.clear()
  resetToasts()
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 8, 9, 0, 15))
  vi.mocked(WorkbenchService.ReadDailyReport).mockReset().mockResolvedValue('')
})
afterEach(() => {
  resetToasts()
  piniaInstances.splice(0).forEach(disposePinia)
  vi.useRealTimers()
})

describe('today work log navigation', () => {
  it('reads the local calendar day before opening its saved Markdown report', async () => {
    const { workspace, router, openTodayWorkLog } = await renderWorkLog()
    const originalTreeVersion = workspace.fileTreeVersion
    vi.mocked(WorkbenchService.ReadDailyReport).mockResolvedValueOnce('今日完成：\n1. 联调完成')
    expect(await openTodayWorkLog()).toBe(true)
    expect(WorkbenchService.ReadDailyReport).toHaveBeenCalledWith('/a', '2026-09-09')
    expect(workspace.activeFile).toBe('Daily/Reports/2026-09-09-日报.md')
    expect(router.currentRoute.value.fullPath).toBe('/editor?file=Daily/Reports/2026-09-09-%E6%97%A5%E6%8A%A5.md')
    expect(workspace.fileTreeVersion).toBe(originalTreeVersion)
  })

  it.each(['', '  \n'])('opens the existing report generator for an empty saved draft (%j)', async content => {
    const { workspace, router, openTodayWorkLog } = await renderWorkLog()
    vi.mocked(WorkbenchService.ReadDailyReport).mockResolvedValueOnce(content)
    const openReport = vi.fn()
    window.addEventListener('notevault:daily-report', openReport)
    try {
      expect(await openTodayWorkLog()).toBe(false)
      expect(openReport).toHaveBeenCalledTimes(1)
      expect(workspace.openFiles).toEqual([])
      expect(router.currentRoute.value.path).toBe('/projects')
      expect(useToast().toasts.value).toEqual([expect.objectContaining({ kind: 'info', message: expect.stringContaining('尚未生成') })])
    } finally {
      window.removeEventListener('notevault:daily-report', openReport)
    }
  })

  it('reports read failures without opening a generator or a missing editor file', async () => {
    const { workspace, router, openTodayWorkLog } = await renderWorkLog()
    vi.mocked(WorkbenchService.ReadDailyReport).mockRejectedValueOnce(new Error('权限不足'))
    const openReport = vi.fn()
    window.addEventListener('notevault:daily-report', openReport)
    try {
      expect(await openTodayWorkLog()).toBe(false)
      expect(workspace.openFiles).toEqual([])
      expect(router.currentRoute.value.path).toBe('/projects')
      expect(openReport).not.toHaveBeenCalled()
      expect(useToast().toasts.value).toEqual([expect.objectContaining({ kind: 'error', message: expect.stringContaining('权限不足') })])
    } finally {
      window.removeEventListener('notevault:daily-report', openReport)
    }
  })

  it.each(['saved', 'missing', 'error'])('ignores a late %s response after leaving and returning to the workspace', async outcome => {
    const pending = deferred<string>()
    vi.mocked(WorkbenchService.ReadDailyReport).mockReturnValueOnce(pending.promise)
    const { workspace, router, openTodayWorkLog } = await renderWorkLog()
    const openReport = vi.fn()
    window.addEventListener('notevault:daily-report', openReport)
    try {
      const result = openTodayWorkLog()
      workspace.setCurrentWorkspace({ ...workspaceB })
      workspace.setCurrentWorkspace({ ...workspaceA })
      if (outcome === 'error') pending.reject(new Error('A stale error'))
      else pending.resolve(outcome === 'saved' ? '# Saved report' : '')
      expect(await result).toBe(false)
      expect(workspace.openFiles).toEqual([])
      expect(router.currentRoute.value.path).toBe('/projects')
      expect(openReport).not.toHaveBeenCalled()
      expect(useToast().toasts.value).toEqual([])
    } finally {
      window.removeEventListener('notevault:daily-report', openReport)
    }
  })

  it('ignores a report read after its owner has unmounted', async () => {
    const pending = deferred<string>()
    vi.mocked(WorkbenchService.ReadDailyReport).mockReturnValueOnce(pending.promise)
    const { wrapper, workspace, router, openTodayWorkLog } = await renderWorkLog()
    const result = openTodayWorkLog()
    wrapper.unmount()
    const routeAfterUnmount = router.currentRoute.value.fullPath
    pending.resolve('# Saved')
    expect(await result).toBe(false)
    expect(workspace.activeFile).toBeNull()
    expect(router.currentRoute.value.fullPath).toBe(routeAfterUnmount)
  })

  it('does not open yesterday as today when midnight passes during a report read', async () => {
    vi.setSystemTime(new Date(2026, 8, 9, 23, 59))
    const pending = deferred<string>()
    vi.mocked(WorkbenchService.ReadDailyReport).mockReturnValueOnce(pending.promise)
    const { workspace, router, openTodayWorkLog } = await renderWorkLog()
    const result = openTodayWorkLog()
    vi.setSystemTime(new Date(2026, 8, 10, 0, 1))
    pending.resolve('# Yesterday')
    expect(await result).toBe(false)
    expect(workspace.activeFile).toBeNull()
    expect(router.currentRoute.value.path).toBe('/projects')
  })

  it('discards an older click when a newer report read has completed', async () => {
    const first = deferred<string>()
    vi.mocked(WorkbenchService.ReadDailyReport).mockReturnValueOnce(first.promise).mockResolvedValueOnce('# Saved')
    const { workspace, openTodayWorkLog } = await renderWorkLog()
    const openReport = vi.fn()
    window.addEventListener('notevault:daily-report', openReport)
    try {
      const olderResult = openTodayWorkLog()
      expect(await openTodayWorkLog()).toBe(true)
      first.resolve('')
      expect(await olderResult).toBe(false)
      expect(workspace.activeFile).toBe('Daily/Reports/2026-09-09-日报.md')
      expect(openReport).not.toHaveBeenCalled()
    } finally {
      window.removeEventListener('notevault:daily-report', openReport)
    }
  })

  it('opens Today without reading files when no workspace is selected', async () => {
    const { router, openTodayWorkLog } = await renderWorkLog(false)
    expect(await openTodayWorkLog()).toBe(false)
    expect(WorkbenchService.ReadDailyReport).not.toHaveBeenCalled()
    expect(router.currentRoute.value.path).toBe('/today')
  })
})
