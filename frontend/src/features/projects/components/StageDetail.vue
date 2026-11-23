<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { isApiError } from '@/shared/api/problemDetails'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import RepositoryPanel from '@/features/repository/components/RepositoryPanel.vue'
import TaskPlan from '@/features/tasks/components/TaskPlan.vue'
import StageEstimate from '@/features/costs/components/StageEstimate.vue'
import { subtractMoney } from '@/shared/format/money'
import type { Stage } from '../api/projectsApi'
import { parallelStages } from '../composables/parallelStages'
import { currentProjectApi, useCurrentProjectStore } from '../stores/currentProjectStore'

/**
 * US-49 CA-2: the chosen stage with its agent, waves (rework included), artifacts, time, tokens,
 * cost, findings and the approval it needs. It re-reads itself whenever the project page reloads
 * after a live event, so state changes show without reloading the page (CA-3).
 */
const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const format = useFormat()
const store = useCurrentProjectStore()
const stage = ref<Stage>()

const projectId = computed(() => String(route.params.projectId))
const stageId = computed(() => String(route.params.stageId))
const notFound = computed(() => isApiError(loader.error.value) && loader.error.value.status === 404)

const loader = useLoader(async () => {
  stage.value = await currentProjectApi().getStage(projectId.value, stageId.value)
})

const stageName = computed(() => {
  const key = `stages.${stageId.value}`
  return i18n.te(key) ? t(key) : (stage.value?.short_name ?? stage.value?.name ?? stageId.value)
})

/** US-18: the architect plans the tasks; US-19, US-42: the developer runs them into the repository. */
const showsTasks = computed(() => ['architecture', 'implementation'].includes(stageId.value))
const isImplementation = computed(() => stageId.value === 'implementation')
/** US-20 CA-3: Data Modeling and Design run in parallel after Architecture (ESP-09, ESP-10). */
const parallel = computed(() => parallelStages(store.workflow?.stages ?? [], stageId.value))
/** ESP-16 3.2 / ESP-12 ER-12-3: critical or high findings of this stage that block its gate. */
const blockingFindings = computed(
  () =>
    stage.value?.findings?.filter(
      (finding) => finding.blocking && finding.origin_stage === stageId.value,
    ) ?? [],
)
/** US-34 CA-1: blocked because a stage it depends on was reopened by a finding. */
const blockedByReopening = computed(
  () =>
    stage.value?.state === 'blocked' &&
    (store.workflow?.stages ?? []).some(
      (item) => item.state === 'reopened' && stage.value?.depends_on.includes(item.id),
    ),
)
const isApprover = computed(() => store.project?.my_roles.includes('approver') ?? false)
/** US-40: the budget balance, to say beforehand when the estimate exceeds it. */
const balance = computed(() =>
  subtractMoney(store.project?.summary?.stop_limit, store.project?.summary?.accumulated_cost),
)

function siblingName(id: string): string {
  const key = `stages.${id}`
  return i18n.te(key) ? t(key) : id
}

watch([stageId, () => store.revision], () => void loader.run(), { immediate: true })
</script>

