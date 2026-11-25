import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, finding, waveDetail, waveSummary } from './support/fakeApi'

const PROJECT_ID = '7c1e2a5b-4f3a-4b6c-9d8e-2b3c4d5e6f7a'
const LOAD_TIMEOUT = 30_000
const OPENGREP = 'opengrep scan --config /opt/easycode/opengrep-rules app'
const TRIVY = 'trivy fs --scanners vuln,secret,misconfig app'
const GITLEAKS = 'gitleaks dir --redact app'

function artifact(id: string, type: string, header: Record<string, unknown>, state = 'draft') {
  const current = {
    id,
    type,
    title: `${type} ${id}`,
    state,
    version: 1,
    stage: 'security',
    author: { type: 'agent', agent: 'security-agent' },
    wave: 'SEG-02',
    updated_at: '2026-11-24T10:00:00-03:00',
    header: { id, tipo: type, ...header },
    situation: null,
    body: '',
    commit: null,
  }
  return { current, versions: [], byVersion: { 1: current } }
}

const REL_SEG = {
  onda: 'SEG-02',
  execucao: 1,
  resumo: {
    critico: { abertos: 0, fechados: 0, aceitos: 0 },
    alto: { abertos: 1, fechados: 1, aceitos: 0 },
    medio: { abertos: 0, fechados: 0, aceitos: 1 },
    baixo: { abertos: 0, fechados: 0, aceitos: 0 },
  },
  varreduras: [
    {
      ferramenta: 'opengrep',
      versao: '1.33.1',
      comando: OPENGREP,
      achados: ['ACH-06'],
      falsos_positivos: 1,
      resultados: [
        { id: 'go-sql-string-concatenation', achado: 'ACH-06' },
        { id: 'go-math-rand-for-secret', falso_positivo: true },
      ],
    },
    {
      ferramenta: 'trivy',
      versao: '0.76.2',
      comando: TRIVY,
      achados: ['ACH-08'],
      falsos_positivos: 0,
      resultados: [{ id: 'CVE-2026-0001', achado: 'ACH-08' }],
    },
    {
      ferramenta: 'gitleaks',
      versao: '8.30.1',
      comando: GITLEAKS,
      achados: ['ACH-07'],
      falsos_positivos: 0,
      resultados: [{ id: 'github-pat', achado: 'ACH-07' }],
    },
  ],
  owasp: ['A01', 'A02', 'A03', 'A04', 'A05', 'A06', 'A07', 'A08', 'A09', 'A10'].map(
    (categoria) => ({
      categoria,
      verificado: true,
      resultado: categoria === 'A09' ? 'token do parceiro em log' : 'sem problema',
      achados: categoria === 'A09' ? ['ACH-07'] : [],
    }),
  ),
  ameacas_conferidas: [
    { ameaca: 'AM-1', resultado: 'mitigada' },
    { ameaca: 'AM-2', resultado: 'nao_mitigada', achados: ['ACH-07'] },
  ],
  falsos_positivos: [
    {
      ferramenta: 'opengrep',
      regra: 'go-math-rand-for-secret',
      local: 'app/retry.go:14',
      justificativa: 'math/rand só para a espera entre tentativas, não para segredo.',
    },
  ],
  comandos: [OPENGREP, TRIVY, GITLEAKS],
  veredito: 'reprovado',
}

