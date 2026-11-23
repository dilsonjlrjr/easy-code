import { expect, test, type Page } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, phaseOneWorkflow } from './support/fakeApi'
import { openSelect } from './support/select'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'

function record(
  id: number,
  action: string,
  when: string,
  extra: Record<string, unknown> = {},
): Record<string, unknown> {
  return {
    id: `00000000-0000-4000-8000-${String(id).padStart(12, '0')}`,
    when,
    actor: { type: 'human', user: { id: 'ana', name: 'Ana Souza' } },
    origin: 'web',
    action,
    target: { type: 'project', id: PROJECT_ID },
    project: PROJECT_ID,
    reason: null,
    lost_cost: null,
    correlationid: `0193a5c0-44aa-7e01-b2c9-${String(id).padStart(12, '0')}`,
    ...extra,
  }
}

const TRAIL = [
  record(1, 'project.create', '2026-11-01T09:00:00-03:00'),
  record(2, 'gate.approve', '2026-11-05T10:00:00-03:00', {
    target: { type: 'gate_round', id: 'gate.requirements/1' },
    reason: 'Requisitos completos e revisados com a área.',
  }),
  record(3, 'wave.pause_now', '2026-11-10T11:00:00-03:00', {
    target: { type: 'wave', id: 'ARQ-01' },
    reason: 'Ajuste do par de modelos.',
    lost_cost: { value: '0.4127', currency: 'BRL' },
  }),
  record(4, 'member.assign', '2026-11-12T08:00:00-03:00', {
    target: { type: 'member', id: 'bruno' },
    reason: 'Transferência de responsável para Bruno Lima.',
  }),
  record(5, 'gate.approve', '2026-11-20T15:00:00-03:00', {
    target: { type: 'gate_round', id: 'gate.architecture/2' },
  }),
  record(6, 'project.access_as_administrator', '2026-11-21T15:00:00-03:00', {
    actor: { type: 'human', user: { id: 'adm', name: 'Administradora' } },
  }),
]

function seed(api: FakeApi, roles: string[] = ['approver']) {
  return api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-01T09:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      my_roles: roles,
      last_sequence: 3,
    },
    workflow: phaseOneWorkflow('started'),
    audit: TRAIL,
  })
}

function signedIn(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  return api
}

function rows(page: Page) {
  return page.getByTestId('audit-records').locator('tbody tr')
}

test('US-35 CA-1/CA-3: the approver sees the trail newest first with who, when, where, action, target and reason; nothing edits it', async ({
  page,
}) => {
  const api = signedIn()
  seed(api)
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}`)
  await page.getByRole('tab', { name: 'Auditoria' }).click()

  await expect(rows(page)).toHaveCount(6, { timeout: 30_000 })
  // CA-27-9: the newest first.
  await expect(rows(page).first()).toHaveAttribute('data-action', 'project.access_as_administrator')
  await expect(rows(page).first()).toContainText('Administradora')
  const approval = page.getByTestId(`audit-record-${String(TRAIL[1]?.id)}`)
  await expect(approval).toContainText('Ana Souza')
  await expect(approval).toContainText('05/11/2026')
  await expect(approval).toContainText('web')
  await expect(approval).toContainText('Gate aprovado (gate.approve)')
  await expect(approval).toContainText('Requisitos completos e revisados com a área.')
  await expect(approval.getByRole('link', { name: 'gate.requirements/1' })).toBeVisible()
  const pause = page.getByTestId(`audit-record-${String(TRAIL[2]?.id)}`)
  await expect(pause).toContainText('0,4127')
  // CA-27-5: the transfer of the lead says who, to whom and when.
  await expect(page.getByTestId(`audit-record-${String(TRAIL[3]?.id)}`)).toContainText(
    'Transferência de responsável para Bruno Lima.',
  )
  await expect(page.getByTestId('audit-end')).toBeVisible()
  // CA-3: no change or delete action anywhere.
  await expect(
    page.getByTestId('audit-trail').getByRole('button', { name: /editar|apagar|excluir|remover/i }),
  ).toHaveCount(0)
  expect(
    api.requests.some((request) => request.path.includes('/audit') && request.method !== 'GET'),
  ).toBe(false)
  await expectNoSeriousA11yViolations(page)

  // CA-27-9: the order turns to oldest first.
  await page.getByTestId('audit-order').click()
  await expect(rows(page).first()).toHaveAttribute('data-action', 'project.create')
  await expect(page).toHaveURL(/order=asc/)
})

test('US-35 CA-2: filtering by action and period shows only the matching records', async ({
  page,
}) => {
  const api = signedIn()
  seed(api)
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/audit`)
  await expect(rows(page)).toHaveCount(6, { timeout: 30_000 })

  await openSelect(page, 'Tipo de ação')
  await page.getByRole('option', { name: /Gate aprovado/ }).click()
  await page.keyboard.press('Escape')
  await page.getByLabel('De', { exact: true }).fill('2026-11-15')
  await page.getByLabel('Até', { exact: true }).fill('2026-11-30')
  await page.getByRole('button', { name: 'Filtrar' }).click()

  await expect(rows(page)).toHaveCount(1)
  await expect(rows(page).first()).toContainText('gate.architecture/2')
  await expect(page).toHaveURL(/action=gate.approve/)
  const request = api.requests
    .filter((item) => item.path.startsWith(`/projects/${PROJECT_ID}/audit`))
    .at(-1)
  expect(request?.path).toContain('action=gate.approve')
  expect(request?.path).toContain('from=')
  expect(request?.path).toContain('until=')
  expect(request?.path).toContain('order=desc')

  // A filter without records says so and offers to clear.
  await page.getByLabel('De', { exact: true }).fill('2026-11-25')
  await page.getByRole('button', { name: 'Filtrar' }).click()
  await expect(page.getByTestId('audit-empty')).toContainText(
    'Nenhum registro para os filtros escolhidos.',
  )
  await page.getByTestId('audit-empty').getByRole('button', { name: 'Limpar filtros' }).click()
  await expect(rows(page)).toHaveCount(6)

  // ESP-27 4: an inverted period is refused next to the field.
  await page.getByLabel('De', { exact: true }).fill('2026-11-30')
  await page.getByLabel('Até', { exact: true }).fill('2026-11-01')
  await expect(
    page.getByText('A data final precisa ser igual ou posterior à inicial.'),
  ).toBeVisible()
  await expect(page.getByRole('button', { name: 'Filtrar' })).toBeDisabled()
})

