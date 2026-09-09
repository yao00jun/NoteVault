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

describe('FileTree', () => {
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
