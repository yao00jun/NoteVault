/**
 * useDailyNote - 今日日记的创建/打开（UI-WORKBENCH-REDESIGN 侧栏「今日日记」直达）
 *
 * 之前 KnowledgeView 内嵌一份实现，WorkbenchWidgets.addTodo 又内嵌一份日期路径拼接；
 * 侧栏也要一键直达后抽到这里统一：日期口径、模板回退、已存在直接打开全部一处维护。
 */
import { useWorkspaceStore } from '@/stores/workspace'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { FileService, TemplateService } from '@/api'
import { useToast } from '@/composables/useToast'

/** 今日日记的约定路径（Obsidian 风格 "Daily/YYYY-MM-DD.md"） */
export function todayNotePath(now = new Date()): string {
  const mm = String(now.getMonth() + 1).padStart(2, '0')
  const dd = String(now.getDate()).padStart(2, '0')
  return `Daily/${now.getFullYear()}-${mm}-${dd}.md`
}

export function useDailyNote() {
  const workspaceStore = useWorkspaceStore()
  const router = useRouter()
  const { t } = useI18n()
  const toast = useToast()

  /** 创建（或打开已存在的）今日日记并跳转编辑器；返回是否成功 */
  async function openTodayNote(): Promise<boolean> {
    const ws = workspaceStore.currentWorkspace
    if (!ws?.path) {
      router.push('/knowledge')
      return false
    }
    const fileName = todayNotePath()
    const dateStr = fileName.slice('Daily/'.length, -'.md'.length)
    try {
      let node: unknown
      try {
        // P2-2：工作区提供 Templates/Daily.md 则优先用模板渲染（支持 {{date}} 等占位符）
        node = await TemplateService.CreateFromTemplate(ws.path, 'Daily', fileName, {})
      } catch {
        // 无 Daily 模板 → 内置默认结构
        node = await FileService.CreateFile(
          ws.path,
          fileName,
          `# ${dateStr}\n\n## 📅 今日计划\n\n- [ ] \n\n## 📝 笔记\n\n## 💭 想法\n\n`,
        )
      }
      if (node) {
        workspaceStore.incrementFileTreeVersion()
        workspaceStore.openFile((node as { path?: string })?.path ?? fileName)
      }
      router.push('/editor')
      return true
    } catch (e) {
      if ((e as Error).message?.includes('exist')) {
        // 文件已存在，直接打开
        workspaceStore.openFile(fileName)
        router.push('/editor')
        return true
      }
      console.error('Failed to create daily note:', e)
      toast.error(t('knowledge.dailyFailed', { msg: (e as Error).message }))
      return false
    }
  }

  return { openTodayNote, todayNotePath }
}
