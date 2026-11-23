import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { EVENTS } from '@/bridge'
import { listen, store } from '@/store'
import DashboardView from '@/views/DashboardView.vue'
import {
  expectAccessible,
  goError,
  installBridge,
  mountWith,
  PROJECT_ID,
  type FakeBridge,
} from './helpers'

const projects = {
  items: [
    {
      id: PROJECT_ID,
      name: 'Portal de compras',
      state: 'in_progress',
      summary: {
        current_stages: [{ stage: 'requirements', state: 'running' }],
        gates_awaiting: 1,
        open_questions: 0,
        accumulated_cost: { value: '1.20', currency: 'BRL' },
      },
    },
  ],
  next_cursor: null,
}

const pending = {
  items: [
    {
      type: 'gate',
      project: { id: PROJECT_ID, name: 'Portal de compras' },
      stage: 'requirements',
      target: { type: 'gate_round', id: 'gate.requirements/2' },
      opened_at: '2026-11-23T10:00:00Z',
      title: 'Gate de Requisitos, rodada 2',
    },
  ],
  total: 1,
}

describe('DashboardView (US-46 CA-1, CA-3, CA-4)', () => {
  let bridge: FakeBridge
  beforeEach(() => {
    bridge = installBridge()
    bridge.responses.Projects = () => projects
    bridge.responses.PendingItems = () => pending
  })

  it('shows each project with state and current stage in text', async () => {
    const wrapper = await mountWith(DashboardView)
    const card = wrapper.get('.card')
    expect(card.text()).toContain('Portal de compras')
    expect(card.text()).toContain('Estado: Em andamento')
    expect(card.text()).toContain('Etapa atual: Requisitos (Em execução)')
    expect(card.text()).toContain('Gates aguardando: 1')
    await expectAccessible(wrapper.element)
  })

  it('has no control or decision action: pending items only open the web', async () => {
    const wrapper = await mountWith(DashboardView)
    const labels = wrapper.findAll('button').map((b) => b.attributes('aria-label') ?? b.text())
    expect(labels).toEqual([
      'Portal de compras',
      'Abrir na web: Portal de compras',
      'Abrir na web: Gate de Requisitos, rodada 2',
    ])
    await wrapper.findAll('button')[2]?.trigger('click')
    expect(bridge.called('OpenPendingItem')).toEqual([
      ['gate', PROJECT_ID, 'gate_round', 'gate.requirements/2'],
    ])
    await wrapper.findAll('button')[1]?.trigger('click')
    expect(bridge.called('OpenWeb')).toEqual([[{ kind: 'project', project_id: PROJECT_ID }]])
  })

  it('opens the project view', async () => {
    const wrapper = await mountWith(DashboardView)
    await wrapper.get('.card button').trigger('click')
    expect(store.view).toEqual({ name: 'project', projectId: PROJECT_ID })
  })

  it('reads again after a notice and lists it among recent notices', async () => {
    vi.useFakeTimers()
    const stop = listen(() => undefined)
    const wrapper = await mountWith(DashboardView)
    expect(bridge.called('Projects')).toHaveLength(1)
    bridge.emit(EVENTS.notice, { type: 'gate.awaiting_approval', project_id: PROJECT_ID })
    await flushPromises()
    vi.advanceTimersByTime(400)
    await flushPromises()
    expect(bridge.called('Projects')).toHaveLength(2)
    expect(wrapper.text()).toContain('Gate aguardando aprovação · Portal de compras')
    stop()
    vi.useRealTimers()
  })

  it('shows the error of a refused read', async () => {
    bridge.responses.Projects = () => {
      throw goError({
        kind: 'problem',
        problem_type: 'urn:easycode:problem:forbidden',
        status: 403,
      })
    }
    const wrapper = await mountWith(DashboardView)
    expect(wrapper.get('[role="alert"]').text()).toBe('Sem permissão.')
  })
})
