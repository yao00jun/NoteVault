import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useNavigationStore } from '@/stores/navigation'
import { contextBreadcrumbs, pageContext } from '@/utils/navigation'
import { useWorkbenchStore } from '@/stores/workbench'

export function usePageContext() {
  const route = useRoute()
  const navigation = useNavigationStore()
  const workbench = useWorkbenchStore()
  const context = computed(() => {
    const visit = navigation.visits[navigation.cursor]
    const context = { ...(visit?.fullPath === route.fullPath ? visit.context : pageContext(route)) }
    const selected = route.path === '/projects' ? workbench.projects.find(item => [item.name, item.path, item.folder].includes(String(route.query.project || '')))
      : route.path === '/learning' ? workbench.books.find(item => [item.name, item.path, item.folder].includes(String(route.query.book || ''))) : undefined
    if (selected) {
      context.folder = selected.folder
      context.entityFolder = selected.folder
      context.entityKind = route.path === '/projects' ? 'project' : 'book'
      context.title = selected.name
    }
    return context
  })
  const breadcrumbs = computed(() => contextBreadcrumbs(context.value, route))
  return { context, breadcrumbs }
}
