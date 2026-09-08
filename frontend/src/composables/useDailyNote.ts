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
      router.push('/')
      return false
    }
    const fileName = todayNotePath()
    const dateStr = fileName.slice('Daily/'.length, -'.md'.length)
    try {
      try {
        // P2-2：工作区提供 Templates/Daily.md 则优先用模板渲染（支持 {{date}} 等占位符）
        await TemplateService.CreateFromTemplate(ws.path, 'Daily', fileName, {})
      } catch {
        // 无 Daily 模板 → 内置默认结构
        await FileService.CreateFile(
          ws.path,
          fileName,
          `# ${dateStr}\n\n## 📅 今日计划\n\n- [ ] \n\n## 📝 笔记\n\n## 💭 想法\n\n`,
        )
      }
    } catch (e) {
      // 两个创建通道都失败，绝大多数情况 = 今日日记已存在（FileService 原子创建
      // O_EXCL）。判定不能只看英文 'exist'：NVError 的 Message 是中文
      // 「文件已存在」，Cause 才渲染出 "The file exists."——两边都要兜住。
      // 真正的磁盘故障（权限等）则照常提示并中止。
      const msg = (e as Error).message ?? ''
      if (!/exist|已存在/i.test(msg)) {
        console.error('Failed to create daily note:', e)
        toast.error(t('knowledge.dailyFailed', { msg }))
        return false
      }
    }
    // 无论创建路径走没走通，路径是确定的：直接打开它。必须带 ?file= query：
    // 只 push 路径 + openFile 有一个真 no-op 盲区——用户已停在 /editor 且
    // 活动标签恰好就是今日日记时，activeFile 写同值不触发 watcher、
    // push 同路径是重复导航，点击表现为「无反应」。query.file 的变化
    // 会命中 EditorView 的 route.query.file watcher（openFileByPath 对已开
    // tab 只切换不覆盖草稿，安全）。
    workspaceStore.openFile(fileName)
    workspaceStore.incrementFileTreeVersion()
    router.push({ path: '/editor', query: { file: fileName } })
    return true
  }

  return { openTodayNote, todayNotePath }
}
