import { describe, expect, it } from 'vitest'
import type { ArtifactSummary } from '@/features/artifacts/api/artifactsApi'
import type {
  AmeacasHeader,
  RelSegHeader,
} from '@/features/artifacts/composables/structuredArtifact'
import type { FindingSummary } from '@/features/findings/api/findingsApi'
import type { SandboxExecution } from '@/features/waves/api/wavesApi'
import {
  acceptedFindings,
  latestSecurityReport,
  owaspRows,
  scanRows,
  threatRows,
  triageItems,
} from '../securityView'

function summary(id: string, version: number, updated: string): ArtifactSummary {
  return {
    id,
    type: id.startsWith('REL-SEG') ? 'REL-SEG' : 'AMEACAS',
    title: id,
    state: 'draft',
    version,
    stage: 'security',
    author: { type: 'agent', agent: 'security-agent' },
    wave: 'SEG-01',
    updated_at: updated,
  }
}

function finding(
  id: string,
  status: FindingSummary['status'],
  extra: Partial<FindingSummary> = {},
): FindingSummary {
  return {
    id,
    title: `title of ${id}`,
    severity: 'high',
    category: 'vulnerability',
    status,
    destination_stage: 'implementation',
    author: 'security-agent',
    blocking: status !== 'closed' && status !== 'accepted',
    ...extra,
  }
}

const report = {
  execucao: 1,
  resumo: {
    critico: { abertos: 0, fechados: 0, aceitos: 0 },
    alto: { abertos: 1, fechados: 1, aceitos: 0 },
    medio: { abertos: 0, fechados: 0, aceitos: 0 },
    baixo: { abertos: 0, fechados: 0, aceitos: 0 },
  },
  varreduras: [
    {
      ferramenta: 'opengrep',
      versao: '1.33.1',
      comando: 'opengrep scan --config /opt/easycode/opengrep-rules app',
      achados: ['ACH-06'],
      falsos_positivos: 2,
      resultados: [
        { id: 'go-sql-string-concatenation', achado: 'ACH-06' },
        { id: 'go-weak-hash', falso_positivo: true },
      ],
    },
    {
      ferramenta: 'trivy',
      versao: '0.76.2',
      comando: 'trivy fs app',
      achados: ['ACH-07'],
      falsos_positivos: 0,
    },
    {
      ferramenta: 'gitleaks',
      versao: '8.30.1',
      comando: 'gitleaks dir app',
      achados: [],
      falsos_positivos: 0,
    },
  ],
  owasp: [
    { categoria: 'A02', verificado: true, resultado: 'sem problema', achados: [] },
    { categoria: 'A01', verificado: true, resultado: 'IDOR na reserva', achados: ['ACH-07'] },
  ],
  ameacas_conferidas: [
    'AM-1',
    { ameaca: 'AM-2', resultado: 'nao_mitigada', achados: ['ACH-07'] },
    { ameaca: 'AM-3', resultado: 'mitigada' },
  ],
  falsos_positivos: [
    {
      ferramenta: 'opengrep',
      regra: 'go-weak-hash',
      local: 'app/etag.go:12',
      justificativa: 'MD5 só para ETag, não protege dado.',
    },
  ],
  comandos: [],
  veredito: 'reprovado',
} as unknown as RelSegHeader

const executions = [
  { id: 'e1', command: 'go build ./...', category: 'build' },
  {
    id: 'e2',
    command: 'opengrep scan --config /opt/easycode/opengrep-rules app',
    category: 'scanner',
  },
  { id: 'e3', command: 'trivy fs app', category: 'scanner' },
] as unknown as SandboxExecution[]

