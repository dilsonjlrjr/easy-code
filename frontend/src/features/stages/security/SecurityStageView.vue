<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { isApiError } from '@/shared/api/problemDetails'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import type { ChipTone } from '@/shared/ui/stateFamilies'
import { artifactsApi, type Artifact } from '@/features/artifacts/api/artifactsApi'
import {
  structuredArtifact,
  type AmeacasHeader,
  type RelSegHeader,
} from '@/features/artifacts/composables/structuredArtifact'
import { wavesApi, type SandboxExecution } from '@/features/waves/api/wavesApi'
import { outputFileName } from '@/features/waves/composables/sandboxEvidence'
import type { StageViewProps } from '../stageViews'
import {
  acceptedFindings,
  latestSecurityReport,
  owaspRows,
  scanRows,
  threatRows,
  triageItems,
  type ScanRow,
  type ThreatRow,
  type TriageItem,
} from './securityView'

/**
 * ESP-16 (canvas EtapaSeguranca): the newest REL-SEG with its state and verdict (3.1), then the tabs
 * Scans (tool, version, what it looks for, results with the id in the tool and the ACH, false
 * positives, evidence of the sandbox execution; 3.4, PL-21), Triage (every result with its written
 * justification and classification; 3.5), OWASP (A01 to A10; 3.6), Threats (the AMEACAS × the audit
 * check; 3.7) and Report (verdict, summary by severity, accepted risks; 3.8, 3.9). The blocking notice,
 * the exit criterion and the findings card stay in StageDetail. No "question the triage" action
 * (ER-16-17, PL-43). Report and tool output are untrusted content: plain text only (ER-16-15).
 */
const props = defineProps<StageViewProps>()
const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const router = useRouter()
const { message, announce } = useAnnouncer()

/** ESP-16 section 2: the tab in the query (`?aba=varreduras|triagem|owasp|ameacas|parecer`). */
const TABS = ['varreduras', 'triagem', 'owasp', 'ameacas', 'parecer'] as const
type Tab = (typeof TABS)[number]

const tab = computed<Tab>({
  get: () => {
    const value = route.query.aba
    return typeof value === 'string' && (TABS as readonly string[]).includes(value)
      ? (value as Tab)
      : 'varreduras'
  },
  set: (value) => void router.replace({ query: { ...route.query, aba: value } }),
})

function onTabKey(event: KeyboardEvent): void {
  const index = TABS.indexOf(tab.value)
  const next =
    event.key === 'ArrowRight'
      ? (index + 1) % TABS.length
      : event.key === 'ArrowLeft'
        ? (index - 1 + TABS.length) % TABS.length
        : event.key === 'Home'
          ? 0
          : event.key === 'End'
            ? TABS.length - 1
            : undefined
  const target = next === undefined ? undefined : TABS[next]
  if (target === undefined) return
  event.preventDefault()
  tab.value = target
  void nextTick(() => document.getElementById(`sec-tab-${target}`)?.focus())
}

// ------------------------------------------------------------------ data

const reportSummary = computed(() => latestSecurityReport(props.stage.artifacts))
const report = ref<Artifact>()
const model = ref<Artifact>()
const executions = ref<SandboxExecution[]>([])
const auditWave = ref<string>()

/** AMEACAS is optional (the threat activity may not have run yet): 404 means none. */
async function readModel(): Promise<Artifact | undefined> {
  try {
    return await artifactsApi.getArtifact(props.projectId, 'AMEACAS')
  } catch (caught) {
    if (isApiError(caught) && caught.status === 404) return undefined
    throw caught
  }
}

const loader = useLoader(async () => {
  const [reportRead, modelRead] = await Promise.all([
    reportSummary.value
      ? artifactsApi.getArtifact(props.projectId, reportSummary.value.id)
      : undefined,
    readModel(),
  ])
  report.value = reportRead
  model.value = modelRead
  // The evidence of the scans is in the audit wave of the report version (checagem rel_seg.scans_executed).
  const onda = reportRead?.header.onda
  auditWave.value = typeof onda === 'string' && onda ? onda : reportRead?.wave
  executions.value = auditWave.value
    ? await wavesApi.listSandboxExecutions(props.projectId, auditWave.value)
    : []
})

