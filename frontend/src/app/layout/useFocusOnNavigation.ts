import { nextTick } from 'vue'
import { useRouter } from 'vue-router'

/**
 * After an in-app navigation, moves focus to the main content so keyboard and screen-reader
 * users start at the new page instead of the link they used (ESP-00 section 8).
 */
export function useFocusOnNavigation(mainContentId: string): void {
  const router = useRouter()
  let isFirstNavigation = true

  router.afterEach(async (to, from) => {
    if (isFirstNavigation || to.path === from.path) {
      isFirstNavigation = false
      return
    }
    await nextTick()
    document.getElementById(mainContentId)?.focus()
  })
}
