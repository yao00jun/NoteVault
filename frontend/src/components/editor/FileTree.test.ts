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

  it('reveals a newly selected file inside a manually collapsed directory', async () => {
    const wrapper = mount(FileTree, { props: { nodes: learningTree, activeFilePath: 'Learning/Go/Chapter.md' } })
    expect(wrapper.find('.tree-node.is-active').exists()).toBe(true)
    await wrapper.get('[data-path="Learning"]').trigger('click')
    await wrapper.setProps({ nodes: structuredClone(learningTree) })
    expect(wrapper.find('.tree-children').exists()).toBe(false)

    await wrapper.setProps({ activeFilePath: 'Learning/Go/Next.md' })
    expect(wrapper.find('.tree-node.is-active').exists()).toBe(true)
    expect(wrapper.get('.tree-node.is-active').attributes('data-path')).toBe('Learning/Go/Next.md')
    expect(wrapper.findAll('.tree-children')).toHaveLength(2)
  })

  it.each(['Learning/Go/Next.md', 'Learning\\Go\\Next.md'])('reveals every collapsed ancestor when switching to %s', async (activeFilePath) => {
    const wrapper = mount(FileTree, { props: { nodes: learningTree, activeFilePath: 'Learning/Go/Chapter.md' } })
    await wrapper.get('[data-path="Learning/Go"]').trigger('click')
    await wrapper.get('[data-path="Learning"]').trigger('click')

    await wrapper.setProps({ activeFilePath })
    expect(wrapper.find('.tree-node.is-active').exists()).toBe(true)
    expect(wrapper.get('.tree-node.is-active').attributes('data-path')).toBe('Learning/Go/Next.md')
    expect(wrapper.findAll('.tree-children')).toHaveLength(2)
  })

  it('preserves a nested manual collapse when its parent is rebuilt for the same file', async () => {
    const wrapper = mount(FileTree, { props: { nodes: learningTree, activeFilePath: 'Learning/Go/Chapter.md' } })
    await wrapper.get('[data-path="Learning/Go"]').trigger('click')
    await wrapper.get('[data-path="Learning"]').trigger('click')
    await wrapper.setProps({ nodes: structuredClone(learningTree) })
    await wrapper.get('[data-path="Learning"]').trigger('click')

    expect(wrapper.find('[data-path="Learning/Go"]').exists()).toBe(true)
    expect(wrapper.find('.tree-node.is-active').exists()).toBe(false)
    await wrapper.get('[data-path="Learning/Go"]').trigger('click')
    await wrapper.setProps({ nodes: structuredClone(learningTree) })
    expect(wrapper.get('.tree-node.is-active').attributes('data-path')).toBe('Learning/Go/Chapter.md')
  })

  it('preserves manual collapse when refreshed directory separators change back', async () => {
    const wrapper = mount(FileTree, { props: { nodes: learningTree, activeFilePath: 'Learning/Go/Chapter.md' } })
    const refreshedTree = structuredClone(learningTree)
    refreshedTree[0]!.children![0]!.path = 'Learning\\Go'
    for (const child of refreshedTree[0]!.children![0]!.children!) child.path = child.path.replace(/\//g, '\\')

    await wrapper.setProps({ nodes: refreshedTree })
    const directory = wrapper.findAll('.tree-node').find(node => node.attributes('data-path') === 'Learning\\Go')!
    await directory.trigger('click')
    expect(wrapper.find('.tree-node.is-active').exists()).toBe(false)

    await wrapper.setProps({ nodes: structuredClone(learningTree) })
    expect(wrapper.find('.tree-node.is-active').exists()).toBe(false)
    expect(wrapper.findAll('.tree-children')).toHaveLength(1)
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

  it('sinks system directories like assets and templates into the system section and auto-expands when active', async () => {
    const mixedNodes: FileNode[] = [
      { name: 'Learning', path: 'Learning', fullPath: '', isDir: true, children: [] },
      { name: 'assets', path: 'assets', fullPath: '', isDir: true, children: [
        { name: 'logo.png', path: 'assets/logo.png', fullPath: '', isDir: false },
      ] },
      { name: 'Templates', path: 'Templates', fullPath: '', isDir: true, children: [] },
    ]
    const wrapper = mount(FileTree, { props: { nodes: mixedNodes, activeFilePath: null } })
    // Main tree contains Learning, but not assets or Templates directly in primary list
    const primaryNodes = wrapper.findAll('.tree-nodes > .tree-node')
    expect(primaryNodes).toHaveLength(1)
    expect(primaryNodes[0]!.attributes('data-path')).toBe('Learning')

    // System section exists and shows count
    const systemHeader = wrapper.get('.system-header-node')
    expect(systemHeader.text()).toContain('系统与模板')
    expect(systemHeader.text()).toContain('2')
    expect(wrapper.find('.system-children').exists()).toBe(false)

    // Expand system section
    await systemHeader.trigger('click')
    expect(wrapper.find('.system-children').exists()).toBe(true)

    // Auto-expands when active file is inside assets
    const activeWrapper = mount(FileTree, { props: { nodes: mixedNodes, activeFilePath: 'assets/logo.png' } })
    expect(activeWrapper.find('.system-children').exists()).toBe(true)
  })

  it('filters file tree nodes based on search query', async () => {
    const wrapper = mount(FileTree, { props: { nodes: learningTree } })
    const searchInput = wrapper.get('.tree-search-input')
    await searchInput.setValue('Chapter')
    await nextTick()

    // Chapter is shown, Next is not in filtered tree
    expect(wrapper.find('[data-path="Learning/Go/Chapter.md"]').exists()).toBe(true)
    expect(wrapper.find('[data-path="Learning/Go/Next.md"]').exists()).toBe(false)

    // Clearing search restores all
    await wrapper.get('.search-clear-btn').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-path="Learning-Archive"]').exists()).toBe(true)
  })

  it('collapses all directories on collapse-all click', async () => {
    const wrapper = mount(FileTree, { props: { nodes: learningTree, activeFilePath: 'Learning/Go/Chapter.md' } })
    expect(wrapper.findAll('.tree-children').length).toBeGreaterThan(0)

    await wrapper.get('.tree-action-btn[title="全部折叠"]').trigger('click')
    await nextTick()
    expect(wrapper.findAll('.tree-children')).toHaveLength(0)
  })
})

