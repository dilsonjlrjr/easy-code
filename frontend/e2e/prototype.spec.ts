import { expect, test, type Page } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, gateRound, type ProjectState } from './support/fakeApi'

const PROJECT_ID = '7c1d2e3f-4a5b-4c6d-8e7f-9a0b1c2d3e4f'

function workflow(designState: string) {
  const stage = (id: string, name: string, agent: string, dependsOn: string[], state: string) => ({
    id,
    name,
    short_name: name,
    type: 'stage',
    agent,
    depends_on: dependsOn,
    required_capabilities: [],
    state,
    gate: `gate.${id}`,
    active_waves: 0,
    current_round: id === 'design' ? 1 : undefined,
  })
  return {
    id: 'workflow-padrao',
    version: 1,
    mode: 'fixed',
    stages: [
      stage('requirements', 'Requisitos', 'requirements-agent', [], 'approved'),
      stage('architecture', 'Arquitetura', 'architect-agent', ['requirements'], 'approved'),
      stage('design', 'Design', 'designer-agent', ['architecture'], designState),
      stage('implementation', 'Implementação', 'developer-agent', ['design'], 'blocked'),
    ],
  }
}

const SCREENS = [
  {
    id: 'TL-1',
    nome: 'Login',
    arquivo: 'docs/design/prototipo/login.html',
    us: ['US-01'],
    criterios: ['US-01/CA-1'],
    estados: ['erro', 'carregando'],
  },
  {
    id: 'TL-2',
    nome: 'Guias da empresa',
    arquivo: 'docs/design/prototipo/guias.html',
    us: ['US-01', 'US-04'],
    criterios: ['US-01/CA-2', 'US-04/CA-1'],
    estados: ['vazio'],
  },
  {
    id: 'TL-3',
    nome: 'Detalhe da guia',
    arquivo: 'docs/design/prototipo/detalhe.html',
    us: ['US-02'],
    criterios: ['US-02/CA-1'],
    estados: ['erro'],
  },
]

function telas(version: number, state: string) {
  return {
    id: 'TELAS',
    type: 'TELAS',
    title: 'Mapa de telas',
    state,
    version,
    stage: 'design',
    author: { type: 'agent', agent: 'designer-agent' },
    wave: version === 1 ? 'DSG-01' : 'DSG-02',
    path: 'docs/design/TELAS.md',
    updated_at: '2026-11-24T10:00:00-03:00',
    header: {
      id: 'TELAS',
      tipo: 'TELAS',
      prototipo: 'docs/design/prototipo/',
      fluxo: 'docs/design/fluxo.md',
      estilo: 'docs/design/estilo.md',
      telas: version === 1 ? SCREENS.slice(0, 1) : SCREENS,
      acessibilidade: { nivel: 'wcag22_aa', verificado: true },
      lacunas_contrato: [],
    },
    body: '# Mapa de telas',
    commit: 'b'.repeat(40),
    attachments: [
      { path: 'fluxo.md', media_type: 'text/markdown', size_bytes: 120, sha256: 'c'.repeat(64) },
      {
        path: 'prototipo/login.html',
        media_type: 'text/html',
        size_bytes: 200,
        sha256: 'd'.repeat(64),
      },
    ],
  }
}

/** ESP-10 "Fluxo": fluxo.md is untrusted Markdown; the script must never run nor show as HTML. */
const FLOW = `# Fluxo de navegação

Login leva a **Guias da empresa**.<script>window.__flowRan = true</script>`

const page = (title: string, extra = '') =>
  `<!doctype html><html lang="pt-BR"><head><title>${title}</title></head><body><h1>${title}</h1>${extra}<a href="guias.html">Guias</a></body></html>`

/**
 * A malicious prototype (CA-11-2, CA-11-3, RNF-05): the script tries to reach the platform page, its
 * cookie and its api, to navigate the top window and to post a form; it writes what happened in itself.
 */
