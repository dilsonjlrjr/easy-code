import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { describe, expect, it, vi } from 'vitest'
import App from '@/app/App.vue'
import { registerPlugins } from '@/app/plugins'
import { router } from '@/app/router'
import { setSessionApi } from '@/shared/session/sessionStore'

setSessionApi({
  getCurrentUser: () =>
    Promise.resolve({
      id: 'dev',
      name: 'Dev',
      organization_roles: ['administrator'],
      access_medium: 'web',
    }),
  startDevSession: vi.fn(),
  endSession: vi.fn(),
})

async function mountApp() {
  setActivePinia(createPinia())
  await router.push('/projects')
  await router.isReady()
  const wrapper = mount(App, {
    global: { plugins: [{ install: registerPlugins }] },
    attachTo: document.body,
  })
  await flushPromises()
  return wrapper
}

// The first mount loads Vuetify and a lazy page; on a busy CI host it can pass the 5 s default.
describe('application shell', { timeout: 20_000 }, () => {
  it('renders the banner, the main navigation and the main content landmarks', async () => {
    const wrapper = await mountApp()

    expect(wrapper.find('header').exists()).toBe(true)
    expect(wrapper.find('nav[aria-label="Principal"]').exists()).toBe(true)
    expect(wrapper.find('main#main-content').exists()).toBe(true)
    expect(wrapper.find('h1').text()).toBe('Projetos')
    expect(document.documentElement.lang).toBe('pt-BR')

    wrapper.unmount()
  })

  it('marks the current destination with aria-current', async () => {
    const wrapper = await mountApp()

    const current = wrapper.findAll('nav [aria-current="page"]')
    expect(current).toHaveLength(1)
    expect(current[0]?.text()).toContain('Projetos')

    wrapper.unmount()
  })
})
