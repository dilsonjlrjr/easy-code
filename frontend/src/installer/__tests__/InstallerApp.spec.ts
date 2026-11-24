import { flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mountWithPlugins } from '@/test/mountWithPlugins'
import InstallerApp from '../InstallerApp.vue'

function stubFetch(installed: boolean) {
  vi.stubGlobal(
    'fetch',
    vi.fn((path: string) =>
      Promise.resolve(
        path === '/api/state'
          ? new Response(JSON.stringify({ installed, version: 'v1' }), { status: 200 })
          : new Response(JSON.stringify({ code: 'invalid_token' }), { status: 401 }),
      ),
    ),
  )
}

describe('InstallerApp', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('opens on the welcome step with "Continue" disabled until the token passes', async () => {
    stubFetch(false)
    const wrapper = mountWithPlugins(InstallerApp)
    await flushPromises()
    expect(wrapper.get('[data-testid="step-title"]').text()).toBe('Boas-vindas')
    expect(wrapper.text()).toContain('Etapa 1 de 10: Boas-vindas')
    expect(wrapper.get('[data-testid="step-next"]').attributes('disabled')).toBeDefined()
  })

  it('shows only the installed message once the platform is installed (CA-31-9)', async () => {
    stubFetch(true)
    const wrapper = mountWithPlugins(InstallerApp)
    await flushPromises()
    expect(wrapper.text()).toContain('A plataforma já está instalada')
    expect(wrapper.find('[data-testid="step-title"]').exists()).toBe(false)
  })
})
