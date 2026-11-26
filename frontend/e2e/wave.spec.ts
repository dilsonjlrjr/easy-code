import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import {
  domainEvent,
  FakeApi,
  phaseOneWorkflow,
  sseFrame,
  waveDetail,
  waveSummary,
  type ProjectState,
} from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'
const WAVE = 'REQ-01'
const BRL = (value: string) => ({ value, currency: 'BRL' })
const tokens = (input: number, output: number) => ({ input, output, read_cache: 0, write_cache: 0 })

function modelCall(number: number, overrides: Record<string, unknown> = {}) {
  return {
    number,
    type: 'model',
    start: '2026-11-22T10:01:00-03:00',
    duration_ms: 4200,
    result: 'success',
    attempt: 1,
    provider: 'anthropic',
    model: 'execution-model',
    role: 'execution',
    tokens: tokens(1000 * number, 200),
    cost: BRL('0.0250'),
    summary: `gravou US-0${String(number)}.md`,
    ...overrides,
  }
}

function checkpoint(number: number, cost: string) {
  return {
    number,
    written_at: '2026-11-22T10:01:00-03:00',
    after: number === 0 ? null : 'model_call',
    attempt: 1,
    accumulated_tokens: tokens(1000 * number, 200 * number),
    accumulated_cost: { useful: BRL(cost), lost: BRL('0'), total: BRL(cost) },
  }
}

function waveEvent(sequence: number, type: string, data: Record<string, unknown> = {}) {
  return domainEvent(
    PROJECT_ID,
    sequence,
    type,
    {
      wave: WAVE,
      stage: 'requirements',
      agent: 'requirements-agent',
      purpose: 'execution',
      ...data,
    },
    WAVE,
  )
}

function seed(
  api: FakeApi,
  wave: Record<string, unknown>,
  extra: Partial<ProjectState> = {},
): ProjectState {
  return api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      lead: { id: 'dev', name: 'Dev' },
      my_roles: ['lead', 'approver'],
      available_actions: ['pause', 'cancel'],
      last_sequence: 3,
    },
    workflow: phaseOneWorkflow('started'),
    waves: [waveSummary(WAVE, String(wave.state))],
    waveDetails: { [WAVE]: wave },
    ...extra,
  })
}

function signedIn(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  return api
}

