import { flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { mountWithPlugins } from '@/test/mountWithPlugins'
import type { Question } from '../api/wavesApi'
import WaveQuestions from '../components/WaveQuestions.vue'

const question: Question = {
  id: 'PERG-1',
  opened_at: '2026-11-22T10:00:00Z',
  items: [
    { number: 1, text: 'Reservas recorrentes entram no escopo?' },
    { number: 2, text: 'Quem aprova a reserva de auditório?', suggestion: 'O gestor da área.' },
  ],
}

describe('WaveQuestions', () => {
  it('CA-18-7: blocks sending while a numbered question is unanswered and names the missing numbers', async () => {
    const submit = vi.fn().mockResolvedValue(undefined)
    const wrapper = mountWithPlugins(WaveQuestions, {
      props: { questions: [question], canAnswer: true, focusOnOpen: false, submit },
      attachTo: document.body,
    })

    await wrapper.findAll('textarea:not(.v-textarea__sizer)')[0]?.setValue('Sim, semanais.')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(submit).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Faltam respostas para os itens: 2.')
    wrapper.unmount()
  })

  it('US-10 CA-1: sends every numbered answer with one idempotency key per question', async () => {
    const submit = vi.fn().mockResolvedValue(undefined)
    const wrapper = mountWithPlugins(WaveQuestions, {
      props: { questions: [question], canAnswer: true, focusOnOpen: false, submit },
      attachTo: document.body,
    })

    const fields = wrapper.findAll('textarea:not(.v-textarea__sizer)')
    await fields[0]?.setValue('Sim, semanais.')
    await fields[1]?.setValue('Usar a sugestão.')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(submit).toHaveBeenCalledWith(
      question,
      [
        { number: 1, text: 'Sim, semanais.' },
        { number: 2, text: 'Usar a sugestão.' },
      ],
      expect.any(String),
    )
    wrapper.unmount()
  })

  it('shows agent text as plain text, never as HTML (RNF-05)', () => {
    const hostile: Question = {
      ...question,
      items: [{ number: 1, text: '<img src=x onerror="alert(1)">' }],
    }
    const wrapper = mountWithPlugins(WaveQuestions, {
      props: { questions: [hostile], canAnswer: false, focusOnOpen: false, submit: vi.fn() },
    })
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('<img src=x onerror="alert(1)">')
  })
})
