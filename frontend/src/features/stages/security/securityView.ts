import type { ArtifactSummary } from '@/features/artifacts/api/artifactsApi'
import type {
  AmeacasHeader,
  RelSegHeader,
} from '@/features/artifacts/composables/structuredArtifact'
import type { FindingSummary } from '@/features/findings/api/findingsApi'
import type { SandboxExecution } from '@/features/waves/api/wavesApi'

/**
 * Pure reading of the Security stage (ESP-16 3.4 to 3.8; canvas EtapaSeguranca). Everything comes from
 * the newest REL-SEG, the approved AMEACAS and the findings of the stage: the screen computes no
 * result of its own. Headers are untrusted (RNF-05), so every field is read defensively.
 */

export const SCAN_TOOLS = ['opengrep', 'trivy', 'gitleaks'] as const

export interface ScanResult {
  /** Id of the result in the tool (rule, CVE or local id; never the secret, PL-21). */
  id?: string
  finding?: string
}

export interface ScanRow {
  tool: string
  version: string
  command: string
  total: number
  /** Real results, each with the tool id when the report gives it and the ACH (PL-21). */
  real: ScanResult[]
  falsePositives: number
  /** The sandbox execution whose command is the scan command (US-41 evidence). */
  execution?: SandboxExecution
  /** 1-based position of the execution in its wave (file name of the download). */
  position?: number
}

export type TriageClass = 'false_positive' | 'fixed' | 'accepted' | 'finding'

export interface TriageItem {
  key: string
  /** Result id in the tool, or the rule of a false positive. */
  id: string
  tool?: string
  /** File:line of a false positive. */
  location?: string
  /** Written justification (false positive) or the ACH title (real result). */
  text: string
  classification: TriageClass
  finding?: string
  findingStatus?: FindingSummary['status']
}

export interface OwaspRow {
  category: string
  result: string
  findings: string[]
}

export type ThreatCheck = 'mitigated' | 'not_mitigated' | 'checked' | 'not_checked'

export interface ThreatRow {
  id: string
  severity: string
  description: string
  mitigation?: string
  check: ThreatCheck
  findings: string[]
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : typeof value === 'number' ? String(value) : ''
}

function strings(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === 'string')
    : []
}

function count(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : 0
}

function reportNumber(id: string): number {
  const match = /^REL-SEG-([0-9]+)$/.exec(id)
  return match ? Number(match[1]) : -1
}

/** ESP-16 3.1: the newest REL-SEG (highest number, then last updated, then highest version). */
export function latestSecurityReport(artifacts: ArtifactSummary[]): ArtifactSummary | undefined {
  return [...artifacts]
    .filter((artifact) => artifact.type === 'REL-SEG')
    .sort(
      (left, right) =>
        reportNumber(right.id) - reportNumber(left.id) ||
        (right.updated_at ?? '').localeCompare(left.updated_at ?? '') ||
        right.version - left.version,
    )[0]
}

/**
 * ESP-16 3.4 (PL-21): one row per scan with tool, version, total, real results (with the id in the
 * tool and the ACH) and false positives, linked to the sandbox execution of the same command.
 */
export function scanRows(
  report: RelSegHeader | undefined,
  executions: SandboxExecution[] = [],
): ScanRow[] {
  const scans = Array.isArray(report?.varreduras) ? report.varreduras : []
  return scans.filter(isRecord).map((scan) => {
    const command = text(scan.comando)
    const findings = strings(scan.achados)
    const results = Array.isArray(scan.resultados) ? scan.resultados.filter(isRecord) : []
    const falsePositives = count(scan.falsos_positivos)
    let real: ScanResult[]
    if (results.length) {
      real = results
        .filter((result) => result.falso_positivo !== true)
        .map((result) => ({
          id: text(result.id) || undefined,
          finding: text(result.achado) || undefined,
        }))
      const linked = new Set(real.map((result) => result.finding))
      for (const finding of findings) if (!linked.has(finding)) real.push({ finding })
    } else {
      real = findings.map((finding) => ({ finding }))
    }
    const index = executions.findIndex(
      (execution) => execution.category === 'scanner' && execution.command === command,
    )
    return {
      tool: text(scan.ferramenta),
      version: text(scan.versao),
      command,
      total: real.length + falsePositives,
      real,
      falsePositives,
      ...(index >= 0 ? { execution: executions[index], position: index + 1 } : {}),
    }
  })
}

