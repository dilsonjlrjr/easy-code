import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { EVENTS } from '@/bridge'
import WaveView from '@/views/WaveView.vue'
import { expectAccessible, installBridge, mountWith, PROJECT_ID, type FakeBridge } from './helpers'

function snapshot(state = 'running') {
  return {
    project: { id: PROJECT_ID, name: 'Portal de compras', state: 'in_progress', last_sequence: 41 },
    wave: {
      id: 'REQ-01',
      stage: 'requirements',
      agent: 'requirements-agent',
      state,
      purpose: 'production',
      attempt: 1,
      created_at: '2026-11-23T10:00:00Z',
      models: {
        reasoning: { provider: 'openai', model: 'gpt-x' },
        execution: { provider: 'anthropic', model: 'claude-y' },
      },
      cost: { total: { value: '0.50', currency: 'BRL' } },
    },
    calls: {
      items: [
        {
          number: 1,
          type: 'model',
          start: '2026-11-23T10:00:01Z',
          duration_ms: 1200,
          result: 'success',
          model: 'gpt-x',
          summary: 'Leu a necessidade',
        },
        {
          number: 2,
          type: 'tool',
          start: '2026-11-23T10:00:03Z',
          duration_ms: 40,
          result: 'refused',
          tool: 'shell',
          summary: null,
        },
      ],
    },
  }
}

const props = { projectId: PROJECT_ID, waveId: 'REQ-01' }

describe('WaveView (US-46 CA-2, CA-4, CA-5, ESP-28)', () => {
  let bridge: FakeBridge
  beforeEach(() => {
    bridge = installBridge()
    bridge.responses.OpenWave = () => snapshot()
    bridge.responses.WaveSnapshot = () => snapshot('paused')
    bridge.responses.Following = () => false
  })

  it('shows state, agent, models, cost and the call log in text', async () => {
    const wrapper = await mountWith(WaveView, { props })
    expect(bridge.called('OpenWave')).toEqual([[PROJECT_ID, 'REQ-01']])
    const facts = wrapper.get('dl').text()
    expect(facts).toContain('Em execução')
    expect(facts).toContain('Agente de Requisitos')
    expect(facts).toContain('Raciocínio: openai / gpt-x')
    expect(facts).toContain('Execução: anthropic / claude-y')
    expect(facts).toMatch(/R\$\s?0,50/)
    const rows = wrapper.findAll('tbody tr').map((r) => r.text())
    expect(rows[0]).toContain('Leu a necessidade')
    expect(rows[1]).toContain('Ferramenta')
    expect(rows[1]).toContain('Recusada')
    await expectAccessible(wrapper.element)
  })

  it('offers no control: only "open on the web" and the follow preference', async () => {
    const wrapper = await mountWith(WaveView, { props })
    const buttons = wrapper.findAll('button').map((b) => b.text())
    expect(buttons).toEqual([
      'Início',
      'Portal de compras',
      'Abrir na web para pausar, retomar, cancelar ou responder',
    ])
    await wrapper.findAll('button')[2]?.trigger('click')
    expect(bridge.called('OpenWeb')).toEqual([
      [{ kind: 'wave', project_id: PROJECT_ID, wave_id: 'REQ-01' }],
    ])
    await wrapper.get('input[type="checkbox"]').setValue(true)
    expect(bridge.called('Follow')).toEqual([[PROJECT_ID, 'REQ-01', true]])
  })

  it('applies live events once, reads the wave again and announces the new state', async () => {
    vi.useFakeTimers()
    const wrapper = await mountWith(WaveView, { props })
    const event = {
      id: 'ev-1',
      type: 'br.easycode.wave.pause_requested.v1',
      data: { transition: { machine: 'wave', from_state: 'running', to_state: 'pausing' } },
    }
    bridge.emit(EVENTS.wave, event)
    bridge.emit(EVENTS.wave, event)
    bridge.emit(EVENTS.wave, {
      id: 't-1',
      type: 'br.easycode.agent.progress_reported.v1',
      data: { step: 'writing US-01' },
    })
    await flushPromises()
    expect(wrapper.get('dl').text()).toContain('Pausando (pausa no próximo checkpoint)')
    const log = wrapper.findAll('[role="log"] li').map((li) => li.text())
    expect(log).toEqual(['Passo: writing US-01', 'Atualização: wave.pause_requested'])
    vi.advanceTimersByTime(400)
    await flushPromises()
    expect(bridge.called('WaveSnapshot')).toHaveLength(1)
    expect(wrapper.get('dl').text()).toContain('Pausada (pausada por uma pessoa)')
    vi.useRealTimers()
  })

  it('reloads from the server after a gap or a reconnection (CA-5)', async () => {
    const wrapper = await mountWith(WaveView, { props })
    bridge.emit(EVENTS.gap, { stream: 'wave' })
    bridge.emit(EVENTS.connection, { stream: 'wave', state: 'live', reconnected: true })
    bridge.emit(EVENTS.gap, { stream: 'notices' })
    await flushPromises()
    expect(bridge.called('WaveSnapshot')).toHaveLength(2)
    wrapper.unmount()
    expect(bridge.called('CloseWave')).toHaveLength(1)
  })

  it('tells when access to the project was removed', async () => {
    const wrapper = await mountWith(WaveView, { props })
    bridge.emit(EVENTS.waveClosed, { stream: 'wave', reason: 'access_removed' })
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Você não tem mais acesso a este projeto.')
  })
})
