import { onScopeDispose, watch } from 'vue'
import { useRouter } from 'vue-router'
import { WorkbenchService } from '@/api/workbench'
import { i18n } from '@/i18n'
import { useWorkspaceStore } from '@/stores/workspace'
import { localDateKey } from '@/utils/workbench'
import { useToast } from './useToast'

/** A work log is the saved daily report; an empty draft belongs in the generator. */
export function useWorkLog() {
  const workspace = useWorkspaceStore()
  const router = useRouter()
  const toast = useToast()
  let generation = 0
  let requestID = 0
  let disposed = false

  watch(() => workspace.currentWorkspace?.path, () => { generation++ }, { flush: 'sync' })
  onScopeDispose(() => { disposed = true })

  async function openTodayWorkLog(): Promise<boolean> {
    const workspacePath = workspace.currentWorkspace?.path
    if (!workspacePath) {
      await router.push('/today')
      return false
    }
    const date = localDateKey()
    const session = generation
    const request = ++requestID
    const current = () => !disposed && session === generation && request === requestID
      && workspace.currentWorkspace?.path === workspacePath && localDateKey() === date
    try {
      const content = await WorkbenchService.ReadDailyReport(workspacePath, date)
      if (!current()) return false
      if (!content.trim()) {
        toast.info(i18n.global.t('workbench.log.notGenerated'))
        window.dispatchEvent(new CustomEvent('notevault:daily-report'))
        return false
      }
      const filePath = `Daily/Reports/${date}-日报.md`
      workspace.openFile(filePath)
      await router.push({ path: '/editor', query: { file: filePath } })
      return true
    } catch (cause) {
      if (current()) toast.error(i18n.global.t('workbench.log.readFailed', { msg: cause instanceof Error ? cause.message : String(cause) }))
      return false
    }
  }

  return { openTodayWorkLog }
}
