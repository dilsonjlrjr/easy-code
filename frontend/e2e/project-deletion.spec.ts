import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, phaseOneWorkflow } from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'
const NAME = 'Reserva de salas'

function seed(state: string, roles: string[] = ['lead', 'approver']): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  api.addProject({
    project: {
      id: PROJECT_ID,
      name: NAME,
      state,
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      lead: { id: 'dev', name: 'Dev' },
      my_roles: roles,
      available_actions: [],
      last_sequence: 1,
    },
    workflow: phaseOneWorkflow('started'),
  })
  return api
}

test('US-04 CA-1, CA-3: the lead deletes a canceled project by typing its name', async ({
  page,
}) => {
  const api = seed('canceled')
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/configuration`)

  const section = page.getByTestId('project-deletion')
  await section.getByRole('button', { name: 'Excluir projeto' }).click()
  const dialog = page.getByRole('dialog', { name: `Excluir o projeto ${NAME}?` })
  // RN-1: the dialog says what happens to the data before anything is sent.
  await expect(dialog).toContainText('anonimizado em até 24 h')
  await expect(dialog).toContainText('até 30 dias')
  await expect(dialog.getByRole('button', { name: 'Manter o projeto' })).toBeFocused()
  await expectNoSeriousA11yViolations(page)

  // CA-29-16: a different name keeps the confirmation off.
  const confirm = dialog.getByRole('button', { name: 'Excluir definitivamente' })
  await dialog.getByLabel(`Digite o nome do projeto para confirmar: ${NAME}`).fill('Outro nome')
  await expect(confirm).toBeDisabled()
  await dialog.getByLabel(`Digite o nome do projeto para confirmar: ${NAME}`).fill(NAME)
  await confirm.click()

  // CA-1: back to the list, the project is gone; the notice speaks of the anonymization.
  await expect(page).toHaveURL(/\/projects\?notice=deleted$/)
  await expect(page.getByRole('alert').or(page.getByRole('status')).first()).toBeVisible()
  await expect(page.getByText('Projeto excluído.', { exact: false })).toBeVisible()
  await expect(page.getByText(NAME, { exact: true })).toHaveCount(0)
  const request = api.requests.find((item) => item.path.endsWith('/deletion'))
  expect(request?.body).toEqual({ name_confirmation: NAME })
  expect(request?.headers['idempotency-key']).toBeTruthy()

  // CA-3: the organization trail records the deletion with the id, no content.
  await page.goto('/settings/audit')
  await expect(page.getByTestId('audit-records')).toContainText(
    'Projeto excluído (project.delete)',
    {
      timeout: 30_000,
    },
  )
})

test('US-04 CA-2: a project in progress shows "cancel before deleting" and no action', async ({
  page,
}) => {
  const api = seed('in_progress')
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/configuration`)
  const section = page.getByTestId('project-deletion')
  await expect(section.getByTestId('deletion-cancel-first')).toContainText(
    'Cancele o projeto antes de excluir.',
  )
  await expect(section.getByRole('button', { name: 'Excluir projeto' })).toHaveCount(0)
})

test('ER-29-1: a member of a completed project sees no deletion', async ({ page }) => {
  const api = seed('completed', ['member'])
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/configuration`)
  await expect(page.getByTestId('gate-modes')).toBeVisible()
  await expect(page.getByTestId('project-deletion')).toHaveCount(0)
})
