// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { i18n } from '@/i18n'

vi.mock('@bindings/github.com/notevault/notevault/index.js', () => ({
  TemplateService: {
    ListTemplates: vi.fn(async () => []),
    GetTemplateContent: vi.fn(async () => ''),
    CreateFromTemplate: vi.fn(async () => null),
  },
}))

import TemplateCreateDialog from './TemplateCreateDialog.vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { TemplateService } from '@bindings/github.com/notevault/notevault/index.js'

const mockedList = vi.mocked(TemplateService.ListTemplates)
const mockedCreate = vi.mocked(TemplateService.CreateFromTemplate)

enableAutoUnmount(afterEach)

function mountDialog() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const workspaceStore = useWorkspaceStore()
  workspaceStore.setCurrentWorkspace({ id: 'ws', name: 'Vault', path: 'E:\\Notes' } as never)
  const wrapper = mount(TemplateCreateDialog, {
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
      { name: '会议', variables: ['project', 'people'] },
      { name: '读书笔记', variables: [] },
    ])
    const { wrapper } = mountDialog()
    await flushPromises()

    // 默认选中第一个模板（按名称排序后是「会议」）
    const select = wrapper.find('select')
    expect((select.element as HTMLSelectElement).value).toBe('会议')
    // 两个自定义变量各一个输入框
    const varInputs = wrapper.findAll('.tcd-vars input')
    expect(varInputs.length).toBe(2)
    // 目标路径默认为 模板名.md
    const target = wrapper.find('.tcd-target')
    expect((target.element as HTMLInputElement).value).toBe('会议.md')
    expect(wrapper.text()).toContain('内置变量')
  })

  it('填写变量后创建并触发 created 事件', async () => {
    mockedList.mockResolvedValue([{ name: '会议', variables: ['project'] }])
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
    mockedList.mockResolvedValue([{ name: '会议', variables: [] }])
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
})