<template>
  <section aria-labelledby="stage-heading">
    <router-link :to="{ name: ROUTE_NAMES.project, params: { projectId } }" class="text-body-2">
      {{ $t('projects.stage.back') }}
    </router-link>
    <h2 id="stage-heading" class="text-h5 mt-2 mb-2">
      {{ $t('projects.stage.heading', { stage: stageName }) }}
    </h2>
    <LoadingState v-if="loader.loading.value && !stage" />
    <v-alert v-else-if="notFound" type="warning" variant="tonal" role="alert">
      {{ $t('problems.not-found') }}
    </v-alert>
    <ProblemAlert
      v-else-if="loader.error.value && !stage"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else-if="stage">
      <div class="d-flex flex-wrap align-center ga-3 mb-4">
        <StateLabel machine="stage" :state="stage.state ?? 'not_started'" />
        <!-- ER-06-20: derived marks stay apart from the state label. -->
        <span
          v-if="stage.reevaluation_pending"
          class="text-body-2"
          data-testid="stage-reevaluation"
        >
          {{ $t('projects.stage.reevaluationPending') }}
        </span>
        <span class="text-body-2">{{ $t(`agents.${stage.agent}`) }}</span>
        <!-- ESP-09/ESP-10: the agent skills and configuration live in "Agentes e skills" (PL-31). -->
        <router-link
          v-if="stage.agent !== 'orchestrator'"
          :to="{ name: ROUTE_NAMES.projectAgents, params: { projectId, agent: stage.agent } }"
          class="text-body-2"
          data-testid="stage-agent-link"
        >
          {{ $t('projects.stage.agentConfiguration') }}
        </router-link>
        <router-link
          v-if="stage.gate && stage.current_round"
          :to="{
            name: ROUTE_NAMES.gate,
            params: { projectId, gateId: stage.gate, round: stage.current_round },
          }"
          data-testid="stage-gate-link"
        >
          {{ $t('projects.stage.openGate', { round: stage.current_round }) }}
        </router-link>
      </div>

      <!-- US-34 / ESP-08 6: the finding that reopened the stage, in link. -->
      <p
        v-if="stage.state === 'reopened' && stage.reopened_by_finding"
        class="text-body-2 mb-2"
        data-testid="stage-reopened-by"
      >
        {{ $t('projects.stage.reopenedBy') }}
        <router-link
          :to="{
            name: ROUTE_NAMES.finding,
            params: { projectId, findingId: stage.reopened_by_finding },
          }"
          class="monospace"
        >
          {{ stage.reopened_by_finding }}
        </router-link>
      </p>
      <p v-if="blockedByReopening" class="text-body-2 mb-2" data-testid="stage-blocked-reopening">
        {{ $t('projects.stage.blockedByReopening') }}
      </p>
      <p v-if="stage.reevaluation_pending" class="text-body-2 mb-2">
        {{ $t('projects.stage.reevaluationHelp') }}
      </p>
      <v-alert
        v-if="blockingFindings.length > 0"
        type="error"
        variant="tonal"
        class="mb-4"
        data-testid="stage-blocking-findings"
      >
        <p>{{ $t('projects.stage.blockingFindingsNotice') }}</p>
        <ul class="plain-list">
          <li v-for="finding in blockingFindings" :key="finding.id">
            <span class="monospace">{{ finding.id }}</span> ·
            {{ $t(`findings.severity.${finding.severity}`) }} ·
            {{ $t(`states.finding.${finding.status}`) }} ·
            <router-link
              :to="{
                name: ROUTE_NAMES.finding,
                params: { projectId, findingId: finding.id },
                hash: finding.status === 'escalated' && isApprover ? '#decision' : '',
              }"
            >
              {{ $t('projects.stage.decideFinding', { id: finding.id }) }}
            </router-link>
          </li>
        </ul>
      </v-alert>

      <p v-if="parallel.length > 0" class="text-body-2 mb-4" data-testid="stage-parallel">
        {{ $t('projects.stage.parallelWith') }}
        <template v-for="(sibling, index) in parallel" :key="sibling.id">
          <router-link
            :to="{ name: ROUTE_NAMES.stage, params: { projectId, stageId: sibling.id } }"
            >{{ siblingName(sibling.id) }}</router-link><span v-if="index < parallel.length - 1"
            >,
          </span>
        </template>
      </p>

      <dl class="summary-list mb-6" data-testid="stage-summary">
        <dt>{{ $t('projects.stage.approval') }}</dt>
        <dd>
          {{
            stage.approval?.mode === 'automatic'
              ? $t('projects.stage.approvalAutomatic')
              : $t('projects.stage.approvalHuman')
          }}
        </dd>
        <dt>{{ $t('waves.cost.executionTime') }}</dt>
        <dd>{{ format.duration(stage.time.execution_ms) }}</dd>
        <dt>{{ $t('waves.cost.cycleTime') }}</dt>
        <dd>{{ format.duration(stage.time.cycle_ms) }}</dd>
        <dt>{{ $t('projects.stage.tokens') }}</dt>
        <dd>
          {{
            $t('projects.stage.tokensValue', {
              input: format.integer(stage.tokens.input),
              output: format.integer(stage.tokens.output),
              cache: format.integer(stage.tokens.read_cache + stage.tokens.write_cache),
            })
          }}
        </dd>
        <dt>{{ $t('waves.cost.useful') }}</dt>
        <dd>{{ format.money(stage.detailed_cost.useful) }}</dd>
        <dt>{{ $t('waves.cost.lost') }}</dt>
        <dd>{{ format.money(stage.detailed_cost.lost) }}</dd>
        <dt>{{ $t('waves.cost.total') }}</dt>
        <dd>{{ format.money(stage.detailed_cost.total) }}</dd>
      </dl>

      <!-- US-40 (ESP-07 3, ESP-08 3): estimate with its basis, estimated × performed. -->
      <StageEstimate
        :project-id="projectId"
        :stage="stage.id"
        :balance="balance"
        :revision="store.revision"
      />

      <h3 class="text-subtitle-1 font-weight-medium mb-2">{{ $t('pages.project.waves') }}</h3>
      <p v-if="stage.waves.length === 0" class="text-body-2 mb-4">
        {{ $t('projects.stage.noWaves') }}
      </p>
      <ul v-else class="plain-list mb-6" data-testid="stage-waves">
        <li v-for="wave in stage.waves" :key="wave.id" class="text-body-2 mb-1">
          <router-link
            :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: wave.id } }"
            class="monospace"
          >
            {{ wave.id }}
          </router-link>
          <template v-if="wave.task"> · {{ $t('waves.task', { task: wave.task }) }}</template>
          · {{ $t(`waves.purpose.${wave.purpose}`) }} · {{ $t(`states.wave.${wave.state}`) }}
          <template v-if="wave.cost_total"> · {{ format.money(wave.cost_total) }}</template>
          <template v-if="wave.agent === 'developer-agent'">
            ·
            <router-link
              :to="{
                name: ROUTE_NAMES.wave,
                params: { projectId, waveId: wave.id },
                hash: '#evidence',
              }"
              :data-testid="`stage-evidence-${wave.id}`"
            >
              {{ $t('evidence.openOf', { wave: wave.id }) }}
            </router-link>
          </template>
        </li>
      </ul>

      <TaskPlan
        v-if="showsTasks"
        :project-id="projectId"
        :waves="stage.waves"
        :show-execution="isImplementation"
        :revision="store.revision"
      />
      <RepositoryPanel v-if="isImplementation" :project-id="projectId" :heading-level="3" />

      <h3 class="text-subtitle-1 font-weight-medium mb-2">{{ $t('pages.project.artifacts') }}</h3>
      <p v-if="stage.artifacts.length === 0" class="text-body-2 mb-4">
        {{ $t('artifacts.empty') }}
      </p>
      <ul v-else class="plain-list mb-6" data-testid="stage-artifacts">
        <li v-for="artifact in stage.artifacts" :key="artifact.id" class="text-body-2 mb-1">
          <router-link
            :to="{ name: ROUTE_NAMES.artifact, params: { projectId, artifactId: artifact.id } }"
            class="monospace"
          >
            {{ artifact.id }}
          </router-link>
          · {{ artifact.title }} · {{ $t('artifacts.versionNumber', { n: artifact.version }) }} ·
          {{ $t(`states.artifact.${artifact.state}`) }}
        </li>
      </ul>

      <h3 class="text-subtitle-1 font-weight-medium mb-2">{{ $t('projects.stage.findings') }}</h3>
      <p v-if="!stage.findings?.length" class="text-body-2">
        {{ $t('projects.stage.noFindings') }}
      </p>
      <ul v-else class="plain-list" data-testid="stage-findings">
        <li v-for="finding in stage.findings" :key="finding.id" class="text-body-2 mb-1">
          <router-link
            :to="{ name: ROUTE_NAMES.finding, params: { projectId, findingId: finding.id } }"
            class="monospace"
          >
            {{ finding.id }}
          </router-link>
          · {{ finding.title }} · {{ $t(`findings.severity.${finding.severity}`) }} ·
          {{ $t(`states.finding.${finding.status}`) }}
          <template v-if="finding.blocking"> · {{ $t('projects.stage.blocking') }}</template>
        </li>
      </ul>
      <router-link
        :to="{ name: ROUTE_NAMES.projectFindings, params: { projectId } }"
        class="d-inline-block text-body-2 mt-2"
      >
        {{ $t('projects.stage.allFindings') }}
      </router-link>
    </template>
  </section>
</template>
