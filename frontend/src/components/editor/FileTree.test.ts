// @vitest-environment jsdom
import { describe, it, expect } from 'vitest'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import { afterEach } from 'vitest'
import { nextTick } from 'vue'
import FileTree from './FileTree.vue'
import type { FileNode } from './FileTree.vue'

enableAutoUnmount(afterEach)

const nodes: FileNode[] = [
  {
    name: 'notes',
    path: 'notes',
    fullPath: 'notes',
    isDir: true,
    children: [
      {
        name: 'nested.md',
        path: 'notes/nested.md',
        fullPath: 'notes/nested.md',
        isDir: false,
      },
    ],
  },
]

const learningTree: FileNode[] = [{
  name: 'Learning', path: 'Learning', fullPath: '', isDir: true, children: [{
    name: 'Go', path: 'Learning/Go', fullPath: '', isDir: true, children: [
      { name: 'Chapter.md', path: 'Learning/Go/Chapter.md', fullPath: '', isDir: false },
      { name: 'Next.md', path: 'Learning/Go/Next.md', fullPath: '', isDir: false },
    ],
  }],
}, { name: 'Learning-Archive', path: 'Learning-Archive', fullPath: '', isDir: true, children: [] }]

describe('FileTree', () => {
  it('uses display aliases only for matching directories and preserves paths for opening and renaming', async () => {
    const wrapper = mount(FileTree, { props: {
      nodes: learningTree, activeFilePath: 'Learning/Go/Chapter.md',
      folderDisplayNames: { Learning: '学习书架', 'Learning/Go': 'Go 技术', 'Learning/Go/Chapter.md': '不应用到文件' },
    } })
    expect(wrapper.get('[data-path="Learning"] .node-name').text()).toBe('学习书架')
    expect(wrapper.get('[data-path="Learning/Go"] .node-name').text()).toBe('Go 技术')
    expect(wrapper.get('[data-path="Learning/Go/Chapter.md"] .node-name').text()).toBe('Chapter.md')
    await wrapper.get('[data-path="Learning/Go/Chapter.md"]').trigger('click')
    expect(wrapper.emitted('open-file')?.[0]?.[0]).toMatchObject({ path: 'Learning/Go/Chapter.md' })
    await wrapper.get('[data-path="Learning/Go"]').trigger('contextmenu')
    await wrapper.findAll('.context-menu-item').find(item => item.text() === '重命名')!.trigger('click')
    expect(wrapper.emitted('rename')?.[0]?.[0]).toMatchObject({ name: 'Go', path: 'Learning/Go' })
    await wrapper.setProps({ folderDisplayNames: {} })
    expect(wrapper.get('[data-path="Learning"] .node-name').text()).toBe('Learning')
  })

  it('reveals all ancestors and highlights an active path with Windows separators', () => {
    const wrapper = mount(FileTree, { props: { nodes: learningTree, activeFilePath: 'Learning\\Go\\Chapter.md' } })
    expect(wrapper.find('.tree-node.is-active').exists()).toBe(true)
    expect(wrapper.get('.tree-node.is-active').attributes('data-path')).toBe('Learning/Go/Chapter.md')
    expect(wrapper.findAll('.tree-children')).toHaveLength(2)
  })

  it('reveals a file when its ancestor nodes arrive asynchronously', async () => {
    const wrapper = mount(FileTree, { props: { nodes: [], activeFilePath: 'Learning/Go/Chapter.md' } })
    await wrapper.setProps({ nodes: learningTree })
    expect(wrapper.find('.tree-node.is-active').exists()).toBe(true)
  })

  it.each(['Learning/Go/Chapter.md', 'Learning\\Go\\Chapter.md'])('reveals %s when the file opens after the tree loads', async (activeFilePath) => {
    const wrapper = mount(FileTree, { props: { nodes: [] } })
    await wrapper.setProps({ nodes: structuredClone(learningTree) })
    expect(wrapper.find('.tree-children').exists()).toBe(false)

    await wrapper.setProps({ activeFilePath })
    await wrapper.setProps({ nodes: structuredClone(learningTree) })
    expect(wrapper.get('.tree-node.is-active').attributes('data-path')).toBe('Learning/Go/Chapter.md')
    expect(wrapper.findAll('.tree-children')).toHaveLength(2)
  })

  it('reveals the active file as nested directory levels finish loading', async () => {
    const wrapper = mount(FileTree, { props: { nodes: [] } })
    await wrapper.setProps({ nodes: [{ ...learningTree[0]!, children: [] }] })
    await wrapper.setProps({ activeFilePath: 'Learning/Go/Chapter.md' })
    await wrapper.setProps({ nodes: [{ ...learningTree[0]!, children: [{ ...learningTree[0]!.children![0]!, children: [] }] }] })
    await wrapper.setProps({ nodes: structuredClone(learningTree) })
    expect(wrapper.get('.tree-node.is-active').attributes('data-path')).toBe('Learning/Go/Chapter.md')
    expect(wrapper.findAll('.tree-children')).toHaveLength(2)
  })

  it('resynchronizes ancestors when refreshed tree paths use Windows separators', async () => {
    const wrapper = mount(FileTree, { props: { nodes: learningTree, activeFilePath: 'Learning/Go/Chapter.md' } })
    const refreshedTree = structuredClone(learningTree)
    refreshedTree[0]!.children![0]!.path = 'Learning\\Go'
    for (const child of refreshedTree[0]!.children![0]!.children!) child.path = child.path.replace(/\//g, '\\')

    await wrapper.setProps({ nodes: refreshedTree })
    expect(wrapper.find('.tree-node.is-active').exists()).toBe(true)
    expect(wrapper.get('.tree-node.is-active').attributes('data-path')).toBe('Learning\\Go\\Chapter.md')
    expect(wrapper.findAll('.tree-children')).toHaveLength(2)
  })

  it('preserves manual collapse across tree refreshes and file selection changes', async () => {
    const wrapper = mount(FileTree, { props: { nodes: learningTree, activeFilePath: 'Learning/Go/Chapter.md' } })
    expect(wrapper.find('.tree-node.is-active').exists()).toBe(true)
    await wrapper.get('[data-path="Learning"]').trigger('click')
    await wrapper.setProps({ nodes: [...learningTree] })
    expect(wrapper.find('.tree-children').exists()).toBe(false)

    await wrapper.setProps({ activeFilePath: 'root.md' })
    await wrapper.setProps({ activeFilePath: 'Learning/Go/Chapter.md' })
    expect(wrapper.find('.tree-children').exists()).toBe(false)
    await wrapper.setProps({ activeFilePath: 'Learning/Go/Next.md' })
    expect(wrapper.find('.tree-children').exists()).toBe(false)

    await wrapper.get('[data-path="Learning"]').trigger('click')
    expect(wrapper.get('.tree-node.is-active').attributes('data-path')).toBe('Learning/Go/Next.md')
  })

  it('preserves a nested manual collapse when its parent is closed and reopened', async () => {
    const wrapper = mount(FileTree, { props: { nodes: learningTree, activeFilePath: 'Learning/Go/Chapter.md' } })
    await wrapper.get('[data-path="Learning/Go"]').trigger('click')
    await wrapper.get('[data-path="Learning"]').trigger('click')
    await wrapper.setProps({ activeFilePath: 'root.md' })
    await wrapper.setProps({ activeFilePath: 'Learning/Go/Next.md' })
    await wrapper.get('[data-path="Learning"]').trigger('click')

    expect(wrapper.find('[data-path="Learning/Go"]').exists()).toBe(true)
    expect(wrapper.find('.tree-node.is-active').exists()).toBe(false)
    await wrapper.get('[data-path="Learning/Go"]').trigger('click')
    await wrapper.setProps({ nodes: structuredClone(learningTree) })
    expect(wrapper.get('.tree-node.is-active').attributes('data-path')).toBe('Learning/Go/Next.md')
  })

  it('keeps manual collapse choices local to each root tree', async () => {
    const props = { nodes: learningTree, activeFilePath: 'Learning/Go/Chapter.md' }
    const first = mount(FileTree, { props })
    await first.get('[data-path="Learning/Go"]').trigger('click')
    const second = mount(FileTree, { props })

    expect(first.find('.tree-node.is-active').exists()).toBe(false)
    expect(second.get('.tree-node.is-active').attributes('data-path')).toBe('Learning/Go/Chapter.md')
  })

  it.each([['新建文档', 'new-file'], ['新建文件夹', 'new-folder']])('nested %s keeps its workspace-relative parent path', async (label, event) => {
    const nested: FileNode = {
      name: 'Chapter', path: 'Learning/Go/Chapter', fullPath: '', isDir: true, children: [],
    }
    const wrapper = mount(FileTree, { props: { nodes: [{
      name: 'Learning', path: 'Learning', fullPath: '', isDir: true, children: [{
        name: 'Go', path: 'Learning/Go', fullPath: '', isDir: true, children: [nested],
      }],
    }], focusFolder: 'Learning/Go/Chapter' } })
    await nextTick()
    const row = wrapper.findAll('.tree-node').find(item => item.text() === 'Chapter')!
    await row.trigger('contextmenu', { clientX: 50, clientY: 100 })
    const button = wrapper.findAll('.context-menu-item').find(item => item.text() === label)!
    await button.trigger('click')
    expect(wrapper.emitted(event)).toEqual([['Learning/Go/Chapter']])
    expect(wrapper.find('.context-menu').exists()).toBe(false)
  })

  it('forwards the original nested node for rename', async () => {
    const wrapper = mount(FileTree, { props: { nodes, focusFolder: 'notes' } })
    await nextTick()
    await wrapper.findAll('.tree-node')[1]!.trigger('contextmenu')
    await wrapper.findAll('.context-menu-item').find(item => item.text() === '重命名')!.trigger('click')
    expect(wrapper.emitted('rename')).toEqual([[nodes[0]!.children![0]]])
  })

  it('递归子树中不再渲染根级新建按钮', async () => {
    const wrapper = mount(FileTree, {
      props: { nodes },
    })

    expect(wrapper.findAll('.root-action-btn')).toHaveLength(0)

    await wrapper.find('.tree-node.is-dir').trigger('click')
    await nextTick()
    await nextTick()
    expect(wrapper.findAll('.root-action-btn')).toHaveLength(0)
  })
})
