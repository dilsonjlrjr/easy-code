import { describe, expect, it } from 'vitest'
import { ApiError } from '@/shared/api/problemDetails'
import { mountWithPlugins } from '@/test/mountWithPlugins'
import ProblemAlert from '../ProblemAlert.vue'

describe('ProblemAlert', () => {
  it('US-29 CA-1: lists the schema errors of an artifact-invalid problem, with the trace id', () => {
    const error = new ApiError(400, {
      type: 'urn:easycode:problem:artifact-invalid',
      title: 'Artifact invalid',
      status: 400,
      trace_id: '4bf92f3577b34da6a3ce929d0e0e4736',
      schema_errors: [{ path: '/objetivo', message: 'campo obrigatório ausente' }],
    })
    const wrapper = mountWithPlugins(ProblemAlert, { props: { error } })

    expect(wrapper.text()).toContain('O artefato está fora do esquema do seu tipo:')
    expect(wrapper.find('[data-testid="schema-errors"]').text()).toContain(
      '/objetivo · campo obrigatório ausente',
    )
    expect(wrapper.text()).toContain('4bf92f3577b34da6a3ce929d0e0e4736')
  })
})
