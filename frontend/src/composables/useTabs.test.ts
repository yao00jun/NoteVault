import { describe, it, expect, beforeEach } from 'vitest'
import { useTabs } from './useTabs'

describe('useTabs', () => {
  let tabs: ReturnType<typeof useTabs>

  beforeEach(() => {
    tabs = useTabs()
  })

  it('初始状态应为空', () => {
    expect(tabs.tabCount.value).toBe(0)
    expect(tabs.activeTabIndex.value).toBe(-1)
    expect(tabs.activeTab.value).toBeNull()
  })

  it('addTab 应添加新标签页并激活', () => {
    const index = tabs.addTab({
      path: 'doc1.md',
      name: 'doc1.md',
      content: '# Doc1',
    })

    expect(index).toBe(0)
    expect(tabs.tabCount.value).toBe(1)
    expect(tabs.activeTabIndex.value).toBe(0)
    expect(tabs.activeTab.value?.path).toBe('doc1.md')
    expect(tabs.activeTab.value?.name).toBe('doc1.md')
    expect(tabs.activeTab.value?.content).toBe('# Doc1')
    expect(tabs.activeTab.value?.isDirty).toBe(false)
  })

  it('addTab 添加已存在的标签页应切换到该标签页而非重复添加', () => {
    tabs.addTab({ path: 'doc1.md', name: 'doc1.md', content: '# Doc1' })
    tabs.addTab({ path: 'doc2.md', name: 'doc2.md', content: '# Doc2' })

    // 再次添加 doc1，应切换到索引 0
    const index = tabs.addTab({ path: 'doc1.md', name: 'doc1.md', content: '# Updated' })

    expect(index).toBe(0)
    expect(tabs.tabCount.value).toBe(2) // 不应重复添加
    expect(tabs.activeTabIndex.value).toBe(0)
  })

  it('switchToTab 应切换到有效索引', () => {
    tabs.addTab({ path: 'doc1.md', name: 'doc1.md', content: '# Doc1' })
    tabs.addTab({ path: 'doc2.md', name: 'doc2.md', content: '# Doc2' })

    const result = tabs.switchToTab(0)
    expect(result).toBe(true)
    expect(tabs.activeTabIndex.value).toBe(0)
    expect(tabs.activeTab.value?.path).toBe('doc1.md')
  })

  it('switchToTab 切换到无效索引应返回 false', () => {
    tabs.addTab({ path: 'doc1.md', name: 'doc1.md', content: '# Doc1' })

    expect(tabs.switchToTab(-1)).toBe(false)
    expect(tabs.switchToTab(5)).toBe(false)
    expect(tabs.activeTabIndex.value).toBe(0) // 不应改变
  })

  it('closeTab 应关闭指定索引的标签页', () => {
    tabs.addTab({ path: 'doc1.md', name: 'doc1.md', content: '# Doc1' })
    tabs.addTab({ path: 'doc2.md', name: 'doc2.md', content: '# Doc2' })

    const result = tabs.closeTab(0)
    expect(result).toBe(true)
    expect(tabs.tabCount.value).toBe(1)
    expect(tabs.activeTab.value?.path).toBe('doc2.md')
  })

  it('closeTab 关闭当前激活的标签页后应切换到前一个标签页', () => {
    tabs.addTab({ path: 'doc1.md', name: 'doc1.md', content: '# Doc1' })
    tabs.addTab({ path: 'doc2.md', name: 'doc2.md', content: '# Doc2' })
    tabs.addTab({ path: 'doc3.md', name: 'doc3.md', content: '# Doc3' })

    // 当前激活的是 doc3（索引 2），关闭它
    tabs.closeTab(2)
    expect(tabs.activeTabIndex.value).toBe(1)
    expect(tabs.activeTab.value?.path).toBe('doc2.md')
  })

  it('closeTab 关闭第一个标签页后应调整激活索引', () => {
    tabs.addTab({ path: 'doc1.md', name: 'doc1.md', content: '# Doc1' })
    tabs.addTab({ path: 'doc2.md', name: 'doc2.md', content: '# Doc2' })
    tabs.switchToTab(1) // 激活 doc2

    // 关闭 doc1（索引 0），当前激活的是索引 1
    tabs.closeTab(0)
    expect(tabs.activeTabIndex.value).toBe(0) // 索引减 1
    expect(tabs.activeTab.value?.path).toBe('doc2.md')
  })

  it('closeTab 关闭最后一个标签页后 activeTabIndex 应为 -1', () => {
    tabs.addTab({ path: 'doc1.md', name: 'doc1.md', content: '# Doc1' })
    tabs.closeTab(0)

    expect(tabs.tabCount.value).toBe(0)
    expect(tabs.activeTabIndex.value).toBe(-1)
    expect(tabs.activeTab.value).toBeNull()
  })

  it('closeTab 关闭无效索引应返回 false', () => {
    tabs.addTab({ path: 'doc1.md', name: 'doc1.md', content: '# Doc1' })

    expect(tabs.closeTab(-1)).toBe(false)
    expect(tabs.closeTab(5)).toBe(false)
    expect(tabs.tabCount.value).toBe(1)
  })

  it('closeAllTabs 应关闭所有标签页', () => {
    tabs.addTab({ path: 'doc1.md', name: 'doc1.md', content: '# Doc1' })
    tabs.addTab({ path: 'doc2.md', name: 'doc2.md', content: '# Doc2' })

    tabs.closeAllTabs()
    expect(tabs.tabCount.value).toBe(0)
    expect(tabs.activeTabIndex.value).toBe(-1)
    expect(tabs.activeTab.value).toBeNull()
  })

  it('updateActiveContent 应更新当前标签页内容并标记为 dirty', () => {
    tabs.addTab({ path: 'doc1.md', name: 'doc1.md', content: '# Original' })
    tabs.updateActiveContent('# Updated')

    expect(tabs.activeTab.value?.content).toBe('# Updated')
    expect(tabs.activeTab.value?.isDirty).toBe(true)
  })

  it('updateActiveContent 在没有激活标签页时不应报错', () => {
    expect(() => tabs.updateActiveContent('test')).not.toThrow()
  })

  it('markActiveSaved 应标记当前标签页为已保存', () => {
    tabs.addTab({ path: 'doc1.md', name: 'doc1.md', content: '# Doc1' })
    tabs.updateActiveContent('# Updated') // 标记为 dirty
    expect(tabs.activeTab.value?.isDirty).toBe(true)

    tabs.markActiveSaved()
    expect(tabs.activeTab.value?.isDirty).toBe(false)
    expect(tabs.activeTab.value?.lastSavedAt).not.toBe('')
  })

  it('getTabByPath 应根据路径获取标签页', () => {
    tabs.addTab({ path: 'docs/doc1.md', name: 'doc1.md', content: '# Doc1' })

    const tab = tabs.getTabByPath('docs/doc1.md')
    expect(tab).toBeDefined()
    expect(tab?.name).toBe('doc1.md')

    const notFound = tabs.getTabByPath('nonexistent.md')
    expect(notFound).toBeUndefined()
  })

  it('findTabIndex 应返回标签页索引', () => {
    tabs.addTab({ path: 'doc1.md', name: 'doc1.md', content: '# Doc1' })
    tabs.addTab({ path: 'doc2.md', name: 'doc2.md', content: '# Doc2' })

    expect(tabs.findTabIndex('doc1.md')).toBe(0)
    expect(tabs.findTabIndex('doc2.md')).toBe(1)
    expect(tabs.findTabIndex('nonexistent.md')).toBe(-1)
  })

  it('多个标签页切换时内容应保持独立', () => {
    tabs.addTab({ path: 'doc1.md', name: 'doc1.md', content: '# Doc1 Content' })
    tabs.addTab({ path: 'doc2.md', name: 'doc2.md', content: '# Doc2 Content' })

    // 切换到 doc1，内容应为 doc1 的
    tabs.switchToTab(0)
    expect(tabs.activeTab.value?.content).toBe('# Doc1 Content')

    // 切换到 doc2，内容应为 doc2 的
    tabs.switchToTab(1)
    expect(tabs.activeTab.value?.content).toBe('# Doc2 Content')
  })
})
