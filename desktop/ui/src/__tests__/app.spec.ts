import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'
import App from '@/App.vue'
import { EVENTS } from '@/bridge'
import { store } from '@/store'
import AccountView from '@/views/AccountView.vue'
import GateView from '@/views/GateView.vue'
import ProjectView from '@/views/ProjectView.vue'
import {
  expectAccessible,
  installBridge,
  mountWith,
  PROJECT_ID,
  signedInStatus,
  type FakeBridge,
} from './helpers'

describe('App shell (RNF-19)', () => {
  let bridge: FakeBridge
  beforeEach(() => {
    bridge = installBridge()
    bridge.responses.Projects = () => ({ items: [], next_cursor: null })
    bridge.responses.PendingItems = () => ({ items: [], total: 0 })
  })

  it('shows the sign-in when signed out and the dashboard when signed in', async () => {
    bridge.responses.Status = () => ({ ...signedInStatus(), phase: 'signed_out', user: undefined })
    const wrapper = await mountWith(App)
    expect(wrapper.get('h1').text()).toBe('Entrar no Easy Code')
    expect(wrapper.find('nav').exists()).toBe(false)
    bridge.emit(EVENTS.session, signedInStatus())
    await flushPromises()
    expect(wrapper.get('h1').text()).toBe('Painel')
    expect(document.activeElement?.tagName).toBe('H1')
    expect(wrapper.get('nav').attributes('aria-label')).toBe('Navegação principal')
    await expectAccessible(document.body)
    wrapper.unmount()
  })

  it('has a skip link, moves the focus to the new view heading and shows the connection in text', async () => {
    bridge.responses.Status = () => signedInStatus()
    const wrapper = await mountWith(App)
    expect(wrapper.get('a.skip-link').attributes('href')).toBe('#main')
    bridge.emit(EVENTS.connection, { stream: 'notices', state: 'reconnecting', reconnected: false })
    await flushPromises()
    expect(wrapper.get('.connection').text()).toBe('Avisos: reconectando')
    await wrapper.findAll('nav button')[1]?.trigger('click')
    await flushPromises()
    expect(document.activeElement?.textContent).toBe('Conta')
    expect(wrapper.findAll('nav button')[1]?.attributes('aria-current')).toBe('page')
    wrapper.unmount()
  })

  it('warns about a different server version with both versions', async () => {
    bridge.responses.Status = () =>
      signedInStatus({ version_mismatch: true, server_version: '1.1.0' })
    const wrapper = await mountWith(App)
    expect(wrapper.text()).toContain(
      'Versão do app (1.0.0) diferente da versão do servidor (1.1.0).',
    )
    wrapper.unmount()
  })

  it('switches the interface to English', async () => {
    bridge.responses.Status = () => signedInStatus({ locale: 'en' })
    const wrapper = await mountWith(App)
    expect(wrapper.get('h1').text()).toBe('Dashboard')
    expect(document.documentElement.lang).toBe('en')
    wrapper.unmount()
  })

  it('a click on a finished-wave notification opens the wave view', async () => {
    bridge.responses.Status = () => signedInStatus()
    bridge.responses.OpenWave = () => new Promise(() => undefined)
    const wrapper = await mountWith(App)
    bridge.emit(EVENTS.navigate, { kind: 'wave', project_id: PROJECT_ID, wave_id: 'REQ-01' })
    await flushPromises()
    expect(store.view).toEqual({ name: 'wave', projectId: PROJECT_ID, waveId: 'REQ-01' })
    expect(wrapper.get('h1').text()).toBe('Onda REQ-01')
    wrapper.unmount()
  })
})

describe('ProjectView, GateView and AccountView', () => {
  let bridge: FakeBridge
  beforeEach(() => {
    bridge = installBridge()
  })

  it('project: stages and waves in text, gate summary reachable', async () => {
    bridge.responses.Project = () => ({
      project: { id: PROJECT_ID, name: 'Portal de compras', state: 'in_progress' },
      workflow: {
        stages: [
          {
            id: 'requirements',
            name: 'Discovery e Requisitos',
            short_name: 'Requisitos',
            state: 'awaiting_gate',
            gate: 'gate.requirements',
            current_round: 2,
            gate_state: 'awaiting_human',
          },
          { id: 'architecture', name: 'Arquitetura', state: null },
        ],
      },
      waves: {
        items: [
          {
            id: 'REQ-01',
            stage: 'requirements',
            agent: 'requirements-agent',
            state: 'completed',
            purpose: 'production',
            attempt: 1,
            created_at: '',
          },
        ],
      },
    })
    const wrapper = await mountWith(ProjectView, { props: { projectId: PROJECT_ID } })
    expect(wrapper.text()).toContain('Requisitos: Aguardando gate')
    expect(wrapper.text()).toContain('Gate gate.requirements, rodada 2: Aguardando aprovação')
    expect(wrapper.text()).toContain('Arquitetura: Não iniciada')
    expect(wrapper.text()).toContain('REQ-01 · Agente de Requisitos · Concluída')
    await expectAccessible(wrapper.element)
    await wrapper
      .findAll('button')
      .find((b) => b.text().startsWith('Ver resumo'))
      ?.trigger('click')
    expect(store.view).toEqual({
      name: 'gate',
      projectId: PROJECT_ID,
      gateId: 'gate.requirements',
      round: 2,
    })
  })

  it('gate: summary only, the decision opens the web', async () => {
    bridge.responses.GateRound = () => ({
      gate: 'gate.requirements',
      stage: 'requirements',
      round: 2,
      state: 'awaiting_human',
      checks: [{ check: 'artifact.schema_valid', name: 'Esquema válido', status: 'passed' }],
      blocking_findings: [],
    })
    const wrapper = await mountWith(GateView, {
      props: { projectId: PROJECT_ID, gateId: 'gate.requirements', round: 2 },
    })
    expect(wrapper.text()).toContain('Estado: Aguardando aprovação')
    expect(wrapper.text()).toContain('Esquema válido: passou')
    const decide = wrapper.findAll('button').find((b) => b.text() === 'Abrir na web para decidir')
    await decide?.trigger('click')
    expect(bridge.called('OpenWeb')).toEqual([
      [{ kind: 'gate_round', project_id: PROJECT_ID, gate_id: 'gate.requirements', round: 2 }],
    ])
    expect(wrapper.findAll('button').some((b) => /aprovar|reprovar/i.test(b.text()))).toBe(false)
    await expectAccessible(wrapper.element)
  })

  it('account: sound preference, sessions on the web and sign out', async () => {
    bridge.responses.SetMuteSound = (mute) => signedInStatus({ mute_sound: mute === true })
    bridge.responses.SignOut = () => ({ ...signedInStatus(), phase: 'signed_out' })
    const wrapper = await mountWith(AccountView, { props: { status: signedInStatus() } })
    expect(wrapper.get('dl').text()).toContain('Ana Souza')
    expect(wrapper.get('dl').text()).toContain('Mateus')
    await wrapper.get('input[type="checkbox"]').setValue(false)
    expect(bridge.called('SetMuteSound')).toEqual([[true]])
    await wrapper
      .findAll('button')
      .find((b) => b.text().startsWith('Ver e revogar'))
      ?.trigger('click')
    expect(bridge.called('OpenWeb')).toEqual([[{ kind: 'account_local_client' }]])
    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Sair')
      ?.trigger('click')
    await flushPromises()
    expect(store.status?.phase).toBe('signed_out')
    await expectAccessible(wrapper.element)
  })
})
