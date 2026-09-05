// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'

const confirmDialogMock = vi.fn()
vi.mock('@/composables/useConfirm', () => ({
  confirmDialog: (...args: unknown[]) => confirmDialogMock(...args),
}))

vi.mock('@bindings/github.com/notevault/notevault/index.js', () => ({
  SnapshotService: {
    ListSnapshotFiles: vi.fn(),
    ListSnapshots: vi.fn(),
    DiffWithCurrent: vi.fn(),
    DiffSnapshots: vi.fn(),
    RestoreSnapshot: vi.fn(),
    DeleteSnapshot: vi.fn(),
    ClearSnapshots: vi.fn(),
    CreateManualSnapshot: vi.fn(),
    PruneSnapshots: vi.fn(),
    GetSnapshotStats: vi.fn(),
  },
}))

import HistoryView from './HistoryView.vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { SnapshotService } from '@bindings/github.com/notevault/notevault/index.js'

const mocked = {
  listFiles: vi.mocked(SnapshotService.ListSnapshotFiles),
  listSnapshots: vi.mocked(SnapshotService.ListSnapshots),
  diffCurrent: vi.mocked(SnapshotService.DiffWithCurrent),
  diffSnapshots: vi.mocked(SnapshotService.DiffSnapshots),
  restore: vi.mocked(SnapshotService.RestoreSnapshot),
  del: vi.mocked(SnapshotService.DeleteSnapshot),
  clear: vi.mocked(SnapshotService.ClearSnapshots),
  manual: vi.mocked(SnapshotService.CreateManualSnapshot),
  prune: vi.mocked(SnapshotService.PruneSnapshots),
  stats: vi.mocked(SnapshotService.GetSnapshotStats),
}

function summary(path: string, count: number, latestAt: string, bytes = 100) {
  return { path, count, latestAt, bytes }
}

function snapshot(id: string, path: string, createdAt: string, reason = 'save', size = 100) {
  return { id, path, hash: 'h_' + id, size, createdAt, reason }
}

function diff(overrides: Record<string, unknown> = {}) {
  return {
    path: 'notes/a.md',
    fromId: 'snap_1',
    toId: '',
    fromAt: '2026-08-30T10:00:00Z',
    toAt: '2026-08-31T10:00:00Z',
    ops: [
      { type: 'equal', oldLine: 1, newLine: 1, text: '# Title' },
      { type: 'delete', oldLine: 2, newLine: 0, text: 'old line' },
      { type: 'insert', oldLine: 0, newLine: 2, text: 'new line' },
      { type: 'gap', oldLine: 0, newLine: 0, text: '', count: 12 },
    ],
    added: 1,
    removed: 1,
    truncated: false,
    identical: false,
    ...overrides,
  }
}

enableAutoUnmount(afterEach)

function mountHistory(withWorkspace = true) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/history', component: { template: '<div />' } },
      { path: '/editor', component: { template: '<div />' } },
    ],
  })
  const workspaceStore = useWorkspaceStore()
  if (withWorkspace) {
    workspaceStore.setCurrentWorkspace({
      id: 'ws_1', name: '测试库', path: '/tmp/vault', createdAt: '', lastOpenedAt: '',
    })
  }
  const wrapper = mount(HistoryView, { global: { plugins: [pinia, router, i18n] } })
  return { wrapper, workspaceStore, router }
}

