import { createRouter, createWebHistory, type Router } from 'vue-router'
import { onApiUnauthorized } from '@/shared/api/apiClient'
import { useSessionStore } from '@/shared/session/sessionStore'
import { ROUTE_NAMES } from './routeNames'
import { routes } from './routes'

export type AppLayout = 'shell' | 'blank'

declare module 'vue-router' {
  interface RouteMeta {
    /** i18n key of the page title, used for `document.title`. */
    titleKey?: string
    /** `shell` shows the app bar and the navigation; `blank` shows the page alone (login). */
    layout?: AppLayout
    /** Open without a session (login). */
    public?: boolean
    /** ESP-00 / ESP-23 ER-23-15: only the organization administrator; others see "no permission". */
    administratorOnly?: boolean
  }
}

/** Without a session every address leads to the login page and back (US-01 CA-1, ESP-00 section 2). */
export function installSessionGuard(target: Router): void {
  target.beforeEach(async (to) => {
    if (to.meta.public) return true
    const session = useSessionStore()
    await session.ensureLoaded()
    if (session.status === 'anonymous') {
      return { name: ROUTE_NAMES.login, query: { return: to.fullPath } }
    }
    return true
  })

  onApiUnauthorized(() => {
    const session = useSessionStore()
    session.markSignedOut()
    const current = target.currentRoute.value
    if (current.meta.public) return
    void target.push({ name: ROUTE_NAMES.login, query: { return: current.fullPath } })
  })
}

export const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
})

installSessionGuard(router)

export { ROUTE_NAMES } from './routeNames'
export { safeReturnPath } from './safeReturnPath'
