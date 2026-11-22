import { ref } from 'vue'

/** Loading and load-error state of a screen block (ESP-00 section 6: loading, load error, retry). */
export function useLoader(load: () => Promise<void>) {
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref<unknown>()

  async function run(): Promise<void> {
    loading.value = true
    error.value = undefined
    try {
      await load()
      loaded.value = true
    } catch (caught) {
      error.value = caught
    } finally {
      loading.value = false
    }
  }

  return { loading, loaded, error, run }
}
