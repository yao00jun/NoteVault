import { reactive, watch } from 'vue'
import { useRoute } from 'vue-router'

/** Cached pages keep their last route context while a different page is active. */
export function useViewRoute(path: string) {
  const current = useRoute()
  const snapshot = reactive({ ...current, query: { ...current.query } })
  watch(() => current.fullPath, () => {
    if (current.path === path) Object.assign(snapshot, { ...current, query: { ...current.query } })
  }, { flush: 'sync' })
  return snapshot
}
