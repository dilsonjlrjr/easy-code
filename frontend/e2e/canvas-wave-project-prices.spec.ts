import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, phaseOneWorkflow, waveDetail, waveSummary } from './support/fakeApi'

// MARCO-10 (PLANO, "Interface completa do canvas"): Onda, Projeto and Configurações lines.

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'
const WAVE = 'REQ-01'
const BRL = (value: string) => ({ value, currency: 'BRL' })
const tokens = (input: number, output: number) => ({ input, output, read_cache: 0, write_cache: 0 })
const minutesAgo = (minutes: number) => new Date(Date.now() - minutes * 60_000).toISOString()

function signedIn(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  return api
}

function project(state: string) {
  return {
    id: PROJECT_ID,
    name: 'Reserva de salas',
    state,
    workflow: 'workflow-padrao',
    created_at: '2026-11-22T10:00:00-03:00',
    created_by: { id: 'dev', name: 'Dev' },
    lead: { id: 'dev', name: 'Dev' },
    my_roles: ['lead', 'approver'],
    available_actions: state === 'created' ? ['start', 'cancel'] : ['pause', 'cancel'],
    last_sequence: 3,
  }
}

test('ESP-18 PD-20/PL-39/PL-31: the wave shows progress by checkpoint, calls and checkpoints in one timeline and skills with origin', async ({
  page,
}) => {
  const api = signedIn()
  api.skills.push(
    {
      id: 'ux-accessibility',
      name: 'UX e acessibilidade',
      description: 'Catálogo nativo.',
      origin: 'native',
      current_version: 2,
      status: 'unblocked',
    },
    {
      id: 'database-conventions',
      name: 'Convenções de banco da empresa',
      description: 'Skill da organização.',
      origin: 'organization',
      current_version: 1,
      status: 'unblocked',
    },
  )
  api.addProject({
    project: project('in_progress'),
    workflow: phaseOneWorkflow('started'),
    waves: [waveSummary(WAVE, 'running')],
    waveDetails: {
      [WAVE]: waveDetail(WAVE, 'running', {
        last_checkpoint: 2,
        versions: {
          skills: [
            { skill: 'ux-accessibility', version: 2 },
            { skill: 'database-conventions', version: 1 },
          ],
        },
      }),
    },
    calls: {
      [WAVE]: [
        {
          number: 1,
          type: 'model',
          start: minutesAgo(9),
          duration_ms: 4200,
          result: 'success',
          attempt: 1,
          provider: 'anthropic',
          model: 'execution-model',
          role: 'execution',
          tokens: tokens(1000, 200),
          cost: BRL('0.0250'),
          summary: 'montou a visão',
        },
        {
          number: 2,
          type: 'tool',
          start: minutesAgo(6),
          duration_ms: 300,
          result: 'success',
          attempt: 1,
          tool: 'write_artifact',
          summary: 'gravou VISAO.md',
        },
      ],
    },
    checkpoints: {
      [WAVE]: [1, 2].map((number) => ({
        number,
        written_at: minutesAgo(number === 1 ? 8 : 3),
        after: number === 1 ? 'model_call' : 'tool',
        attempt: 1,
        accumulated_tokens: tokens(1000, 200),
        accumulated_cost: { useful: BRL('0.0250'), lost: BRL('0'), total: BRL('0.0250') },
      })),
    },
  })
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  const progress = page.getByTestId('wave-progress')
  await expect(progress).toContainText('Progresso da onda', { timeout: 30_000 })
  await expect(progress.getByTestId('progress-checkpoint')).toContainText('checkpoint 2')
  await expect(progress).toContainText('2 (1 de modelo, 1 de ferramenta)')
  await expect(page.locator('body')).not.toContainText('Passos')

  // Calls and checkpoints share the timeline, oldest first, each with its type.
  const timeline = page.getByTestId('wave-timeline')
  await expect(timeline.locator('li')).toHaveCount(4)
  await expect(timeline.locator('li').nth(0)).toContainText('Modelo')
  await expect(timeline.locator('li').nth(1)).toContainText('Checkpoint 1 gravado')
  await expect(page.getByTestId('call-2')).toContainText('Ferramenta')
  await expect(page.getByTestId('call-2')).toContainText('gravou VISAO.md')
  await expect(timeline.locator('li').nth(3)).toContainText('Checkpoint 2 gravado')

  await expect(page.getByTestId('wave-last-checkpoint')).toContainText(/checkpoint 2, há 3 min/)
  await expect(page.getByTestId('wave-skill-ux-accessibility')).toContainText('UX e acessibilidade')
  await expect(page.getByTestId('wave-skill-ux-accessibility')).toContainText('catálogo')
  await expect(page.getByTestId('wave-skill-database-conventions')).toContainText('organização')
  await expectNoSeriousA11yViolations(page)
})

test('ESP-06 CA-06-11/CA-06-25: the waves table has tokens and time, and the header the clone address', async ({
  page,
}) => {
  const api = signedIn()
  api.addProject({
    project: project('in_progress'),
    workflow: phaseOneWorkflow('started'),
    waves: [
      {
        ...waveSummary(WAVE, 'completed'),
        started_at: '2026-11-22T10:00:00-03:00',
        ended_at: '2026-11-22T10:41:00-03:00',
        cost_total: BRL('0.4100'),
        tokens: { input: 150000, output: 32000, read_cache: 48000, write_cache: 0 },
      },
    ],
  })
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}`)

  const table = page.getByTestId('project-waves')
  await expect(table.getByRole('columnheader', { name: 'Tokens' })).toBeVisible({ timeout: 30_000 })
  await expect(table.getByRole('columnheader', { name: 'Tempo' })).toBeVisible()
  const row = page.getByTestId(`project-wave-${WAVE}`)
  await expect(row.getByTestId('wave-tokens')).toContainText('182.000')
  await expect(row.getByTestId('wave-tokens')).toContainText('48.000 em cache')
  await expect(row.getByTestId('wave-time')).toContainText('41')

  const clone = page.getByTestId('header-clone-url')
  await expect(clone).toContainText(`https://easycode.empresa.local/git/${PROJECT_ID}.git`)
  await expect(clone.getByRole('button', { name: 'Copiar endereço de clone' })).toBeVisible()
  await expectNoSeriousA11yViolations(page)
})

