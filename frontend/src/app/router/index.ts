import { createRouter, createWebHistory, type Router } from 'vue-router'
import { onApiUnauthorized } from '@/shared/api/apiClient'
import { problemSlug } from '@/shared/api/problemDetails'
import { redirectToSso } from '@/shared/session/sessionApi'
import { useSessionStore } from '@/shared/session/sessionStore'
import { LOGIN_REASONS, ROUTE_NAMES } from './routeNames'
import { routes } from './routes'
import { safeReturnPath } from './safeReturnPath'

export type AppLayout = 'shell' | 'blank'

declare module 'vue-router' {
  interface RouteMeta {
    /** i18n key of the page title, used for `document.title`. */
    titleKey?: string
    /** `shell` shows the app bar and the navigation; `blank` shows the page alone (login). */
    layout?: AppLayout
    /** Open without a session (login, access denied). */
    public?: boolean
    /** ESP-00 / ESP-23 ER-23-15: only the organization administrator; others see "no permission". */
    administratorOnly?: boolean
  }
}

/** The step-up of a critical decision is handled by the decision screen, not as a lost session. */
const REAUTHENTICATION_REQUIRED = 'reauthentication-required'
const SESSION_EXPIRED = 'session-expired'

/**
 * Without a session every address goes straight to the SSO and back (US-01 CA-1, ER-02-10); the
 * login page shows only after signing out, an expired session, denied access or a failed SSO return.
 */
export function installSessionGuard(target: Router): void {
  target.beforeEach(async (to) => {
    const session = useSessionStore()
    if (to.name === ROUTE_NAMES.login) {
      // ESP-02 section 2: with an active session the login page leads to the return path.
      await session.ensureLoaded()
      return session.status === 'authenticated' ? safeReturnPath(to.query.return) : true
    }
    if (to.meta.public) return true
    await session.ensureLoaded()
    if (session.status === 'authenticated') return true
    if (session.anonymousReason === SESSION_EXPIRED) {
      return {
        name: ROUTE_NAMES.login,
        query: { reason: LOGIN_REASONS.sessionExpired, return: to.fullPath },
      }
    }
    redirectToSso(to.fullPath)
    return false
  })

  onApiUnauthorized((problem) => {
    const slug = problemSlug(problem)
    if (slug === REAUTHENTICATION_REQUIRED) return
    const session = useSessionStore()
    const wasAuthenticated = session.status === 'authenticated'
    session.markSignedOut(slug)
    // The first read of the session is the guard's business; this is a session lost while in use.
    if (!wasAuthenticated) return
    const current = target.currentRoute.value
    if (current.meta.public) return
    if (slug === SESSION_EXPIRED) {
      // ER-02-5 / CA-02-6: the login page says why, and the return path is kept.
      void target.push({
        name: ROUTE_NAMES.login,
        query: { reason: LOGIN_REASONS.sessionExpired, return: current.fullPath },
      })
      return
    }
    redirectToSso(current.fullPath)
  })
}

export const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
})

installSessionGuard(router)

export { LOGIN_REASONS, ROUTE_NAMES } from './routeNames'
export { safeReturnPath } from './safeReturnPath'