watch(
  () => [props.projectId, props.revision, reportSummary.value?.version],
  () => void loader.run(),
  { immediate: true },
)

const relSeg = computed<RelSegHeader | undefined>(() => {
  const read = report.value && structuredArtifact('REL-SEG', report.value.header)
  return read?.type === 'REL-SEG' ? read.header : undefined
})
const ameacas = computed<AmeacasHeader | undefined>(() => {
  const read = model.value && structuredArtifact('AMEACAS', model.value.header)
  return read?.type === 'AMEACAS' ? read.header : undefined
})

const findings = computed(() => props.stage.findings ?? [])
const scans = computed(() => scanRows(relSeg.value, executions.value))
const triage = computed(() => triageItems(relSeg.value, findings.value))
const owasp = computed(() => owaspRows(relSeg.value))
const threats = computed(() => threatRows(ameacas.value, relSeg.value))
const accepted = computed(() => acceptedFindings(findings.value))

const counts = computed<Record<Tab, number>>(() => ({
  varreduras: scans.value.length,
  triagem: triage.value.length,
  owasp: owasp.value.length,
  ameacas: threats.value.length,
  parecer: report.value ? 1 : 0,
}))

const SEVERITIES = ['critico', 'alto', 'medio', 'baixo'] as const

// ------------------------------------------------------------------ labels

function findingRoute(findingId: string) {
  return { name: ROUTE_NAMES.finding, params: { projectId: props.projectId, findingId } }
}

function artifactRoute(artifactId: string) {
  return { name: ROUTE_NAMES.artifact, params: { projectId: props.projectId, artifactId } }
}

function toolName(tool: string): string {
  const key = `stageSecurity.tools.${tool}.name`
  return i18n.te(key) ? t(key) : tool
}

function toolPurpose(tool: string): string {
  const key = `stageSecurity.tools.${tool}.purpose`
  return i18n.te(key) ? t(key) : t('stageSecurity.tools.outra.purpose')
}

function owaspName(category: string): string {
  const key = `structured.owasp.${category}`
  return i18n.te(key) ? `${category} ${t(key)}` : category
}

function severityName(severity: string): string {
  const key = `structured.severities.${severity}`
  return i18n.te(key) ? t(key) : severity
}

function severityTone(severity: string): ChipTone {
  if (severity === 'critico' || severity === 'alto') return 'crit'
  return severity === 'medio' ? 'warn' : 'mute'
}

function triageText(item: TriageItem): string {
  return t(`stageSecurity.triage.classes.${item.classification}`)
}

function triageTone(item: TriageItem): ChipTone {
  if (item.classification === 'fixed') return 'ok'
  if (item.classification === 'false_positive') return 'mute'
  return item.classification === 'accepted' ? 'warn' : 'crit'
}

function threatText(row: ThreatRow): string {
  return t(`stageSecurity.threats.checks.${row.check}`)
}

function threatTone(row: ThreatRow): ChipTone {
  if (row.check === 'mitigated' || row.check === 'checked') return 'ok'
  return row.check === 'not_mitigated' ? 'crit' : 'mute'
}

function verdictTone(verdict: string): ChipTone {
  return verdict === 'aprovado' ? 'ok' : 'crit'
}

// ------------------------------------------------------------------ evidence

const downloadFailure = ref<unknown>()
const downloading = ref<string>()

/** ESP-16 4 "Ver evidência": the full output as a file, never rendered (RNF-05). */
async function downloadEvidence(row: ScanRow): Promise<void> {
  if (!row.execution || !auditWave.value) return
  downloadFailure.value = undefined
  downloading.value = row.execution.id
  try {
    const output = await wavesApi.downloadExecutionOutput(
      props.projectId,
      auditWave.value,
      row.execution.id,
    )
    const url = URL.createObjectURL(new Blob([output], { type: 'text/plain;charset=utf-8' }))
    const link = document.createElement('a')
    link.href = url
    link.download = outputFileName(auditWave.value, row.position ?? 1)
    link.click()
    URL.revokeObjectURL(url)
    announce(t('stageSecurity.scans.downloaded', { tool: toolName(row.tool) }))
  } catch (caught) {
    downloadFailure.value = caught
  } finally {
    downloading.value = undefined
  }
}
</script>