test('ESP-06 PC-19: a created project shows the estimate of the first stage', async ({ page }) => {
  const api = signedIn()
  api.addProject({
    project: project('created'),
    estimates: {
      requirements: {
        stage: 'requirements',
        estimated: BRL('3.50'),
        no_history: true,
        basis: { input_size_tokens: 4000, price_table_version: '7' },
        performed: null,
      },
    },
  })
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}`)

  await expect(page.getByTestId('first-stage-estimate')).toContainText(
    'Estimativa da primeira etapa: R$ 3,50',
    { timeout: 30_000 },
  )
  await expectNoSeriousA11yViolations(page)
})

test('ESP-23 US-37 PC-28: the price table shows each model with its own period and the two cache columns', async ({
  page,
}) => {
  const api = signedIn()
    .withActiveKey()
    .withModel(
      { model: 'reasoning-model', display_name: 'Raciocínio' },
      {
        id: 7,
        table_version: '7',
        effective_since: '2026-11-01T00:00:00-03:00',
        currency: 'BRL',
        per_million_tokens: { input: '15', output: '75', read_cache: '1.5', write_cache: '18.75' },
        source: 'Página oficial de preços, consultada em 01/11/2026',
        used: true,
        recorded_at: '2026-11-01T09:00:00-03:00',
        recorded_by: { id: 'dev', name: 'Dev' },
      },
    )
    .withModel({ model: 'light-model', display_name: 'Leve' })
  await api.install(page)
  await page.goto('/settings/prices')

  const matrix = page.getByTestId('current-prices')
  await expect(
    matrix.getByRole('columnheader', { name: 'Leitura de cache (1 milhão)' }),
  ).toBeVisible({ timeout: 30_000 })
  await expect(
    matrix.getByRole('columnheader', { name: 'Escrita de cache (1 milhão)' }),
  ).toBeVisible()
  const reasoning = page.getByTestId('current-price-reasoning-model')
  await expect(reasoning).toContainText('01/11/2026')
  await expect(reasoning).toContainText('18,75')
  await expect(page.getByTestId('current-price-light-model')).toContainText('sem preço vigente')
  await expect(page.locator('body')).not.toContainText('Nova versão da tabela')
  await expectNoSeriousA11yViolations(page)

  await page.getByRole('button', { name: 'Ver vigências de Raciocínio' }).click()
  await expect(page.getByRole('heading', { name: 'Vigências de Raciocínio' })).toBeFocused()
  await expect(page.getByTestId('price-period-7')).toContainText('já usada por onda')
})

test('a long wave activity shows the latest entries, the earlier ones on request and a filter for problems', async ({
  page,
}) => {
  const api = signedIn()
  const calls = Array.from({ length: 58 }, (_, i) => ({
    number: i + 1,
    type: i % 2 === 0 ? 'model' : 'tool',
    start: minutesAgo(60 - i),
    duration_ms: 300,
    result: i === 4 ? 'refused' : 'success',
    attempt: 1,
    ...(i % 2 === 0
      ? {
          provider: 'anthropic',
          model: 'execution-model',
          role: 'execution',
          tokens: tokens(10, 2),
        }
      : { tool: 'write_file', summary: `gravou US-${String(i).padStart(2, '0')}.md` }),
    ...(i === 4 ? { type: 'tool', tool: 'write_file', refusal_reason: 'path_not_allowed' } : {}),
  }))
  api.addProject({
    project: project('in_progress'),
    workflow: phaseOneWorkflow('started'),
    waves: [waveSummary(WAVE, 'running')],
    waveDetails: { [WAVE]: waveDetail(WAVE, 'running') },
    calls: { [WAVE]: calls },
    checkpoints: { [WAVE]: [] },
  })
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  const timeline = page.getByTestId('wave-timeline')
  await expect(timeline.locator('li')).toHaveCount(10, { timeout: 30_000 })
  await expect(page.getByTestId('timeline-count')).toContainText(
    'Mostrando as 10 mais recentes de 58',
  )
  await expect(page.getByTestId('call-58')).toBeVisible()
  await expect(page.getByTestId('call-1')).toHaveCount(0)

  await page.getByTestId('timeline-earlier').click()
  await expect(timeline.locator('li')).toHaveCount(40)
  await page.getByTestId('timeline-all').click()
  await expect(timeline.locator('li')).toHaveCount(58)
  await page.getByTestId('timeline-latest').click()
  await expect(timeline.locator('li')).toHaveCount(10)

  await page.getByTestId('timeline-filter-problem').click()
  await expect(page.getByTestId('timeline-filter-problem')).toHaveAttribute('aria-pressed', 'true')
  await expect(timeline.locator('li')).toHaveCount(1)
  await expect(page.getByTestId('call-5')).toContainText('caminho fora do permitido')
  await expectNoSeriousA11yViolations(page)
})
