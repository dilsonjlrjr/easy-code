<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import {
  artifactsApi,
  type Artifact,
  type ArtifactSummary,
} from '@/features/artifacts/api/artifactsApi'
import {
  projectsApi,
  type Project,
  type StageSummary,
  type Workflow,
} from '@/features/projects/api/projectsApi'
import { isApiError } from '@/shared/api/problemDetails'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import type { CloudEvent, EventClass } from '@/shared/events/eventStream'
import { useProjectEventStream } from '@/shared/events/useProjectEventStream'
import { useFormat } from '@/shared/format/useFormat'
import AppBreadcrumbs from '@/shared/ui/AppBreadcrumbs.vue'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import LiveConnectionStatus from '@/shared/ui/LiveConnectionStatus.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import { findingsApi, type Finding, type FindingCycle } from '../api/findingsApi'
import FindingDecisionForm from '../components/FindingDecisionForm.vue'
import {
  ACTIVE_STATUSES,
  DECISION_ACTIONS,
  blockingText,
  isInternalCycle,
  takePendingFindingDecision,
  today,
} from '../composables/findingRules'

/**
 * ESP-20: one finding (ACH) with its evidence, cycles of rework and revalidation, escalations, the
 * effect on gates and release, the reopening it caused (US-34) and, for the approver, the decision
 * (US-32 CA-6/CA-7, US-33). Evidence, impact and fix are agent content: text only (RNF-05).
 */
const REFRESH_DELAY_MS = 250
const REFRESHING_GROUPS = ['finding.', 'stage.', 'gate.', 'artifact.']

const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const format = useFormat()
const live = useProjectEventStream()
const { message, announce } = useAnnouncer()

const project = ref<Project>()
const finding = ref<Finding>()
const evidence = ref<Artifact>()
const workflow = ref<Workflow>()
const requirementsArtifacts = ref<ArtifactSummary[]>([])
const conflict = ref(false)
const decisionForm = ref<InstanceType<typeof FindingDecisionForm>>()
let refreshTimer: ReturnType<typeof setTimeout> | undefined

const projectId = computed(() => String(route.params.projectId))
const findingId = computed(() => String(route.params.findingId))

async function loadFinding(): Promise<void> {
  const loaded = await findingsApi.get(projectId.value, findingId.value)
  finding.value = loaded
  const [loadedWorkflow, loadedEvidence] = await Promise.all([
    projectsApi.getWorkflow(projectId.value).catch(() => undefined),
    // The ACH as an artifact carries evidence, impact and suggested fix; without it the rest shows.
    artifactsApi.getArtifact(projectId.value, loaded.artifact ?? loaded.id).catch(() => undefined),
  ])
  workflow.value = loadedWorkflow
  evidence.value = loadedEvidence
  if (loaded.available_actions.includes('decide_scope_change')) {
    const artifacts = await artifactsApi.listArtifacts(projectId.value).catch(() => [])
    requirementsArtifacts.value = artifacts.filter((item) => item.stage === 'requirements')
  }
}

const loader = useLoader(async () => {
  project.value = await projectsApi.getProject(projectId.value)
  await loadFinding()
  live.open({
    projectId: projectId.value,
    since: project.value.last_sequence,
    query: { class: 'domain' },
    onEvent: handleEvent,
    reload: async () => {
      await loadFinding()
      return undefined
    },
  })
  await resumeAfterStepUp()
})

watch([projectId, findingId], () => void loader.run(), { immediate: true })

/** ESP-20 section 6: a status change by `finding.*` refreshes the page and is announced. */
function handleEvent(event: CloudEvent, eventClass: EventClass): void {
  if (eventClass !== 'domain') return
  const type = event.type.replace(/^br\.easycode\./, '')
  if (!REFRESHING_GROUPS.some((group) => type.startsWith(group))) return
  if (refreshTimer !== undefined) clearTimeout(refreshTimer)
  refreshTimer = setTimeout(() => {
    refreshTimer = undefined
    const before = finding.value?.status
    void loadFinding()
      .then(() => {
        if (finding.value && finding.value.status !== before) {
          announce(
            t('findings.detail.statusChanged', {
              status: t(`states.finding.${finding.value.status}`),
            }),
          )
        }
      })
      .catch(() => undefined)
  }, REFRESH_DELAY_MS)
}

onBeforeUnmount(() => {
  if (refreshTimer !== undefined) clearTimeout(refreshTimer)
})

