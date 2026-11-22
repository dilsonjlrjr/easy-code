export const ROUTE_NAMES = {
  home: 'home',
  projects: 'projects',
  newProject: 'new-project',
  project: 'project',
  projectWaves: 'project-waves',
  projectArtifacts: 'project-artifacts',
  projectConfiguration: 'project-configuration',
  wave: 'wave',
  artifact: 'artifact',
  pendingItems: 'pending-items',
  agents: 'agents',
  settings: 'settings',
  login: 'login',
} as const

export type RouteName = (typeof ROUTE_NAMES)[keyof typeof ROUTE_NAMES]

/** Main navigation destinations that also own nested pages (Projects owns every project page). */
export const NAVIGATION_SECTIONS: Partial<Record<RouteName, RouteName>> = {
  [ROUTE_NAMES.newProject]: ROUTE_NAMES.projects,
  [ROUTE_NAMES.project]: ROUTE_NAMES.projects,
  [ROUTE_NAMES.projectWaves]: ROUTE_NAMES.projects,
  [ROUTE_NAMES.projectArtifacts]: ROUTE_NAMES.projects,
  [ROUTE_NAMES.projectConfiguration]: ROUTE_NAMES.projects,
  [ROUTE_NAMES.wave]: ROUTE_NAMES.projects,
  [ROUTE_NAMES.artifact]: ROUTE_NAMES.projects,
}

/** Tabs of Settings in MARCO-1 (ESP-23): providers and models, price table. */
export const SETTINGS_TABS = ['providers', 'prices'] as const
