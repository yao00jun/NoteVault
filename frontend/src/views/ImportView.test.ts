// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'

vi.mock('@bindings/github.com/notevault/notevault/index.js', () => ({
  AppService: {
    OpenFolderDialog: vi.fn(async () => ''),
    OpenFileDialog: vi.fn(async () => ''),
  },
  ImportService: {
    ImportMarkdownFolder: vi.fn(async () => ({ imported: 0, skipped: 0, renamed: 0 })),
    ImportZip: vi.fn(async () => ({ imported: 0, skipped: 0, renamed: 0 })),
  },
  GitService: {
    Status: vi.fn(async () => ({ installed: false, isRepo: false, branch: '', changed: 0, untracked: 0 })),
    InitRepo: vi.fn(async () => undefined),
    EnsureGitignore: vi.fn(async () => true),
    CommitAll: vi.fn(async () => ''),
  },
}))

import ImportView from './ImportView.vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { GitService } from '@bindings/github.com/notevault/notevault/index.js'
import type { Workspace } from '@bindings/github.com/notevault/notevault/models.js'

const mockedStatus = vi.mocked(GitService.Status)
const mockedInit = vi.mocked(GitService.InitRepo)
const mockedCommit = vi.mocked(GitService.CommitAll)

enableAutoUnmount(afterEach)

function mountImport() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/import', component: { template: '<div />' } },
    ],
  })
  const workspaceStore = useWorkspaceStore()
  const wrapper = mount(ImportView, {
    global: { plugins: [pinia, router, i18n] },
  })
  return { wrapper, workspaceStore }
}

const fakeWorkspace = {
  id: 'ws-1',
  name: 'Vault',
  path: 'E:\\Notes',
} as Workspace

describe('ImportView · P2-4 Git 版本管理卡片', () => {
  beforeEach(() => {
    ;(i18n.global.locale as any).value = 'zh-CN'
    mockedStatus.mockReset()
    mockedInit.mockReset()
    mockedCommit.mockReset()
  })

  it('git 未安装时显示安装提示，不出现初始化按钮', async () => {
    mockedStatus.mockResolvedValue({ installed: false, isRepo: false, branch: '', changed: 0, untracked: 0 })
    const { wrapper, workspaceStore } = mountImport()
    workspaceStore.setCurrentWorkspace(fakeWorkspace)
    await flushPromises()

    expect(wrapper.text()).toContain('未检测到 git 命令')
    expect(wrapper.text()).not.toContain('初始化仓库')
  })

  it('非仓库时显示初始化按钮，点击后调用 InitRepo 并刷新状态', async () => {
    mockedStatus.mockResolvedValueOnce({ installed: true, isRepo: false, branch: '', changed: 0, untracked: 0 })
    // 初始化后刷新：变成本地仓库
    mockedStatus.mockResolvedValueOnce({ installed: true, isRepo: true, branch: 'main', changed: 2, untracked: 1 })
    mockedInit.mockResolvedValue(undefined)
    const { wrapper, workspaceStore } = mountImport()
    workspaceStore.setCurrentWorkspace(fakeWorkspace)
    await flushPromises()

    const initBtn = wrapper.findAll('button').find(b => b.text().includes('初始化仓库'))
    expect(initBtn).toBeTruthy()
    await initBtn!.trigger('click')
    await flushPromises()

    expect(mockedInit).toHaveBeenCalledWith(fakeWorkspace.path)
    expect(wrapper.text()).toContain('仓库初始化完成')
    // 刷新后显示分支统计
    expect(wrapper.text()).toContain('main')
  })

  it('已是仓库时显示统计并支持一键提交', async () => {
    mockedStatus
      .mockResolvedValueOnce({ installed: true, isRepo: true, branch: 'main', changed: 2, untracked: 3 })
      .mockResolvedValueOnce({ installed: true, isRepo: true, branch: 'main', changed: 0, untracked: 0 })
    mockedCommit.mockResolvedValue('提交成功')
    const { wrapper, workspaceStore } = mountImport()
    workspaceStore.setCurrentWorkspace(fakeWorkspace)
    await flushPromises()

    expect(wrapper.text()).toContain('main')
    const input = wrapper.find('.iv-git-input')
    expect(input.exists()).toBe(true)
    await input.setValue('写今天的笔记')
    const commitBtn = wrapper.findAll('button').find(b => b.text().includes('一键提交'))
    await commitBtn!.trigger('click')
    await flushPromises()

    expect(mockedCommit).toHaveBeenCalledWith(fakeWorkspace.path, '写今天的笔记')
    expect(wrapper.text()).toContain('提交成功')
  })

  it('提交返回「没有可提交」文案时映射为干净提示', async () => {
    mockedStatus
      .mockResolvedValueOnce({ installed: true, isRepo: true, branch: 'main', changed: 0, untracked: 0 })
      .mockResolvedValueOnce({ installed: true, isRepo: true, branch: 'main', changed: 0, untracked: 0 })
    mockedCommit.mockResolvedValue('没有可提交的变更')
    const { wrapper, workspaceStore } = mountImport()
    workspaceStore.setCurrentWorkspace(fakeWorkspace)
    await flushPromises()

    const commitBtn = wrapper.findAll('button').find(b => b.text().includes('一键提交'))
    await commitBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('工作区干净')
  })

  it('InitRepo 报错时展示失败横幅', async () => {
    mockedStatus.mockResolvedValue({ installed: true, isRepo: false, branch: '', changed: 0, untracked: 0 })
    mockedInit.mockRejectedValue(new Error('unsafe repository'))
    const { wrapper, workspaceStore } = mountImport()
    workspaceStore.setCurrentWorkspace(fakeWorkspace)
    await flushPromises()

    const initBtn = wrapper.findAll('button').find(b => b.text().includes('初始化仓库'))
    await initBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Git 操作失败')
    expect(wrapper.text()).toContain('unsafe repository')
  })

  it('没有工作区时不渲染 Git 卡片', async () => {
    mockedStatus.mockResolvedValue({ installed: true, isRepo: true, branch: 'main', changed: 0, untracked: 0 })
    const { wrapper } = mountImport()
    await flushPromises()
    expect(wrapper.text()).not.toContain('版本管理')
  })
})