async function resumeAfterStepUp(): Promise<void> {
  const pending = takePendingFindingDecision(projectId.value, findingId.value)
  if (!pending || !finding.value?.available_actions.includes(pending.decision.action)) return
  if (!isApprover.value) return
  announce(t('findings.decision.sendingAfterStepUp'))
  await nextTick()
  await decisionForm.value?.resume(pending.decision)
}

function onDecided(decided: Finding): void {
  finding.value = decided
  conflict.value = false
  announce(t('findings.detail.statusChanged', { status: t(`states.finding.${decided.status}`) }))
  void loadFinding().catch(() => undefined)
}

/** ER-20-15: decided by someone else meanwhile; the page shows the current state, text kept. */
function onConflict(): void {
  conflict.value = true
  void loadFinding().catch(() => undefined)
}

// ------------------------------------------------------------------ display

const isApprover = computed(() => project.value?.my_roles.includes('approver') ?? false)
const projectClosed = computed(() => ['completed', 'canceled'].includes(project.value?.state ?? ''))
const hasDecision = computed(() =>
  DECISION_ACTIONS.some((action) => finding.value?.available_actions.includes(action)),
)
const showDecision = computed(() => isApprover.value && hasDecision.value && !projectClosed.value)
/** ESP-20 section 6: a member sees who decides instead of the options. */
const decidable = computed(
  () =>
    finding.value !== undefined &&
    (ACTIVE_STATUSES.includes(finding.value.status) || finding.value.status === 'accepted') &&
    finding.value.status !== 'awaiting_scope',
)
const notFound = computed(() => isApiError(loader.error.value) && loader.error.value.status === 404)
const internalCycle = computed(() => (finding.value ? isInternalCycle(finding.value) : false))
const acceptanceExpired = computed(() => {
  const until = finding.value?.acceptance?.valid_until
  return Boolean(finding.value?.acceptance_expired || (until && until < today()))
})

const breadcrumbs = computed(() => [
  { title: t('pages.projects.title'), to: { name: ROUTE_NAMES.projects } },
  {
    title: project.value?.name ?? '',
    to: { name: ROUTE_NAMES.project, params: { projectId: projectId.value } },
  },
  {
    title: t('pages.project.findings'),
    to: { name: ROUTE_NAMES.projectFindings, params: { projectId: projectId.value } },
  },
  { title: findingId.value },
])

function stageName(stage: string): string {
  const key = `stages.${stage}`
  return i18n.te(key) ? t(key) : stage
}

/** Canvas "Achado": a blocking finding shows its effect in the critical note tone. */
const blocks = computed(() => (finding.value ? blockingText(finding.value) !== undefined : false))
const SEVERITY_TONES: Record<string, string> = {
  critical: 'crit',
  high: 'crit',
  medium: 'warn',
  low: 'mute',
}

const blocking = computed(() => {
  if (!finding.value) return ''
  const text = blockingText(finding.value)
  if (!text) return t('findings.blocking.none')
  return t(text.key, { stage: text.stage ? stageName(text.stage) : '' })
})

function headerText(field: string): string {
  const value = evidence.value?.header[field]
  return typeof value === 'string' ? value : ''
}

function cycleResult(cycle: FindingCycle): string {
  if (cycle.result === 'fixed') return t('findings.cycles.fixed')
  if (cycle.result === 'not_fixed') return t('findings.cycles.notFixed')
  return t('findings.cycles.inProgress')
}

const stages = computed(() => workflow.value?.stages ?? [])
const destination = computed<StageSummary | undefined>(() =>
  stages.value.find((stage) => stage.id === finding.value?.destination_stage),
)
/** US-34 / ER-20-12: the reopening this finding caused (`reopened_by_finding` of the stage). */
const reopening = computed(() => {
  const stage = destination.value
  if (!finding.value || internalCycle.value || !stage) return undefined
  if (stage.state !== 'reopened' || stage.reopened_by_finding !== finding.value.id) return undefined
  return {
    stage,
    // CA-20-19: the stage that issued the finding is never listed as blocked.
    blocked: stages.value.filter(
      (item) => item.state === 'blocked' && item.id !== finding.value?.origin_stage,
    ),
  }
})
/** CA-20-18: back approved, the later stages that were approved get a reevaluation wave. */
const reevaluation = computed(() => {
  if (!finding.value || internalCycle.value || destination.value?.state !== 'approved') return []
  return stages.value.filter((item) => item.reevaluation_pending)
})
const requirementsStage = computed(() => stages.value.find((item) => item.id === 'requirements'))
const scopeEscalation = computed(() =>
  [...(finding.value?.escalations ?? [])]
    .reverse()
    .find((item) => item.decision === 'scope_change'),
)
</script>

