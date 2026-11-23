import { expect, test, type Page } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi } from './support/fakeApi'

const NATIVE = {
  id: 'sql-review',
  name: 'Revisão de SQL',
  description: 'Confere DDL e índices.',
  origin: 'native',
  stages: ['data'],
  agents: ['dba-agent'],
  current_version: 1,
  estimated_tokens: 1200,
  status: 'unblocked',
  versions: [{ version: 1, published_at: '2026-11-01T10:00:00-03:00', status: 'unblocked' }],
}

function signedIn(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  api.skills = [structuredClone(NATIVE)]
  return api
}

async function sendPackage(page: Page, name: string): Promise<void> {
  const section = page.getByTestId('skill-import')
  await section.locator('input[type="file"]').setInputFiles({
    name,
    mimeType: 'application/zip',
    buffer: Buffer.from('PK fake zip'),
  })
  await section.getByRole('button', { name: 'Enviar' }).click()
}

test('US-44 CA-1: a valid package is followed until the skill is in the library at version 1', async ({
  page,
}) => {
  // Two accessibility scans and several round trips: slow on the shared CI host.
  test.slow()
  const api = signedIn()
  await api.install(page)
  await page.goto('/settings/skills')
  await expect(page.getByRole('tab', { name: 'Biblioteca de skills' })).toHaveAttribute(
    'aria-selected',
    'true',
    { timeout: 30_000 },
  )
  await expect(page.getByTestId('native-skills')).toContainText('Revisão de SQL')
  await expect(page.getByText('Nenhuma skill da organização.')).toBeVisible()
  await expectNoSeriousA11yViolations(page)

  await sendPackage(page, 'lgpd.zip')
  await expect(page.getByTestId('skill-imports')).toContainText('Skill lgpd v1: liberada.', {
    timeout: 30_000,
  })
  const row = page.getByTestId('skill-lgpd')
  await expect(row).toContainText('Skill lgpd')
  await expect(row).toContainText('v1')
  await expect(row).toContainText('liberada')
  await expect(row).toContainText('enviada por pacote')
  const request = api.requests.find(
    (item) => item.method === 'POST' && item.path === '/skills/imports',
  )
  expect(request?.headers['content-type']).toContain('multipart/form-data')
  expect(request?.headers['idempotency-key']).toBeTruthy()
})

test('US-44 CA-2, CA-3: a package without SKILL.md or above the limit is refused with the reason', async ({
  page,
}) => {
  const api = signedIn()
  await api.install(page)
  await page.goto('/settings/skills')

  await sendPackage(page, 'missing.zip')
  const section = page.getByTestId('skill-import')
  const failure = section.getByTestId('skill-send-failure')
  await expect(failure).toContainText('Pacote inválido', { timeout: 30_000 })
  await expect(failure).toContainText('O pacote não tem SKILL.md na raiz.')

  await sendPackage(page, 'big.zip')
  await expect(failure).toContainText('Pacote acima do tamanho máximo.', { timeout: 30_000 })
  await expect(section.getByTestId('problem-limit')).toContainText('Limite: 2')
  expect(api.skills).toHaveLength(1)
})

test('US-44 CA-4: a suspicious skill stays blocked until an administrator releases it with a justification', async ({
  page,
}) => {
  // Two accessibility scans and several round trips: slow on the shared CI host.
  test.slow()
  const api = signedIn()
  await api.install(page)
  await page.goto('/settings/skills')

  // The git source: https only; as built, refused until the egress gateway exists (ADR-16).
  const section = page.getByTestId('skill-import')
  await section.getByRole('radio', { name: 'Repositório git' }).check({ timeout: 30_000 })
  await section
    .getByLabel('Endereço do repositório (https)')
    .fill('http://git.local/suspicious.git')
  await expect(section.getByText('Informe um endereço https.')).toBeVisible()
  await expect(section.getByRole('button', { name: 'Enviar' })).toBeDisabled()
  await section
    .getByLabel('Endereço do repositório (https)')
    .fill('https://git.empresa.local/skills/suspicious.git')
  await section.getByRole('button', { name: 'Enviar' }).click()
  await expect(section.getByTestId('git-unavailable')).toContainText('Envie a pasta compactada.', {
    timeout: 30_000,
  })

  await section.getByRole('radio', { name: 'Pasta compactada (zip)' }).check()
  await sendPackage(page, 'suspicious.zip')

  const row = page.getByTestId('skill-suspicious')
  await expect(row).toContainText('bloqueada')
  await expect(row).toContainText('Motivos: conteúdo suspeito')
  await expect(page.getByTestId('skill-imports')).toContainText('conteúdo suspeito')

  await row.getByRole('button', { name: 'Liberar a skill Skill suspicious' }).click()
  const dialog = page.getByTestId('skill-action-dialog')
  await expect(dialog).toContainText('bloqueada por conteúdo suspeito')
  const confirm = dialog.getByRole('button', { name: 'Liberar' })
  await dialog.getByLabel('Justificativa').fill('curto')
  await expect(confirm).toBeDisabled()
  await dialog.getByLabel('Justificativa').fill('Revisado pela segurança: sem instrução maliciosa.')
  await expectNoSeriousA11yViolations(page)
  await confirm.click()
  await expect(dialog).toBeHidden()
  await expect(row).toContainText('liberada')
  await expect(row).toContainText('Liberada por Dev')
})

test('ER-23-15: Settings, skill tab included, is only for the administrator', async ({ page }) => {
  const api = signedIn()
  api.user = { ...api.user, organization_roles: [] }
  await api.install(page)
  await page.goto('/settings/skills')
  await expect(page.getByRole('alert')).toContainText('Sem permissão')
  await expect(page.getByTestId('skill-import')).toHaveCount(0)
})