test('US-09 CA-1/CA-2/CA-3, US-36 CA-2: the wave changes live, resumes with Last-Event-ID without duplicates, and the cost follows each call', async ({
  page,
}) => {
  const api = signedIn()
  const state = seed(
    api,
    waveDetail(WAVE, 'running', {
      cost: { useful: BRL('0.0250'), lost: BRL('0'), total: BRL('0.0250') },
    }),
    {
      calls: { [WAVE]: [modelCall(1)] },
      checkpoints: { [WAVE]: [checkpoint(0, '0'), checkpoint(1, '0.0250')] },
      history: [
        waveEvent(2, 'wave.created', {
          transition: { machine: 'wave', from_state: null, to_state: 'queued' },
        }),
        waveEvent(3, 'wave.started', {
          transition: { machine: 'wave', from_state: 'queued', to_state: 'running' },
        }),
      ],
    },
  )
  const secondCall = waveEvent(4, 'wave.checkpoint_written', {
    checkpoint: 2,
    after: 'model_call',
    accumulated_tokens: tokens(3000, 400),
    accumulated_cost: BRL('0.0500'),
  })
  const questionOpened = waveEvent(5, 'wave.question_opened', {
    question: 'PERG-1',
    items: 2,
    transition: { machine: 'wave', from_state: 'running', to_state: 'awaiting_user' },
  })
  state.streams.push(
    {
      body: `retry: 200\n\n${sseFrame(secondCall)}`,
      onServe: () => {
        state.calls[WAVE] = [modelCall(1), modelCall(2)]
        state.checkpoints[WAVE] = [
          checkpoint(0, '0'),
          checkpoint(1, '0.0250'),
          checkpoint(2, '0.0500'),
        ]
        Object.assign(state.waveDetails[WAVE] ?? {}, {
          last_checkpoint: 2,
          tokens: tokens(3000, 400),
          cost: { useful: BRL('0.0500'), lost: BRL('0'), total: BRL('0.0500') },
        })
      },
    },
    {
      // After the drop the server replays from Last-Event-ID; event 4 again must be ignored.
      body: `retry: 60000\n\n${sseFrame(secondCall)}${sseFrame(questionOpened)}`,
      onServe: () => {
        Object.assign(state.waveDetails[WAVE] ?? {}, {
          state: 'awaiting_user',
          available_actions: ['answer', 'cancel'],
          question_open: 'PERG-1',
        })
        state.questions[WAVE] = [
          {
            id: 'PERG-1',
            opened_at: '2026-11-22T10:02:00-03:00',
            items: [
              { number: 1, text: 'Reservas recorrentes entram no escopo?' },
              { number: 2, text: 'Quem aprova auditórios?', suggestion: 'O gestor da área.' },
            ],
          },
        ]
      },
    },
  )
  await api.install(page)

  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  await expect(page.getByRole('heading', { level: 1 })).toContainText(WAVE)
  await expect(page.getByTestId('call-2')).toContainText('gravou US-02.md')
  await expect(page.getByTestId('wave-cost-total')).toContainText('0,05')
  await expect(page.locator('.state-label').first()).toContainText('Aguardando usuário')
  await expect(page.getByTestId('question-PERG-1')).toBeVisible()

  const streams = api.streamRequests(PROJECT_ID)
  expect(streams[0]?.url).toContain(`waveId=${WAVE}`)
  expect(streams[0]?.url).toContain('since=3')
  expect(streams[1]?.lastEventId).toBe('4')
  await page.getByTestId('wave-tab-history').click()
  await expect(page.getByTestId('wave-history').getByText('Checkpoint 2 gravado')).toHaveCount(1)
  await expectNoSeriousA11yViolations(page)

  // US-10 CA-1: answer every numbered question; the wave goes back to running.
  const question = page.getByTestId('question-PERG-1')
  await question.getByLabel('Resposta 1').fill('Sim, semanais.')
  await question.getByLabel('Resposta 2').fill('Usar a sugestão.')
  await question.getByRole('button', { name: 'Enviar respostas' }).click()
  await expect(page.getByText('Respostas registradas; a onda voltou a executar.')).toBeVisible()
  await expect(page.locator('.state-label').first()).toContainText('Em execução')
  await expect(page.getByText(/Respondida por Dev em .*, pela web\./)).toBeVisible()
  const answer = api.requests.find((request) => request.path.endsWith('/answer'))
  expect(answer?.body).toEqual({
    answers: [
      { number: 1, text: 'Sim, semanais.' },
      { number: 2, text: 'Usar a sugestão.' },
    ],
  })
  expect(answer?.headers['idempotency-key']).toBeTruthy()
})

test('US-11 CA-1/CA-2: a wave resumed after a platform restart shows the checkpoint, no duplicated calls and the aborted call as lost cost', async ({
  page,
}) => {
  const api = signedIn()
  seed(
    api,
    waveDetail(WAVE, 'running', {
      last_checkpoint: 6,
      cost: {
        useful: BRL('0.1250'),
        lost: BRL('0.0100'),
        total: BRL('0.1350'),
        estimated_lost: BRL('0.0100'),
      },
    }),
    {
      calls: {
        [WAVE]: [
          ...[1, 2, 3, 4, 5].map((number) => modelCall(number)),
          modelCall(6, {
            result: 'aborted',
            lost: true,
            lost_cost: true,
            estimated_cost: true,
            cost: BRL('0.0100'),
            summary: null,
          }),
          modelCall(7),
        ],
      },
      history: [
        waveEvent(2, 'wave.started', {
          transition: { machine: 'wave', from_state: 'queued', to_state: 'running' },
        }),
        waveEvent(3, 'wave.resumed_after_crash', {
          checkpoint: 5,
          lease_epoch: 2,
          redone_call: true,
          lost_cost: BRL('0.0100'),
          estimated_lost_cost: true,
        }),
      ],
    },
  )
  await api.install(page)

  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  await expect(page.getByTestId('resume-notice')).toContainText(
    'Retomada do checkpoint 5 após reinício da plataforma.',
  )
  await expect(page.getByTestId('resume-notice')).toContainText('(estimado)')
  await expect(page.locator('[data-testid^="call-"]')).toHaveCount(7)
  await expect(page.getByTestId('call-6')).toContainText('abortada')
  await expect(page.getByTestId('call-6')).toContainText('perdida na interrupção')
  await expect(page.getByTestId('call-6')).toContainText('custo perdido')
  await expectNoSeriousA11yViolations(page)
})