describe('securityView', () => {
  it('ESP-16 3.1: picks the newest REL-SEG by number', () => {
    const artifacts = [
      summary('REL-SEG-01', 3, '2026-11-24T10:00:00Z'),
      summary('REL-SEG-02', 1, '2026-11-23T10:00:00Z'),
      summary('AMEACAS', 1, '2026-11-25T10:00:00Z'),
    ]
    expect(latestSecurityReport(artifacts)?.id).toBe('REL-SEG-02')
    expect(latestSecurityReport([])).toBeUndefined()
  })

  it('ESP-16 3.4, PL-21: scan rows with real results, false positives and the execution', () => {
    const rows = scanRows(report, executions)
    expect(rows.map((row) => row.tool)).toEqual(['opengrep', 'trivy', 'gitleaks'])
    expect(rows[0]).toMatchObject({
      version: '1.33.1',
      total: 3,
      falsePositives: 2,
      real: [{ id: 'go-sql-string-concatenation', finding: 'ACH-06' }],
      position: 2,
    })
    expect(rows[0]?.execution?.id).toBe('e2')
    // Simple form: the ACH list only.
    expect(rows[1]?.real).toEqual([{ finding: 'ACH-07' }])
    expect(rows[1]?.execution?.id).toBe('e3')
    expect(rows[2]?.execution).toBeUndefined()
    expect(rows[2]?.total).toBe(0)
  })

  it('ESP-16 3.5: every result is triaged with its justification and classification', () => {
    const items = triageItems(report, [finding('ACH-06', 'closed'), finding('ACH-07', 'escalated')])
    expect(items).toHaveLength(3)
    expect(items[0]).toMatchObject({
      id: 'go-sql-string-concatenation',
      classification: 'fixed',
      finding: 'ACH-06',
      text: 'title of ACH-06',
    })
    expect(items[1]).toMatchObject({
      id: 'ACH-07',
      classification: 'finding',
      findingStatus: 'escalated',
    })
    expect(items[2]).toMatchObject({
      id: 'go-weak-hash',
      classification: 'false_positive',
      location: 'app/etag.go:12',
      text: 'MD5 só para ETag, não protege dado.',
    })
    expect(triageItems(report, [finding('ACH-06', 'accepted')])[0]?.classification).toBe('accepted')
  })

  it('ESP-16 3.6: OWASP rows in category order with their findings', () => {
    expect(owaspRows(report)).toEqual([
      { category: 'A01', result: 'IDOR na reserva', findings: ['ACH-07'] },
      { category: 'A02', result: 'sem problema', findings: [] },
    ])
    expect(owaspRows(undefined)).toEqual([])
  })

  it('ESP-16 3.7, PL-21: threats with the audit check, in both report forms', () => {
    const model = {
      ameacas: [
        {
          id: 'AM-1',
          severidade: 'alto',
          descricao: 'Webhook falsificado',
          mitigacao: 'assinatura',
          status: 'mitigada',
        },
        {
          id: 'AM-2',
          severidade: 'alto',
          descricao: 'Vazamento de credencial',
          status: 'achado',
          achado: 'ACH-07',
        },
        { id: 'AM-3', severidade: 'medio', descricao: 'Envio em massa', status: 'mitigada' },
        { id: 'AM-4', severidade: 'baixo', descricao: 'Enumeração', status: 'mitigada' },
        {
          id: 'AM-5',
          severidade: 'baixo',
          descricao: 'Sem conferência',
          status: 'achado',
          achado: 'ACH-08',
        },
      ],
    } as unknown as AmeacasHeader
    const rows = threatRows(model, report)
    expect(rows.map((row) => [row.id, row.check, row.findings])).toEqual([
      ['AM-1', 'checked', []],
      ['AM-2', 'not_mitigated', ['ACH-07']],
      ['AM-3', 'mitigated', []],
      ['AM-4', 'not_checked', []],
      ['AM-5', 'not_mitigated', ['ACH-08']],
    ])
    expect(rows[0]?.mitigation).toBe('assinatura')
    expect(threatRows(undefined, report)).toEqual([])
  })

  it('ESP-16 3.9: accepted findings, the expired ones first', () => {
    const list = acceptedFindings([
      finding('ACH-02', 'accepted'),
      finding('ACH-03', 'open'),
      finding('ACH-04', 'accepted', { acceptance_expired: true }),
    ])
    expect(list.map((item) => item.id)).toEqual(['ACH-04', 'ACH-02'])
  })

  it('RNF-05: tolerates malformed headers', () => {
    const broken = { varreduras: [null, { ferramenta: 3 }], owasp: 'x', falsos_positivos: [7] }
    expect(() => scanRows(broken as unknown as RelSegHeader)).not.toThrow()
    expect(triageItems(broken as unknown as RelSegHeader)).toEqual([])
    expect(owaspRows(broken as unknown as RelSegHeader)).toEqual([])
  })
})
