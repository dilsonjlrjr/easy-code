import { expect, test, type Page } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, type FakeUser } from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a20'

function apiWithProject(myRoles: string[], user?: FakeUser): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  if (user) api.user = user
  api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state: 'created',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      lead: { id: 'dev', name: 'Dev' },
      my_roles: myRoles,
      available_actions: [],
      last_sequence: 1,
    },
  })
  return api
}

async function openMembers(page: Page): Promise<void> {
  await page.goto(`/projects/${PROJECT_ID}/members`)
  await expect(page.getByRole('heading', { level: 2, name: 'Pessoas e papéis' })).toBeVisible()
}

test('US-02 CA-1 / CA-26-1: the lead adds Ana as approver and the list shows it', async ({
  page,
}) => {
  const api = apiWithProject(['lead', 'approver'])
  await api.install(page)
  await openMembers(page)
  await expect(page.getByTestId('member-dev')).toContainText('Responsável, Aprovador')
  await expectNoSeriousA11yViolations(page)

  await page.getByRole('button', { name: 'Adicionar pessoa' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Buscar pessoa por nome ou e-mail').fill('Ana')
  await dialog.getByRole('radio', { name: 'Ana Souza' }).check()
  await dialog.getByRole('checkbox', { name: 'Membro' }).uncheck()
  await dialog.getByRole('checkbox', { name: 'Aprovador' }).check()
  await expectNoSeriousA11yViolations(page)
  await dialog.getByRole('button', { name: 'Adicionar', exact: true }).click()

  await expect(page.getByTestId('member-ana')).toContainText('Aprovador')
  await expect(page.getByTestId('member-ana')).toContainText('por Dev')
  await expect(page.getByRole('status').filter({ hasText: 'Ana Souza adicionada' })).toBeAttached()
  const put = api.requests.find(
    (request) => request.method === 'PUT' && request.path.endsWith('/members/ana'),
  )
  expect(put?.body).toEqual({ roles: ['approver'] })
})

test('US-02 CA-4: removing the role of the only approver is refused with the reason', async ({
  page,
}) => {
  const api = apiWithProject(['lead', 'approver'])
  api.projects[PROJECT_ID]?.members.push({
    user: { id: 'ana', name: 'Ana Souza' },
    roles: ['member'],
    assigned_by: { id: 'dev', name: 'Dev' },
    assigned_at: '2026-11-21T10:00:00-03:00',
  })
  await api.install(page)
  await openMembers(page)

  await page.getByRole('button', { name: 'Alterar papéis de Dev' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('checkbox', { name: 'Aprovador' }).uncheck()
  await dialog.getByRole('checkbox', { name: 'Membro' }).check()
  await dialog.getByRole('button', { name: 'Salvar' }).click()

  await expect(page.getByTestId('members-failure')).toContainText(
    'O projeto precisa de ao menos um aprovador.',
  )
  await expect(page.getByTestId('member-dev')).toContainText('Aprovador')

  // CA-26-10: the lead cannot remove themselves; the message says to ask for the transfer.
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: 'Remover Dev do projeto' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Remover' }).click()
  await expect(page.getByTestId('members-failure')).toContainText('transferir o papel')
})

test('CA-26-2 / CA-26-8: a member sees the list without actions, with the 24 h grace of a self-assigned approver', async ({
  page,
}) => {
  const api = apiWithProject(['member'], {
    id: 'carla',
    name: 'Carla',
    organization_roles: [],
  })
  api.projects[PROJECT_ID]?.members.push({
    user: { id: 'ana', name: 'Ana Souza' },
    roles: ['approver'],
    assigned_by: { id: 'ana', name: 'Ana Souza' },
    assigned_at: new Date().toISOString(),
  })
  await api.install(page)
  await openMembers(page)

  await expect(page.getByTestId('member-ana')).toContainText('não decide gate até')
  await expect(page.getByRole('button', { name: 'Adicionar pessoa' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /Alterar papéis|Remover|Transferir/ })).toHaveCount(
    0,
  )
  await expectNoSeriousA11yViolations(page)
})

test('US-02 CA-5 / CA-6: the administrator lists the organization projects and transfers the lead to Bruno', async ({
  page,
}) => {
  const api = apiWithProject([], {
    id: 'admin',
    name: 'Admin',
    organization_roles: ['administrator'],
  })
  api.projects[PROJECT_ID]?.members.push({
    user: { id: 'bruno', name: 'Bruno Lima' },
    roles: ['approver'],
    assigned_by: { id: 'dev', name: 'Dev' },
    assigned_at: '2026-11-21T10:00:00-03:00',
  })
  await api.install(page)

  await page.goto('/projects')
  await page.getByRole('button', { name: 'Projetos da organização' }).click()
  await expect(page.getByRole('button', { name: 'Projetos da organização' })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await expect
    .poll(() => api.requests.some((request) => request.path.includes('scope=organization')))
    .toBe(true)

  await openMembers(page)
  await expect(page.getByTestId('administrator-view')).toBeVisible()
  // CA-26-12: the administrator does not assign approver or member, only transfers the lead.
  await expect(page.getByRole('button', { name: 'Adicionar pessoa' })).toHaveCount(0)
  await page.getByRole('button', { name: 'Transferir responsável para Bruno Lima' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText('Dev deixa de ser responsável e Bruno Lima passa a ser')
  await expectNoSeriousA11yViolations(page)
  await dialog.getByRole('button', { name: 'Transferir responsável' }).click()

  await expect(page.getByTestId('member-bruno')).toContainText('Responsável, Aprovador')
  await expect(page.getByTestId('member-dev')).not.toContainText('Responsável')
})
