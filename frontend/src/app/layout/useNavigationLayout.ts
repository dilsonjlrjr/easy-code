import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { NAVIGATION_SECTIONS, type RouteName } from '@/app/router/routeNames'
import { useSessionStore } from '@/shared/session/sessionStore'
import { visibleNavigationItems } from './navigationItems'

/**
 * The destinations the user may see and which one is current. The canvas layout is one side menu
 * that becomes a horizontal strip on narrow screens (ESP-00), so there is no mode to choose.
 */
export function useNavigationLayout() {
  const route = useRoute()
  const session = useSessionStore()
  const items = computed(() => visibleNavigationItems(session.isAdministrator))

  /** Nested pages (a project, a wave) keep their section marked as current. */
  function isCurrent(routeName: RouteName): boolean {
    const current = route.name as RouteName | undefined
    if (current === undefined) return false
    return current === routeName || NAVIGATION_SECTIONS[current] === routeName
  }

  return { items, isCurrent }
}
