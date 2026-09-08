import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'
import { isNavigationFailure, NavigationFailureType, type Router } from 'vue-router'
import type { useWorkspaceStore } from './workspace'
import { navigationIdentity, pageContext, sectionFallback, type PageContext } from '@/utils/navigation'

interface Visit { fullPath: string; identity: string; context: PageContext; position?: number }

export const useNavigationStore = defineStore('navigation', () => {
  const visits = ref<Visit[]>([])
  const cursor = ref(-1)
  const context = ref<PageContext>(pageContext({ path: '/', query: {} }))
  const hasWorkspace = ref(false)
  const canGoBack = computed(() => cursor.value > 0)
  const canGoForward = computed(() => cursor.value >= 0 && cursor.value < visits.value.length - 1)
  let replaceNext = false
  let disconnect: (() => void) | undefined

  function connect(router: Router, workspace: ReturnType<typeof useWorkspaceStore>) {
    disconnect?.()
    const history = router.options.history
    let pop: { delta: number } | null = null
    let key = workspace.currentWorkspace?.path || ''
    let generation = 0
    hasWorkspace.value = Boolean(key)
    const position = () => typeof history.state.position === 'number' ? history.state.position : undefined

    function observe(initial = false) {
      const route = router.currentRoute.value
      const at = position()
      const prior = visits.value[cursor.value]
      let target = -1
      if (pop) {
        target = at === undefined ? cursor.value + pop.delta : visits.value.findIndex(item => item.position === at)
      }
      const existing = target >= 0 ? visits.value[target] : undefined
      const origin = existing?.context.section ?? (initial ? undefined : prior?.context.section)
      const next: Visit = { fullPath: route.fullPath, identity: navigationIdentity(route), context: pageContext(route, origin), position: at }
      if (existing) {
        cursor.value = target
        visits.value[target] = next
      } else if (!initial && prior && (replaceNext || (at !== undefined && prior.position === at) || prior.identity === next.identity)) {
        visits.value[cursor.value] = next
      } else {
        visits.value.splice(cursor.value + 1)
        visits.value.push(next)
        cursor.value = visits.value.length - 1
      }
      context.value = next.context
      pop = null
      replaceNext = false
    }
    observe(true)
    const stopHistory = history.listen((_to, _from, info) => { pop = { delta: info.delta } })
    const stopGuard = router.beforeEach(() => {
      if (!pop) return
      const at = position()
      const target = at === undefined ? cursor.value + pop.delta : visits.value.findIndex(item => item.position === at)
      // Browser/mouse back is subject to the same workspace boundary as the toolbar.
      if (target < 0 || target >= visits.value.length) return false
    })
    const stopAfter = router.afterEach((_to, _from, failure) => {
      if (failure) { pop = null; replaceNext = false; return }
      observe()
    })
    const stopWorkspace = watch(() => workspace.currentWorkspace?.path || '', nextKey => {
      const previousKey = key
      key = nextKey
      generation++
      const version = generation
      hasWorkspace.value = Boolean(key)
      visits.value = []
      cursor.value = -1
      pop = null
      context.value = pageContext({ path: hasWorkspace.value ? '/today' : '/', query: {} })
      if (previousKey) {
        replaceNext = true
        const home = hasWorkspace.value ? '/today' : '/'
        void router.replace(home).then(failure => {
          // A workspace picker can supersede this replace with its own push.
          // A cancelled navigation must not register the old page as a visit
          // in the new workspace. A same-home duplicate still needs a seed.
          if (failure && !isNavigationFailure(failure, NavigationFailureType.duplicated)) return
          if (version === generation && !visits.value.length && router.currentRoute.value.fullPath === home) observe(true)
        })
      } else observe(true) // Startup restoration preserves a legitimate deep link.
    }, { flush: 'sync' })
    disconnect = () => { stopWorkspace(); stopHistory(); stopGuard(); stopAfter(); disconnect = undefined }
    return disconnect
  }

  function back(router: Router) {
    if (canGoBack.value) router.back()
    else {
      replaceNext = true
      void router.replace(sectionFallback(router.currentRoute.value, hasWorkspace.value, context.value.section))
    }
  }
  function forward(router: Router) { if (canGoForward.value) router.forward() }
  function home(router: Router) { return router.push(hasWorkspace.value ? '/today' : '/') }

  return { visits, cursor, context, hasWorkspace, canGoBack, canGoForward, connect, back, forward, home }
})