const MALICIOUS = `<!doctype html><html><head><title>Ataque</title></head><body>
<p id="result">rodando</p>
<form id="steal" action="http://localhost:4173/api/v1/forms" method="post"><input name="x" value="1"></form>
<a id="escape" href="http://localhost:4173/hacked" target="_top">sair</a>
<script>
(async () => {
  const out = []
  try { window.top.document.body.innerHTML = 'hacked'; out.push('top-open') } catch (e) { out.push('top-blocked') }
  try { parent.document.title = 'hacked'; out.push('parent-open') } catch (e) { out.push('parent-blocked') }
  try { out.push('cookie:' + document.cookie) } catch (e) { out.push('cookie-blocked') }
  try { localStorage.setItem('x', '1'); out.push('storage-open') } catch (e) { out.push('storage-blocked') }
  try { window.top.location.href = 'http://localhost:4173/hacked'; out.push('nav-open') } catch (e) { out.push('nav-blocked') }
  try { await fetch('http://localhost:4173/api/v1/stolen', { credentials: 'include' }); out.push('fetch-open') } catch (e) { out.push('fetch-blocked') }
  try { document.getElementById('steal').submit() } catch (e) { out.push('form-blocked') }
  try { document.getElementById('escape').click() } catch (e) { out.push('link-blocked') }
  document.getElementById('result').textContent = out.join(' ')
})()
</script></body></html>`

function seed(api: FakeApi, roles: string[], overrides: Partial<ProjectState> = {}): ProjectState {
  api.signedIn = true
  return api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Controle de DAS',
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      my_roles: roles,
      last_sequence: 4,
    },
    workflow: workflow('awaiting_gate'),
    artifacts: {
      TELAS: {
        current: telas(2, 'in_review'),
        versions: [2, 1].map((version) => ({
          version,
          data: `2026-11-2${String(version + 2)}`,
          wave: version === 1 ? 'DSG-01' : 'DSG-02',
          author: { type: 'agent', agent: 'designer-agent' },
          description: `Versão ${String(version)}`,
          approved: false,
        })),
        byVersion: { 1: telas(1, 'replaced'), 2: telas(2, 'in_review') },
      },
    },
    gates: {
      'gate.design': [
        gateRound('gate.design', 'design', 1, {
          outputs: [{ artifact: 'TELAS', version: 2, type: 'TELAS', state: 'in_review' }],
          checks: [],
        }),
      ],
    },
    attachmentContent: { 'TELAS:2:fluxo.md': FLOW, 'TELAS:1:fluxo.md': FLOW },
    prototype: {
      1: { 'prototipo/login.html': page('Login versão um') },
      2: {
        'prototipo/login.html': page('Login versão dois'),
        'prototipo/guias.html': page('Guias'),
      },
    },
    ...overrides,
  })
}

async function frameUrl(target: Page): Promise<URL> {
  return new URL((await target.getByTestId('prototype-iframe').getAttribute('src')) ?? '')
}

test('ESP-10 CA-10-9: the Design stage lists the screens of TELAS and opens one in the prototype', async ({
  page: browser,
}) => {
  const api = new FakeApi()
  seed(api, ['member'])
  await api.install(browser)

  await browser.goto(`/projects/${PROJECT_ID}/stages/design`)
  const table = browser.getByTestId('design-screens')
  await expect(table.getByRole('row')).toHaveCount(4)
  await expect(browser.getByTestId('design-screen-TL-2')).toContainText('Guias da empresa')
  await expect(browser.getByTestId('design-screen-TL-2')).toContainText('US-04/CA-1')
  await expect(browser.getByTestId('design-screen-TL-2')).toContainText('guias.html')
  await expect(browser.getByTestId('design-accessibility')).toContainText('verificada')
  // CA-10-12: no action that changes TELAS or sends the stage to the gate.
  await expect(browser.getByRole('button', { name: /Concluir|Pedir revisão/ })).toHaveCount(0)
  await browser.getByRole('tab', { name: /Fluxo/ }).click()
  await expect(browser.getByTestId('design-flow').getByRole('listitem')).toHaveCount(3)
  // ESP-10 "Fluxo": the content of fluxo.md through the sanitizer, not only its path.
  const flow = browser.getByTestId('design-flow-content')
  await expect(flow.getByRole('heading', { name: 'Fluxo de navegação' })).toBeVisible()
  await expect(flow.locator('strong')).toHaveText('Guias da empresa')
  await expect(flow.locator('script')).toHaveCount(0)
  expect(
    await browser.evaluate(() => (window as { __flowRan?: boolean }).__flowRan),
  ).toBeUndefined()
  await expectNoSeriousA11yViolations(browser)

  await browser.getByRole('tab', { name: /Telas/ }).click()
  await browser.getByTestId('design-open-TL-2').click()
  await expect(browser).toHaveURL(/\/artifacts\/TELAS\/prototype\?screen=TL-2/)
  await expect(browser.getByTestId('prototype-screen-TL-2')).toHaveAttribute('aria-pressed', 'true')
  await expect(
    browser.frameLocator('[data-testid="prototype-iframe"]').getByRole('heading'),
  ).toHaveText('Guias')
})