const AMEACAS = {
  ativos: ['credencial do parceiro'],
  atores: ['parceiro', 'atacante externo'],
  fluxo: 'docs/seguranca/fluxo.mmd',
  ameacas: [
    {
      id: 'AM-1',
      fronteira: 'webhook do parceiro',
      stride: ['s'],
      descricao: 'Webhook falsificado por terceiro',
      probabilidade: 'media',
      impacto: 'alto',
      severidade: 'alto',
      mitigacao: 'assinatura do parceiro',
      onde: ['ADR-03'],
      status: 'mitigada',
    },
    {
      id: 'AM-2',
      fronteira: 'logs',
      stride: ['i'],
      descricao: 'Vazamento de credencial do parceiro',
      probabilidade: 'media',
      impacto: 'alto',
      severidade: 'alto',
      onde: ['RNF-01'],
      status: 'achado',
      achado: 'ACH-07',
    },
    {
      id: 'AM-3',
      fronteira: 'envio',
      stride: ['d'],
      descricao: 'Envio em massa por usuário comum',
      probabilidade: 'baixa',
      impacto: 'medio',
      severidade: 'medio',
      mitigacao: 'limite por papel',
      onde: ['RNF-09'],
      status: 'mitigada',
    },
  ],
}

function execution(id: string, command: string) {
  return {
    id,
    command,
    category: 'scanner',
    start: '2026-11-24T10:10:00-03:00',
    duration_ms: 9000,
    exit_code: 1,
    end_reason: 'completed',
    output_bytes: 40,
  }
}

function seed(api: FakeApi): void {
  const stage = (id: string, name: string, agent: string, dependsOn: string[], state: string) => ({
    id,
    name,
    short_name: name,
    type: 'stage',
    agent,
    depends_on: dependsOn,
    required_capabilities: [],
    state,
    active_waves: 0,
    ...(id === 'security' ? { gate: 'gate.security' } : {}),
  })
  const findings = [
    finding('ACH-06', { status: 'closed', blocking: false, title: 'Consulta SQL concatenada' }),
    finding('ACH-07', { status: 'escalated', cycles: 3, title: 'Token do parceiro em log' }),
    finding('ACH-08', {
      status: 'accepted',
      blocking: false,
      severity: 'medium',
      title: 'Biblioteca HTTP com CVE sem correção',
      acceptance_expired: true,
    }),
  ]
  api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Notificador',
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      lead: { id: 'dev', name: 'Dev' },
      my_roles: ['lead', 'approver'],
      available_actions: ['pause', 'cancel'],
      last_sequence: 3,
    },
    workflow: {
      id: 'workflow-padrao',
      version: 1,
      mode: 'fixed',
      stages: [
        stage('qa', 'QA', 'qa-agent', [], 'approved'),
        stage('security', 'Segurança', 'security-agent', ['qa'], 'running'),
      ],
    },
    waves: [{ ...waveSummary('SEG-02', 'completed'), stage: 'security', agent: 'security-agent' }],
    waveDetails: { 'SEG-02': waveDetail('SEG-02', 'completed') },
    executions: {
      'SEG-02': [
        execution('sx-1', OPENGREP),
        execution('sx-2', TRIVY),
        execution('sx-3', GITLEAKS),
      ],
    },
    outputs: { 'sx-3': 'gitleaks: leaks found: 1 (redacted)\n' },
    artifacts: {
      'REL-SEG-01': artifact('REL-SEG-01', 'REL-SEG', REL_SEG),
      AMEACAS: artifact('AMEACAS', 'AMEACAS', AMEACAS, 'approved'),
    },
    findings,
    stageDetails: { security: { findings } },
  })
}

test.beforeEach(async ({ page }) => {
  const api = new FakeApi()
  api.signedIn = true
  seed(api)
  await api.install(page)
})

