import { ref, computed } from 'vue'

/**
 * 标签页数据结构
 */
export interface Tab {
  path: string
  name: string
  content: string
  isDirty: boolean
  lastSavedAt: string
}

/**
 * 多标签页管理 composable
 * 纯逻辑，不涉及文件 I/O，便于单元测试
 */
export function useTabs() {
  const tabs = ref<Tab[]>([])
  const activeTabIndex = ref(-1)

  const activeTab = computed(() => {
    if (activeTabIndex.value >= 0 && activeTabIndex.value < tabs.value.length) {
      return tabs.value[activeTabIndex.value]
    }
    return null
  })

  const tabCount = computed(() => tabs.value.length)

  /**
   * 查找文件是否已在标签页中
   */
  function findTabIndex(path: string): number {
    return tabs.value.findIndex((t) => t.path === path)
  }

  /**
   * 添加新标签页
   * 如果文件已在标签页中，切换到该标签页
   * 返回新标签页的索引
   */
  function addTab(tab: Omit<Tab, 'isDirty' | 'lastSavedAt'> & Partial<Pick<Tab, 'isDirty' | 'lastSavedAt'>>): number {
    const existingIndex = findTabIndex(tab.path)
    if (existingIndex >= 0) {
      activeTabIndex.value = existingIndex
      return existingIndex
    }

    const newTab: Tab = {
      path: tab.path,
      name: tab.name,
      content: tab.content,
      isDirty: tab.isDirty ?? false,
      lastSavedAt: tab.lastSavedAt ?? '',
    }
    tabs.value.push(newTab)
    activeTabIndex.value = tabs.value.length - 1
    return activeTabIndex.value
  }

  /**
   * 切换到指定索引的标签页
   */
  function switchToTab(index: number): boolean {
    if (index < 0 || index >= tabs.value.length) return false
    activeTabIndex.value = index
    return true
  }

  /**
   * 关闭指定索引的标签页
   * 返回是否关闭成功
   */
  function closeTab(index: number): boolean {
    if (index < 0 || index >= tabs.value.length) return false

    tabs.value.splice(index, 1)

    if (tabs.value.length === 0) {
      activeTabIndex.value = -1
    } else if (index <= activeTabIndex.value) {
      activeTabIndex.value = Math.max(0, activeTabIndex.value - 1)
    }
    return true
  }

  /**
   * 关闭所有标签页
   */
  function closeAllTabs() {
    tabs.value = []
    activeTabIndex.value = -1
  }

  /**
   * 更新当前激活标签页的内容
   */
  function updateActiveContent(content: string) {
    if (activeTab.value) {
      activeTab.value.content = content
      activeTab.value.isDirty = true
    }
  }

  /**
   * 标记当前激活标签页为已保存
   */
  function markActiveSaved() {
    if (activeTab.value) {
      activeTab.value.isDirty = false
      activeTab.value.lastSavedAt = new Date().toLocaleTimeString()
    }
  }

  /**
   * 获取指定路径的标签页
   */
  function getTabByPath(path: string): Tab | undefined {
    return tabs.value.find((t) => t.path === path)
  }

  return {
    tabs,
    activeTabIndex,
    activeTab,
    tabCount,
    findTabIndex,
    addTab,
    switchToTab,
    closeTab,
    closeAllTabs,
    updateActiveContent,
    markActiveSaved,
    getTabByPath,
  }
}