test('CA-27-8: "load more" brings the next records after the shown ones, without repeating', async ({
  page,
}) => {
  const api = signedIn()
  api.auditPageSize = 4
  seed(api)
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/audit`)
  await expect(rows(page)).toHaveCount(4, { timeout: 30_000 })
  await page.getByTestId('audit-load-more').click()
  await expect(rows(page)).toHaveCount(6)
  await expect(page.getByTestId('audit-end')).toBeVisible()
  // ESP-27 8: the focus goes to the first new record.
  await expect(rows(page).nth(4)).toBeFocused()
  const ids = await rows(page).evaluateAll((items) => items.map((item) => item.dataset.testid))
  expect(new Set(ids).size).toBe(6)
})

test('CA-27-6: a member without approver or lead gets "no permission" and no audit tab', async ({
  page,
}) => {
  const api = signedIn()
  api.user = { id: 'dev', name: 'Dev', organization_roles: [] }
  seed(api, ['member'])
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}`)
  await expect(page.getByRole('tab', { name: 'Workflow' })).toBeVisible({ timeout: 30_000 })
  await expect(page.getByRole('tab', { name: 'Auditoria' })).toHaveCount(0)
  await page.goto(`/projects/${PROJECT_ID}/audit`)
  await expect(page.getByTestId('audit-forbidden')).toContainText(
    'Você não tem permissão para ver a trilha de auditoria.',
  )
})

test('CA-27-7 / CA-27-10: the administrator sees the organization trail with the project column', async ({
  page,
}) => {
  const api = signedIn()
  api.organizationAudit = [
    record(11, 'price.record', '2026-11-02T09:00:00-03:00', {
      target: { type: 'price', id: 'anthropic/model-a' },
      project: null,
      correlationid: undefined,
    }),
    record(12, 'project.delete', '2026-11-03T09:00:00-03:00', { reason: null }),
    record(13, 'session.login', '2026-11-04T09:00:00-03:00', {
      origin: 'local_client',
      target: { type: 'session', id: 'ana' },
      project: null,
    }),
    record(14, 'session.login_failed', '2026-11-04T09:05:00-03:00', {
      target: { type: 'session', id: 'ana' },
      project: null,
    }),
  ]
  await api.install(page)
  await page.goto('/settings/audit')
  const trail = page.getByTestId('audit-records')
  await expect(trail.locator('tbody tr')).toHaveCount(4, { timeout: 30_000 })
  await expect(trail.getByRole('columnheader', { name: 'Projeto' })).toBeVisible()
  await expect(trail).toContainText('Preço registrado (price.record)')
  await expect(trail).toContainText('Projeto excluído (project.delete)')
  await expect(trail).toContainText('cliente local')
  await expect(trail).toContainText('Falha de login (session.login_failed)')
  await expectNoSeriousA11yViolations(page)
})
