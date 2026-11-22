import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, phaseOneWorkflow, waveDetail } from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'

function artifactVersion(version: number, body: string) {
  return {
    id: 'US-01',
    type: 'US',
    title: 'Reservar sala',
    state: 'draft',
    version,
    stage: 'requirements',
    author: { type: 'agent', agent: 'requirements-agent' },
    wave: version === 1 ? 'REQ-01' : 'REQ-02',
    path: 'docs/requisitos/US-01.md',
    updated_at: '2026-11-22T10:00:00-03:00',
    header: {
      id: 'US-01',
      titulo: 'Reservar sala',
      prioridade: 'must',
      criterios_aceite: [{ id: 'CA-1' }],
    },
    body,
    commit: 'a'.repeat(40),
  }
}

test('US-26 CA-1/CA-2/CA-3: the artifact shows its current version, the exact version 1 and the wave that produced it', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  const current = artifactVersion(
    3,
    '# Reservar sala\n\nVersão **três**.\n\n<script>window.hacked = true</script>',
  )
  api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      my_roles: ['lead'],
      last_sequence: 9,
    },
    workflow: phaseOneWorkflow('started'),
    waveDetails: {
      'REQ-01': waveDetail('REQ-01', 'completed'),
      'REQ-02': waveDetail('REQ-02', 'completed'),
    },
    artifacts: {
      'US-01': {
        current,
        versions: [1, 2, 3].map((version) => ({
          version,
          data: `2026-11-2${String(version)}`,
          wave: version === 1 ? 'REQ-01' : 'REQ-02',
          author: { type: 'agent', agent: 'requirements-agent' },
          description: `Versão ${String(version)} da história`,
          approved: false,
        })),
        byVersion: {
          1: artifactVersion(1, 'Texto exato da versão um.'),
          2: artifactVersion(2, 'Versão dois.'),
          3: current,
        },
      },
    },
  })
  await api.install(page)

  await page.goto(`/projects/${PROJECT_ID}/artifacts`)
  await page.getByRole('link', { name: 'US-01' }).click()

  await expect(page.getByRole('heading', { level: 1 })).toContainText('Reservar sala')
  await expect(page.getByTestId('artifact-body')).toContainText('Versão três.')
  await expect(page.getByTestId('artifact-body')).toContainText('<script>')
  expect(
    await page.evaluate(() => (window as unknown as { hacked?: boolean }).hacked),
  ).toBeUndefined()
  const versions = page.getByTestId('version-list')
  await expect(versions.locator('li')).toHaveCount(3)
  await expect(versions).toContainText('Versão 1 da história')
  await expect(page.getByText('criterios_aceite')).toBeVisible()
  await expectNoSeriousA11yViolations(page)

  await versions.getByRole('button', { name: 'Versão 1', exact: true }).click()
  await expect(page.getByTestId('older-version')).toContainText('versão 1 de 3')
  await expect(page.getByTestId('artifact-body')).toHaveText('Texto exato da versão um.')

  await page.getByTestId('origin-wave').click()
  await expect(page).toHaveURL(new RegExp(`/projects/${PROJECT_ID}/waves/REQ-01$`))
})
