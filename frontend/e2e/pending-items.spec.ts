import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { domainEvent, FakeApi, gateRound } from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a50'

const gateItem = {
  type: 'gate',
  project: { id: PROJECT_ID, name: 'Reserva de salas' },
  stage: 'requirements',
  target: { type: 'gate_round', id: 'gate.requirements/1' },
  opened_at: '2026-11-22T08:00:00-03:00',
  title: 'Gate de Requisitos, rodada 1',
  summary: { failed_checks: 0, blocks: false, you_decide: true },
}

const questionItem = {
  type: 'question',
  project: { id: PROJECT_ID, name: 'Reserva de salas' },
  stage: 'requirements',
  agent: 'requirements-agent',
  target: { type: 'question', id: 'REQ-01/PERG-1' },
  opened_at: '2026-11-22T09:30:00-03:00',
  title: '2 perguntas do Agente de Requisitos',
  summary: { questions: 2 },
}

function apiWithItems(items: Array<Record<string, unknown>>): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  const state = api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T07:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      lead: { id: 'dev', name: 'Dev' },
      my_roles: ['lead', 'approver'],
      available_actions: [],
      last_sequence: 3,
    },
  })
  state.gates['gate.requirements'] = [gateRound('gate.requirements', 'requirements', 1)]
  api.pendingItems = items
  return api
}

test('US-48 CA-1 / CA-19-10 / CA-19-13: items show type, project, stage and age, oldest first, with phase 1 filters', async ({
  page,
}) => {
  // The api answers newest first here: the page still shows the oldest first.
  const api = apiWithItems([questionItem, gateItem])
  await api.install(page)
  await page.goto('/pending')

  const filters = page.getByRole('group', { name: 'Filtrar por tipo' }).getByRole('button')
  await expect(filters).toHaveText([
    'Todas (2)',
    'Gates (1)',
    'Perguntas (1)',
    'Achados escalados (0)',
    'Orçamento (0)',
  ])
  await expect(filters.first()).toHaveAttribute('aria-pressed', 'true')

  const items = page.getByTestId('pending-list').locator(':scope > li')
  await expect(items).toHaveCount(2)
  await expect(items.nth(0)).toContainText('Gate · Reserva de salas')
  await expect(items.nth(0)).toContainText('Gate de Requisitos, rodada 1')
  await expect(items.nth(0)).toContainText('Requisitos')
  await expect(items.nth(0)).toContainText('esperando')
  await expect(items.nth(1)).toContainText('Pergunta de agente')
  await expect(items.nth(1)).toContainText('Perguntas: 2')
  await expect(page.getByText(/só acompanha/)).toBeVisible()
  await expectNoSeriousA11yViolations(page)

  // ESP-19 section 2: "Decidir" opens the gate round.
  await items
    .nth(0)
    .getByRole('link', { name: /^Decidir/ })
    .click()
  await expect(page).toHaveURL(
    new RegExp(`/projects/${PROJECT_ID}/gates/gate.requirements/rounds/1$`),
  )
})

test('CA-19-8: a filter without items says so and keeps its count at zero', async ({ page }) => {
  const api = apiWithItems([gateItem])
  await api.install(page)
  await page.goto('/pending')

  await page.getByRole('button', { name: 'Perguntas (0)' }).click()

  await expect(page).toHaveURL(/\/pending\?type=question$/)
  await expect(page.getByTestId('pending-empty')).toHaveText('Nada deste tipo esperando você.')
  await expect(page.getByRole('button', { name: 'Perguntas (0)' })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await expectNoSeriousA11yViolations(page)
})

test('US-48 CA-2 / CA-19-3: a gate decided elsewhere leaves the list without reloading', async ({
  page,
}) => {
  const api = apiWithItems([gateItem, questionItem])
  const event = domainEvent(PROJECT_ID, 4, 'gate.approved', { gate: 'gate.requirements' })
  // watchNotices: the SSE id is the outbox position, not the project sequence (sse.md 3).
  api.noticeStreams.push(
    { body: 'retry: 1500\n\n: hb\n\n' },
    {
      body: `retry: 60000\n\nid: 900\nevent: ${String(event.type)}\ndata: ${JSON.stringify(event)}\n\n`,
      onServe: () => {
        api.pendingItems = [questionItem]
      },
    },
  )
  await api.install(page)
  await page.goto('/pending')
  const items = page.getByTestId('pending-list').locator(':scope > li')
  await expect(items).toHaveCount(2)

  await expect(items).toHaveCount(1)
  await expect(items.first()).toContainText('Pergunta de agente')
  await expect(page.getByRole('button', { name: 'Gates (0)' })).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: '1 saíram' })).toBeAttached()
})

test('US-39 CA-1, RN-5: a budget alert waits in Pending items and opens the budget', async ({
  page,
}) => {
  const api = apiWithItems([
    {
      type: 'budget',
      project: { id: PROJECT_ID, name: 'Reserva de salas' },
      target: { type: 'project', id: PROJECT_ID },
      opened_at: '2026-11-22T09:00:00-03:00',
      summary: { budget_percentage: 80 },
    },
  ])
  await api.install(page)
  await page.goto('/pending?type=budget')

  const items = page.getByTestId('pending-list').locator(':scope > li')
  await expect(items).toHaveCount(1)
  await expect(items.first()).toContainText('Orçamento atingido: 80%')
  await expectNoSeriousA11yViolations(page)
  await items
    .first()
    .getByRole('link', { name: /^Ver orçamento/ })
    .click()
  await expect(page).toHaveURL(new RegExp(`/projects/${PROJECT_ID}/configuration$`))
})
