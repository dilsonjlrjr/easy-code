export const ROUTE_NAMES = {
  home: 'home',
  projects: 'projects',
  pendingItems: 'pending-items',
  agents: 'agents',
  settings: 'settings',
  login: 'login',
} as const

export type RouteName = (typeof ROUTE_NAMES)[keyof typeof ROUTE_NAMES]
