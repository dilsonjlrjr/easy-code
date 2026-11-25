<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import MarkdownContent from '@/shared/ui/MarkdownContent.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import type { ChipTone } from '@/shared/ui/stateFamilies'
import {
  artifactsApi,
  type Artifact,
  type ArtifactSummary,
} from '@/features/artifacts/api/artifactsApi'
import {
  referencedArtifact,
  structuredArtifact,
  type RelQaHeader,
} from '@/features/artifacts/composables/structuredArtifact'
import {
  PRIORITIES,
  traceabilityApi,
  type CoverageMatrix,
} from '@/features/traceability/api/traceabilityApi'
import { wavesApi, type SandboxExecution } from '@/features/waves/api/wavesApi'
import { executionOutcome } from '@/features/waves/composables/sandboxEvidence'
import { useCurrentProjectStore } from '@/features/projects/stores/currentProjectStore'
import type { StageViewProps } from '../stageViews'
import {
  coverageGaps,
  latestReport,
  matrixRows,
  matrixTotals,
  planAttachment,
  scenarioRange,
  waveExecutions,
  type MatrixRow,
} from './qaView'

/**
 * ESP-15 (canvas EtapaQA): the newest REL-QA with its verdict and totals and the link to open it
 * (3.1), then the tabs Result (story × criterion × scenario × result, gaps at the top; 3.3, US-22
 * CA-2, PC-26), Executions (the sandbox commands of the QA waves, US-41; 3.4) and Plan (the scenarios
 * of the `qa-plan` activity and where it was approved; 3.6, PL-63). The QA findings stay in the
 * findings card of StageDetail.
 */
const props = defineProps<StageViewProps>()
const i18n = useI18n()
const { t } = i18n
const format = useFormat()
const route = useRoute()
const router = useRouter()
const store = useCurrentProjectStore()

const TABS = ['result', 'executions', 'plan'] as const
type Tab = (typeof TABS)[number]