/**
 * ESP-16 3.5: one item per scan result with its written justification and classification: the
 * false positives of the report (tool, rule, location, justification) and every real result linked to
 * its ACH, classified by the ACH status (closed: fixed; accepted: accepted; else the open finding).
 */
export function triageItems(
  report: RelSegHeader | undefined,
  findings: FindingSummary[] = [],
): TriageItem[] {
  const byId = new Map(findings.map((finding) => [finding.id, finding]))
  const items: TriageItem[] = []
  for (const row of scanRows(report)) {
    row.real.forEach((result, index) => {
      const finding = result.finding ? byId.get(result.finding) : undefined
      const classification: TriageClass =
        finding?.status === 'closed'
          ? 'fixed'
          : finding?.status === 'accepted'
            ? 'accepted'
            : 'finding'
      items.push({
        key: `${row.tool}-real-${String(index)}`,
        id: result.id ?? result.finding ?? row.tool,
        tool: row.tool,
        text: finding?.title ?? '',
        classification,
        ...(result.finding ? { finding: result.finding } : {}),
        ...(finding ? { findingStatus: finding.status } : {}),
      })
    })
  }
  const falsePositives = Array.isArray(report?.falsos_positivos) ? report.falsos_positivos : []
  falsePositives.filter(isRecord).forEach((item, index) => {
    items.push({
      key: `false-positive-${String(index)}`,
      id: text(item.regra),
      tool: text(item.ferramenta) || undefined,
      location: text(item.local) || undefined,
      text: text(item.justificativa),
      classification: 'false_positive',
    })
  })
  return items
}

/** ESP-16 3.6: the ten OWASP categories, A01 to A10, in order. */
export function owaspRows(report: RelSegHeader | undefined): OwaspRow[] {
  const rows = Array.isArray(report?.owasp) ? report.owasp : []
  return rows
    .filter(isRecord)
    .map((row) => ({
      category: text(row.categoria),
      result: text(row.resultado),
      findings: strings(row.achados),
    }))
    .sort((left, right) => left.category.localeCompare(right.category))
}

/**
 * ESP-16 3.7 (PL-21): the threats of the AMEACAS with the result of the audit check from
 * `REL-SEG.ameacas_conferidas` (object form with result and ACH; the plain id means checked without
 * detail). A threat the report does not list is "not checked" unless the model already links it to a
 * finding.
 */
export function threatRows(
  model: AmeacasHeader | undefined,
  report: RelSegHeader | undefined,
): ThreatRow[] {
  const checked = new Map<string, { check: ThreatCheck; findings: string[] }>()
  const conferred: unknown[] = Array.isArray(report?.ameacas_conferidas)
    ? report.ameacas_conferidas
    : []
  for (const item of conferred) {
    if (typeof item === 'string') {
      checked.set(item, { check: 'checked', findings: [] })
    } else if (isRecord(item) && typeof item.ameaca === 'string') {
      checked.set(item.ameaca, {
        check: item.resultado === 'nao_mitigada' ? 'not_mitigated' : 'mitigated',
        findings: strings(item.achados),
      })
    }
  }
  const threats = Array.isArray(model?.ameacas) ? model.ameacas : []
  return threats.filter(isRecord).map((threat) => {
    const id = text(threat.id)
    const fromModel = text(threat.achado)
    const audit = checked.get(id) ?? {
      check: (threat.status === 'achado' && fromModel
        ? 'not_mitigated'
        : 'not_checked') as ThreatCheck,
      findings: fromModel ? [fromModel] : [],
    }
    return {
      id,
      severity: text(threat.severidade),
      description: text(threat.descricao),
      mitigation: text(threat.mitigacao) || undefined,
      check: audit.check,
      findings: audit.findings,
    }
  })
}

/** ESP-16 3.9: the accepted findings of the stage, the expired ones first. */
export function acceptedFindings(findings: FindingSummary[] = []): FindingSummary[] {
  return findings
    .filter((finding) => finding.status === 'accepted')
    .sort(
      (left, right) =>
        Number(right.acceptance_expired ?? false) - Number(left.acceptance_expired ?? false) ||
        left.id.localeCompare(right.id, undefined, { numeric: true }),
    )
}
