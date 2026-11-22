import { ref } from 'vue'

/**
 * Text for a polite live region (`role="status"`): results and live changes are announced without
 * moving the focus (RNF-18, ESP-00 section 8). Setting the same text twice still re-announces it.
 */
export function useAnnouncer() {
  const message = ref('')

  function announce(text: string): void {
    message.value = ''
    queueMicrotask(() => {
      message.value = text
    })
  }

  return { message, announce }
}
