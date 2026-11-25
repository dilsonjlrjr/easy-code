import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, gateRound, phaseOneWorkflow, waveSummary } from './support/fakeApi'

const PROJECT_ID = '7c1d2e3f-4a5b-4c6d-8e9f-0a1b2c3d4e5f'
const LOAD_TIMEOUT = 30_000

function artifact(
  id: string,
  type: string,
  stage: string,
  header: Record<string, unknown>,
  extra: Record<string, unknown> = {},
) {
  const current = {
    id,
    type,
    title: `${type} ${id}`,
    state: 'in_review',
    version: 1,
    stage,
    author: {
      type: 'agent',
      agent: stage === 'architecture' ? 'architect-agent' : 'requirements-agent',
    },
    wave: stage === 'architecture' ? 'ARQ-01' : 'REQ-01',
    path: `docs/${id}.md`,
    updated_at: '2026-11-22T10:00:00-03:00',
    header: { id, tipo: type, versao: 1, ...header },
    situation: null,
    body: '# Corpo\n',
    commit: 'a'.repeat(40),
    attachments: [],
    ...extra,
  }
  return {
    current,
    versions: [{ version: 1, data: '2026-11-22', wave: current.wave, description: 'criação' }],
    byVersion: { 1: current },
  }
}

function seed(api: FakeApi) {
  const workflow = phaseOneWorkflow('started') as { stages: Array<Record<string, unknown>> }
  const architecture = workflow.stages.find((stage) => stage.id === 'architecture')
  if (architecture) Object.assign(architecture, { state: 'running', gate: 'gate.architecture' })
  const requirements = workflow.stages.find((stage) => stage.id === 'requirements')
  if (requirements) Object.assign(requirements, { gate: 'gate.requirements' })
  return api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state: 'in_progress',
      workflow: 'workflow-padrao',
      business_need: 'Reservar salas pelo navegador, sem conflito de horário.',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      my_roles: ['lead', 'approver'],
      last_sequence: 9,
    },
    workflow,
    waves: [
      { ...waveSummary('REQ-01', 'completed'), stage: 'requirements', agent: 'requirements-agent' },
      { ...waveSummary('ARQ-01', 'completed'), stage: 'architecture', agent: 'architect-agent' },
    ],
    attachments: [
      {
        id: '0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b01',
        file_name: 'ata-reuniao.md',
        media_type: 'text/markdown',
        size_bytes: 2048,
        sha256: 'b'.repeat(64),
        submitted_at: '2026-11-22T09:00:00-03:00',
        submitted_by: { id: 'dev', name: 'Dev' },
      },
    ],
    artifacts: {
      'US-01': artifact('US-01', 'US', 'requirements', {
        ator: 'Funcionário',
        quero: 'reservar uma sala',
        para: 'reunir o time',
        prioridade: 'must',
        origem: 'ata-reuniao.md, item 2',
        criterios_aceite: [
          { id: 'CA-1', dado: 'uma sala livre', quando: 'reservo', entao: 'é rápido' },
        ],
        regras_negocio: [{ id: 'RN-1', descricao: 'Sem conflito de horário' }],
      }),
      'RNF-01': artifact('RNF-01', 'RNF', 'requirements', {
        requisito: 'Responder em 500 ms',
        categoria: 'desempenho',
      }),
      VISAO: artifact('VISAO', 'VISAO', 'requirements', {
        atores: [{ nome: 'Funcionário', quem_e: 'quem reserva', necessidade: 'sala livre' }],
        glossario: [{ termo: 'Sala', definicao: 'Espaço reservável' }],
      }),
      BACKLOG: artifact('BACKLOG', 'BACKLOG', 'requirements', {
        suposicoes: [
          { id: 'SUP-1', descricao: 'Só horário comercial', motivo: 'a necessidade não diz' },
        ],
      }),
      'ADR-01': artifact('ADR-01', 'ADR', 'architecture', {
        contexto: 'Precisamos de uma API simples.',
        opcoes: [
          { nome: 'Go', pros: ['binário único'], contras: ['menos gente no time'] },
          { nome: 'Java', pros: ['time conhece'], contras: ['mais memória'] },
        ],
        decisao: 'Go, pelo binário único e pelo baixo consumo.',
        consequencias: ['Time aprende Go'],
        motivada_por: ['RNF-01'],
      }),
      ARQ: artifact(
        'ARQ',
        'ARQ',
        'architecture',
        {
          c4: 'C4Container\nPerson(user, "Funcionário", "navegador")\nContainer(api, "API", "Go", "reservas")\nSystem_Ext(sso, "Keycloak", "SSO")\nRel(user, api, "Usa", "HTTPS")',
          componentes: [
            { nome: 'API', responsabilidade: 'Reservas', tecnologia: 'Go', atende: ['US-01'] },
          ],
          riscos: [{ id: 'RK-1', descricao: 'Pico às 9h', impacto: 'alto', mitigacao: 'cache' }],
        },
        {
          state: 'approved',
          attachments: [
            {
              path: 'api/openapi.yaml',
              media_type: 'application/yaml',
              size_bytes: 120,
              sha256: 'c'.repeat(64),
            },
            {
              path: 'estrutura.md',
              media_type: 'text/markdown',
              size_bytes: 80,
              sha256: 'd'.repeat(64),
            },
          ],
        },
      ),
    },
    attachmentContent: {
      'ARQ:1:api/openapi.yaml':
        'openapi: 3.1.0\npaths:\n  /rooms:\n    get:\n      summary: Lista as salas\n    post:\n      summary: Reserva uma sala\n',
      'ARQ:1:estrutura.md':
        '# Estrutura\n\n- `cmd/api`: entrada\n\n<script>window.hacked = true</script>',
    },
    gates: {
      'gate.architecture': [
        gateRound('gate.architecture', 'architecture', 1, {
          state: 'rejected',
          decision: {
            result: 'rejected',
            record: {
              per: { type: 'human', user: { id: 'apr', name: 'Ana' } },
              when: '2026-11-22T11:00:00-03:00',
              origin: 'web',
            },
          },
        }),
        gateRound('gate.architecture', 'architecture', 2, {
          state: 'approved',
          outputs: [{ artifact: 'ARQ', version: 1, type: 'ARQ', state: 'approved' }],
          decision: {
            result: 'approved',
            record: {
              per: { type: 'human', user: { id: 'apr', name: 'Ana' } },
              when: '2026-11-22T12:00:00-03:00',
              origin: 'web',
            },
          },
        }),
      ],
    },
    links: {
      ARQ: {
        root: 'ARQ',
        nodes: [
          { id: 'ARQ', type: 'ARQ', title: 'Arquitetura', exists: true },
          { id: 'US-01', type: 'US', title: 'Reservar sala', exists: true },
          { id: 'T-01', type: 'T', title: 'API de reservas', exists: true },
          { id: 'T-09', type: 'T', exists: false },
        ],
        edges: [],
      },
    },
  })
}