test('ESP-16, US-23 CA-2, PL-21: the scans show tool, version, real results with their ACH and the evidence', async ({
  page,
}) => {
  await page.goto(`/projects/${PROJECT_ID}/stages/security`)
  await expect(page.getByTestId('sec-verdict')).toHaveText('Veredito: reprovado.', {
    timeout: LOAD_TIMEOUT,
  })
  await expect(page.getByTestId('sec-open-report')).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/artifacts/REL-SEG-01`,
  )
  const opengrep = page.getByTestId('sec-scan-opengrep')
  await expect(opengrep).toContainText('Opengrep')
  await expect(opengrep).toContainText('1.33.1')
  await expect(opengrep).toContainText('análise estática do código')
  await expect(opengrep).toContainText('go-sql-string-concatenation')
  await expect(opengrep.getByRole('link', { name: 'ACH-06' })).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/findings/ACH-06`,
  )
  await expect(page.getByTestId('sec-scan-trivy')).toContainText('CVE-2026-0001')
  await expect(page.getByTestId('sec-scan-gitleaks')).toContainText(GITLEAKS)
  await expectNoSeriousA11yViolations(page)

  // ESP-16 4 "Ver evidência": the full output is a downloaded file, never rendered.
  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Ver evidência da varredura do Gitleaks' }).click()
  const download = await downloadPromise
  expect(download.suggestedFilename()).toBe('SEG-02-exec-3.txt')
  await expect(page.getByRole('status').filter({ hasText: 'Gitleaks baixada' })).toBeAttached()
})

test('ESP-16 3.5 to 3.9: triage, OWASP, threats and the report with the accepted risks', async ({
  page,
}) => {
  await page.goto(`/projects/${PROJECT_ID}/stages/security?aba=triagem`)
  const triage = page.getByTestId('sec-triage')
  // CA-16-4: the false positive shows its written justification; CA-16-3: the real one its ACH.
  await expect(triage).toContainText('math/rand só para a espera', { timeout: LOAD_TIMEOUT })
  await expect(triage).toContainText('app/retry.go:14')
  await expect(triage).toContainText('Falso positivo')
  await expect(triage).toContainText('Corrigido')
  await expect(triage).toContainText('Escalado')
  await expect(triage.getByRole('link', { name: 'ACH-07' })).toBeVisible()
  // ER-16-17 (PL-43): no "question the triage" action.
  await expect(triage.getByRole('button')).toHaveCount(0)
  await expectNoSeriousA11yViolations(page)

  // Keyboard: arrows move between the tabs and update the route.
  await page.getByRole('tab', { name: /Triagem/ }).focus()
  await page.keyboard.press('ArrowRight')
  await expect(page).toHaveURL(/aba=owasp/)
  await expect(page.getByRole('tab', { name: /OWASP/ })).toBeFocused()
  await expect(page.getByTestId('sec-owasp-A02')).toContainText('A02 Configuração insegura')
  await expect(page.getByTestId('sec-owasp-A02')).toContainText('Sem problema')
  await expect(page.getByTestId('sec-owasp-A09')).toContainText('Falhas de log e alerta')
  await expect(page.getByTestId('sec-owasp-A09').getByRole('link')).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/findings/ACH-07`,
  )

  // CA-16-17: each threat with the audit result and the linked ACH.
  await page.getByRole('tab', { name: /Ameaças/ }).click()
  await expect(page.getByTestId('sec-threat-AM-1')).toContainText('Mitigada')
  await expect(page.getByTestId('sec-threat-AM-1')).toContainText('assinatura do parceiro')
  await expect(page.getByTestId('sec-threat-AM-2')).toContainText('Não mitigada')
  await expect(
    page.getByTestId('sec-threat-AM-2').getByRole('link', { name: 'ACH-07' }),
  ).toBeVisible()
  await expect(page.getByTestId('sec-threat-AM-3')).toContainText('Ainda não conferida')
  await expect(page.getByTestId('sec-open-model')).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/artifacts/AMEACAS`,
  )
  await expectNoSeriousA11yViolations(page)

  // 3.8, 3.9, CA-16-13: verdict, summary by severity and the expired accepted risk.
  await page.getByRole('tab', { name: /Parecer/ }).click()
  await expect(page.getByTestId('sec-parecer')).toContainText('Parecer (REL-SEG-01, Rascunho)')
  await expect(page.getByTestId('sec-summary')).toContainText('Alto')
  await expect(page.getByTestId('sec-accepted-ACH-08')).toContainText(
    'vencido · bloqueia a Release',
  )
  await expectNoSeriousA11yViolations(page)
})