const tab = computed<Tab>({
  get: () => {
    const value = route.query.qaTab
    return typeof value === 'string' && (TABS as readonly string[]).includes(value)
      ? (value as Tab)
      : 'result'
  },
  set: (value) => void router.replace({ query: { ...route.query, qaTab: value } }),
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
  void nextTick(() => document.getElementById(`qa-tab-${target}`)?.focus())
}

// ------------------------------------------------------------------ data

const reportSummary = computed(() => latestReport(props.stage.artifacts))
const report = ref<Artifact>()
const coverage = ref<CoverageMatrix>()
const scenarios = ref<ArtifactSummary[]>([])
const executions = ref<Array<{ wave: string; position: number; execution: SandboxExecution }>>([])
/** A QA wave whose executions failed to load keeps its own error; the others still show. */
const executionFailures = ref<Array<{ wave: string; error: unknown }>>([])

const qaWaves = computed(() =>
  [...props.stage.waves]
    .filter((wave) => wave.agent === 'qa-agent' && wave.state !== 'queued')
    .sort((left, right) => right.created_at.localeCompare(left.created_at)),
)

const loader = useLoader(async () => {
  const [reportRead, coverageRead, scenarioList] = await Promise.all([
    reportSummary.value
      ? artifactsApi.getArtifact(props.projectId, reportSummary.value.id)
      : undefined,
    traceabilityApi.coverage(props.projectId, [...PRIORITIES]),
    artifactsApi.listArtifacts(props.projectId, { type: 'CT' }),
  ])
  report.value = reportRead
  coverage.value = coverageRead
  scenarios.value = scenarioList
})

const executionsLoader = useLoader(async () => {
  const waves = qaWaves.value.map((wave) => wave.id)
  const settled = await Promise.allSettled(
    waves.map((wave) => wavesApi.listSandboxExecutions(props.projectId, wave)),
  )
  const read = waveExecutions(waves, settled)
  executions.value = read.items
  executionFailures.value = read.failures
})

/**
 * ESP-15 3.6 (PL-63, CA-15-13): the plan is an attachment of the CT versions; its path comes from
 * `Artifact.attachments` of the latest CT and its content from getArtifactAttachment, rendered only
 * through the sanitizer (RNF-05).
 */
const planCt = ref<Artifact>()
const planText = ref<string>()
const planPath = computed(() => planAttachment(planCt.value?.attachments))
const planLoader = useLoader(async () => {
  planText.value = undefined
  const id = scenarioRange(scenarios.value).last
  planCt.value = id ? await artifactsApi.getArtifact(props.projectId, id) : undefined
  const path = planPath.value
  if (planCt.value && path) {
    planText.value = await artifactsApi.getAttachment(
      props.projectId,
      planCt.value.id,
      planCt.value.version,
      path,
    )
  }
})

watch(
  () => [props.projectId, props.revision, reportSummary.value?.version],
  () => {
    void loader.run()
    void executionsLoader.run()
  },
  { immediate: true },
)

const relQa = computed<RelQaHeader | undefined>(() => {
  const read = report.value && structuredArtifact('REL-QA', report.value.header)
  return read?.type === 'REL-QA' ? read.header : undefined
})
const matrix = computed(() => matrixRows(relQa.value, coverage.value))
const gaps = computed(() => coverageGaps(relQa.value, coverage.value))
const totals = computed(() => matrixTotals(relQa.value, matrix.value.rows))
const range = computed(() => scenarioRange(scenarios.value))
watch(
  () => [
    props.projectId,
    props.revision,
    scenarios.value.map((item) => `${item.id}:${String(item.version)}`).join(),
  ],
  () => void planLoader.run(),
)
const planStage = computed(() => store.workflow?.stages.find((stage) => stage.id === 'qa-plan'))

const counts = computed<Record<Tab, number>>(() => ({
  result: matrix.value.rows.length,
  executions: executions.value.length,
  plan: range.value.count,
}))

// ------------------------------------------------------------------ labels

function artifactRoute(id: string) {
  const artifactId = referencedArtifact(id) ?? (/^(CT|US)-[0-9]+$/.test(id) ? id : undefined)
  return artifactId
    ? { name: ROUTE_NAMES.artifact, params: { projectId: props.projectId, artifactId } }
    : undefined
}

function resultText(row: MatrixRow): string {
  if (row.finding && row.result === 'passou')
    return t('stageQa.matrix.passedAfter', { finding: row.finding })
  if (row.finding && row.result === 'falhou')
    return t('stageQa.matrix.failedWith', { finding: row.finding })
  return t(`structured.results.${row.result}`)
}

function resultTone(row: MatrixRow): ChipTone {
  if (row.result === 'passou') return 'ok'
  if (row.result === 'falhou') return 'crit'
  return row.result === 'bloqueado' ? 'warn' : 'mute'
}

function levelText(row: MatrixRow): string {
  if (!row.level) return t('stageQa.matrix.noLevel')
  const key = `structured.levels.${row.level}`
  return i18n.te(key) ? t(key) : row.level
}

function executionText(execution: SandboxExecution): string {
  const outcome = executionOutcome(execution)
  if (outcome === 'succeeded') return t('stageQa.executions.green')
  if (outcome === 'limit') return t(`evidence.endReasons.${execution.end_reason}`)
  if (outcome === 'failed') return t('stageQa.executions.failed')
  return t(`evidence.outcome.${outcome}`)
}

function executionTone(execution: SandboxExecution): ChipTone {
  const outcome = executionOutcome(execution)
  if (outcome === 'succeeded') return 'ok'
  if (outcome === 'limit') return 'warn'
  return outcome === 'aborted' ? 'mute' : 'crit'
}
</script>

<template>
  <div class="qa-view">
    <!-- Look: canvas EtapaQA header line ("REL-QA-01 · gate …" and "Abrir o REL-QA"). -->
    <section class="card qa-card" aria-labelledby="qa-report-heading" data-testid="qa-report">
      <div class="qa-head">
        <h3 id="qa-report-heading">{{ $t('structured.title.REL-QA') }}</h3>
        <router-link
          v-if="reportSummary"
          :to="{
            name: ROUTE_NAMES.artifact,
            params: { projectId: props.projectId, artifactId: reportSummary.id },
          }"
          class="btn"
          data-testid="qa-open-report"
        >
          {{ $t('stageQa.report.open') }}
        </router-link>
      </div>
      <p v-if="!reportSummary" class="t2 qa-text" data-testid="qa-report-none">
        {{ $t('stageQa.report.none') }}
      </p>
      <template v-else>
        <p class="qa-text">
          <span class="mono">{{ reportSummary.id }}</span>
          · {{ $t('artifacts.versionNumber', { n: reportSummary.version }) }} ·
          <StateLabel machine="artifact" :state="reportSummary.state" />
        </p>
        <template v-if="relQa">
          <p class="qa-text" data-testid="qa-verdict">
            {{
              $t('structured.verdict', {
                verdict: $t(`structured.verdicts.${relQa.veredito}`),
              })
            }}
          </p>
          <p class="t2 qa-text">
            {{
              $t('structured.relQa.summary', {
                executed: relQa.resumo.ct_executados,
                passed: relQa.resumo.passaram,
                failed: relQa.resumo.falharam,
                blocked: relQa.resumo.bloqueados,
                covered: relQa.resumo.ca_must_cobertos,
                total: relQa.resumo.ca_must_total,
              })
            }}
          </p>
        </template>
      </template>
    </section>

    <div class="tabs" role="tablist" :aria-label="$t('stageQa.tabsLabel')">
      <button
        v-for="item in TABS"
        :id="`qa-tab-${item}`"
        :key="item"
        type="button"
        role="tab"
        class="tab"
        :class="{ on: tab === item }"
        :aria-selected="tab === item ? 'true' : 'false'"
        :aria-controls="`qa-panel-${item}`"
        :tabindex="tab === item ? 0 : -1"
        :data-testid="`qa-tab-${item}`"
        @click="tab = item"
        @keydown="onTabKey"
      >
        {{ $t(`stageQa.tabs.${item}`) }}
        <span class="chip mute">{{ counts[item] }}</span>
      </button>
    </div>

    <div
      :id="`qa-panel-${tab}`"
      role="tabpanel"
      :aria-labelledby="`qa-tab-${tab}`"
      tabindex="0"
      class="qa-panel"
    >
      <LoadingState v-if="tab !== 'executions' && loader.loading.value && !loader.loaded.value" />
      <ProblemAlert
        v-else-if="tab !== 'executions' && loader.error.value"
        :error="loader.error.value"
        retry
        @retry="loader.run()"
      />

      <!-- Result: canvas EtapaQA "História × cenário × resultado". -->
      <section
        v-else-if="tab === 'result'"
        class="card qa-card"
        aria-labelledby="qa-matrix-heading"
        data-testid="qa-matrix"
      >
        <div class="qa-head">
          <h3 id="qa-matrix-heading">{{ $t('stageQa.matrix.title') }}</h3>
          <span class="qa-chips">
            <span class="chip ok">{{ $t('stageQa.matrix.passed', { n: totals.passed }) }}</span>
            <span class="chip crit">{{ $t('stageQa.matrix.failing', { n: totals.failing }) }}</span>
            <span v-if="totals.fixed" class="chip mute">{{
              $t('stageQa.matrix.fixed', { n: totals.fixed })
            }}</span>
          </span>
        </div>
        <div v-if="gaps.length" class="note note-warn qa-gaps" data-testid="qa-gaps">
          <div>
            <strong>{{ $t('stageQa.matrix.gapsTitle', { n: gaps.length }) }}</strong>
            <ul class="qa-gap-list">
              <li v-for="gap in gaps" :key="`${gap.us}/${gap.criterion}`">
                <span class="mono">{{ gap.us }} {{ gap.criterion }}</span>
                <template v-if="gap.reason"> · {{ gap.reason }}</template>
              </li>
            </ul>
          </div>
        </div>
        <p v-if="matrix.source === 'coverage' && matrix.rows.length" class="t2 qa-text">
          {{ $t('stageQa.matrix.fromCoverage') }}
        </p>
        <p v-if="matrix.rows.length === 0" class="t2 qa-text" data-testid="qa-matrix-empty">
          {{ $t('stageQa.matrix.empty') }}
        </p>
        <div
          v-else
          class="qa-table-wrap"
          role="region"
          :aria-label="$t('stageQa.matrix.title')"
          tabindex="0"
        >
          <table class="tbl">
            <thead>
              <tr>
                <th scope="col">{{ $t('stageQa.matrix.story') }}</th>
                <th scope="col">{{ $t('stageQa.matrix.criterion') }}</th>
                <th scope="col">{{ $t('stageQa.matrix.scenario') }}</th>
                <th scope="col">{{ $t('stageQa.matrix.level') }}</th>
                <th scope="col">{{ $t('stageQa.matrix.result') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="(row, index) in matrix.rows"
                :key="`${row.us}-${row.criterion}-${row.ct}-${index}`"
                :data-testid="`qa-row-${index + 1}`"
              >
                <th scope="row" class="mono qa-row-head">
                  <router-link v-if="artifactRoute(row.us)" :to="artifactRoute(row.us)!">
                    {{ row.us }}
                  </router-link>
                  <template v-else>{{ row.us }}</template>
                </th>
                <td class="mono">{{ row.criterion }}</td>
                <td class="mono">
                  <router-link v-if="artifactRoute(row.ct)" :to="artifactRoute(row.ct)!">
                    {{ row.ct }}
                  </router-link>
                  <template v-else>{{ row.ct }}</template>
                </td>
                <td>{{ levelText(row) }}</td>
                <td>
                  <span class="chip" :class="resultTone(row)">{{ resultText(row) }}</span>
                  <router-link
                    v-if="row.finding"
                    :to="{
                      name: ROUTE_NAMES.finding,
                      params: { projectId: props.projectId, findingId: row.finding },
                    }"
                    class="qa-finding"
                  >
                    {{ $t('stageQa.matrix.openFinding', { finding: row.finding }) }}
                  </router-link>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- Executions: canvas EtapaQA "Execuções em ambiente limpo" (`.li` rows). -->
      <section
        v-else-if="tab === 'executions'"
        class="card qa-card"
        aria-labelledby="qa-executions-heading"
        data-testid="qa-executions"
      >
        <h3 id="qa-executions-heading" class="qa-title">{{ $t('stageQa.executions.title') }}</h3>
        <p class="t2 qa-text">{{ $t('stageQa.executions.intro') }}</p>
        <LoadingState v-if="executionsLoader.loading.value && !executionsLoader.loaded.value" />
        <ProblemAlert
          v-else-if="executionsLoader.error.value"
          :error="executionsLoader.error.value"
          retry
          @retry="executionsLoader.run()"
        />
        <template v-else>
          <div
            v-for="failure in executionFailures"
            :key="failure.wave"
            class="qa-wave-failure"
            :data-testid="`qa-executions-failed-${failure.wave}`"
          >
            <p class="t2 qa-text">
              {{ $t('stageQa.executions.waveFailed', { wave: failure.wave }) }}
            </p>
            <ProblemAlert :error="failure.error" retry @retry="executionsLoader.run()" />
          </div>
          <p
            v-if="executions.length === 0 && executionFailures.length === 0"
            class="t2 qa-text"
            data-testid="qa-executions-empty"
          >
            {{ $t('stageQa.executions.empty') }}
          </p>
        </template>
        <ul v-if="executions.length > 0 && !executionsLoader.error.value" class="qa-list">
          <li
            v-for="item in executions"
            :key="item.execution.id"
            class="li qa-exec"
            :data-testid="`qa-execution-${item.execution.id}`"
          >
            <span class="qa-exec-main">
              <strong>{{ $t(`evidence.categories.${item.execution.category}`) }}</strong>
              <span class="mono t2 qa-exec-command">{{ item.execution.command }}</span>
              <span class="t2 qa-exec-meta">
                {{ $t('stageQa.executions.wave', { wave: item.wave }) }} ·
                {{ format.dateTime(item.execution.start) }} ·
                {{ format.duration(item.execution.duration_ms) }} ·
                {{ $t('evidence.exitCode') }}
                {{ item.execution.exit_code ?? $t('evidence.noExitCode') }}
              </span>
            </span>
            <span v-if="item.execution.tests" class="t2 qa-exec-tests">
              {{
                $t('evidence.testCounts', {
                  total: format.integer(item.execution.tests.total),
                  passed: format.integer(item.execution.tests.passed),
                  failed: format.integer(item.execution.tests.failed),
                  ignored: format.integer(item.execution.tests.ignored ?? 0),
                })
              }}
            </span>
            <span class="chip" :class="executionTone(item.execution)">{{
              executionText(item.execution)
            }}</span>
            <router-link
              :to="{
                name: ROUTE_NAMES.wave,
                params: { projectId: props.projectId, waveId: item.wave },
                hash: '#evidence',
              }"
              class="ib qa-evidence"
              :aria-label="
                $t('stageQa.executions.evidenceOf', { n: item.position, wave: item.wave })
              "
            >
              {{ $t('stageQa.executions.evidence') }}
            </router-link>
          </li>
        </ul>
      </section>

      <!-- Plan: canvas EtapaQA "Plano de testes (modo plano)" (`.kv` pairs). -->
      <section v-else class="card qa-card" aria-labelledby="qa-plan-heading" data-testid="qa-plan">
        <h3 id="qa-plan-heading" class="qa-title">{{ $t('stageQa.plan.title') }}</h3>
        <p class="t2 qa-text">{{ $t('stageQa.plan.intro') }}</p>
        <dl class="qa-kv">
          <div class="kv">
            <dt class="t2">{{ $t('stageQa.plan.scenarios') }}</dt>
            <dd data-testid="qa-plan-scenarios">
              <template v-if="range.count === 0">{{ $t('stageQa.plan.none') }}</template>
              <template v-else-if="range.count === 1">{{ range.first }}</template>
              <template v-else>{{
                $t('stageQa.plan.scenariosValue', {
                  n: range.count,
                  first: range.first,
                  last: range.last,
                })
              }}</template>
            </dd>
          </div>
          <div v-if="planStage" class="kv">
            <dt class="t2">{{ $t('stageQa.plan.activity') }}</dt>
            <dd><StateLabel machine="stage" :state="planStage.state ?? 'not_started'" /></dd>
          </div>
          <div class="kv">
            <dt class="t2">{{ $t('stageQa.plan.approvedIn') }}</dt>
            <dd class="mono">{{ planStage?.approved_in_gate ?? 'gate.validation' }}</dd>
          </div>
          <div class="kv">
            <dt class="t2">{{ $t('stageQa.plan.file') }}</dt>
            <dd data-testid="qa-plan-file">
              <router-link
                v-if="planCt && planPath"
                class="mono"
                :to="{
                  name: ROUTE_NAMES.artifact,
                  params: { projectId: props.projectId, artifactId: planCt.id },
                  query: { version: String(planCt.version) },
                }"
                >{{ planPath }}</router-link
              >
              <template v-else-if="!planLoader.loading.value">{{
                $t('stageQa.plan.noFile')
              }}</template>
            </dd>
          </div>
        </dl>
        <LoadingState v-if="planLoader.loading.value" type="paragraph" />
        <ProblemAlert
          v-else-if="planLoader.error.value"
          :error="planLoader.error.value"
          retry
          @retry="planLoader.run()"
        />
        <MarkdownContent
          v-else-if="planText !== undefined"
          :source="planText"
          class="qa-plan-content"
          data-testid="qa-plan-content"
        />
        <div class="qa-actions">
          <router-link
            :to="{
              name: ROUTE_NAMES.stage,
              params: { projectId: props.projectId, stageId: 'qa-plan' },
            }"
            class="btn"
            data-testid="qa-open-plan"
          >
            {{ $t('stageQa.plan.open') }}
          </router-link>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.qa-wave-failure {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.qa-view {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.qa-card {
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.qa-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.qa-head h3,
.qa-title {
  margin: 0;
  font-size: 18px;
  line-height: 24px;
  font-weight: 700;
}

.qa-text {
  margin: 0;
  font-size: 13px;
}

.qa-chips {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.qa-gaps {
  font-size: 13px;
}

.qa-gap-list {
  margin: 4px 0 0;
  padding-left: 18px;
}

.qa-table-wrap {
  overflow-x: auto;
}

.qa-row-head {
  font-weight: 400;
  color: inherit;
  border-bottom: 1px solid rgba(140, 150, 255, 0.08);
  padding: 10px 10px 10px 0;
  vertical-align: top;
}

.qa-finding {
  display: block;
  margin-top: 4px;
  font-size: 12px;
}

.qa-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.qa-exec {
  flex-wrap: wrap;
}

.qa-exec-main {
  flex: 1 1 220px;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.qa-exec-command {
  font-size: 12px;
  overflow-wrap: anywhere;
}

.qa-exec-meta,
.qa-exec-tests {
  font-size: 12px;
}

.qa-evidence {
  display: inline-flex;
  align-items: center;
  min-height: 44px;
  text-decoration: none;
}

.qa-kv {
  margin: 0;
}

.qa-kv dd {
  margin: 0;
  text-align: right;
}

.qa-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
</style>