test('ESP-07: the Requisitos stage lists the items, edits a story through editArtifact and shows Fontes and Descoberta', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  seed(api)
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/stages/requirements`)

  const view = page.getByTestId('requirements-view')
  await expect(view).toBeVisible({ timeout: LOAD_TIMEOUT })
  const items = page.getByTestId('requirements-items')
  await expect(items).toContainText('Funcionário quer reservar uma sala')
  await expect(items).toContainText('ata-reuniao.md, item 2')

  // CA-07-11: the NFR without metric says it in text.
  await page.getByRole('tab', { name: /RNF/ }).click()
  await expect(items).toContainText('Sem métrica')
  await page.getByRole('tab', { name: /Histórias/ }).click()

  // CA-07-25, ER-07-19: the edit goes through editArtifact with the whole file and the base version.
  await page.getByRole('button', { name: 'Editar US-01' }).click()
  await expect(page.getByText('termo vago', { exact: false })).toBeVisible()
  await page.getByLabel('CA-1 Então').fill('a reserva aparece na agenda')
  await page.getByLabel('Motivo da mudança').fill('critério testável')
  const sent = page.waitForRequest(
    (request) => request.url().endsWith('/artifact-edits') && request.method() === 'POST',
  )
  await page.getByTestId('requirements-save').click()
  const body = (await sent).postDataJSON() as {
    operation: string
    artifact: string
    base_version: number
    content: string
  }
  expect(body).toMatchObject({ operation: 'modify', artifact: 'US-01', base_version: 1 })
  expect(body.content).toContain('entao: "a reserva aparece na agenda"')
  await expect(page.getByTestId('requirements-edit-notice')).toBeVisible()

  // CA-07-27: removing a rule asks in line and keeps the focus there.
  await page.getByRole('tab', { name: /Regras/ }).click()
  await page.getByRole('button', { name: 'Excluir RN-1' }).click()
  await expect(page.getByRole('button', { name: 'Sim, excluir' })).toBeFocused()
  await page.getByRole('button', { name: 'Não' }).click()

  // CA-07-28: the sources of the creation, read-only.
  await page.getByRole('tab', { name: /Fontes/ }).click()
  const sources = page.getByTestId('requirements-sources')
  await expect(sources).toContainText('Reservar salas pelo navegador')
  await expect(sources).toContainText('ata-reuniao.md')
  await expect(sources).toContainText('Citada por 1 itens')
  await expect(page.getByRole('button', { name: /Enviar arquivo|Importar/ })).toHaveCount(0)

  // CA-07-29: the assumptions, read-only, checked at the Gate.
  await page.getByRole('tab', { name: /Descoberta/ }).click()
  await expect(page.getByTestId('requirements-assumptions')).toContainText('Só horário comercial')
  await expect(page.getByRole('button', { name: /Confirmar|Virar pergunta/ })).toHaveCount(0)
  await expectNoSeriousA11yViolations(page)
})

test('ESP-08: the Arquitetura stage shows the ADR with the chosen option and the design from the ARQ and its OpenAPI attachment', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  seed(api)
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/stages/architecture`)

  await expect(page.getByTestId('architecture-view')).toBeVisible({ timeout: LOAD_TIMEOUT })
  await expect(page.getByTestId('architecture-adrs')).toContainText('Escolhida: Go')
  const detail = page.getByTestId('architecture-adr')
  await expect(detail).toContainText('Precisamos de uma API simples.')
  await expect(detail.getByText('Escolhida', { exact: true })).toBeVisible()
  await expect(detail).toContainText('binário único')

  await page.getByRole('tab', { name: /Desenho/ }).click()
  const c4 = page.getByTestId('architecture-c4')
  await expect(c4).toContainText('Keycloak')
  await expect(c4).toContainText('Sistema externo')
  await expect(c4).toContainText('user → api: Usa · HTTPS')
  const apiCard = page.getByTestId('architecture-api')
  await expect(apiCard).toContainText('/rooms')
  await expect(apiCard).toContainText('Reserva uma sala')
  await expectNoSeriousA11yViolations(page)
})

test('ESP-21: the Artefato page lists the attachments of the version, the traceability chain, "Depende deste" and the approving round', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  seed(api)
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/artifacts/ARQ`)

  const attachments = page.getByTestId('artifact-attachments')
  await expect(attachments).toContainText('api/openapi.yaml', { timeout: LOAD_TIMEOUT })
  await attachments.getByRole('button', { name: 'Abrir' }).nth(1).click()
  await expect(attachments).toContainText('cmd/api')
  // RNF-05: the generated Markdown is sanitized, the script never runs.
  expect(
    await page.evaluate(() => (window as unknown as { hacked?: boolean }).hacked),
  ).toBeUndefined()

  const trace = page.getByTestId('artifact-trace')
  await expect(page.getByTestId('artifact-chain')).toContainText('US-01')
  await expect(trace).toContainText('não existe')
  await expect(page.getByTestId('artifact-dependents')).toContainText('2 artefatos T')

  await expect(page.getByTestId('artifact-approved-in')).toContainText(
    'Gate de Arquitetura, rodada 2',
  )
  await expect(
    page.getByTestId('artifact-approval').getByRole('link', { name: 'Ver a rodada 2 do gate' }),
  ).toBeVisible()
  await expectNoSeriousA11yViolations(page)
})
