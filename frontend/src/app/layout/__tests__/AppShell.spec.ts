import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import App from '@/app/App.vue'
import { registerPlugins } from '@/app/plugins'
import { router } from '@/app/router'

async function mountApp() {
  await router.push('/projects')
  await router.isReady()
  const wrapper = mount(App, {
    global: { plugins: [{ install: registerPlugins }] },
    attachTo: document.body,
  })
  await flushPromises()
  return wrapper
}

describe('application shell', () => {
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
