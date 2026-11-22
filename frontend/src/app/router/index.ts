import { createRouter, createWebHistory } from 'vue-router'
import { routes } from './routes'

export type AppLayout = 'shell' | 'blank'

declare module 'vue-router' {
  interface RouteMeta {
    /** i18n key of the page title, used for `document.title`. */
    titleKey?: string
    /** `shell` shows the app bar and the navigation; `blank` shows the page alone (login). */
    layout?: AppLayout
  }
}

export const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
})

export { ROUTE_NAMES } from './routeNames'
