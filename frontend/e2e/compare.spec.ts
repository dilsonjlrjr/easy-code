import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, phaseOneWorkflow, waveDetail } from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'

function version(number: number, body: string) {
  return {
    id: 'US-01',
    type: 'US',
    title: 'Reservar sala',
    state: 'in_review',
    version: number,
    stage: 'requirements',
    author: { type: 'agent', agent: 'requirements-agent' },
    wave: `REQ-0${String(number)}`,
    path: 'docs/requisitos/US-01.md',
    header: { id: 'US-01', titulo: 'Reservar sala' },
    body,
    commit: null,
  }
}

function seed(api: FakeApi, versions = [1, 2, 3]) {
  const current = version(versions.length, 'Versão atual.')
  api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      my_roles: ['approver'],
      last_sequence: 9,
    },
    workflow: phaseOneWorkflow('started'),
    waveDetails: { 'REQ-01': waveDetail('REQ-01', 'completed') },
    artifacts: {
      'US-01': {
        current,
        versions: versions.map((number) => ({
          version: number,
          data: `2026-11-2${String(number)}`,
          wave: `REQ-0${String(number)}`,
          author: { type: 'agent', agent: 'requirements-agent' },
          description: number === 3 ? 'Retrabalho do ACH-04' : `Versão ${String(number)}`,
          finding: number === 3 ? 'ACH-04' : null,
          approved: false,
        })),
        byVersion: Object.fromEntries(versions.map((number) => [number, version(number, 'x')])),
      },
    },
    comparisons: {
      'US-01:2:3': {
        artifact: 'US-01',
        from: 2,
        to: 3,
        header: [
          { path: '/titulo', operation: 'modified', before: 'Reservar', after: 'Reservar sala' },
          { path: '/vinculos/0', operation: 'added', after: 'US-02' },
        ],
        body: [
          { operation: 'equal', line_from: 1, line_to: 1, text: '# Reservar sala' },
          { operation: 'removed', line_from: 2, line_to: null, text: 'Linha antiga.' },
          {
            operation: 'added',
            line_from: null,
            line_to: 2,
            text: 'Linha nova <script>x</script>',
          },
        ],
        driving_findings: ['ACH-04'],
      },
      'US-01:1:3': {
        artifact: 'US-01',
        from: 1,
        to: 3,
        header: [{ path: '/rnf/0', operation: 'removed', before: 'RNF-09' }],
        body: [{ operation: 'added', line_from: null, line_to: 5, text: 'Desde a primeira.' }],
        driving_findings: [],
      },
    },
  })
}

test('US-27 CA-1/CA-2: comparing two versions marks header fields and body lines, with the driving finding', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  seed(api)
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/artifacts/US-01`)

  const panel = page.getByTestId('artifact-comparison')
  await expect(panel.getByRole('button', { name: 'Comparar' })).toBeEnabled({ timeout: 30_000 })
  // ER-21-7: the history shows the finding behind the rework version, with a link.
  await expect(page.getByTestId('version-finding-3')).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/findings/ACH-04`,
  )

  await panel.getByRole('button', { name: 'Comparar' }).click()
  await expect(page).toHaveURL(/from=2&to=3/)
  await expect(page.getByTestId('comparison-summary')).toContainText(
    'Versão 2 → versão 3: 2 adicionados, 1 removidos, 1 alterados.',
  )
  const header = page.getByTestId('comparison-header')
  await expect(header.locator('[data-operation="modified"]')).toContainText('alterado')
  await expect(header.locator('[data-operation="modified"]')).toContainText('Reservar sala')
  await expect(header.locator('[data-operation="added"]')).toContainText('adicionado')
  const body = page.getByTestId('comparison-body')
  await expect(body.locator('[data-operation="removed"]')).toContainText('Linha antiga.')
  // RNF-05: agent text in a line is shown, never run.
  await expect(body.locator('[data-operation="added"]')).toContainText('<script>x</script>')
  // CA-2: the finding that drove the version, with a link.
  await expect(
    page.getByTestId('comparison-findings').getByRole('link', { name: 'ACH-04' }),
  ).toHaveAttribute('href', `/projects/${PROJECT_ID}/findings/ACH-04`)
  await expectNoSeriousA11yViolations(page)

  // CA-21-8: any two versions, not only neighbours.
  await page.goto(`/projects/${PROJECT_ID}/artifacts/US-01?from=1&to=3`)
  await expect(page.getByTestId('comparison-summary')).toContainText(
    'Versão 1 → versão 3: 1 adicionados, 1 removidos, 0 alterados.',
    { timeout: 30_000 },
  )
  await page.getByRole('button', { name: 'Ocultar diferenças' }).click()
  await expect(page.getByTestId('comparison-summary')).toHaveCount(0)
  await expect(page).not.toHaveURL(/from=/)
})

test('ESP-21 6: with a single version the comparison is unavailable, with the reason', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  seed(api, [1])
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/artifacts/US-01`)
  await expect(page.getByTestId('comparison-unavailable')).toContainText(
    'Comparação indisponível: o artefato tem uma única versão.',
    { timeout: 30_000 },
  )
})
