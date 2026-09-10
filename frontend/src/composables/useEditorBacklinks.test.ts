import { afterEach, describe, expect, it, vi } from 'vitest'
import { computed, effectScope, ref } from 'vue'
import type { FileNode } from '@/components/editor/FileTree.vue'
import { useEditorBacklinks } from './useEditorBacklinks'

const search = vi.hoisted(() => vi.fn())
vi.mock('@/api', () => ({ SearchService: { Search: search } }))

afterEach(() => vi.clearAllMocks())

describe('useEditorBacklinks', () => {
  it('falls back to the filename for placeholder or empty titles without dropping documents', async () => {
    search.mockResolvedValue([
      { path: 'Templates/笔记.md', title: ' {{title}} ' },
      { path: 'Learning\\Go\\来源.md', title: '{{ topic }}' },
      { path: 'Inbox/空标题.md', title: '  ' },
      { path: 'Inbox/未命名.md', title: null },
      { path: 'Projects/Go/来源.md', title: '正常标题' },
      { path: 'Learning/当前.md', title: '当前文档' },
      null,
    ])
    const scope = effectScope()
    const backlinks = scope.run(() => useEditorBacklinks({
      workspacePath: computed(() => 'C:/vault'),
      activeTabName: computed(() => '当前.md'),
      activeTabPath: computed(() => 'Learning/当前.md'),
      activeTabIndex: ref(0),
      fileTree: ref<FileNode[]>([]),
      openFile: () => undefined,
    }))!
    try {
      await backlinks.loadBacklinks()
      expect(backlinks.backlinks.value).toEqual([
        { path: 'Templates/笔记.md', name: '笔记.md' },
        { path: 'Learning\\Go\\来源.md', name: '来源.md' },
        { path: 'Inbox/空标题.md', name: '空标题.md' },
        { path: 'Inbox/未命名.md', name: '未命名.md' },
        { path: 'Projects/Go/来源.md', name: '正常标题' },
      ])
    } finally {
      scope.stop()
    }
  })
})
