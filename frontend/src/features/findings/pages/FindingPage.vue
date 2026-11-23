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
  <v-container max-width="1200">
    <LoadingState v-if="loader.loading.value && !finding" />
    <template v-else-if="loader.error.value && !finding">
      <h1 class="text-h4 mb-4">{{ $t('pages.finding.title') }}</h1>
      <!-- RNF-04: same message for missing and without access. -->
      <v-alert v-if="notFound" type="warning" variant="tonal" role="alert">
        {{ $t('findings.detail.notFound') }}
      </v-alert>
      <ProblemAlert v-else :error="loader.error.value" retry @retry="loader.run" />
    </template>
    <template v-else-if="project && finding">
      <AppBreadcrumbs :items="breadcrumbs" />
      <div role="status" class="sr-only">{{ message }}</div>

      <div class="d-flex flex-wrap align-center ga-3 mb-2">
        <h1 class="text-h4">
          <span class="monospace">{{ finding.id }}</span> · {{ finding.title }}
        </h1>
        <StateLabel
          machine="finding"
          :state="finding.status"
          :detail="
            finding.status === 'accepted' && acceptanceExpired ? $t('findings.expired') : undefined
          "
        />
        <v-spacer />
        <LiveConnectionStatus :status="live.status.value" />
      </div>

      <v-alert v-if="conflict" type="warning" variant="tonal" role="alert" class="my-4">
        {{ $t('findings.detail.conflict') }}
      </v-alert>

      <dl class="summary-list mb-6" data-testid="finding-summary">
        <dt>{{ $t('findings.columns.severity') }}</dt>
        <dd>{{ $t(`findings.severity.${finding.severity}`) }}</dd>
        <dt>{{ $t('findings.columns.category') }}</dt>
        <dd>{{ finding.category }}</dd>
        <dt>{{ $t('findings.detail.author') }}</dt>
        <dd>{{ $t(`agents.${finding.author}`) }}</dd>
        <template v-if="finding.origin_wave">
          <dt>{{ $t('findings.detail.originWave') }}</dt>
          <dd>
            <router-link
              :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: finding.origin_wave } }"
              class="monospace"
            >
              {{ finding.origin_wave }}
            </router-link>
          </dd>
        </template>
        <template v-if="finding.origin_stage">
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
        </template>
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
        <dt>{{ $t('findings.columns.blocking') }}</dt>
        <dd data-testid="finding-blocking">{{ blocking }}</dd>
        <dt>{{ $t('findings.columns.cycles') }}</dt>
        <dd>
          {{ $t('findings.cyclesOf', { n: finding.cycles ?? finding.cycle_history.length }) }}
        </dd>
      </dl>

      <section aria-labelledby="evidence-heading" class="mb-6">
        <h2 id="evidence-heading" class="text-h6 mb-2">{{ $t('findings.detail.evidence') }}</h2>
        <p class="text-body-2 text-medium-emphasis mb-2">
          {{ $t('artifacts.sanitizedNotice') }}
        </p>
        <template v-if="evidence">
          <h3 class="text-subtitle-2">{{ $t('findings.detail.evidenceText') }}</h3>
          <pre class="evidence mb-3" data-testid="finding-evidence">{{
            headerText('evidencia')
          }}</pre>
          <h3 class="text-subtitle-2">{{ $t('findings.detail.impact') }}</h3>
          <p class="pre-wrap text-body-2 mb-3">{{ headerText('impacto') }}</p>
          <h3 class="text-subtitle-2">{{ $t('findings.detail.suggestedFix') }}</h3>
          <p class="pre-wrap text-body-2 mb-3">{{ headerText('correcao_sugerida') }}</p>
        </template>
        <p v-else class="text-body-2">{{ $t('findings.detail.noEvidence') }}</p>
        <h3 class="text-subtitle-2">{{ $t('findings.detail.affected') }}</h3>
        <ul class="plain-list" data-testid="finding-affected">
          <li v-for="item in finding.affected_artifacts" :key="item.artifact" class="text-body-2">
            <router-link
              :to="{
                name: ROUTE_NAMES.artifact,
                params: { projectId, artifactId: item.artifact },
              }"
              class="monospace"
            >
              {{ item.artifact }}
            </router-link>
            <template v-if="item.item">
              · <span class="monospace">{{ item.item }}</span></template
            >
          </li>
        </ul>
      </section>

      <section aria-labelledby="cycles-heading" class="mb-6">
        <h2 id="cycles-heading" class="text-h6 mb-2">{{ $t('findings.cycles.heading') }}</h2>
        <p class="text-body-2 mb-2" data-testid="finding-cycle-type">
          {{
            internalCycle
              ? $t('findings.cycles.internal')
              : $t('findings.cycles.reopens', { stage: stageName(finding.destination_stage) })
          }}
        </p>
        <p v-if="finding.cycle_history.length === 0" class="text-body-2">
          {{ $t('findings.cycles.none') }}
        </p>
        <ol v-else class="cycles" data-testid="finding-cycles">
          <li v-for="cycle in finding.cycle_history" :key="cycle.number" class="text-body-2 mb-1">
            {{ $t('findings.cycles.number', { n: cycle.number }) }}
            <template v-if="cycle.rework_wave">
              · {{ $t('findings.cycles.rework') }}
              <router-link
                :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: cycle.rework_wave } }"
                class="monospace"
              >
                {{ cycle.rework_wave }}
              </router-link>
            </template>
            <template v-if="cycle.commit_or_version">
              · <span class="monospace">{{ cycle.commit_or_version }}</span>
            </template>
            <template v-if="cycle.revalidation_wave">
              · {{ $t('findings.cycles.revalidation') }}
              <router-link
                :to="{
                  name: ROUTE_NAMES.wave,
                  params: { projectId, waveId: cycle.revalidation_wave },
                }"
                class="monospace"
              >
                {{ cycle.revalidation_wave }}
              </router-link>
            </template>
            · {{ cycleResult(cycle) }}
          </li>
        </ol>
      </section>

      <section
        v-if="reopening || reevaluation.length > 0"
        aria-labelledby="reopening-heading"
        class="mb-6"
        data-testid="finding-reopening"
      >
        <h2 id="reopening-heading" class="text-h6 mb-2">{{ $t('findings.reopening.heading') }}</h2>
        <template v-if="reopening">
          <p class="text-body-2 mb-1">
            {{ $t('findings.reopening.reopened') }}
            <router-link
              :to="{ name: ROUTE_NAMES.stage, params: { projectId, stageId: reopening.stage.id } }"
            >
              {{ stageName(reopening.stage.id) }}
            </router-link>
          </p>
          <p v-if="reopening.blocked.length === 0" class="text-body-2">
            {{ $t('findings.reopening.noneBlocked') }}
          </p>
          <p v-else class="text-body-2">
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
          </p>
        </template>
        <p v-else class="text-body-2">
          {{ $t('findings.reopening.reevaluation') }}
          <template v-for="(stage, index) in reevaluation" :key="stage.id">
            <router-link
              :to="{ name: ROUTE_NAMES.stage, params: { projectId, stageId: stage.id } }"
            >
              {{ stageName(stage.id) }}</router-link><span v-if="index < reevaluation.length - 1"
              >,
            </span>
          </template>
        </p>
      </section>

      <section
        v-if="finding.escalations.length > 0"
        aria-labelledby="escalations-heading"
        class="mb-6"
      >
        <h2 id="escalations-heading" class="text-h6 mb-2">
          {{ $t('findings.escalations.heading') }}
        </h2>
        <ol class="cycles" data-testid="finding-escalations">
          <li v-for="(item, index) in finding.escalations" :key="index" class="text-body-2 mb-2">
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
            <p v-if="item.justification" class="pre-wrap">{{ item.justification }}</p>
          </li>
        </ol>
      </section>

      <section
        v-if="finding.status === 'awaiting_scope'"
        aria-labelledby="scope-heading"
        class="mb-6"
        data-testid="finding-scope"
      >
        <h2 id="scope-heading" class="text-h6 mb-2">{{ $t('findings.scope.heading') }}</h2>
        <dl class="summary-list">
          <template v-if="scopeEscalation?.modified_by">
            <dt>{{ $t('findings.decision.modifiedBy') }}</dt>
            <dd>{{ $t(`findings.modifiedBy.${scopeEscalation.modified_by}`) }}</dd>
          </template>
          <template v-if="scopeEscalation?.starting_point_artifacts?.length">
            <dt>{{ $t('findings.decision.startingPoints') }}</dt>
            <dd>
              <template v-for="(id, index) in scopeEscalation.starting_point_artifacts" :key="id">
                <router-link
                  :to="{ name: ROUTE_NAMES.artifact, params: { projectId, artifactId: id } }"
                  class="monospace"
                  >{{ id }}</router-link><span
                  v-if="index < scopeEscalation.starting_point_artifacts.length - 1"
                  >,
                </span>
              </template>
            </dd>
          </template>
          <template v-if="requirementsStage?.gate_state">
            <dt>{{ $t('findings.scope.gate') }}</dt>
            <dd>{{ $t(`states.gate.${requirementsStage.gate_state}`) }}</dd>
          </template>
        </dl>
        <p class="text-body-2 mt-2">{{ $t('findings.scope.closesWhen') }}</p>
        <p v-if="scopeEscalation?.modified_by === 'human'" class="text-body-2 mt-2">
          {{ $t('findings.scope.humanEdit') }}
        </p>
        <router-link
          :to="{ name: ROUTE_NAMES.stage, params: { projectId, stageId: 'requirements' } }"
          class="text-body-2"
        >
          {{ $t('findings.scope.openRequirements') }}
        </router-link>
      </section>

      <section
        v-if="finding.acceptance || finding.resolution || finding.discard_reason"
        aria-labelledby="result-heading"
        class="mb-6"
        data-testid="finding-result"
      >
        <h2 id="result-heading" class="text-h6 mb-2">{{ $t('findings.result.heading') }}</h2>
        <dl class="summary-list">
          <template v-if="finding.acceptance">
            <dt>{{ $t('findings.result.acceptedBy') }}</dt>
            <dd>{{ finding.acceptance.per.name }}</dd>
            <dt>{{ $t('gates.when') }}</dt>
            <dd>{{ format.dateTime(finding.acceptance.when) }}</dd>
            <template v-if="finding.acceptance.origin">
              <dt>{{ $t('gates.from') }}</dt>
              <dd>{{ $t(`waves.origin.${finding.acceptance.origin}`) }}</dd>
            </template>
            <dt>{{ $t('findings.decision.justification') }}</dt>
            <dd class="pre-wrap">{{ finding.acceptance.justification }}</dd>
            <dt>{{ $t('findings.decision.validUntil') }}</dt>
            <dd>
              {{
                finding.acceptance.valid_until
                  ? format.date(finding.acceptance.valid_until)
                  : $t('findings.noDeadline')
              }}
              <template v-if="acceptanceExpired"> · {{ $t('findings.result.expired') }}</template>
            </dd>
          </template>
          <template v-if="finding.resolution">
            <dt>{{ $t('findings.result.resolution') }}</dt>
            <dd>{{ $t(`findings.resolution.${finding.resolution}`) }}</dd>
          </template>
          <template v-if="finding.modified_artifacts?.length">
            <dt>{{ $t('findings.result.modified') }}</dt>
            <dd>
              <template v-for="(id, index) in finding.modified_artifacts" :key="id">
                <router-link
                  :to="{ name: ROUTE_NAMES.artifact, params: { projectId, artifactId: id } }"
                  class="monospace"
                  >{{ id }}</router-link><span v-if="index < finding.modified_artifacts.length - 1"
                  >,
                </span>
              </template>
            </dd>
          </template>
          <template v-if="finding.discard_reason">
            <dt>{{ $t('findings.decision.discardReason') }}</dt>
            <dd class="pre-wrap">{{ finding.discard_reason }}</dd>
          </template>
        </dl>
      </section>

      <FindingDecisionForm
        v-if="showDecision"
        ref="decisionForm"
        :project-id="projectId"
        :finding="finding"
        :requirements-artifacts="requirementsArtifacts"
        @decided="onDecided"
        @conflict="onConflict"
      />
      <p
        v-else-if="!isApprover && decidable && !projectClosed"
        class="text-body-2 mb-6"
        data-testid="finding-approver-only"
      >
        {{ $t('findings.decision.approverOnly') }}
      </p>
    </template>
  </v-container>
</template>

<style scoped>
.pre-wrap {
  white-space: pre-wrap;
}

.evidence {
  white-space: pre;
  overflow-x: auto;
  max-width: 100%;
  padding: 8px 12px;
  border-radius: 4px;
  background: rgba(var(--v-theme-on-surface), 0.05);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.875rem;
}

.cycles {
  padding-left: 1.5rem;
}
</style>