test('ESP-11 CA-11-1, CA-11-4 to CA-11-6, CA-11-10, CA-11-14: the viewer shows each screen isolated from the content origin', async ({
  page: browser,
}) => {
  const api = new FakeApi()
  seed(api, ['member'])
  await api.install(browser)

  await browser.goto(`/projects/${PROJECT_ID}/artifacts/TELAS/prototype`)
  await expect(browser.getByRole('heading', { level: 1 })).toHaveText('Protótipo navegável')
  await expect(browser.getByTestId('prototype-version')).toContainText('versão 2')
  const iframe = browser.getByTestId('prototype-iframe')
  // CA-11-1 / ER-11-1: sandbox="allow-scripts" only, the page on the content origin.
  await expect(iframe).toHaveAttribute('sandbox', 'allow-scripts')
  await expect(iframe).toHaveAttribute('title', 'Protótipo: TL-1 Login')
  expect((await frameUrl(browser)).origin).toBe(api.contentOrigin)
  const frame = browser.frameLocator('[data-testid="prototype-iframe"]')
  await expect(frame.getByRole('heading')).toHaveText('Login versão dois')
  // CA-11-6: "Esta tela atende" lists the stories and the criteria of the screen.
  await expect(browser.getByTestId('prototype-serves')).toHaveCount(1)
  await expect(browser.getByTestId('prototype-serves')).toContainText('US-01')
  await expect(browser.getByTestId('prototype-serves')).toContainText('CA-1')
  // CA-11-10: the new tab is the content origin URL without a reference to the platform window.
  const newTab = browser.getByTestId('prototype-new-tab')
  await expect(newTab).toHaveAttribute('rel', 'noopener noreferrer')
  expect(new URL((await newTab.getAttribute('href')) ?? '').origin).toBe(api.contentOrigin)

  // CA-11-14: the state goes in ?estado= of the same page.
  await browser.getByTestId('prototype-state-erro').click()
  await expect(browser.getByTestId('prototype-state-erro')).toHaveAttribute('aria-pressed', 'true')
  await expect.poll(async () => (await frameUrl(browser)).searchParams.get('estado')).toBe('erro')
  await browser.getByTestId('prototype-width-narrow').click()
  await expect(browser.getByTestId('prototype-frame')).toHaveCSS('width', '420px')
  await expectNoSeriousA11yViolations(browser)

  // CA-11-7: another screen asks for a new signed URL; CA-11-5: a screen without file in the version.
  const issued = api.requests.filter((request) => request.path.includes('/content-url')).length
  await browser.getByTestId('prototype-screen-TL-2').click()
  await expect(frame.getByRole('heading')).toHaveText('Guias')
  expect(api.requests.filter((request) => request.path.includes('/content-url'))).toHaveLength(
    issued + 1,
  )
  await expect(browser.getByRole('status').filter({ hasText: 'Tela TL-2' })).toBeAttached()
  await browser.getByTestId('prototype-screen-TL-3').click()
  await expect(browser.getByTestId('prototype-missing')).toContainText(
    'ainda não existe nesta versão',
  )
  await expect(browser.getByTestId('prototype-iframe')).toHaveCount(0)

  // CA-11-4: version 1 shows exactly the file of that version.
  await browser.getByTestId('prototype-version-select').selectOption('1')
  await expect(browser.getByTestId('prototype-version')).toContainText('versão 1')
  await expect(frame.getByRole('heading')).toHaveText('Login versão um')
})

test('ESP-11 CA-11-2, CA-11-3, CA-11-13 (ADR-19): the prototype content cannot reach the platform page', async ({
  page: browser,
  context,
}) => {
  const api = new FakeApi()
  seed(api, ['lead', 'approver'], {
    prototype: { 2: { 'prototipo/login.html': MALICIOUS, 'prototipo/guias.html': page('Guias') } },
  })
  await api.install(browser)
  await context.addCookies([
    { name: 'easycode_session', value: 'platform-secret', domain: 'localhost', path: '/' },
  ])

  await browser.goto(`/projects/${PROJECT_ID}/artifacts/TELAS/prototype`)
  const result = browser.frameLocator('[data-testid="prototype-iframe"]').locator('#result')
  await expect(result).toContainText('fetch-', { timeout: 15_000 })
  const outcome = (await result.textContent()) ?? ''
  for (const blocked of [
    'top-blocked',
    'parent-blocked',
    'cookie-blocked',
    'storage-blocked',
    'nav-blocked',
    'fetch-blocked',
  ])
    expect(outcome).toContain(blocked)
  expect(outcome).not.toContain('-open')

  // The platform page is untouched: same URL, same heading, nothing written by the frame.
  await expect(browser).toHaveURL(new RegExp(`/projects/${PROJECT_ID}/artifacts/TELAS/prototype`))
  await expect(browser.getByRole('heading', { level: 1 })).toHaveText('Protótipo navegável')
  expect(await browser.title()).not.toBe('hacked')
  // No api call left the frame, with or without the session; the content origin never got the cookie.
  expect(
    api.requests.filter((request) => /stolen|forms|hacked/.test(request.path)).map((r) => r.path),
  ).toEqual([])
  expect(api.contentRequests.length).toBeGreaterThan(0)
  expect(api.contentRequests.every((request) => !request.cookie?.includes('platform-secret'))).toBe(
    true,
  )
})

