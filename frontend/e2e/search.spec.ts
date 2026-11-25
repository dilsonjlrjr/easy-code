import { expect, test, type Page } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, finding, phaseOneWorkflow } from './support/fakeApi'

const ROOMS = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a61'
const PORTAL = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a62'
const HIDDEN = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a63'

function project(id: string, name: string, roles: string[]) {
  return {
    id,
    name,
    state: 'in_progress',
    workflow: 'workflow-padrao',
    created_at: '2026-11-22T10:00:00-03:00',
    created_by: { id: 'dev', name: 'Dev' },
    my_roles: roles,
    available_actions: [],
    last_sequence: 1,
  }
}

function artifact(id: string, type: string, title: string) {
  const current = {
    id,
    type,
    title,
    state: 'approved',
    version: 1,
    stage: 'requirements',
    author: { type: 'agent', agent: 'requirements-agent' },
    wave: 'REQ-01',
    path: `docs/requisitos/${id}.md`,
    updated_at: '2026-11-22T10:00:00-03:00',
    header: { id, titulo: title },
    body: title,
    commit: 'a'.repeat(40),
  }
  return { current, versions: [], byVersion: { 1: current } }
}

function apiWithProjects(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  api.addProject({
    project: project(ROOMS, 'Reserva de salas', ['lead']),
    workflow: phaseOneWorkflow('started'),
    artifacts: { 'US-12': artifact('US-12', 'US', 'Reservar sala sem conflito') },
    findings: [
      finding('ACH-07', { title: 'Reserva aceita horário passado', severity: 'critical' }),
    ],
  })
  api.addProject({
    project: project(PORTAL, 'Portal RH', ['member']),
    workflow: phaseOneWorkflow('started'),
    artifacts: { 'ADR-03': artifact('ADR-03', 'ADR', 'Fila de reservas no PostgreSQL') },
  })
  // RN-1 in the fake: a project without a role never answers.
  api.addProject({ project: project(HIDDEN, 'Reservas secretas', []) })
  return api
}

function searchRequests(api: FakeApi) {
  return api.requests.filter((request) => /\/search\?/.test(request.path))
}

async function openSearch(page: Page) {
  await page.goto('/')
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
  await page.keyboard.press('/')
  const field = page.getByRole('searchbox', { name: 'Buscar' })
  await expect(field).toBeFocused()
  return field
}

test('US-56 CA-2 / CA-3 / CA-5 / RN-3: grouped results with the keyboard, the count announced, "/" focuses', async ({
  page,
}) => {
  const api = apiWithProjects()
  await api.install(page)
  const field = await openSearch(page)
  await expect(field).toHaveAttribute('placeholder', 'Buscar projetos, artefatos, achados…')

  // CA-3: one character is not sent; the field says what is missing.
  await field.pressSequentially('r')
  const panel = page.getByTestId('header-search-panel')
  await expect(panel).toContainText('Digite mais 1 caractere para buscar.')
  await expect(field).toHaveAccessibleDescription(/Digite mais 1 caractere para buscar\./)

  await field.pressSequentially('eserv')
  await expect(page.getByTestId('header-search-projects')).toContainText('Projetos (1)')
  // Nothing went for "r" alone (CA-3), and the last request is the whole word. The debounce usually sends only
  // that one; on a slow host typing can pause past 250 ms and send a partial term first, so the count is not fixed.
  const sent = searchRequests(api).map((request) => request.path)
  expect(sent.some((path) => path.endsWith('search=r'))).toBe(false)
  expect(sent.at(-1)).toBe('/search?search=reserv')
  await expect(page.getByTestId('header-search-projects').getByRole('link')).toHaveText([
    /Reserva de salas\s*Em andamento/,
    'Ver todos os projetos',
  ])
  const artifacts = page.getByTestId('header-search-artifacts')
  await expect(artifacts).toContainText('Artefatos (2)')
  await expect(artifacts.getByRole('link').first()).toContainText(
    'US-12 Reservar sala sem conflito',
  )
  await expect(artifacts.getByRole('link').first()).toContainText('Reserva de salas')
  await expect(artifacts.getByRole('link', { name: 'Ver todos os artefatos' })).toHaveAttribute(
    'href',
    '/projects',
  )
  const findings = page.getByTestId('header-search-findings')
  await expect(findings.getByRole('link').first()).toContainText(
    'ACH-07 Reserva aceita horário passado',
  )
  await expect(findings.getByRole('link').first()).toContainText('Crítico')
  // Every finding is in one project: "see all" opens its finding list.
  await expect(findings.getByRole('link', { name: 'Ver todos os achados' })).toHaveAttribute(
    'href',
    `/projects/${ROOMS}/findings`,
  )
  await expect(panel).not.toContainText('Reservas secretas')
  await expect(page.getByRole('status').filter({ hasText: '4 resultados' })).toHaveCount(1)
  await expect(field).toBeFocused()
  await expectNoSeriousA11yViolations(page)

  // CA-5: the arrows walk the results (up from the first goes back to the field), Enter opens one.
  await page.keyboard.press('ArrowDown')
  await expect(panel.getByRole('link', { name: /Reserva de salas\s*Em andamento/ })).toBeFocused()
  await page.keyboard.press('ArrowUp')
  await expect(field).toBeFocused()
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('ArrowDown')
  await expect(artifacts.getByRole('link').first()).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(field).toBeFocused()
  await expect(panel).toBeHidden()
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('End')
  await expect(findings.getByRole('link', { name: 'Ver todos os achados' })).toBeFocused()
  await page.keyboard.press('Home')
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('ArrowDown')
  await expect(artifacts.getByRole('link').first()).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(new RegExp(`/projects/${ROOMS}/artifacts/US-12$`))
  await expect(panel).toBeHidden()
  await expect(field).toHaveValue('')
})

test('US-56 CA-2: a finding by its id opens the finding', async ({ page }) => {
  const api = apiWithProjects()
  await api.install(page)
  const field = await openSearch(page)
  await field.fill('ach-07')
  const findings = page.getByTestId('header-search-findings')
  await expect(findings).toContainText('Achados (1)')
  await expect(page.getByTestId('header-search-projects')).toHaveCount(0)
  await findings.getByRole('link').first().click()
  await expect(page).toHaveURL(new RegExp(`/projects/${ROOMS}/findings/ACH-07$`))
})

test('US-56 CA-4: no result says so and offers the project list', async ({ page }) => {
  const api = apiWithProjects()
  await api.install(page)
  const field = await openSearch(page)
  await field.fill('zzz')
  const panel = page.getByTestId('header-search-panel')
  await expect(panel).toContainText('Nenhum resultado para zzz')
  await expect(
    page.getByRole('status').filter({ hasText: 'Nenhum resultado para zzz' }),
  ).toHaveCount(1)
  await expectNoSeriousA11yViolations(page)
  await panel.getByRole('link', { name: 'Abrir a lista de projetos' }).click()
  await expect(page).toHaveURL(/\/projects$/)
})

test('US-56: a failed search says so and can be retried', async ({ page }) => {
  const api = apiWithProjects()
  api.searchFailure = true
  await api.install(page)
  const field = await openSearch(page)
  await field.fill('portal')
  const panel = page.getByTestId('header-search-panel')
  await expect(panel).toContainText('Não foi possível buscar agora.')
  api.searchFailure = false
  await panel.getByRole('button', { name: 'Tentar de novo' }).click()
  await expect(page.getByTestId('header-search-projects')).toContainText('Portal RH')
})
