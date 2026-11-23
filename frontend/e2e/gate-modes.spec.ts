import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, phaseOneWorkflow, type ProjectState } from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'

function seed(roles: string[] = ['lead', 'approver']): { api: FakeApi; state: ProjectState } {
  const api = new FakeApi()
  api.signedIn = true
  const state = api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      lead: { id: 'dev', name: 'Dev' },
      my_roles: roles,
      available_actions: [],
      last_sequence: 1,
    },
    workflow: phaseOneWorkflow('started'),
    configuration: {
      model_pair: null,
      gate_approval: {
        'gate.requirements': { mode: 'human', allowed: ['human'] },
        'gate.implementation': { mode: 'human', allowed: ['human', 'automatic'] },
        'gate.release': { mode: 'human', allowed: ['human'], always_human: true },
      },
      budget: null,
      artifact_language: 'pt-BR',
    },
  })
  return { api, state }
}

function patches(api: FakeApi): unknown[] {
  return api.requests
    .filter((request) => request.method === 'PATCH' && request.path.endsWith('/configuration'))
    .map((request) => request.body)
}

test('US-07 CA-3, CA-6, RN-3: the lead switches the Implementation gate; the Release has no choice', async ({
  page,
}) => {
  // Two accessibility scans and several round trips: slow on the shared CI host.
  test.slow()
  const { api } = seed()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/configuration`)

  const section = page.getByTestId('gate-modes')
  await expect(section.getByRole('heading', { name: 'Gates' })).toBeVisible({ timeout: 30_000 })
  // CA-3: the Release shows "always human" in text, with no control; so does Requirements.
  await expect(section.getByTestId('gate-mode-gate.release')).toContainText('Sempre humana')
  await expect(section.getByTestId('gate-mode-gate.release').getByRole('radio')).toHaveCount(0)
  await expect(section.getByTestId('gate-mode-gate.requirements').getByRole('radio')).toHaveCount(0)
  const implementation = section.getByTestId('gate-mode-gate.implementation')
  await expect(implementation.getByRole('radio', { name: 'Humana' })).toBeChecked()
  await expectNoSeriousA11yViolations(page)

  // CA-6: to automatic, only from the next round on; the dialog says it before saving.
  await implementation.getByRole('radio', { name: 'Automática (orquestrador)' }).click()
  const dialog = page.getByTestId('gate-mode-dialog')
  await expect(dialog).toContainText('a partir da próxima rodada', { timeout: 30_000 })
  await expectNoSeriousA11yViolations(page)
  await dialog.getByRole('button', { name: 'Mudar o modo' }).click()
  await expect(dialog).toBeHidden({ timeout: 30_000 })
  await expect(
    implementation.getByRole('radio', { name: 'Automática (orquestrador)' }),
  ).toBeChecked()
  expect(patches(api)).toEqual([{ gate_approval: { 'gate.implementation': 'automatic' } }])

  // CA-5: back to human applies at once, also to the open round.
  await implementation.getByRole('radio', { name: 'Humana' }).click()
  await expect(dialog).toContainText('vale na hora')
  await dialog.getByRole('button', { name: 'Manter o modo' }).click()
  await expect(
    implementation.getByRole('radio', { name: 'Automática (orquestrador)' }),
  ).toBeChecked()
  expect(patches(api)).toHaveLength(1)
})

test('US-07 CA-3: a refusal of the api is shown in the dialog', async ({ page }) => {
  const { api, state } = seed()
  // The api only allows `human` for this gate although the screen was read with both modes.
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/configuration`)
  const implementation = page.getByTestId('gate-mode-gate.implementation')
  await expect(implementation.getByRole('radio', { name: 'Humana' })).toBeChecked({
    timeout: 30_000,
  })
  const modes = state.configuration.gate_approval as Record<string, { allowed: string[] }>
  modes['gate.implementation'] = { ...modes['gate.implementation'], allowed: ['human'] }
  await implementation.getByRole('radio', { name: 'Automática (orquestrador)' }).click()
  const dialog = page.getByTestId('gate-mode-dialog')
  await dialog.getByRole('button', { name: 'Mudar o modo' }).click()
  await expect(dialog.getByRole('alert')).toContainText(
    'O modo automático só vale para os gates de Validação técnica e de Implementação.',
  )
})

test('US-07 CA-8, RN-4: a member sees each mode in text; the stage shows it and leads to the configuration', async ({
  page,
}) => {
  const { api } = seed(['member'])
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/configuration`)
  const section = page.getByTestId('gate-modes')
  await expect(section.getByTestId('gate-mode-gate.implementation')).toContainText(
    'Gate da etapa Implementação: Humana',
    { timeout: 30_000 },
  )
  await expect(section.getByRole('radio')).toHaveCount(0)

  await page.goto(`/projects/${PROJECT_ID}/stages/implementation`)
  await expect(page.getByTestId('stage-approval-mode')).toContainText(
    'Aprovação do gate: Humana.',
    {
      timeout: 30_000,
    },
  )
  await page.getByTestId('stage-approval-configuration').click()
  await expect(page).toHaveURL(new RegExp(`/projects/${PROJECT_ID}/configuration$`))
})
