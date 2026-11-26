import axe from 'axe-core'
import {
  enableAutoUnmount,
  flushPromises,
  mount,
  type ComponentMountingOptions,
} from '@vue/test-utils'
import { afterEach, expect } from 'vitest'
import type { Component } from 'vue'
import { setBridge, type Bridge } from '@/bridge'
import { createAppI18n, type Locale } from '@/i18n'
import { store } from '@/store'
import type { Status } from '@/types'

// Every mounted view is unmounted after its test, so no view of a previous test keeps listening.
enableAutoUnmount(afterEach)

/** A Go error as Wails delivers it: an Error whose `cause` is app.UIError. */
export function goError(cause: Record<string, unknown>): Error {
  return Object.assign(new Error('call failed'), { cause })
}

/** In-memory replacement of the Wails bridge: records calls and lets tests emit Go events. */
export class FakeBridge implements Bridge {
  calls: { method: string; args: unknown[] }[] = []
  handlers = new Map<string, Set<(data: unknown) => void>>()
  responses: Record<string, (...args: unknown[]) => unknown> = {}

  call<T>(method: string, ...args: unknown[]): Promise<T> {
    this.calls.push({ method, args })
    const response = this.responses[method]
    try {
      return Promise.resolve((response ? response(...args) : undefined) as T)
    } catch (error) {
      return Promise.reject(error instanceof Error ? error : new Error(String(error)))
    }
  }

  on(event: string, handler: (data: unknown) => void): () => void {
    const set = this.handlers.get(event) ?? new Set()
    set.add(handler)
    this.handlers.set(event, set)
    return () => set.delete(handler)
  }

  emit(event: string, data: unknown): void {
    this.handlers.get(event)?.forEach((handler) => {
      handler(data)
    })
  }

  called(method: string): unknown[][] {
    return this.calls.filter((c) => c.method === method).map((c) => c.args)
  }
}

export function installBridge(): FakeBridge {
  const fake = new FakeBridge()
  setBridge(fake)
  store.status = null
  store.view = { name: 'dashboard' }
  store.connection = {}
  store.recent = []
  store.announcement = ''
  store.pendingTotal = null
  store.menuOpen = false
  return fake
}

export async function mountWith<C extends Component>(
  component: C,
  options: ComponentMountingOptions<C> = {},
  locale: Locale = 'pt-BR',
) {
  const wrapper = mount(component, {
    attachTo: document.body,
    ...options,
    global: { plugins: [createAppI18n(locale)] },
  })
  await flushPromises()
  return wrapper
}

/**
 * RNF-19: axe scan (WCAG 2.2 AA rules) with no violation. Color contrast is not computed by jsdom; it is
 * covered by the manual review of the theme tokens.
 */
export async function expectAccessible(element: Element): Promise<void> {
  const result = await axe.run(element, {
    runOnly: {
      type: 'tag',
      values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa', 'best-practice'],
    },
    rules: { 'color-contrast': { enabled: false }, region: { enabled: false } },
  })
  const violations = result.violations.map(
    (v) => `${v.id} (${v.impact ?? ''}): ${v.nodes.map((n) => n.html).join(' | ')}`,
  )
  expect(violations).toEqual([])
}

export const PROJECT_ID = '0193a2b4-0000-7000-8000-000000000001'

export function signedInStatus(overrides: Partial<Status> = {}): Status {
  return {
    phase: 'signed_in',
    server: 'https://easycode.example',
    server_from_env: false,
    locale: 'pt-BR',
    mute_sound: false,
    app_version: '1.0.0',
    server_version: '1.0.0',
    version_mismatch: false,
    user: { id: 'u1', name: 'Ana Souza', organization: { name: 'Mateus' } },
    ...overrides,
  }
}
