import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'
import LoginView from '@/views/LoginView.vue'
import type { Status } from '@/types'
import { expectAccessible, goError, installBridge, mountWith, type FakeBridge } from './helpers'

function status(overrides: Partial<Status> = {}): Status {
  return {
    phase: 'signed_out',
    server_from_env: false,
    locale: 'pt-BR',
    mute_sound: false,
    app_version: '1.0.0',
    version_mismatch: false,
    ...overrides,
  }
}

describe('LoginView (US-45, ESP-28)', () => {
  let bridge: FakeBridge
  beforeEach(() => {
    bridge = installBridge()
  })

  it('asks for the server on the first sign-in and stores it', async () => {
    bridge.responses.SetServer = () => status({ server: 'https://easycode.example' })
    const wrapper = await mountWith(LoginView, { props: { status: status() } })
    expect(wrapper.text()).toContain('Você não está autenticado.')
    const input = wrapper.get('input#server')
    expect(wrapper.get('label[for="server"]').text()).toBe('Endereço do servidor')
    await input.setValue('https://easycode.example')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(bridge.called('SetServer')).toEqual([['https://easycode.example']])
    await expectAccessible(wrapper.element)
  })

  it('shows the error of an invalid address', async () => {
    bridge.responses.SetServer = () => {
      throw goError({ kind: 'invalid_server' })
    }
    const wrapper = await mountWith(LoginView, { props: { status: status() } })
    await wrapper.get('input#server').setValue('http://remote.example')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('Endereço inválido')
  })

  it('starts the device flow with a configured server', async () => {
    bridge.responses.SignIn = () => status({ phase: 'awaiting_approval' })
    const wrapper = await mountWith(LoginView, {
      props: { status: status({ server: 'https://easycode.example' }) },
    })
    expect(wrapper.text()).toContain('Servidor: https://easycode.example')
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()
    expect(bridge.called('SignIn')).toHaveLength(1)
  })

  it('shows the code, the address and the time left while waiting (CA-1)', async () => {
    const expiresAt = new Date(Date.now() + 9.5 * 60_000).toISOString()
    const wrapper = await mountWith(LoginView, {
      props: {
        status: status({
          phase: 'awaiting_approval',
          server: 'https://easycode.example',
          device: {
            user_code: 'WXYZ-1234',
            verification_uri: 'https://sso.example/device',
            expires_at: expiresAt,
          },
        }),
      },
    })
    expect(wrapper.text()).toContain(
      'Abra https://sso.example/device e digite o código abaixo. O código expira em 10 min.',
    )
    expect(wrapper.get('output').text()).toBe('WXYZ-1234')
    const buttons = wrapper.findAll('button').map((b) => b.text())
    expect(buttons).toEqual(['Abrir no navegador', 'Cancelar'])
    await wrapper.findAll('button')[0]?.trigger('click')
    expect(bridge.called('OpenVerificationPage')).toHaveLength(1)
    await expectAccessible(wrapper.element)
  })

  it.each([
    ['code_expired', 'O código expirou sem aprovação. Nenhum token foi emitido.', 'Tentar de novo'],
    ['keychain_unavailable', 'Cofre do sistema não disponível; o token não foi gravado.', 'Entrar'],
    ['session_revoked', 'Sessão revogada. Entre de novo.', 'Entrar'],
  ])('explains why it is signed out: %s (CA-2, CA-4, ER-28-1)', async (reason, text, button) => {
    const wrapper = await mountWith(LoginView, {
      props: { status: status({ server: 'https://easycode.example', reason }) },
    })
    expect(wrapper.get('[role="alert"]').text()).toBe(text)
    expect(wrapper.get('button.primary').text()).toBe(button)
  })

  it('shows the server set by EASYCODE_SERVER without asking for it', async () => {
    const wrapper = await mountWith(LoginView, {
      props: { status: status({ server: 'https://env.example', server_from_env: true }) },
    })
    expect(wrapper.find('input#server').exists()).toBe(false)
    expect(wrapper.text()).toContain('EASYCODE_SERVER: https://env.example')
  })

  it('speaks English', async () => {
    const wrapper = await mountWith(LoginView, { props: { status: status() } }, 'en')
    expect(wrapper.text()).toContain('You are not signed in.')
  })
})