<template>
  <!-- Layout: canvas "Achado" (Achado.dc.html). -->
  <LoadingState v-if="loader.loading.value && !finding" />
  <template v-else-if="loader.error.value && !finding">
    <h1>{{ $t('pages.finding.title') }}</h1>
    <!-- RNF-04: same message for missing and without access. -->
    <p v-if="notFound" class="note note-warn" role="alert">
      {{ $t('findings.detail.notFound') }}
    </p>
    <ProblemAlert v-else :error="loader.error.value" retry @retry="loader.run" />
  </template>
  <template v-else-if="project && finding">
    <AppBreadcrumbs :items="breadcrumbs" />
    <div role="status" class="sr-only">{{ message }}</div>

    <div class="finding-head">
      <div class="finding-title">
        <h1>
          <span class="mono finding-id">{{ finding.id }}</span> · {{ finding.title }}
        </h1>
        <dl class="finding-meta" data-testid="finding-summary">
          <div>
            <dt class="sr-only">{{ $t('findings.columns.status') }}</dt>
            <dd>
              <StateLabel
                machine="finding"
                :state="finding.status"
                :detail="
                  finding.status === 'accepted' && acceptanceExpired
                    ? $t('findings.expired')
                    : undefined
                "
              />
            </dd>
          </div>
          <div>
            <dt class="sr-only">{{ $t('findings.columns.severity') }}</dt>
            <dd :class="['chip', SEVERITY_TONES[finding.severity] ?? 'mute']">
              {{ $t(`findings.severity.${finding.severity}`) }}
            </dd>
          </div>
          <div>
            <dt class="sr-only">{{ $t('findings.columns.category') }}</dt>
            <dd class="chip mute">{{ finding.category }}</dd>
          </div>
          <div class="t2 meta-text">
            <dt>{{ $t('findings.detail.author') }}</dt>
            <dd>{{ $t(`agents.${finding.author}`) }}</dd>
          </div>
          <div v-if="finding.origin_wave" class="t2 meta-text">
            <dt>{{ $t('findings.detail.originWave') }}</dt>
            <dd>
              <router-link
                :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: finding.origin_wave } }"
                class="mono"
              >
                {{ finding.origin_wave }}
              </router-link>
            </dd>
          </div>
          <div v-if="finding.origin_stage" class="t2 meta-text">
            <dt>{{ $t('findings.columns.origin') }}</dt>
            <dd>
              <router-link
                :to="{
                  name: ROUTE_NAMES.stage,
                  params: { projectId, stageId: finding.origin_stage },
                }"
              >
                {{ stageName(finding.origin_stage) }}
              </router-link>
            </dd>
          </div>
          <div class="t2 meta-text">
            <dt>{{ $t('findings.columns.destination') }}</dt>
            <dd>
              <router-link
                :to="{
                  name: ROUTE_NAMES.stage,
                  params: { projectId, stageId: finding.destination_stage },
                }"
              >
                {{ stageName(finding.destination_stage) }}
              </router-link>
            </dd>
          </div>
          <div class="t2 meta-text">
            <dt>{{ $t('findings.columns.cycles') }}</dt>
            <dd>
              {{ $t('findings.cyclesOf', { n: finding.cycles ?? finding.cycle_history.length }) }}
            </dd>
          </div>
        </dl>
      </div>
      <LiveConnectionStatus :status="live.status.value" />
    </div>

    <div :class="['note', blocks ? 'note-crit' : 'note-ok']">
      <CanvasIcon :name="blocks ? 'alert' : 'check'" :size="20" />
      <span>
        {{ $t('findings.columns.blocking') }}:
        <strong data-testid="finding-blocking">{{ blocking }}</strong>
      </span>
    </div>

    <p v-if="conflict" class="note note-warn" role="alert">
      {{ $t('findings.detail.conflict') }}
    </p>

    <div class="finding-layout">
      <div class="finding-main">
        <section aria-labelledby="evidence-heading" class="card finding-card finding-evidence">
          <h2 id="evidence-heading">{{ $t('findings.detail.evidence') }}</h2>
          <p class="t2 card-note">{{ $t('artifacts.sanitizedNotice') }}</p>
          <template v-if="evidence">
            <h3 class="sr-only">{{ $t('findings.detail.evidenceText') }}</h3>
            <pre class="ev mono" data-testid="finding-evidence">{{ headerText('evidencia') }}</pre>
            <div class="kv">
              <h3 class="t2 kv-label">{{ $t('findings.detail.impact') }}</h3>
              <p class="kv-value pre-wrap">{{ headerText('impacto') }}</p>
            </div>
            <div class="kv">
              <h3 class="t2 kv-label">{{ $t('findings.detail.suggestedFix') }}</h3>
              <p class="kv-value pre-wrap">{{ headerText('correcao_sugerida') }}</p>
            </div>
          </template>
          <p v-else class="card-text">{{ $t('findings.detail.noEvidence') }}</p>
          <div class="kv">
            <h3 class="t2 kv-label">{{ $t('findings.detail.affected') }}</h3>
            <ul class="plain-list kv-value" data-testid="finding-affected">
              <li v-for="item in finding.affected_artifacts" :key="item.artifact">
                <router-link
                  :to="{
                    name: ROUTE_NAMES.artifact,
                    params: { projectId, artifactId: item.artifact },
                  }"
                  class="mono"
                >
                  {{ item.artifact }}
                </router-link>
                <template v-if="item.item">
                  · <span class="mono">{{ item.item }}</span></template
                >
              </li>
            </ul>
          </div>
        </section>

        <section aria-labelledby="cycles-heading" class="card finding-card">
          <h2 id="cycles-heading" class="card-heading">{{ $t('findings.cycles.heading') }}</h2>
          <p class="t2 cycles-type" data-testid="finding-cycle-type">
            {{
              internalCycle
                ? $t('findings.cycles.internal')
                : $t('findings.cycles.reopens', { stage: stageName(finding.destination_stage) })
            }}
          </p>
          <p v-if="finding.cycle_history.length === 0" class="li row-text">
            {{ $t('findings.cycles.none') }}
          </p>
          <ol v-else class="plain-list" data-testid="finding-cycles">
            <li v-for="cycle in finding.cycle_history" :key="cycle.number" class="li">
              <span class="chip mute">{{ $t('findings.cycles.number', { n: cycle.number }) }}</span>
              <span class="row-text">
                <template v-if="cycle.rework_wave">
                  {{ $t('findings.cycles.rework') }}
                  <router-link
                    :to="{
                      name: ROUTE_NAMES.wave,
                      params: { projectId, waveId: cycle.rework_wave },
                    }"
                    class="mono"
                  >
                    {{ cycle.rework_wave }}
                  </router-link>
                  ·
                </template>
                <template v-if="cycle.commit_or_version">
                  <span class="mono">{{ cycle.commit_or_version }}</span> ·
                </template>
                <template v-if="cycle.revalidation_wave">
                  {{ $t('findings.cycles.revalidation') }}
                  <router-link
                    :to="{
                      name: ROUTE_NAMES.wave,
                      params: { projectId, waveId: cycle.revalidation_wave },
                    }"
                    class="mono"
                  >
                    {{ cycle.revalidation_wave }}
                  </router-link>
                  ·
                </template>
                {{ cycleResult(cycle) }}
              </span>
            </li>
          </ol>
        </section>

        <section
          v-if="reopening || reevaluation.length > 0"
          aria-labelledby="reopening-heading"
          class="card finding-card"
          data-testid="finding-reopening"
        >
          <h2 id="reopening-heading" class="card-heading">
            {{ $t('findings.reopening.heading') }}
          </h2>
          <template v-if="reopening">
            <p class="li row-text">
              <span>
                {{ $t('findings.reopening.reopened') }}
                <router-link
                  :to="{
                    name: ROUTE_NAMES.stage,
                    params: { projectId, stageId: reopening.stage.id },
                  }"
                >
                  {{ stageName(reopening.stage.id) }}
                </router-link>
              </span>
            </p>
            <p v-if="reopening.blocked.length === 0" class="li row-text">
              {{ $t('findings.reopening.noneBlocked') }}
            </p>
            <p v-else class="li row-text">
              <span>
                {{ $t('findings.reopening.blocked') }}
                <template v-for="(stage, index) in reopening.blocked" :key="stage.id">
                  <router-link
                    :to="{ name: ROUTE_NAMES.stage, params: { projectId, stageId: stage.id } }"
                  >
                    {{ stageName(stage.id) }}</router-link><span
                    v-if="index < reopening.blocked.length - 1"
                    >,
                  </span>
                </template>
              </span>
            </p>
          </template>
          <p v-else class="li row-text">
            <span>
              {{ $t('findings.reopening.reevaluation') }}
              <template v-for="(stage, index) in reevaluation" :key="stage.id">
                <router-link
                  :to="{ name: ROUTE_NAMES.stage, params: { projectId, stageId: stage.id } }"
                >
                  {{ stageName(stage.id) }}</router-link><span
                  v-if="index < reevaluation.length - 1"
                  >,
                </span>
              </template>
            </span>
          </p>
        </section>

        <section
          v-if="finding.escalations.length > 0"
          aria-labelledby="escalations-heading"
          class="card finding-card"
        >
          <h2 id="escalations-heading" class="card-heading">
            {{ $t('findings.escalations.heading') }}
          </h2>
          <ol class="plain-list" data-testid="finding-escalations">
            <li v-for="(item, index) in finding.escalations" :key="index" class="li escalation">
              <span class="row-text">
                {{ format.dateTime(item.when) }} · {{ item.reason }}
                <template v-if="item.decision">
                  · {{ $t(`findings.escalations.decisions.${item.decision}`) }}
                  <template v-if="item.authorized_by">
                    ·
                    {{
                      $t('findings.escalations.by', {
                        who: item.authorized_by.name,
                        when: item.decided_at ? format.dateTime(item.decided_at) : '',
                      })
                    }}
                  </template>
                </template>
                <template v-else> · {{ $t('findings.escalations.waiting') }}</template>
              </span>
              <p v-if="item.justification" class="t2 row-text pre-wrap">
                {{ item.justification }}
              </p>
            </li>
          </ol>
        </section>

        <section
          v-if="finding.status === 'awaiting_scope'"
          aria-labelledby="scope-heading"
          class="card finding-card"
          data-testid="finding-scope"
        >
          <h2 id="scope-heading" class="card-heading">{{ $t('findings.scope.heading') }}</h2>
          <dl class="kv-list">
            <div v-if="scopeEscalation?.modified_by" class="kv">
              <dt class="t2">{{ $t('findings.decision.modifiedBy') }}</dt>
              <dd class="kv-value">
                {{ $t(`findings.modifiedBy.${scopeEscalation.modified_by}`) }}
              </dd>
            </div>
            <div v-if="scopeEscalation?.starting_point_artifacts?.length" class="kv">
              <dt class="t2">{{ $t('findings.decision.startingPoints') }}</dt>
              <dd class="kv-value">
                <template v-for="(id, index) in scopeEscalation.starting_point_artifacts" :key="id">
                  <router-link
                    :to="{ name: ROUTE_NAMES.artifact, params: { projectId, artifactId: id } }"
                    class="mono"
                    >{{ id }}</router-link><span
                    v-if="index < scopeEscalation.starting_point_artifacts.length - 1"
                    >,
                  </span>
                </template>
              </dd>
            </div>
            <div v-if="requirementsStage?.gate_state" class="kv">
              <dt class="t2">{{ $t('findings.scope.gate') }}</dt>
              <dd class="kv-value">{{ $t(`states.gate.${requirementsStage.gate_state}`) }}</dd>
            </div>
          </dl>
          <p class="t2 card-note">{{ $t('findings.scope.closesWhen') }}</p>
          <p v-if="scopeEscalation?.modified_by === 'human'" class="t2 card-note">
            {{ $t('findings.scope.humanEdit') }}
          </p>
          <router-link
            :to="{ name: ROUTE_NAMES.stage, params: { projectId, stageId: 'requirements' } }"
            class="btn scope-link"
          >
            {{ $t('findings.scope.openRequirements') }}
          </router-link>
        </section>

        <section
          v-if="finding.acceptance || finding.resolution || finding.discard_reason"
          aria-labelledby="result-heading"
          class="card finding-card"
          data-testid="finding-result"
        >
          <h2 id="result-heading" class="card-heading">{{ $t('findings.result.heading') }}</h2>
          <dl class="kv-list">
            <template v-if="finding.acceptance">
              <div class="kv">
                <dt class="t2">{{ $t('findings.result.acceptedBy') }}</dt>
                <dd class="kv-value">{{ finding.acceptance.per.name }}</dd>
              </div>
              <div class="kv">
                <dt class="t2">{{ $t('gates.when') }}</dt>
                <dd class="kv-value">{{ format.dateTime(finding.acceptance.when) }}</dd>
              </div>
              <div v-if="finding.acceptance.origin" class="kv">
                <dt class="t2">{{ $t('gates.from') }}</dt>
                <dd class="kv-value">{{ $t(`waves.origin.${finding.acceptance.origin}`) }}</dd>
              </div>
              <div class="kv">
                <dt class="t2">{{ $t('findings.decision.justification') }}</dt>
                <dd class="kv-value pre-wrap">{{ finding.acceptance.justification }}</dd>
              </div>
              <div class="kv">
                <dt class="t2">{{ $t('findings.decision.validUntil') }}</dt>
                <dd class="kv-value">
                  {{
                    finding.acceptance.valid_until
                      ? format.date(finding.acceptance.valid_until)
                      : $t('findings.noDeadline')
                  }}
                  <template v-if="acceptanceExpired">
                    · {{ $t('findings.result.expired') }}</template
                  >
                </dd>
              </div>
            </template>
            <div v-if="finding.resolution" class="kv">
              <dt class="t2">{{ $t('findings.result.resolution') }}</dt>
              <dd class="kv-value">{{ $t(`findings.resolution.${finding.resolution}`) }}</dd>
            </div>
            <div v-if="finding.modified_artifacts?.length" class="kv">
              <dt class="t2">{{ $t('findings.result.modified') }}</dt>
              <dd class="kv-value">
                <template v-for="(id, index) in finding.modified_artifacts" :key="id">
                  <router-link
                    :to="{ name: ROUTE_NAMES.artifact, params: { projectId, artifactId: id } }"
                    class="mono"
                    >{{ id }}</router-link><span
                    v-if="index < finding.modified_artifacts.length - 1"
                    >,
                  </span>
                </template>
              </dd>
            </div>
            <div v-if="finding.discard_reason" class="kv">
              <dt class="t2">{{ $t('findings.decision.discardReason') }}</dt>
              <dd class="kv-value pre-wrap">{{ finding.discard_reason }}</dd>
            </div>
          </dl>
        </section>
      </div>

      <aside
        v-if="showDecision || (!isApprover && decidable && !projectClosed)"
        class="finding-aside"
      >
        <FindingDecisionForm
          v-if="showDecision"
          ref="decisionForm"
          :project-id="projectId"
          :finding="finding"
          :requirements-artifacts="requirementsArtifacts"
          @decided="onDecided"
          @conflict="onConflict"
        />
        <section v-else class="card decision-member" aria-labelledby="decision-member-heading">
          <h2 id="decision-member-heading">{{ $t('findings.decision.heading') }}</h2>
          <p class="t2 card-note" data-testid="finding-approver-only">
            {{ $t('findings.decision.approverOnly') }}
          </p>
        </section>
      </aside>
    </div>
  </template>
