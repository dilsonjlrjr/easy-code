import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, phaseOneWorkflow, waveDetail, waveSummary } from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'
const WAVE = 'DEV-01'
const BUILD_ID = '0193a5c0-0000-7000-8000-000000000001'
const TESTS_ID = '0193a5c0-0000-7000-8000-000000000002'
const SLOW_ID = '0193a5c0-0000-7000-8000-000000000003'

const developerWave = {
  stage: 'implementation',
  agent: 'developer-agent',
  task: 'T-01',
}

function execution(id: string, overrides: Record<string, unknown>) {
  return {
    id,
    command: 'go build ./...',
    category: 'build',
    start: '2026-11-22T10:02:00-03:00',
    duration_ms: 8300,
    exit_code: 0,
    end_reason: 'completed',
    output_start: 'ok',
    output_bytes: 2,
    image_digest: 'sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0',
    ...overrides,
  }
}

function toolCall(number: number, overrides: Record<string, unknown>) {
  return {
    number,
    type: 'tool',
    start: '2026-11-22T10:01:00-03:00',
    duration_ms: 30,
    result: 'success',
    attempt: 1,
    ...overrides,
  }
}

function seed(api: FakeApi) {
  return api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      lead: { id: 'dev', name: 'Dev' },
      my_roles: ['lead', 'approver'],
      available_actions: ['pause', 'cancel'],
      last_sequence: 3,
    },
    workflow: phaseOneWorkflow('started'),
    waves: [{ ...waveSummary(WAVE, 'failed'), ...developerWave }],
    waveDetails: {
      [WAVE]: waveDetail(WAVE, 'failed', {
        ...developerWave,
        result: 'failed',
        failure_cause: 'build_or_test_failed',
        git_commit: '9c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d',
      }),
    },
    calls: {
      [WAVE]: [
        toolCall(1, { tool: 'run_command', summary: 'rodou testes: 42 ok, 1 falha' }),
        toolCall(2, {
          tool: 'http_request',
          result: 'refused',
          refusal_reason: 'network_not_allowed',
        }),
        toolCall(3, {
          tool: 'write_file',
          result: 'refused',
          refusal_reason: 'foreign_artifact_write',
        }),
      ],
    },
    executions: {
      [WAVE]: [
        execution(BUILD_ID, {}),
        execution(TESTS_ID, {
          command: 'go test ./...',
          category: 'tests',
          exit_code: 1,
          tests: { total: 43, passed: 42, failed: 1, ignored: 0 },
          output_start: '--- FAIL: TestBooking (0.01s)\n<script>alert(1)</script>',
          output_bytes: 120000,
        }),
        execution(SLOW_ID, {
          command: 'go test -run TestSlow ./...',
          category: 'tests',
          exit_code: null,
          end_reason: 'time_limit',
          duration_ms: 600000,
          output_start: '',
          output_bytes: 0,
        }),
      ],
    },
    outputs: { [TESTS_ID]: '--- FAIL: TestBooking (0.01s)\nfull output\n' },
  })
}

test('US-41 CA-1/CA-2/CA-3, US-19 CA-2: the developer wave shows each command with exit code, duration, tests and the limit, and downloads the full output', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  seed(api)
  await api.install(page)

  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)
  await page.getByTestId('wave-tab-evidence').click()
  const evidence = page.getByRole('region', { name: 'Evidência de build e testes' })
  await expect(evidence).toBeVisible()
  await expect(page.getByText('tarefa T-01')).toBeVisible()
  // US-19 CA-2: the failure is recorded with the command output.
  await expect(page.getByTestId('failure-cause')).toContainText('build ou testes falharam')
  await expect(page.getByTestId('wave-commit')).toContainText('9c1d2e3f4a5b')

  const build = page.getByTestId('execution-1')
  await expect(build).toContainText('Comando 1 · build')
  await expect(build.getByTestId('execution-outcome')).toHaveText('sucesso')
  await expect(build).toContainText('go build ./...')

  const tests = page.getByTestId('execution-2')
  await expect(tests.getByTestId('execution-outcome')).toHaveText('falhou (código 1)')
  await expect(tests.getByTestId('execution-tests')).toHaveText(
    '43 no total, 42 passaram, 1 falharam, 0 ignorados',
  )
  // RNF-05: the output is plain text; the markup in it is never interpreted.
  await tests.getByText(/Ver o início da saída/).click()
  const output = tests.getByRole('region', { name: 'Saída do comando 2' })
  await expect(output).toContainText('<script>alert(1)</script>')
  await expect(tests).toContainText('Mostrando só o início da saída')

  // CA-3: a command stopped at the time limit reads "limite excedido".
  const slow = page.getByTestId('execution-3')
  await expect(slow.getByTestId('execution-outcome')).toHaveText('limite excedido')
  await expect(page.getByTestId('evidence-limits')).toContainText(
    'Comandos encerrados por limite de recurso do sandbox: 1',
  )
  await expect(page.getByTestId('download-3')).toHaveCount(0)

  await expectNoSeriousA11yViolations(page)

  // CA-1: the full output is a downloaded file, never rendered.
  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Baixar saída completa do comando 2' }).click()
  const download = await downloadPromise
  expect(download.suggestedFilename()).toBe(`${WAVE}-exec-2.txt`)
  await expect(page.getByText('Saída do comando 2 baixada.')).toBeAttached()
})

test('US-30 CA-1/CA-2/CA-3: refused tool calls show with the reason and the allowed command is recorded', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  seed(api)
  await api.install(page)

  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)
  await expect(page.getByTestId('refused-calls')).toContainText(
    'Chamadas de ferramenta recusadas pelo privilégio mínimo: 2',
  )
  // CA-3: the developer's command ran and is recorded.
  await expect(page.getByTestId('call-1')).toContainText('Ferramenta run_command · sucesso')
  await expect(page.getByTestId('call-2')).toContainText('acesso à rede bloqueado no sandbox')
  // CA-2: a write to an approved artifact of another agent is refused; the agent registers a finding.
  await expect(page.getByTestId('call-3')).toContainText(
    'escrita em artefato aprovado de outro agente; o agente foi orientado a registrar um achado',
  )
})

test('US-41: the evidence link opens the wave at the evidence block, also in English', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  seed(api)
  await api.install(page)
  await page.addInitScript(() => {
    window.localStorage.setItem('easy-code:preferences:locale', 'en')
  })

  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}#evidence`)
  const heading = page.getByRole('heading', { name: 'Build and test evidence' })
  await expect(heading).toBeFocused()
  await expect(page.getByTestId('execution-3').getByTestId('execution-outcome')).toHaveText(
    'limit exceeded',
  )
  await expectNoSeriousA11yViolations(page)
})
