export const ROUTE_NAMES = {
  home: 'home',
  projects: 'projects',
  newProject: 'new-project',
  project: 'project',
  projectWaves: 'project-waves',
  projectArtifacts: 'project-artifacts',
  projectConfiguration: 'project-configuration',
  projectMembers: 'project-members',
  stage: 'stage',
  wave: 'wave',
  gate: 'gate',
  artifact: 'artifact',
  pendingItems: 'pending-items',
  agents: 'agents',
  projectAgents: 'project-agents',
  settings: 'settings',
  login: 'login',
  accessDenied: 'access-denied',
} as const

export type RouteName = (typeof ROUTE_NAMES)[keyof typeof ROUTE_NAMES]

/** Main navigation destinations that also own nested pages (Projects owns every project page). */
export const NAVIGATION_SECTIONS: Partial<Record<RouteName, RouteName>> = {
  [ROUTE_NAMES.newProject]: ROUTE_NAMES.projects,
  [ROUTE_NAMES.project]: ROUTE_NAMES.projects,
  [ROUTE_NAMES.projectWaves]: ROUTE_NAMES.projects,
  [ROUTE_NAMES.projectArtifacts]: ROUTE_NAMES.projects,
  [ROUTE_NAMES.projectConfiguration]: ROUTE_NAMES.projects,
  [ROUTE_NAMES.projectMembers]: ROUTE_NAMES.projects,
  [ROUTE_NAMES.stage]: ROUTE_NAMES.projects,
  [ROUTE_NAMES.wave]: ROUTE_NAMES.projects,
  [ROUTE_NAMES.gate]: ROUTE_NAMES.projects,
  [ROUTE_NAMES.artifact]: ROUTE_NAMES.projects,
  [ROUTE_NAMES.projectAgents]: ROUTE_NAMES.agents,
}

/** Tabs of Settings in MARCO-1 (ESP-23): providers and models, price table. */
export const SETTINGS_TABS = ['providers', 'prices'] as const

/** Why the login page is shown (ER-02-10): it never shows just because there is no session. */
export const LOGIN_REASONS = {
  signedOut: 'signed_out',
  sessionExpired: 'session_expired',
  loginFailed: 'login_failed',
} as const

export type LoginReason = (typeof LOGIN_REASONS)[keyof typeof LOGIN_REASONS]