describe('HistoryView', () => {
  beforeEach(() => {
    ;(i18n.global.locale as any).value = 'zh-CN'
    for (const fn of Object.values(mocked)) fn.mockReset()
    mocked.stats.mockResolvedValue({ snapshots: 3, files: 1, objects: 3, diskBytes: 2048 })
    mocked.listFiles.mockResolvedValue([summary('notes/a.md', 2, '2026-08-31T10:00:00Z', 512)])
    mocked.listSnapshots.mockResolvedValue([
      snapshot('snap_2', 'notes/a.md', '2026-08-31T09:00:00Z', 'manual'),
      snapshot('snap_1', 'notes/a.md', '2026-08-30T09:00:00Z', 'save'),
    ])
    mocked.diffCurrent.mockResolvedValue(diff() as any)
    mocked.diffSnapshots.mockResolvedValue(diff({ toId: 'snap_2' }) as any)
    confirmDialogMock.mockReset()
    confirmDialogMock.mockResolvedValue(true)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('未选择工作区时提示先打开工作区，且不请求后端', async () => {
    const { wrapper } = mountHistory(false)
    await flushPromises()

    expect(wrapper.text()).toContain('尚未选择工作区')
    expect(mocked.listFiles).not.toHaveBeenCalled()
  })

  it('挂载后加载统计、文件列表，并自动选中首个文件的最新版本算与当前的差异', async () => {
    const { wrapper } = mountHistory()
    await flushPromises()

    expect(mocked.stats).toHaveBeenCalledWith('/tmp/vault')
    expect(mocked.listFiles).toHaveBeenCalledWith('/tmp/vault')
    expect(mocked.listSnapshots).toHaveBeenCalledWith('/tmp/vault', 'notes/a.md')
    expect(mocked.diffCurrent).toHaveBeenCalledWith('/tmp/vault', 'snap_2')

    expect(wrapper.find('[data-testid="history-stats"]').text()).toContain('2.0 KB')
    expect(wrapper.findAll('[data-testid="history-file"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid="history-version"]')).toHaveLength(2)
    expect(wrapper.findAll('[data-testid="history-version"]')[0].classes()).toContain('active')
  })

  it('没有任何历史时显示空状态而不是空白面板', async () => {
    mocked.listFiles.mockResolvedValue([])
    const { wrapper } = mountHistory()
    await flushPromises()

    expect(wrapper.find('[data-testid="history-empty"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('暂无历史版本')
    expect(mocked.listSnapshots).not.toHaveBeenCalled()
  })

  it('后端返回 null 列表时按空列表处理，不抛错', async () => {
    mocked.listFiles.mockResolvedValue(null as any)
    const { wrapper } = mountHistory()
    await flushPromises()

    expect(wrapper.find('[data-testid="history-empty"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="history-error"]').exists()).toBe(false)
  })

  it('渲染 diff 时区分增删行并把折叠区域显示为省略提示', async () => {
    const { wrapper } = mountHistory()
    await flushPromises()

    const rows = wrapper.findAll('[data-testid="diff-row"]')
    expect(rows).toHaveLength(4)
    expect(rows[1].classes()).toContain('diff-delete')
    expect(rows[2].classes()).toContain('diff-insert')
    expect(rows[3].text()).toContain('12')
    expect(wrapper.find('[data-testid="diff-summary"]').text()).toContain('+1')
    expect(wrapper.find('[data-testid="diff-summary"]').text()).toContain('-1')
  })

  it('内容一致时显示「没有差异」而不是空 diff 区', async () => {
    mocked.diffCurrent.mockResolvedValue(diff({ identical: true, ops: [], added: 0, removed: 0 }) as any)
    const { wrapper } = mountHistory()
    await flushPromises()

    expect(wrapper.find('[data-testid="diff-identical"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="diff-body"]').exists()).toBe(false)
  })

  it('切到「与上一版本对比」时调用 DiffSnapshots 并按倒序取前驱', async () => {
    const { wrapper } = mountHistory()
    await flushPromises()

    await wrapper.find('[data-testid="mode-previous"]').trigger('click')
    await flushPromises()

    expect(mocked.diffSnapshots).toHaveBeenCalledWith('/tmp/vault', 'snap_1', 'snap_2')
  })

  it('选中最旧版本时「与上一版本对比」不可用，避免无前驱的空请求', async () => {
    const { wrapper } = mountHistory()
    await flushPromises()

    await wrapper.findAll('[data-testid="history-version"]')[1].trigger('click')
    await flushPromises()

    const btn = wrapper.find('[data-testid="mode-previous"]')
    expect(btn.attributes('disabled')).toBeDefined()
    expect(mocked.diffCurrent).toHaveBeenLastCalledWith('/tmp/vault', 'snap_1')
  })

  it('切换版本时旧的 diff 请求返回不会覆盖新选择', async () => {
    let resolveOld!: (v: any) => void
    let resolveNew!: (v: any) => void
    mocked.diffCurrent
      .mockReturnValueOnce(new Promise(r => { resolveOld = r }) as any)
      .mockReturnValueOnce(new Promise(r => { resolveNew = r }) as any)

    const { wrapper } = mountHistory()
    await flushPromises()
    await wrapper.findAll('[data-testid="history-version"]')[1].trigger('click')
    await flushPromises()

    resolveOld(diff({ added: 99, removed: 99 }))
    await flushPromises()
    expect(wrapper.find('[data-testid="diff-summary"]').exists()).toBe(false)

    resolveNew(diff({ added: 7, removed: 3 }))
    await flushPromises()
    expect(wrapper.find('[data-testid="diff-summary"]').text()).toContain('+7')
  })

  it('恢复版本会二次确认、刷新文件树，并在有安全备份时提示', async () => {
    mocked.restore.mockResolvedValue({
      path: 'notes/a.md', restoredId: 'snap_2', backupId: 'snap_3', bytes: 100,
    } as any)
    const { wrapper, workspaceStore } = mountHistory()
    await flushPromises()
    const before = workspaceStore.fileTreeVersion

    await wrapper.findAll('[data-testid="restore-btn"]')[0].trigger('click')
    await flushPromises()

    expect(confirmDialogMock).toHaveBeenCalled()
    expect(mocked.restore).toHaveBeenCalledWith('/tmp/vault', 'snap_2')
    expect(workspaceStore.fileTreeVersion).toBe(before + 1)
    expect(wrapper.find('[data-testid="history-notice"]').text()).toContain('已自动备份')
  })

  it('用户在确认框点取消时不执行恢复', async () => {
    confirmDialogMock.mockResolvedValue(false)
    const { wrapper } = mountHistory()
    await flushPromises()

    await wrapper.findAll('[data-testid="restore-btn"]')[0].trigger('click')
    await flushPromises()

    expect(mocked.restore).not.toHaveBeenCalled()
  })

  it('恢复失败时显示错误横幅而不是静默失败', async () => {
    mocked.restore.mockRejectedValue(new Error('disk full'))
    const { wrapper } = mountHistory()
    await flushPromises()

    await wrapper.findAll('[data-testid="restore-btn"]')[0].trigger('click')
    await flushPromises()

    const err = wrapper.find('[data-testid="history-error"]')
    expect(err.exists()).toBe(true)
    expect(err.text()).toContain('disk full')
  })

  it('删除单个版本后重新拉取列表', async () => {
    mocked.del.mockResolvedValue(undefined as any)
    const { wrapper } = mountHistory()
    await flushPromises()
    mocked.listSnapshots.mockClear()

    await wrapper.findAll('[data-testid="delete-snapshot-btn"]')[1].trigger('click')
    await flushPromises()

    expect(mocked.del).toHaveBeenCalledWith('/tmp/vault', 'snap_1')
    expect(mocked.listSnapshots).toHaveBeenCalled()
  })

  it('手动打点后选中新生成的快照', async () => {
    mocked.manual.mockResolvedValue(snapshot('snap_9', 'notes/a.md', '2026-08-31T11:00:00Z', 'manual') as any)
    mocked.listSnapshots.mockResolvedValue([
      snapshot('snap_9', 'notes/a.md', '2026-08-31T11:00:00Z', 'manual'),
      snapshot('snap_2', 'notes/a.md', '2026-08-31T09:00:00Z', 'manual'),
    ])
    const { wrapper } = mountHistory()
    await flushPromises()

    await wrapper.find('[data-testid="manual-snapshot-btn"]').trigger('click')
    await flushPromises()

    expect(mocked.manual).toHaveBeenCalledWith('/tmp/vault', 'notes/a.md')
    expect(wrapper.find('[data-testid="history-notice"]').text()).toContain('手动快照')
    expect(mocked.diffCurrent).toHaveBeenLastCalledWith('/tmp/vault', 'snap_9')
  })

  it('清空某文档历史后该文件从列表移除并回到空状态', async () => {
    mocked.clear.mockResolvedValue(2 as any)
    const { wrapper } = mountHistory()
    await flushPromises()
    mocked.listFiles.mockResolvedValue([])

    await wrapper.find('[data-testid="clear-file-btn"]').trigger('click')
    await flushPromises()

    expect(mocked.clear).toHaveBeenCalledWith('/tmp/vault', 'notes/a.md')
    expect(wrapper.find('[data-testid="history-empty"]').exists()).toBe(true)
  })

  it('清理过期版本后用返回的统计刷新占用信息', async () => {
    mocked.prune.mockResolvedValue({ snapshots: 1, files: 1, objects: 1, diskBytes: 1024 } as any)
    const { wrapper } = mountHistory()
    await flushPromises()

    await wrapper.find('[data-testid="prune-btn"]').trigger('click')
    await flushPromises()

    expect(mocked.prune).toHaveBeenCalledWith('/tmp/vault')
    expect(wrapper.find('[data-testid="history-stats"]').text()).toContain('1.0 KB')
  })

  it('在编辑器打开时拼出工作区绝对路径并跳转', async () => {
    const { wrapper, workspaceStore, router } = mountHistory()
    await flushPromises()

    await wrapper.find('[data-testid="open-editor-btn"]').trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/editor')
    expect(workspaceStore.activeFile).toBe('/tmp/vault/notes/a.md')
  })

  it('Windows 风格工作区路径用反斜杠拼接，不混用分隔符', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        { path: '/editor', component: { template: '<div />' } },
      ],
    })
    const workspaceStore = useWorkspaceStore()
    workspaceStore.setCurrentWorkspace({
      id: 'ws_win', name: 'win', path: 'E:\\vault', createdAt: '', lastOpenedAt: '',
    })
    const wrapper = mount(HistoryView, { global: { plugins: [pinia, router, i18n] } })
    await flushPromises()

    await wrapper.find('[data-testid="open-editor-btn"]').trigger('click')
    await flushPromises()

    expect(workspaceStore.activeFile).toBe('E:\\vault\\notes\\a.md')
  })

  it('切换工作区时重新加载历史，不残留上一个库的数据', async () => {
    const { wrapper, workspaceStore } = mountHistory()
    await flushPromises()
    expect(wrapper.findAll('[data-testid="history-file"]')).toHaveLength(1)

    mocked.listFiles.mockResolvedValue([
      summary('other/b.md', 1, '2026-08-31T12:00:00Z'),
      summary('other/c.md', 1, '2026-08-31T12:00:00Z'),
    ])
    mocked.listSnapshots.mockResolvedValue([snapshot('snap_b', 'other/b.md', '2026-08-31T12:00:00Z')])
    workspaceStore.setCurrentWorkspace({
      id: 'ws_2', name: '第二个库', path: '/tmp/other', createdAt: '', lastOpenedAt: '',
    })
    await flushPromises()

    expect(mocked.listFiles).toHaveBeenLastCalledWith('/tmp/other')
    expect(wrapper.findAll('[data-testid="history-file"]')).toHaveLength(2)
    expect(mocked.listSnapshots).toHaveBeenLastCalledWith('/tmp/other', 'other/b.md')
  })

  it('文件列表加载失败时给出错误提示', async () => {
    mocked.listFiles.mockRejectedValue(new Error('history dir unreadable'))
    const { wrapper } = mountHistory()
    await flushPromises()

    expect(wrapper.find('[data-testid="history-error"]').text()).toContain('history dir unreadable')
  })

  it('diff 过大被截断时显示简化提示', async () => {
    mocked.diffCurrent.mockResolvedValue(diff({ truncated: true }) as any)
    const { wrapper } = mountHistory()
    await flushPromises()

    expect(wrapper.find('[data-testid="diff-summary"]').text()).toContain('已简化显示')
  })

  it('点击另一个文件时切换到它的版本列表', async () => {
    mocked.listFiles.mockResolvedValue([
      summary('notes/a.md', 2, '2026-08-31T10:00:00Z'),
      summary('notes/b.md', 1, '2026-08-31T11:00:00Z'),
    ])
    const { wrapper } = mountHistory()
    await flushPromises()

    mocked.listSnapshots.mockResolvedValue([snapshot('snap_b1', 'notes/b.md', '2026-08-31T11:00:00Z')])
    await wrapper.findAll('[data-testid="history-file"]')[1].trigger('click')
    await flushPromises()

    expect(mocked.listSnapshots).toHaveBeenLastCalledWith('/tmp/vault', 'notes/b.md')
    expect(wrapper.findAll('[data-testid="history-version"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid="history-file"]')[1].classes()).toContain('active')
  })
})
