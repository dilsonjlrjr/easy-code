import type { RouteRecordRaw } from 'vue-router'
import { ROUTE_NAMES } from './routeNames'

export const routes: RouteRecordRaw[] = [
  {
    path: '/',
    name: ROUTE_NAMES.home,
    component: () => import('@/features/home/pages/HomePage.vue'),
    meta: { titleKey: 'pages.home.title', layout: 'shell' },
  },
  {
    path: '/projects',
    name: ROUTE_NAMES.projects,
    component: () => import('@/features/projects/pages/ProjectsPage.vue'),
    meta: { titleKey: 'pages.projects.title', layout: 'shell' },
  },
  {
    path: '/projects/new',
    name: ROUTE_NAMES.newProject,
    component: () => import('@/features/projects/pages/NewProjectPage.vue'),
    meta: { titleKey: 'pages.newProject.title', layout: 'shell' },
  },
  {
    path: '/projects/:projectId/waves/:waveId',
    name: ROUTE_NAMES.wave,
    component: () => import('@/features/waves/pages/WavePage.vue'),
    meta: { titleKey: 'pages.wave.title', layout: 'shell' },
  },
  {
    path: '/projects/:projectId/gates/:gateId/rounds/:round(\\d+)?',
    name: ROUTE_NAMES.gate,
    component: () => import('@/features/gates/pages/GatePage.vue'),
    meta: { titleKey: 'pages.gate.title', layout: 'shell' },
  },
  {
    path: '/projects/:projectId/artifacts/:artifactId',
    name: ROUTE_NAMES.artifact,
    component: () => import('@/features/artifacts/pages/ArtifactPage.vue'),
    meta: { titleKey: 'pages.artifact.title', layout: 'shell' },
  },
  {
    path: '/projects/:projectId/findings/:findingId',
    name: ROUTE_NAMES.finding,
    component: () => import('@/features/findings/pages/FindingPage.vue'),
    meta: { titleKey: 'pages.finding.title', layout: 'shell' },
  },
  {
    path: '/projects/:projectId/agents/:agent?',
    name: ROUTE_NAMES.projectAgents,
    component: () => import('@/features/agents/pages/ProjectAgentsPage.vue'),
    meta: { titleKey: 'pages.agents.title', layout: 'shell' },
  },
  {
    path: '/projects/:projectId',
    component: () => import('@/features/projects/pages/ProjectPage.vue'),
    meta: { titleKey: 'pages.project.title', layout: 'shell' },
    children: [
      {
        path: '',
        name: ROUTE_NAMES.project,
        component: () => import('@/features/projects/components/ProjectWorkflowTab.vue'),
        meta: { titleKey: 'pages.project.title', layout: 'shell' },
      },
      {
        path: 'waves',
        name: ROUTE_NAMES.projectWaves,
        component: () => import('@/features/projects/components/ProjectWavesTab.vue'),
        meta: { titleKey: 'pages.project.waves', layout: 'shell' },
      },
      {
        path: 'artifacts',
        name: ROUTE_NAMES.projectArtifacts,
        component: () => import('@/features/artifacts/components/ProjectArtifactsTab.vue'),
        meta: { titleKey: 'pages.project.artifacts', layout: 'shell' },
      },
      {
        path: 'configuration',
        name: ROUTE_NAMES.projectConfiguration,
        component: () => import('@/features/projects/components/ProjectConfigurationTab.vue'),
        meta: { titleKey: 'pages.project.configuration', layout: 'shell' },
      },
      {
        path: 'members',
        name: ROUTE_NAMES.projectMembers,
        component: () => import('@/features/members/components/ProjectMembersTab.vue'),
        meta: { titleKey: 'pages.project.members', layout: 'shell' },
      },
      {
        path: 'findings',
        name: ROUTE_NAMES.projectFindings,
        component: () => import('@/features/findings/components/ProjectFindingsTab.vue'),
        meta: { titleKey: 'pages.project.findings', layout: 'shell' },
      },
      {
        path: 'stages/:stageId',
        name: ROUTE_NAMES.stage,
        component: () => import('@/features/projects/components/StageDetail.vue'),
        meta: { titleKey: 'pages.stage.title', layout: 'shell' },
      },
    ],
  },
  {
    path: '/pending',
    name: ROUTE_NAMES.pendingItems,
    component: () => import('@/features/pending-items/pages/PendingItemsPage.vue'),
    meta: { titleKey: 'pages.pendingItems.title', layout: 'shell' },
  },
  {
    path: '/agents',
    name: ROUTE_NAMES.agents,
    component: () => import('@/features/agents/pages/AgentsPage.vue'),
    meta: { titleKey: 'pages.agents.title', layout: 'shell' },
  },
  {
    path: '/settings/:tab(providers|prices)?',
    name: ROUTE_NAMES.settings,
    component: () => import('@/features/settings/pages/SettingsPage.vue'),
    meta: { titleKey: 'pages.settings.title', layout: 'shell', administratorOnly: true },
  },
  {
    path: '/login',
    name: ROUTE_NAMES.login,
    component: () => import('@/features/auth/pages/LoginPage.vue'),
    meta: { titleKey: 'pages.login.title', layout: 'blank', public: true },
  },
  {
    // completeWebLogin sends a disabled account or one without access here (US-01 CA-2).
    path: '/access-denied',
    name: ROUTE_NAMES.accessDenied,
    component: () => import('@/features/auth/pages/LoginPage.vue'),
    props: { denied: true },
    meta: { titleKey: 'pages.login.deniedTitle', layout: 'blank', public: true },
  },
  { path: '/:unknownPath(.*)*', redirect: { name: ROUTE_NAMES.home } },
]
