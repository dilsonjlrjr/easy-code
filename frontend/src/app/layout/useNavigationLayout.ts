import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useDisplay } from 'vuetify'
import type { RouteName } from '@/app/router/routeNames'
import { visibleNavigationItems } from './navigationItems'

export type NavigationMode = 'drawer' | 'rail' | 'bottom-bar'

/**
 * Chooses how the main navigation is shown for the current width:
 * desktop (lg and up) a full drawer, tablet (sm, md) a rail that expands, phone (xs) a bottom bar
 * (ESP-00: on narrow screens the menu becomes a horizontal strip).
 */
export function useNavigationLayout() {
  const display = useDisplay()
  const route = useRoute()
  const railExpanded = ref(false)

  const mode = computed<NavigationMode>(() => {
    if (display.xs.value) return 'bottom-bar'
    if (display.lgAndUp.value) return 'drawer'
    return 'rail'
  })

  // Until authentication exists every destination is shown; the role filter is already in place.
  const items = computed(() => visibleNavigationItems(true))

  function isCurrent(routeName: RouteName): boolean {
    return route.name === routeName
  }

  function toggleRail(): void {
    railExpanded.value = !railExpanded.value
  }

  return { mode, items, railExpanded, isCurrent, toggleRail }
}
