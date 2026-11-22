import {
  mdiAccountGroupOutline,
  mdiBellOutline,
  mdiCogOutline,
  mdiFolderOutline,
  mdiHomeOutline,
} from '@mdi/js'
import { ROUTE_NAMES, type RouteName } from '@/app/router/routeNames'

export interface NavigationItem {
  routeName: RouteName
  labelKey: string
  icon: string
  /** ESP-00: Settings is shown only to the organization administrator. */
  administratorOnly: boolean
}

/** Main navigation destinations, in the order fixed by ESP-00 section 2. */
export const NAVIGATION_ITEMS: readonly NavigationItem[] = [
  {
    routeName: ROUTE_NAMES.home,
    labelKey: 'navigation.home',
    icon: mdiHomeOutline,
    administratorOnly: false,
  },
  {
    routeName: ROUTE_NAMES.projects,
    labelKey: 'navigation.projects',
    icon: mdiFolderOutline,
    administratorOnly: false,
  },
  {
    routeName: ROUTE_NAMES.pendingItems,
    labelKey: 'navigation.pendingItems',
    icon: mdiBellOutline,
    administratorOnly: false,
  },
  {
    routeName: ROUTE_NAMES.agents,
    labelKey: 'navigation.agents',
    icon: mdiAccountGroupOutline,
    administratorOnly: false,
  },
  {
    routeName: ROUTE_NAMES.settings,
    labelKey: 'navigation.settings',
    icon: mdiCogOutline,
    administratorOnly: true,
  },
]

export function visibleNavigationItems(isAdministrator: boolean): NavigationItem[] {
  return NAVIGATION_ITEMS.filter((item) => isAdministrator || !item.administratorOnly)
}
