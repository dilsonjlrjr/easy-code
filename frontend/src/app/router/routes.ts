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
    path: '/settings',
    name: ROUTE_NAMES.settings,
    component: () => import('@/features/settings/pages/SettingsPage.vue'),
    meta: { titleKey: 'pages.settings.title', layout: 'shell' },
  },
  {
    path: '/login',
    name: ROUTE_NAMES.login,
    component: () => import('@/features/auth/pages/LoginPage.vue'),
    meta: { titleKey: 'pages.login.title', layout: 'blank' },
  },
  { path: '/:unknownPath(.*)*', redirect: { name: ROUTE_NAMES.home } },
]