<template>
  <div class="sec-view">
    <div role="status" class="sr-only">{{ message }}</div>
    <!-- Look: canvas EtapaSeguranca header line ("REL-SEG-01, rascunho") and the artifact links. -->
    <section class="card sec-card" aria-labelledby="sec-report-heading" data-testid="sec-report">
      <div class="sec-head">
        <h3 id="sec-report-heading">{{ $t('structured.title.REL-SEG') }}</h3>
        <span class="sec-actions">
          <router-link
            v-if="reportSummary"
            :to="artifactRoute(reportSummary.id)"
            class="btn"
            data-testid="sec-open-report"
          >
            {{ $t('stageSecurity.report.open', { id: reportSummary.id }) }}
          </router-link>
          <router-link
            v-if="model"
            :to="artifactRoute('AMEACAS')"
            class="btn"
            data-testid="sec-open-model"
          >
            {{ $t('stageSecurity.report.openModel') }}
          </router-link>
        </span>
      </div>
      <p v-if="!reportSummary" class="t2 sec-text" data-testid="sec-report-none">
        {{ $t('stageSecurity.report.none') }}
      </p>
      <p v-else class="sec-text">
        <span class="mono">{{ reportSummary.id }}</span>
        · {{ $t('artifacts.versionNumber', { n: reportSummary.version }) }} ·
        <StateLabel machine="artifact" :state="reportSummary.state" />
        <template v-if="relSeg">
          ·
          <span class="chip" :class="verdictTone(relSeg.veredito)" data-testid="sec-verdict">
            {{
              $t('structured.verdict', {
                verdict: $t(`structured.verdicts.${relSeg.veredito}`),
              })
            }}
          </span>
        </template>
      </p>
    </section>

    <div class="tabs" role="tablist" :aria-label="$t('stageSecurity.tabsLabel')">
      <button
        v-for="item in TABS"
        :id="`sec-tab-${item}`"
        :key="item"
        type="button"
        role="tab"
        class="tab"
        :class="{ on: tab === item }"
        :aria-selected="tab === item ? 'true' : 'false'"
        :aria-controls="`sec-panel-${item}`"
        :tabindex="tab === item ? 0 : -1"
        :disabled="loader.loading.value && !loader.loaded.value"
        :data-testid="`sec-tab-${item}`"
        @click="tab = item"
        @keydown="onTabKey"
      >
        {{ $t(`stageSecurity.tabs.${item}`) }}
        <span class="chip mute">{{ counts[item] }}</span>
      </button>
    </div>

    <div
      :id="`sec-panel-${tab}`"
      role="tabpanel"
      :aria-labelledby="`sec-tab-${tab}`"
      tabindex="0"
      class="sec-panel"
    >
      <LoadingState v-if="loader.loading.value && !loader.loaded.value" />
      <ProblemAlert
        v-else-if="loader.error.value"
        :error="loader.error.value"
        retry
        @retry="loader.run()"
      />

      <!-- Scans: canvas "Varreduras" table. -->
      <section
        v-else-if="tab === 'varreduras'"
        class="card sec-card"
        aria-labelledby="sec-scans-heading"
        data-testid="sec-scans"
      >
        <h3 id="sec-scans-heading" class="sec-title">{{ $t('stageSecurity.scans.title') }}</h3>
        <p class="t2 sec-text">{{ $t('stageSecurity.scans.intro') }}</p>
        <ProblemAlert v-if="downloadFailure" :error="downloadFailure" />
        <p v-if="scans.length === 0" class="t2 sec-text" data-testid="sec-scans-empty">
          {{ $t('stageSecurity.scans.empty') }}
        </p>
        <div
          v-else
          class="sec-table-wrap"
          role="region"
          :aria-label="$t('stageSecurity.scans.title')"
          tabindex="0"
        >
          <table class="tbl">
            <thead>
              <tr>
                <th scope="col">{{ $t('stageSecurity.scans.tool') }}</th>
                <th scope="col">{{ $t('stageSecurity.scans.purpose') }}</th>
                <th scope="col">{{ $t('stageSecurity.scans.results') }}</th>
                <th scope="col">{{ $t('stageSecurity.scans.real') }}</th>
                <th scope="col">{{ $t('stageSecurity.scans.falsePositives') }}</th>
                <th scope="col">{{ $t('stageSecurity.scans.evidence') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="(row, index) in scans"
                :key="`${row.tool}-${index}`"
                :data-testid="`sec-scan-${row.tool}`"
              >
                <th scope="row" class="sec-row-head">
                  <strong>{{ toolName(row.tool) }}</strong>
                  <span class="t2 sec-small">{{ row.version }}</span>
                </th>
                <td>{{ toolPurpose(row.tool) }}</td>
                <td>{{ row.total }}</td>
                <td>
                  <template v-if="row.real.length === 0">{{ row.real.length }}</template>
                  <ul v-else class="sec-plain">
                    <li v-for="(result, position) in row.real" :key="position">
                      <span v-if="result.id" class="mono">{{ result.id }}</span>
                      <template v-if="result.id && result.finding"> · </template>
                      <router-link v-if="result.finding" :to="findingRoute(result.finding)">
                        {{ result.finding }}
                      </router-link>
                    </li>
                  </ul>
                </td>
                <td>{{ row.falsePositives }}</td>
                <td>
                  <span class="mono t2 sec-command">{{ row.command }}</span>
                  <button
                    v-if="row.execution"
                    type="button"
                    class="ib sec-evidence"
                    :disabled="downloading === row.execution.id"
                    :aria-label="$t('stageSecurity.scans.evidenceOf', { tool: toolName(row.tool) })"
                    :data-testid="`sec-evidence-${row.tool}`"
                    @click="downloadEvidence(row)"
                  >
                    {{ $t('stageSecurity.scans.seeEvidence') }}
                  </button>
                  <span v-else class="chip warn" :data-testid="`sec-no-execution-${row.tool}`">
                    {{ $t('stageSecurity.scans.noExecution') }}
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- Triage: canvas "Triagem" `.li` rows (without "Questionar": ER-16-17, PL-43). -->
      <section
        v-else-if="tab === 'triagem'"
        class="card sec-card"
        aria-labelledby="sec-triage-heading"
        data-testid="sec-triage"
      >
        <h3 id="sec-triage-heading" class="sec-title">{{ $t('stageSecurity.triage.title') }}</h3>
        <p class="t2 sec-text">{{ $t('stageSecurity.triage.intro') }}</p>
        <p v-if="triage.length === 0" class="t2 sec-text" data-testid="sec-triage-empty">
          {{ $t('stageSecurity.triage.empty') }}
        </p>
        <ul v-else class="sec-plain">
          <li
            v-for="item in triage"
            :key="item.key"
            class="li sec-item"
            :data-testid="`sec-triage-${item.key}`"
          >
            <span class="chip mono mute">{{ item.id }}</span>
            <span class="sec-item-main">
              <span>
                <template v-if="item.tool">{{ toolName(item.tool) }}</template>
                <template v-if="item.location">
                  · <span class="mono">{{ item.location }}</span></template
                >
              </span>
              <span class="t2 sec-small">{{
                item.text || $t('stageSecurity.triage.noJustification')
              }}</span>
            </span>
            <span class="chip" :class="triageTone(item)">{{ triageText(item) }}</span>
            <StateLabel v-if="item.findingStatus" machine="finding" :state="item.findingStatus" />
            <router-link v-if="item.finding" :to="findingRoute(item.finding)" class="sec-link">
              {{ item.finding }}
            </router-link>
          </li>
        </ul>
      </section>

      <!-- OWASP: canvas "Revisão OWASP Top 10:2025" `.kv` grid. -->
      <section
        v-else-if="tab === 'owasp'"
        class="card sec-card"
        aria-labelledby="sec-owasp-heading"
        data-testid="sec-owasp"
      >
        <h3 id="sec-owasp-heading" class="sec-title">{{ $t('stageSecurity.owasp.title') }}</h3>
        <p v-if="owasp.length === 0" class="t2 sec-text" data-testid="sec-owasp-empty">
          {{ $t('stageSecurity.owasp.empty') }}
        </p>
        <dl v-else class="sec-grid">
          <div
            v-for="row in owasp"
            :key="row.category"
            class="kv"
            :data-testid="`sec-owasp-${row.category}`"
          >
            <dt>{{ owaspName(row.category) }}</dt>
            <dd>
              <span v-if="row.findings.length === 0" class="chip ok">{{
                $t('stageSecurity.owasp.clean')
              }}</span>
              <template v-else>
                <router-link
                  v-for="id in row.findings"
                  :key="id"
                  :to="findingRoute(id)"
                  class="sec-link sec-chip-link"
                >
                  {{ $t('stageSecurity.owasp.finding', { id }) }}
                </router-link>
              </template>
              <span v-if="row.result" class="t2 sec-small sec-block">{{ row.result }}</span>
            </dd>
          </div>
        </dl>
        <p class="t2 sec-small">{{ $t('stageSecurity.owasp.source') }}</p>
      </section>

      <!-- Threats: canvas "Modelo de ameaças × código". -->
      <section
        v-else-if="tab === 'ameacas'"
        class="card sec-card"
        aria-labelledby="sec-threats-heading"
        data-testid="sec-threats"
      >
        <h3 id="sec-threats-heading" class="sec-title">
          {{ $t('stageSecurity.threats.title') }}
        </h3>
        <p class="t2 sec-text">{{ $t('stageSecurity.threats.intro') }}</p>
        <p v-if="threats.length === 0" class="t2 sec-text" data-testid="sec-threats-empty">
          {{ $t('stageSecurity.threats.empty') }}
        </p>
        <ul v-else class="sec-plain">
          <li
            v-for="row in threats"
            :key="row.id"
            class="li sec-item"
            :data-testid="`sec-threat-${row.id}`"
          >
            <span class="chip" :class="severityTone(row.severity)">{{
              severityName(row.severity)
            }}</span>
            <span class="sec-item-main">
              <span
                ><span class="mono">{{ row.id }}</span> · {{ row.description }}</span
              >
              <span v-if="row.mitigation" class="t2 sec-small">
                {{ $t('stageSecurity.threats.mitigation', { text: row.mitigation }) }}
              </span>
            </span>
            <span class="chip" :class="threatTone(row)">{{ threatText(row) }}</span>
            <router-link
              v-for="id in row.findings"
              :key="id"
              :to="findingRoute(id)"
              class="sec-link"
            >
              {{ id }}
            </router-link>
          </li>
        </ul>
      </section>

      <!-- Report: canvas "Parecer" `.kv` pairs, plus the accepted risks (3.9). -->
      <section
        v-else
        class="card sec-card"
        aria-labelledby="sec-verdict-heading"
        data-testid="sec-parecer"
      >
        <h3 id="sec-verdict-heading" class="sec-title">
          <template v-if="reportSummary">
            {{
              $t('stageSecurity.verdict.titleOf', {
                id: reportSummary.id,
                state: $t(`states.artifact.${reportSummary.state}`),
              })
            }}
          </template>
          <template v-else>{{ $t('stageSecurity.verdict.title') }}</template>
        </h3>
        <p v-if="!relSeg" class="t2 sec-text" data-testid="sec-parecer-empty">
          {{ $t('stageSecurity.report.none') }}
        </p>
        <template v-else>
          <p class="sec-text">
            <span class="chip" :class="verdictTone(relSeg.veredito)">{{
              $t(`structured.verdicts.${relSeg.veredito}`)
            }}</span>
          </p>
          <div
            class="sec-table-wrap"
            role="region"
            :aria-label="$t('structured.relSeg.summary')"
            tabindex="0"
          >
            <table class="tbl" data-testid="sec-summary">
              <caption class="sr-only">
                {{
                  $t('structured.relSeg.summary')
                }}
              </caption>
              <thead>
                <tr>
                  <th scope="col">{{ $t('stageSecurity.verdict.severity') }}</th>
                  <th scope="col">{{ $t('structured.relSeg.open') }}</th>
                  <th scope="col">{{ $t('structured.relSeg.closed') }}</th>
                  <th scope="col">{{ $t('structured.relSeg.accepted') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="severity in SEVERITIES" :key="severity">
                  <th scope="row" class="sec-row-head">{{ severityName(severity) }}</th>
                  <td>{{ relSeg.resumo?.[severity]?.abertos ?? 0 }}</td>
                  <td>{{ relSeg.resumo?.[severity]?.fechados ?? 0 }}</td>
                  <td>{{ relSeg.resumo?.[severity]?.aceitos ?? 0 }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>
        <h4 class="sec-subtitle">{{ $t('stageSecurity.verdict.accepted') }}</h4>
        <p v-if="accepted.length === 0" class="t2 sec-text" data-testid="sec-accepted-empty">
          {{ $t('stageSecurity.verdict.noneAccepted') }}
        </p>
        <ul v-else class="sec-plain">
          <li
            v-for="item in accepted"
            :key="item.id"
            class="li sec-item"
            :data-testid="`sec-accepted-${item.id}`"
          >
            <span class="chip mono mute">{{ item.id }}</span>
            <span class="sec-item-main">
              <span>{{ item.title }}</span>
              <span class="t2 sec-small">{{ $t(`findings.severity.${item.severity}`) }}</span>
            </span>
            <span v-if="item.acceptance_expired" class="chip crit">{{
              $t('stageSecurity.verdict.expired')
            }}</span>
            <router-link :to="findingRoute(item.id)" class="sec-link">
              {{ $t('stageSecurity.verdict.openFinding', { id: item.id }) }}
            </router-link>
          </li>
        </ul>
        <p class="t2 sec-small">{{ $t('stageSecurity.verdict.onlyHuman') }}</p>
      </section>
    </div>
  </div>
</template>

<style scoped>
.sec-view {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.sec-card {
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.sec-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.sec-head h3,
.sec-title {
  margin: 0;
  font-size: 18px;
  line-height: 24px;
  font-weight: 700;
}

.sec-subtitle {
  margin: 6px 0 0;
  font-size: 15px;
  font-weight: 700;
}

.sec-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.sec-text {
  margin: 0;
  font-size: 13px;
}

.sec-small {
  font-size: 12px;
}

.sec-block {
  display: block;
  margin-top: 2px;
}

.sec-table-wrap {
  overflow-x: auto;
}

.sec-row-head {
  font-weight: 400;
  color: inherit;
  text-align: left;
  border-bottom: 1px solid rgba(140, 150, 255, 0.08);
  padding: 10px 10px 10px 0;
  vertical-align: top;
}

.sec-row-head strong,
.sec-row-head span {
  display: block;
}

.sec-plain {
  list-style: none;
  margin: 0;
  padding: 0;
}

.sec-command {
  display: block;
  font-size: 12px;
  overflow-wrap: anywhere;
}

.sec-evidence {
  display: inline-flex;
  align-items: center;
  min-height: 44px;
  margin-top: 4px;
}

.sec-item {
  flex-wrap: wrap;
  align-items: flex-start;
}

.sec-item-main {
  flex: 1 1 300px;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.sec-link {
  display: inline-flex;
  align-items: center;
  min-height: 44px;
}

.sec-grid {
  margin: 0;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 0 24px;
}

.sec-grid dd {
  margin: 0;
  text-align: right;
}

.sec-chip-link {
  margin-left: 8px;
}
</style>