test('US-57 CA-1 to CA-5: the approver annotates the screens and sends them with the gate.design rejection', async ({
  page: browser,
}) => {
  const api = new FakeApi()
  seed(api, ['lead', 'approver'])
  await api.install(browser)

  await browser.goto(`/projects/${PROJECT_ID}/artifacts/TELAS/prototype?state=erro`)
  await expect(
    browser.frameLocator('[data-testid="prototype-iframe"]').getByRole('heading'),
  ).toHaveText('Login versão dois')
  await browser.getByTestId('prototype-annotate').click()
  await expect(browser.getByTestId('prototype-annotate')).toHaveAttribute('aria-pressed', 'true')

  // CA-1: a click on the frame marks a numbered pin and asks for the comment.
  const overlay = browser.getByTestId('prototype-overlay')
  await expect(overlay).toBeVisible()
  const box = (await overlay.boundingBox()) ?? { x: 0, y: 0, width: 0, height: 0 }
  await overlay.click({ position: { x: box.width * 0.25, y: box.height * 0.5 } })
  await browser.getByTestId('annotation-comment').fill('O botão de entrar precisa ser maior')
  await browser.getByTestId('annotation-save').click()
  await expect(
    browser.getByTestId('prototype-frame').locator('.pin').filter({ hasText: '1' }),
  ).toBeVisible()

  // CA-5: from the keyboard, an annotation without a point.
  await browser.getByTestId('annotation-without-point').focus()
  await browser.keyboard.press('Enter')
  await browser.getByTestId('annotation-comment').fill('Falta a mensagem de erro do SSO')
  await browser.keyboard.press('Tab')
  await browser.keyboard.press('Enter')
  const drafts = browser.getByTestId('annotation-drafts').getByRole('listitem')
  await expect(drafts).toHaveCount(2)
  await expect(drafts.nth(1)).toContainText('sem ponto')
  // CA-4: editing and deleting only change the draft; nothing went to the api yet.
  await browser.getByLabel('Editar o comentário da anotação 2').fill('Falta a mensagem de erro')
  expect(api.requests.some((request) => request.path.includes('/rejection'))).toBe(false)
  await expectNoSeriousA11yViolations(browser)

  // CA-2: "Enviar ao Designer" is the rejection of gate.design with the reason and the annotations.
  await browser.getByTestId('annotation-send').click()
  await browser.getByTestId('annotation-confirm').click()
  await expect(browser.getByRole('alert').filter({ hasText: /motivo/i })).toBeVisible()
  await browser.getByTestId('annotation-reason').fill('Ajustar o login conforme as anotações')
  await browser.getByTestId('annotation-confirm').click()
  await expect(browser.getByTestId('annotations-sent')).toContainText('rodada 1')

  const rejection = api.requests.find((request) => request.path.endsWith('/rounds/1/rejection'))
  const body = rejection?.body as {
    reason: string
    annotations: Array<Record<string, unknown>>
  }
  expect(rejection?.path).toContain('/gates/gate.design/')
  expect(rejection?.headers['x-csrf-token']).toBe('csrf-dev')
  expect(body.reason).toBe('Ajustar o login conforme as anotações')
  expect(body.annotations).toHaveLength(2)
  expect(body.annotations[0]).toMatchObject({
    screen: 'TL-1',
    state: 'erro',
    width: 'wide',
    comment: 'O botão de entrar precisa ser maior',
  })
  expect(body.annotations[0]?.x).toBeCloseTo(0.25, 1)
  expect(body.annotations[0]?.y).toBeCloseTo(0.5, 1)
  expect(body.annotations[1]).toMatchObject({
    x: null,
    y: null,
    comment: 'Falta a mensagem de erro',
  })

  // CA-3: the decided round shows its annotations read only.
  await expect(browser.getByTestId('annotations-recorded')).toContainText('Rodada 1')
  await expect(browser.getByTestId('annotations-recorded')).toContainText(
    'O botão de entrar precisa ser maior',
  )
  await expect(browser.getByTestId('prototype-annotate')).toHaveCount(0)
})

