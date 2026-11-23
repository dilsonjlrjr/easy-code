import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi } from './support/fakeApi'

const BRL = (value: string) => ({ value, currency: 'BRL' })

function project(index: number, state: string, summary: Record<string, unknown> = {}) {
  const id = `00000000-0000-4000-9000-${String(index).padStart(12, '0')}`
  return {
    id,
    name: `Projeto ${String(index).padStart(3, '0')}`,
    state,
    pause_reason: state === 'paused' ? 'budget' : null,
    business_need: `Necessidade do projeto ${String(index)}\nSegunda linha`,
    workflow: 'workflow-padrao',
    created_at: '2026-11-01T10:00:00-03:00',
    created_by: { id: 'dev', name: 'Dev' },
    updated_at: '2026-11-22T10:00:00-03:00',
    lead: { id: 'dev', name: 'Dev' },
    my_roles: ['lead'],
    summary:
      state === 'created'
        ? undefined
        : {
            approved_stages: 2,
            stages_total: 9,
            gates_awaiting: 0,
            open_questions: 0,
            accumulated_cost: BRL('40.00'),
            stop_limit: BRL('200.00'),
            ...summary,
          },
  }
}

function seedDashboard(api: FakeApi): void {
  api.addProject({
    project: project(1, 'in_progress', {
      current_stages: [{ stage: 'architecture', state: 'running' }],
      active_waves: [{ wave: 'ARQ-01', agent: 'architect-agent', state: 'running' }],
      gates_awaiting: 1,
      open_findings: { critical: 1, high: 2, low: 1 },
    }),
  })
  api.addProject({ project: project(2, 'paused') })
  api.addProject({ project: project(3, 'created') })
}

test('US-50 CA-1/CA-2: the dashboard shows each project situation and filters by state', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  seedDashboard(api)
  await api.install(page)
  await page.goto('/projects')

  const first = page.getByRole('listitem').filter({ hasText: 'Projeto 001' })
  await expect(first).toContainText('Em andamento', { timeout: 30_000 })
  await expect(first).toContainText('Etapas aprovadas: 2 de 9')
  await expect(first).toContainText('Arquitetura: Em execução')
  await expect(first).toContainText('Agente Arquiteto: Em execução')
  await expect(first).toContainText('Gates aguardando aprovação: 1')
  await expect(first.getByTestId('project-budget-used')).toContainText('20%')
  await expect(first.getByTestId('project-open-findings')).toContainText(
    '1 Crítico, 2 Alto, 1 Baixo',
  )
  await expect(first).toContainText('Responsável: Dev')
  // CA-04-4: a created project says the workflow did not start, with no stage counter.
  const created = page.getByRole('listitem').filter({ hasText: 'Projeto 003' })
  await expect(created).toContainText('Workflow ainda não iniciado')
  await expect(created).not.toContainText('Etapas aprovadas')
  await expectNoSeriousA11yViolations(page)

  // CA-2: only the projects in the chosen state; the filter stays in the address.
  await page.getByRole('button', { name: 'Pausados (1)' }).click()
  await expect(page).toHaveURL(/state=paused/)
  await expect(page.getByRole('heading', { level: 2 })).toHaveCount(1)
  await expect(page.getByRole('heading', { name: 'Projeto 002' })).toBeVisible()
  await expect(
    page.getByRole('status').filter({ hasText: '1 projetos neste filtro' }),
  ).toBeAttached()
  await page.reload()
  await expect(page.getByRole('button', { name: 'Pausados (1)' })).toHaveAttribute(
    'aria-pressed',
    'true',
    { timeout: 30_000 },
  )
  await page.getByRole('button', { name: 'Concluídos (0)' }).click()
  await expect(page.getByText('Nenhum projeto neste estado.')).toBeVisible()
})

test('US-50 CA-3 / ESP-03: the Home page shows only the projects with a role, agents, pending items and the month cost', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  seedDashboard(api)
  api.pendingItems = [
    {
      type: 'gate',
      project: { id: project(1, 'x').id, name: 'Projeto 001' },
      stage: 'architecture',
      target: { type: 'gate_round', id: 'gate.architecture/1' },
      opened_at: '2026-11-22T09:00:00-03:00',
    },
  ]
  api.userCosts = {
    from: '2026-11-01',
    until: '2026-12-01',
    total: { useful: BRL('70.00'), lost: BRL('0'), total: BRL('70.00'), rework: BRL('14.00') },
    rework_percentage: 20,
    largest_spend: [{ project: project(1, 'x').id, name: 'Projeto 001', total: BRL('40.00') }],
  }
  await api.install(page)
  await page.goto('/')

  await expect(page.getByTestId('home-in-progress')).toHaveText('1', { timeout: 30_000 })
  await expect(page.getByTestId('home-indicators')).toContainText('1 criados, ainda não iniciados')
  await expect(page.getByTestId('home-gates')).toHaveText('1')
  await expect(page.getByTestId('home-findings')).toHaveText('3')
  const table = page.getByTestId('home-projects')
  await expect(table.locator('tbody tr')).toHaveCount(3)
  await expect(table).toContainText('2 de 9 aprovadas')
  await expect(table).toContainText('Pausado (orçamento)')
  await expect(page.getByTestId('home-agents')).toContainText('ARQ-01')
  await expect(page.getByTestId('home-pending')).toContainText('gate.architecture/1')
  await expect(page.getByTestId('home-month-total')).toContainText('70,00')
  await expect(page.getByTestId('home-month-rework')).toContainText('20%')
  await expect(page.getByTestId('home-month-largest')).toContainText('Projeto 001')
  await expectNoSeriousA11yViolations(page)
})

test('RNF-09: the dashboard of 100 projects reads one light page and loads more on demand', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  for (let index = 1; index <= 100; index += 1)
    api.addProject({ project: project(index, index % 5 === 0 ? 'paused' : 'in_progress') })
  await api.install(page)

  const started = Date.now()
  await page.goto('/projects')
  await expect(page.getByRole('heading', { level: 2 })).toHaveCount(20, { timeout: 30_000 })
  const elapsed = Date.now() - started
  // The p95 of RNF-09 is measured on the api by the load test; the screen must stay one page
  // (`limit=20`) and render it well within the budget even on the shared CI host.
  expect(elapsed).toBeLessThan(10_000)
  const listCalls = api.requests.filter(
    (request) => request.method === 'GET' && request.path.startsWith('/projects?'),
  )
  expect(listCalls).toHaveLength(1)
  expect(listCalls[0]?.path).toContain('limit=20')
  // CA-04-13: the counts are the totals of the scope, not of the loaded page.
  await expect(page.getByRole('button', { name: 'Todos (100)' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Pausados (20)' })).toBeVisible()

  // CA-04-8: the next page comes in without repeating or skipping a project.
  await page.getByRole('button', { name: 'Carregar mais' }).click()
  await expect(page.getByRole('heading', { level: 2 })).toHaveCount(40)
  const names = await page.getByRole('heading', { level: 2 }).allTextContents()
  expect(new Set(names).size).toBe(40)
})