test('US-36 CA-1, US-09 CA-4: a completed wave shows tokens, models, useful and lost cost, times, result and its history, without controls', async ({
  page,
}) => {
  const api = signedIn()
  seed(
    api,
    waveDetail(WAVE, 'completed', {
      tokens: { input: 48210, output: 6930, read_cache: 31000, write_cache: 2400 },
      cost: { useful: BRL('0.4127'), lost: BRL('0'), total: BRL('0.4127') },
      time: { execution_ms: 412_300, cycle_ms: 455_900 },
      result: 'success',
      available_actions: [],
      versions: { price_table: '7' },
    }),
    {
      history: [
        waveEvent(2, 'wave.started', {
          transition: { machine: 'wave', from_state: 'queued', to_state: 'running' },
        }),
        waveEvent(3, 'wave.completed', {
          transition: { machine: 'wave', from_state: 'running', to_state: 'completed' },
        }),
      ],
    },
  )
  await api.install(page)

  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  const cost = page.getByTestId('wave-cost')
  await expect(cost).toContainText('48.210')
  await expect(cost).toContainText('31.000')
  await expect(cost).toContainText('execution-model')
  await expect(cost).toContainText('0,4127')
  await expect(cost).toContainText(/6\s?min 52\s?s/)
  await expect(cost).toContainText('sucesso')
  await page.getByTestId('wave-tab-history').click()
  await expect(page.getByTestId('wave-history')).toContainText('Onda REQ-01 concluída')
  await expect(page.getByRole('button', { name: /Pausar|Cancelar|Reiniciar/ })).toHaveCount(0)
  await expectNoSeriousA11yViolations(page)
})

test('US-29 CA-2: a wave that failed for an invalid artifact shows the cause and the schema errors', async ({
  page,
}) => {
  const api = signedIn()
  seed(
    api,
    waveDetail(WAVE, 'failed', {
      result: 'failed',
      failure_cause: 'invalid_artifact',
      artifact_errors: [
        { artifact: 'US-01', path: '/criterios_aceite', message: 'campo obrigatório ausente' },
      ],
      available_actions: ['restart', 'cancel'],
    }),
  )
  await api.install(page)

  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  const failure = page.getByTestId('failure-cause')
  await expect(failure).toContainText('Causa da falha: artefato inválido')
  // The errors fold away and open on request; the note closes.
  await expect(failure.getByText('campo obrigatório ausente')).toBeHidden()
  const toggle = failure.getByTestId('schema-errors-toggle')
  await expect(toggle).toHaveText('Ver o erro de esquema')
  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-expanded', 'true')
  await expect(failure).toContainText('/criterios_aceite')
  await expect(failure.getByText('campo obrigatório ausente')).toBeVisible()
  await expectNoSeriousA11yViolations(page)
  await failure.getByRole('button', { name: 'Fechar' }).click()
  await expect(failure).toHaveCount(0)
  await expect(page.locator('.state-label').first()).toContainText('Falhou')
})

test('US-10 CA-2: the open question appears in Pending items with project, stage, agent and waiting time', async ({
  page,
}) => {
  const api = signedIn()
  seed(api, waveDetail(WAVE, 'awaiting_user'))
  api.pendingItems = [
    {
      type: 'question',
      project: { id: PROJECT_ID, name: 'Reserva de salas' },
      stage: 'requirements',
      agent: 'requirements-agent',
      target: { type: 'question', id: `${WAVE}/PERG-1` },
      opened_at: new Date(Date.now() - 3 * 60 * 60 * 1000).toISOString(),
      summary: { questions: 2 },
    },
  ]
  await api.install(page)

  await page.goto('/pending')

  const item = page.locator('li', { hasText: 'Reserva de salas' })
  await expect(item).toContainText('Pergunta de agente')
  await expect(item).toContainText('Requisitos')
  await expect(item).toContainText('Agente de Requisitos')
  await expect(item).toContainText(/esperando há 3 horas|esperando há 3 h/)
  await expectNoSeriousA11yViolations(page)
  await item.getByRole('link', { name: 'Responder' }).click()
  await expect(page).toHaveURL(new RegExp(`/projects/${PROJECT_ID}/waves/${WAVE}$`))
})
