// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { i18n } from '@/i18n'

vi.mock('@/api', () => ({
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
  TemplateService: {
    ListTemplates: vi.fn(async () => []),
    GetTemplateContent: vi.fn(async () => ''),
    CreateFromTemplate: vi.fn(async () => null),
  },
}))

import TemplateCreateDialog from './TemplateCreateDialog.vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { TemplateService } from '@/api'

const mockedList = vi.mocked(TemplateService.ListTemplates)
const mockedCreate = vi.mocked(TemplateService.CreateFromTemplate)

enableAutoUnmount(afterEach)

function mountDialog(defaultFolder?: string) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const workspaceStore = useWorkspaceStore()
  workspaceStore.setCurrentWorkspace({ id: 'ws', name: 'Vault', path: 'E:\\Notes' } as never)
  const wrapper = mount(TemplateCreateDialog, {
    props: { defaultFolder },
    global: {
      plugins: [i18n],
      stubs: { teleport: true },
    },
  })
  return { wrapper, workspaceStore }
}

describe('TemplateCreateDialog · P2-2 模板系统', () => {
  beforeEach(() => {
    ;(i18n.global.locale as any).value = 'zh-CN'
    mockedList.mockReset()
    mockedCreate.mockReset()
  })

  it('没有模板时展示 Templates 目录引导', async () => {
    mockedList.mockResolvedValue([])
    const { wrapper } = mountDialog()
    await flushPromises()

    expect(wrapper.text()).toContain('还没有模板')
    expect(wrapper.text()).toContain('E:\\Notes/Templates/')
  })

  it('渲染模板下拉、自定义变量输入与目标路径', async () => {
    mockedList.mockResolvedValue([
      { name: '会议', variables: ['project', 'people'], builtin: false },
      { name: '读书笔记', variables: [], builtin: true },
    ])
    const { wrapper } = mountDialog()
    await flushPromises()

    // 默认选中第一个模板（按名称排序后是「会议」）
    const select = wrapper.find('select')
    expect((select.element as HTMLSelectElement).value).toBe('会议')
    // 两个自定义变量各一个输入框；已知变量显示中文标签，原始变量名退到 placeholder
    const varLabels = wrapper.findAll('.tcd-vars label').map((w) => w.text())
    expect(varLabels).toContain('项目')
    expect(varLabels).toContain('参与人')
    const varInputs = wrapper.findAll('.tcd-vars input')
    expect((varInputs[0]!.element as HTMLInputElement).placeholder).toBe('project')
    expect(varInputs.length).toBe(2)
    // 目标路径默认为 模板名.md
    const target = wrapper.find('.tcd-target')
    expect((target.element as HTMLInputElement).value).toBe('会议.md')
    expect(wrapper.text()).toContain('内置变量')
  })

  it('填写变量后创建并触发 created 事件', async () => {
    mockedList.mockResolvedValue([{ name: '会议', variables: ['project'], builtin: true }])
    mockedCreate.mockResolvedValue({ name: '周会.md', path: '周会.md' } as never)
    const { wrapper } = mountDialog()
    await flushPromises()

    const varInput = wrapper.find('.tcd-vars input')
    await varInput.setValue('NoteVault')
    const target = wrapper.find('.tcd-target')
    await target.setValue('Daily/周会.md')

    const createBtn = wrapper.findAll('button').find(b => b.text().includes('创建并打开'))
    await createBtn!.trigger('click')
    await flushPromises()

    expect(mockedCreate).toHaveBeenCalledWith('E:\\Notes', '会议', 'Daily/周会.md', { project: 'NoteVault' })
    expect(wrapper.emitted('created')?.[0]).toEqual(['周会.md'])
  })

  it('创建失败时显示错误不关闭', async () => {
    mockedList.mockResolvedValue([{ name: '会议', variables: [], builtin: true }])
    mockedCreate.mockRejectedValue(new Error('文件已存在'))
    const { wrapper } = mountDialog()
    await flushPromises()

    const createBtn = wrapper.findAll('button').find(b => b.text().includes('创建并打开'))
    await createBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('文件已存在')
    expect(wrapper.emitted('created')).toBeUndefined()
  })

  it('列表加载失败时显示错误提示', async () => {
    mockedList.mockRejectedValue(new Error('boom'))
    const { wrapper } = mountDialog()
    await flushPromises()

    expect(wrapper.text()).toContain('模板列表加载失败')
  })

  it('按用途分组并展示说明和推荐目录，创建项目清单时填写项目名', async () => {
    mockedList.mockResolvedValue([
      { name: '项目任务', variables: ['project'], builtin: true, category: '项目推进', description: '记录任务、进度和阻塞', destination: 'Projects/{{project}}/Tasks.md' },
      { name: '面试卡片', variables: ['question'], builtin: true, category: '学习沉淀', description: '整理题面和答案，加入复习', destination: 'Learning/面试宝典/{{title}}.md' },
    ])
    const { wrapper } = mountDialog()
    await flushPromises()

    expect(wrapper.findAll('optgroup').map(group => group.attributes('label'))).toEqual(['项目推进', '学习沉淀'])
    expect(wrapper.text()).toContain('记录任务、进度和阻塞')
    expect(wrapper.text()).toContain('Projects/')
    expect((wrapper.find('.tcd-target').element as HTMLInputElement).value).toBe('Projects/{{project}}/Tasks.md')
    expect(wrapper.find('.tcd-btn-primary').attributes('disabled')).toBeDefined()
    await wrapper.find('.tcd-vars input').setValue('索引优化')
    expect((wrapper.find('.tcd-target').element as HTMLInputElement).value).toBe('Projects/索引优化/Tasks.md')
    expect(wrapper.find('.tcd-btn-primary').attributes('disabled')).toBeUndefined()

    await wrapper.find('select').setValue('面试卡片')
    expect(wrapper.text()).toContain('整理题面和答案，加入复习')
    expect((wrapper.find('.tcd-target').element as HTMLInputElement).value).toBe('Learning/面试宝典/面试卡片.md')
  })

  it('优先在选定文件夹创建，变量变化不会覆盖手写的目标路径', async () => {
    mockedList.mockResolvedValue([
      { name: '项目概览', variables: ['project'], builtin: true, category: '项目推进', description: '明确项目目标和下一步', destination: 'Projects/{{project}}/project.md' },
    ])
    const { wrapper } = mountDialog('Projects/当前项目/')
    await flushPromises()
    const target = wrapper.find('.tcd-target')
    expect((target.element as HTMLInputElement).value).toBe('Projects/当前项目/project.md')
    await target.setValue('Projects/另一个项目/project.md')
    await wrapper.find('.tcd-vars input').setValue('我的项目')
    expect((target.element as HTMLInputElement).value).toBe('Projects/另一个项目/project.md')
  })

  it('工作报告推荐带日期的 Reports 路径，自定义模板保留普通 Markdown 用法', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 8, 9, 12))
    try {
      mockedList.mockResolvedValue([
        { name: '工作报告', variables: [], builtin: true, category: '协作复盘', description: '汇总交付、问题和下一步', destination: 'Daily/Reports/{{date}}-{{title}}.md' },
        { name: '团队自定义', variables: ['team'], builtin: false },
      ])
      const { wrapper } = mountDialog()
      await flushPromises()
      expect((wrapper.find('.tcd-target').element as HTMLInputElement).value).toBe('Daily/Reports/2026-09-09-工作报告.md')
      await wrapper.find('select').setValue('团队自定义')
      expect((wrapper.find('.tcd-target').element as HTMLInputElement).value).toBe('团队自定义.md')
      expect(wrapper.findAll('optgroup').map(group => group.attributes('label'))).toContain('自定义模板')
      expect(wrapper.find('.tcd-vars label').text()).toBe('team')
    } finally {
      vi.useRealTimers()
    }
  })

  it.each([
    ['Projects', 'Projects/{{project}}/project.md', 'Projects/{{project}}/project.md'],
    ['Learning', 'Learning/面试宝典/{{title}}.md', 'Learning/面试宝典/模板.md'],
    ['Daily', 'Daily/Reports/{{title}}.md', 'Daily/Reports/模板.md'],
  ])('从 %s 根目录新建时保留推荐的工作流子目录', async (folder, destination, expected) => {
    mockedList.mockResolvedValue([{ name: '模板', variables: ['project'], builtin: true, destination }])
    const { wrapper } = mountDialog(folder)
    await flushPromises()
    expect((wrapper.find('.tcd-target').element as HTMLInputElement).value).toBe(expected)
  })

  it('加载过程中切换工作区后关闭旧对话框，不能把模板写入新的工作区', async () => {
    let resolveList!: (value: Awaited<ReturnType<typeof TemplateService.ListTemplates>>) => void
    mockedList.mockReturnValue(new Promise(resolve => { resolveList = resolve }) as ReturnType<typeof TemplateService.ListTemplates>)
    const { wrapper, workspaceStore } = mountDialog()
    workspaceStore.setCurrentWorkspace({ id: 'other', name: 'Other', path: 'E:\\Other' } as never)
    resolveList([{ name: '会议', variables: [], builtin: true }])
    await flushPromises()

    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(mockedCreate).not.toHaveBeenCalled()
  })
})
