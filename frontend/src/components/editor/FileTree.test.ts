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
