import { ROUTE_NAMES, type RouteName } from '@/app/router/routeNames'
import type { CanvasIconName } from '@/shared/ui/canvasIcons'

export interface NavigationItem {
  routeName: RouteName
  labelKey: string
  icon: CanvasIconName
  /** ESP-00: Settings is shown only to the organization administrator. */
  administratorOnly: boolean
  /** The pending-items destination carries the counter (canvas "Menu lateral"). */
  showsPendingCount: boolean
}

/** Main navigation destinations, in the order fixed by ESP-00 section 2. */
export const NAVIGATION_ITEMS: readonly NavigationItem[] = [
  {
    routeName: ROUTE_NAMES.home,
    labelKey: 'navigation.home',
    icon: 'home',
    administratorOnly: false,
    showsPendingCount: false,
  },
  {
    routeName: ROUTE_NAMES.projects,
    labelKey: 'navigation.projects',
    icon: 'folder',
    administratorOnly: false,
    showsPendingCount: false,
  },
  {
    routeName: ROUTE_NAMES.pendingItems,
    labelKey: 'navigation.pendingItems',
    icon: 'inbox',
    administratorOnly: false,
    showsPendingCount: true,
  },
  {
    routeName: ROUTE_NAMES.agents,
    labelKey: 'navigation.agents',
    icon: 'agent',
    administratorOnly: false,
    showsPendingCount: false,
  },
  {
    routeName: ROUTE_NAMES.settings,
    labelKey: 'navigation.settings',
    icon: 'settings',
    administratorOnly: true,
    showsPendingCount: false,
  },
]

export function visibleNavigationItems(isAdministrator: boolean): NavigationItem[] {
  return NAVIGATION_ITEMS.filter((item) => isAdministrator || !item.administratorOnly)
}