</template>

<style scoped>
.pre-wrap {
  white-space: pre-wrap;
}
.plain-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.finding-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.finding-title {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.finding-id {
  font-size: inherit;
}
.finding-meta {
  margin: 0;
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}
.finding-meta > div {
  display: flex;
  align-items: center;
  gap: 4px;
}
.finding-meta dd {
  margin: 0;
}
.meta-text {
  font-size: 13px;
}
.meta-text + .meta-text::before {
  content: '·';
  margin-right: 4px;
}
.meta-text dt::after {
  content: ':';
}
.note-crit {
  border-color: rgba(239, 68, 68, 0.45);
  background: rgba(239, 68, 68, 0.1);
}
.note-ok {
  border-color: rgba(34, 197, 94, 0.45);
  background: rgba(34, 197, 94, 0.12);
}
.note-warn {
  margin: 0;
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.12);
}
.finding-layout {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  align-items: flex-start;
}
.finding-main {
  flex: 3 1 520px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 20px;
}
.finding-aside {
  flex: 2 1 320px;
  min-width: 0;
}
.finding-card {
  padding: 18px 22px;
}
.finding-evidence {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.card-heading {
  margin-bottom: 4px;
}
.card-note {
  margin: 0;
  font-size: 13px;
}
.card-text {
  margin: 0;
}
.ev {
  overflow-wrap: anywhere;
}
.kv-label {
  margin: 0;
  font-size: inherit;
  font-weight: 400;
  line-height: inherit;
}
.kv-value {
  margin: 0;
  text-align: right;
}
.kv-list {
  margin: 0 0 10px;
}
.kv-list dt {
  font-weight: 400;
}
.cycles-type {
  margin: 0 0 6px;
  font-size: 13px;
}
.row-text {
  flex: 1;
  margin: 0;
  font-size: 13px;
}
.escalation {
  flex-direction: column;
  align-items: stretch;
  gap: 4px;
}
.scope-link {
  align-self: flex-start;
}
.decision-member {
  padding: 22px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
</style>
