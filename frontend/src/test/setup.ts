import { afterEach } from 'vitest'

// jsdom lacks ResizeObserver, which Vuetify layout components use.
class ResizeObserverStub {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}
if (!('ResizeObserver' in globalThis)) {
  Object.assign(globalThis, { ResizeObserver: ResizeObserverStub })
}

afterEach(() => {
  window.localStorage.clear()
})