test('US-57 RN-3: a member does not annotate and reads the annotations of a decided round', async ({
  page: browser,
}) => {
  const api = new FakeApi()
  seed(api, ['member'], {
    gates: {
      'gate.design': [
        gateRound('gate.design', 'design', 1, {
          state: 'rejected',
          available_actions: [],
          outputs: [{ artifact: 'TELAS', version: 2, type: 'TELAS', state: 'draft' }],
          decision: {
            result: 'rejected',
            record: {
              per: { type: 'human', id: 'ana' },
              when: '2026-11-24T10:00:00-03:00',
              origin: 'web',
            },
            reason: 'Ajustar',
            annotations: [
              {
                number: 1,
                artifact: 'TELAS',
                version: 2,
                screen: 'TL-1',
                state: null,
                width: 'wide',
                x: 0.5,
                y: 0.4,
                comment: '<b>Texto</b> do aprovador',
              },
            ],
          },
        }),
      ],
    },
  })
  await api.install(browser)

  await browser.goto(`/projects/${PROJECT_ID}/artifacts/TELAS/prototype`)
  await expect(browser.getByTestId('annotations-recorded')).toContainText(
    '<b>Texto</b> do aprovador',
  )
  await expect(browser.getByTestId('prototype-annotate')).toHaveCount(0)
  await expect(browser.getByTestId('prototype-frame').locator('.pin-recorded')).toBeVisible()
  await expectNoSeriousA11yViolations(browser)
})

test('ESP-11 section 2: the gate.design round and the TELAS artifact open the prototype on their version', async ({
  page: browser,
}) => {
  const api = new FakeApi()
  seed(api, ['member'])
  await api.install(browser)

  await browser.goto(`/projects/${PROJECT_ID}/gates/gate.design/rounds/1`)
  const fromGate = browser.getByTestId('gate-open-prototype')
  await expect(fromGate).toHaveText('Abrir o protótipo')
  await fromGate.click()
  await expect(browser).toHaveURL(/\/artifacts\/TELAS\/prototype\?version=2$/)
  await expect(
    browser.frameLocator('[data-testid="prototype-iframe"]').getByRole('heading'),
  ).toHaveText('Login versão dois')

  // The TELAS page opens the prototype of the version it shows (here the older v1).
  await browser.goto(`/projects/${PROJECT_ID}/artifacts/TELAS?version=1`)
  await expect(browser.getByTestId('older-version')).toBeVisible()
  await browser.getByTestId('artifact-open-prototype').click()
  await expect(browser).toHaveURL(/\/artifacts\/TELAS\/prototype\?version=1$/)
  await expect(
    browser.frameLocator('[data-testid="prototype-iframe"]').getByRole('heading'),
  ).toHaveText('Login versão um')
  expect(api.cspViolations).toEqual([])
})

test('ADR-19, ER-11-2: the SPA runs under its Content-Security-Policy and frames only the content origin', async ({
  page: browser,
}) => {
  const api = new FakeApi()
  seed(api, ['member'])
  await api.install(browser)

  const response = await browser.goto(`/projects/${PROJECT_ID}/artifacts/TELAS/prototype`)
  const policy = response?.headers()['content-security-policy'] ?? ''
  expect(policy).toContain(`frame-src ${api.contentOrigin};`)
  expect(policy).toContain("frame-ancestors 'none'")
  expect(policy).toMatch(/script-src 'nonce-[^']+' 'strict-dynamic'/)
  await expect(
    browser.frameLocator('[data-testid="prototype-iframe"]').getByRole('heading'),
  ).toHaveText('Login versão dois')

  // A frame of any other origin is refused by the policy.
  await browser.evaluate(() => {
    const frame = document.createElement('iframe')
    frame.src = 'https://elsewhere.example/'
    document.body.append(frame)
  })
  await expect
    .poll(() => api.cspViolations.join('\n'))
    .toContain('frame-src https://elsewhere.example')
  expect(api.cspViolations).toHaveLength(1)
})
