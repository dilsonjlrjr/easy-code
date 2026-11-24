import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { pendingItemsApi } from '@/features/pending-items/api/pendingItemsApi'

/**
 * How many items wait for the user (US-48), shown on the menu and the bell. It is read again on
 * every navigation; a failure keeps the last value, since the counter is only a hint.
 */
export function usePendingCount() {
  const count = ref(0)
  const router = useRouter()

  async function refresh(): Promise<void> {
    try {
      count.value = (await pendingItemsApi.list({ limit: 1 })).total
    } catch {
      // The pending-items page itself reports the problem.
    }
  }

  router.afterEach(() => void refresh())
  void refresh()
  return { count }
}
