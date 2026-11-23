import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, phaseOneWorkflow, type ProjectState } from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'

function artifact(state: string) {
  return {
    id: 'US-01',
    type: 'US',
    title: 'Reservar sala',
    state,
    version: 2,
    stage: 'requirements',
    author: { type: 'agent', agent: 'requirements-agent' },
    wave: 'REQ-01',
    header: { ator: 'Colaborador' },
    body: 'Corpo da história.',
    commit: null,
  }
}

function seed(state = 'in_review', roles = ['member']): { api: FakeApi; project: ProjectState } {
  const api = new FakeApi()
  api.signedIn = true
  const current = artifact(state)
  const project = api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      my_roles: roles,
      last_sequence: 1,
    },
    workflow: phaseOneWorkflow('started'),
    artifacts: {
      'US-01': {
        current,
        versions: [1, 2].map((version) => ({
          version,
          data: '2026-11-22',
          wave: 'REQ-01',
          author: { type: 'agent', agent: 'requirements-agent' },
          description: `Versão ${String(version)}`,
        })),
        byVersion: { 1: { ...current, version: 1 }, 2: current },
      },
    },
  })
  return { api, project }
}

test('US-53 CA-1: a member edits an item; the new version waits for the agent validation', async ({
  page,
}) => {
  const { api } = seed()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/artifacts/US-01`)

  await page.getByTestId('edit-artifact').click()
  const dialog = page.getByTestId('artifact-edit-dialog')
  await expect(dialog).toContainText('O agente da etapa valida e formata a edição')
  const content = dialog.getByLabel('Conteúdo (Markdown com cabeçalho YAML)')
  await expect(content).toHaveValue(/titulo: Reservar sala/)
  await expectNoSeriousA11yViolations(page)
  const send = dialog.getByRole('button', { name: 'Enviar para validação' })
  await expect(send).toBeDisabled()
  await content.fill('---\nid: US-01\ntitulo: Reservar sala\n---\nCorpo editado pela pessoa.')
  await dialog.getByLabel('Motivo da mudança').fill('Ajuste pedido pela área de facilities.')
  await send.click()

  await expect(dialog).toBeHidden()
  await expect(page.getByTestId('artifact-edit-notice')).toContainText(
    'aguarda a validação do agente',
  )
  await expect(page.getByTestId('artifact-body')).toContainText('Corpo editado pela pessoa.')
  await expect(page.getByText('editado por Dev, aguardando validação do agente')).toBeVisible()
  await expect(page.getByTestId('version-list').locator('li')).toHaveCount(3)
  const request = api.requests.find((item) => item.path.endsWith('/artifact-edits'))
  expect(request?.body).toMatchObject({
    operation: 'modify',
    artifact: 'US-01',
    base_version: 2,
    reason: 'Ajuste pedido pela área de facilities.',
  })
  expect(request?.headers['idempotency-key']).toBeTruthy()
})

test('US-53 CA-2: a refused edit keeps the text typed and shows the reason', async ({ page }) => {
  const { api } = seed()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/artifacts/US-01`)
  await page.getByTestId('edit-artifact').click()
  const dialog = page.getByTestId('artifact-edit-dialog')
  const content = dialog.getByLabel('Conteúdo (Markdown com cabeçalho YAML)')
  await content.fill('---\nid: US-01\n---\nONDA-ATIVA texto da pessoa')
  await dialog.getByLabel('Motivo da mudança').fill('Correção.')
  await dialog.getByRole('button', { name: 'Enviar para validação' }).click()
  await expect(dialog.getByRole('alert')).toContainText('A ação não vale no estado atual.')
  await expect(dialog).toContainText('Seu texto foi mantido')
  await expect(content).toHaveValue(/ONDA-ATIVA texto da pessoa/)
})

test('US-53 CA-4: removing an item is a new version; no earlier version is deleted', async ({
  page,
}) => {
  const { api } = seed('draft', ['lead'])
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/artifacts/US-01`)
  await page.getByTestId('delete-artifact').click()
  const dialog = page.getByTestId('artifact-edit-dialog')
  await expect(dialog).toContainText('nenhuma versão anterior é apagada')
  await dialog.getByLabel('Motivo da mudança').fill('História fora do escopo.')
  await dialog.getByRole('button', { name: 'Excluir item' }).click()
  await expect(dialog).toBeHidden()
  await expect(page.getByTestId('version-list').locator('li')).toHaveCount(3)
  const request = api.requests.find((item) => item.path.endsWith('/artifact-edits'))
  expect(request?.body).toEqual({
    operation: 'delete',
    artifact: 'US-01',
    base_version: 2,
    reason: 'História fora do escopo.',
  })
})

test('US-53 CA-6: an approved artifact offers no edit', async ({ page }) => {
  const { api } = seed('approved')
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/artifacts/US-01`)
  await expect(page.getByTestId('artifact-approved-notice')).toContainText(
    'só muda por reabertura via achado',
  )
  await expect(page.getByTestId('edit-artifact')).toHaveCount(0)
})

test('US-53 CA-7: an approved artifact opened by a scope change is edited with its finding', async ({
  page,
}) => {
  const { api } = seed('approved', ['approver'])
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/artifacts/US-01?finding=ACH-04`)
  await page.getByTestId('edit-artifact').click()
  const dialog = page.getByTestId('artifact-edit-dialog')
  await expect(dialog).toContainText('mudança de escopo do achado ACH-04')
  await dialog.getByLabel('Motivo da mudança').fill('Mudança de escopo decidida.')
  await dialog.getByRole('button', { name: 'Enviar para validação' }).click()
  await expect(dialog).toBeHidden()
  const request = api.requests.find((item) => item.path.endsWith('/artifact-edits'))
  expect(request?.body).toMatchObject({ operation: 'modify', finding: 'ACH-04' })
})

test('US-53 CA-5: a member records an imposed ADR citing a project constraint', async ({
  page,
}) => {
  const { api, project } = seed()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/artifacts`)
  await page.getByTestId('create-artifact').click()
  const dialog = page.getByTestId('artifact-edit-dialog')
  await dialog.getByRole('combobox', { name: 'Tipo' }).click()
  await page.getByRole('option', { name: 'ADR imposta (decisão já tomada)' }).click()
  await expect(dialog).toContainText('A ADR imposta não compara opções')
  const content = dialog.getByLabel('Conteúdo (Markdown com cabeçalho YAML)')
  await expect(content).toHaveValue(/natureza: imposta/)
  await expect(content).toHaveValue(/- RESTR-01/)
  await expectNoSeriousA11yViolations(page)
  const text = (await content.inputValue()).replace('titulo: ', 'titulo: Banco PostgreSQL 16')
  await content.fill(text)
  await dialog.getByLabel('Motivo da mudança').fill('Norma de infraestrutura da empresa.')
  await dialog.getByRole('button', { name: 'Enviar para validação' }).click()
  await expect(dialog).toBeHidden()
  await expect(page.getByTestId('artifact-edit-notice')).toBeVisible()
  await expect(page.getByRole('link', { name: 'ADR-01' })).toBeVisible()
  expect(project.artifacts['ADR-01']?.current.author).toMatchObject({ type: 'human' })

  await page.getByRole('link', { name: 'ADR-01' }).click()
  await expect(page.getByTestId('imposed-adr')).toContainText('ADR imposta por Dev.')
})
