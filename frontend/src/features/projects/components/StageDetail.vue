<script setup lang="ts">
import { computed, defineAsyncComponent, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { isApiError } from '@/shared/api/problemDetails'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import RepositoryPanel from '@/features/repository/components/RepositoryPanel.vue'
import TaskPlan from '@/features/tasks/components/TaskPlan.vue'
import { STAGE_VIEWS } from '@/features/stages/stageViews'
import StageEstimate from '@/features/costs/components/StageEstimate.vue'
import StageExitCriteria from '@/features/gates/components/StageExitCriteria.vue'
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
/** ESP-07 to ESP-16: the stage-specific view, when the stage has one. */
const stageView = computed(() => {
  const load = STAGE_VIEWS[stageId.value]
  return load ? defineAsyncComponent(load) : undefined
})
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
/** US-07 CA-3: only the Validation and Implementation gates have a mode to choose. */
const hasModeChoice = computed(() =>
  ['gate.validation', 'gate.implementation'].includes(stage.value?.gate ?? `gate.${stageId.value}`),
)
const isApprover = computed(() => store.project?.my_roles.includes('approver') ?? false)
/** US-40: the budget balance, to say beforehand when the estimate exceeds it. */
const balance = computed(() =>
  subtractMoney(store.project?.summary?.stop_limit, store.project?.summary?.accumulated_cost),
)

/** ESP-06 section 8: the position of the stage in the pipeline ("Etapa N"), as the workflow tab shows. */
const position = computed(() => {
  const index = (store.workflow?.stages ?? [])
    .filter((item) => item.type === 'stage')
    .findIndex((item) => item.id === stageId.value)
  return index >= 0 ? index + 1 : undefined
})

function siblingName(id: string): string {
  const key = `stages.${id}`
  return i18n.te(key) ? t(key) : id
}

watch([stageId, () => store.revision], () => void loader.run(), { immediate: true })
</script>

<template>
  <!-- Layout: canvas "Etapa" artboards (EtapaRequisitos … EtapaSeguranca): header, notes, strip, cards, side panel. -->
  <section class="stage" aria-labelledby="stage-heading">
    <router-link :to="{ name: ROUTE_NAMES.project, params: { projectId } }" class="stage-back">
      {{ $t('projects.stage.back') }}
    </router-link>
    <div class="stage-head">
      <div class="stage-title">
        <h2 id="stage-heading">{{ $t('projects.stage.heading', { stage: stageName }) }}</h2>
        <span v-if="stage" class="stage-meta">
          <span v-if="position" class="chip mute">
            {{ $t('projects.workflow.position', { n: position }) }}
          </span>
          <StateLabel machine="stage" :state="stage.state ?? 'not_started'" />
          <!-- ER-06-20: derived marks stay apart from the state label. -->
          <span
            v-if="stage.reevaluation_pending"
            class="chip warn"
            data-testid="stage-reevaluation"
          >
            {{ $t('projects.stage.reevaluationPending') }}
          </span>
          <span class="t2 meta-text">{{ $t(`agents.${stage.agent}`) }}</span>
        </span>
      </div>
      <div v-if="stage" class="stage-actions">
        <!-- ESP-09/ESP-10: the agent skills and configuration live in "Agentes e skills" (PL-31). -->
        <router-link
          v-if="stage.agent !== 'orchestrator'"
          :to="{ name: ROUTE_NAMES.projectAgents, params: { projectId, agent: stage.agent } }"
          class="btn"
          data-testid="stage-agent-link"
        >
          <CanvasIcon name="agent" :size="16" />
          {{ $t('projects.stage.agentConfiguration') }}
        </router-link>
        <router-link
          v-if="stage.gate && stage.current_round"
          :to="{
            name: ROUTE_NAMES.gate,
            params: { projectId, gateId: stage.gate, round: stage.current_round },
          }"
          class="btn btn-p"
          data-testid="stage-gate-link"
        >
          <CanvasIcon name="gate" :size="16" />
          {{ $t('projects.stage.openGate', { round: stage.current_round }) }}
        </router-link>
      </div>
    </div>

    <LoadingState v-if="loader.loading.value && !stage" />
    <div v-else-if="notFound" class="note note-warn" role="alert">
      <CanvasIcon name="alert" />
      <span class="note-text">{{ $t('problems.not-found') }}</span>
    </div>
    <ProblemAlert
      v-else-if="loader.error.value && !stage"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else-if="stage">
      <!-- US-34 / ESP-08 6: the finding that reopened the stage, in link. -->
      <p
        v-if="stage.state === 'reopened' && stage.reopened_by_finding"
        class="note note-warn"
        data-testid="stage-reopened-by"
      >
        <CanvasIcon name="alert" />
        <span class="note-text">
          <span>
            {{ $t('projects.stage.reopenedBy') }}
            <router-link
              :to="{
                name: ROUTE_NAMES.finding,
                params: { projectId, findingId: stage.reopened_by_finding },
              }"
              class="mono"
            >
              {{ stage.reopened_by_finding }}
            </router-link>
          </span>
        </span>
      </p>
      <p v-if="blockedByReopening" class="note note-warn" data-testid="stage-blocked-reopening">
        <CanvasIcon name="pause" />
        <span class="note-text">{{ $t('projects.stage.blockedByReopening') }}</span>
      </p>
      <p v-if="stage.reevaluation_pending" class="note">
        <CanvasIcon name="bolt" />
        <span class="note-text">{{ $t('projects.stage.reevaluationHelp') }}</span>
      </p>
      <!-- Canvas EtapaSeguranca: the blocking finding note, with the decision link. -->
      <div
        v-if="blockingFindings.length > 0"
        class="note note-crit"
        data-testid="stage-blocking-findings"
      >
        <CanvasIcon name="alert" />
        <div class="note-text">
          <strong>{{ $t('projects.stage.blockingFindingsNotice') }}</strong>
          <ul class="plain-list note-list">
            <li v-for="finding in blockingFindings" :key="finding.id">
              <span class="mono">{{ finding.id }}</span> ·
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
        </div>
      </div>

      <p v-if="parallel.length > 0" class="note" data-testid="stage-parallel">
        <CanvasIcon name="play" />
        <span class="note-text">
          <span>
            {{ $t('projects.stage.parallelWith') }}
            <template v-for="(sibling, index) in parallel" :key="sibling.id">
              <router-link
                :to="{ name: ROUTE_NAMES.stage, params: { projectId, stageId: sibling.id } }"
                >{{ siblingName(sibling.id) }}</router-link><span v-if="index < parallel.length - 1"
                >,
              </span>
            </template>
          </span>
        </span>
      </p>

      <!-- Canvas "Critério de saída" strip: the gate checks of the stage (PC-20, PL-46). -->
      <StageExitCriteria :project-id="projectId" :stage="stage" :revision="store.revision" />

      <!-- The approval, time, tokens and cost of the stage. -->
      <dl class="card stage-strip" data-testid="stage-summary">
        <div class="strip-item">
          <dt class="t2">{{ $t('projects.stage.approval') }}</dt>
          <dd>
            {{
              stage.approval?.mode === 'automatic'
                ? $t('projects.stage.approvalAutomatic')
                : $t('projects.stage.approvalHuman')
            }}
          </dd>
        </div>
        <div class="strip-item">
          <dt class="t2">{{ $t('waves.cost.executionTime') }}</dt>
          <dd>{{ format.duration(stage.time.execution_ms) }}</dd>
        </div>
        <div class="strip-item">
          <dt class="t2">{{ $t('waves.cost.cycleTime') }}</dt>
          <dd>{{ format.duration(stage.time.cycle_ms) }}</dd>
        </div>
        <div class="strip-item">
          <dt class="t2">{{ $t('projects.stage.tokens') }}</dt>
          <dd>
            {{
              $t('projects.stage.tokensValue', {
                input: format.integer(stage.tokens.input),
                output: format.integer(stage.tokens.output),
                cache: format.integer(stage.tokens.read_cache + stage.tokens.write_cache),
              })
            }}
          </dd>
        </div>
        <div class="strip-item">
          <dt class="t2">{{ $t('waves.cost.useful') }}</dt>
          <dd>{{ format.money(stage.detailed_cost.useful) }}</dd>
        </div>
        <div class="strip-item">
          <dt class="t2">{{ $t('waves.cost.lost') }}</dt>
          <dd>{{ format.money(stage.detailed_cost.lost) }}</dd>
        </div>
        <div class="strip-item">
          <dt class="t2">{{ $t('waves.cost.total') }}</dt>
          <dd>{{ format.money(stage.detailed_cost.total) }}</dd>
        </div>
      </dl>

      <TaskPlan
        v-if="showsTasks"
        :project-id="projectId"
        :waves="stage.waves"
        :show-execution="isImplementation"
        :revision="store.revision"
      />
      <div v-if="isImplementation" class="card stage-card">
        <RepositoryPanel :project-id="projectId" :heading-level="3" />
      </div>

      <component
        :is="stageView"
        v-if="stageView"
        :project-id="projectId"
        :stage="stage"
        :revision="store.revision"
      />

      <div class="stage-cols">
        <div class="stage-main">
          <section class="card list-card" aria-labelledby="stage-waves-heading">
            <div class="list-head">
              <h3 id="stage-waves-heading">{{ $t('pages.project.waves') }}</h3>
              <span class="chip mute">{{ stage.waves.length }}</span>
            </div>
            <p v-if="stage.waves.length === 0" class="t2 list-empty">
              {{ $t('projects.stage.noWaves') }}
            </p>
            <ul v-else class="plain-list" data-testid="stage-waves">
              <li v-for="wave in stage.waves" :key="wave.id" class="row">
                <router-link
                  :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: wave.id } }"
                  class="mono row-id"
                >
                  {{ wave.id }}
                </router-link>
                <span class="row-main">
                  <template v-if="wave.task">
                    · {{ $t('waves.task', { task: wave.task }) }}</template
                  >
                  · {{ $t(`waves.purpose.${wave.purpose}`) }}
                </span>
                <StateLabel machine="wave" :state="wave.state" />
                <span v-if="wave.cost_total" class="t2 row-side">
                  {{ format.money(wave.cost_total) }}
                </span>
                <template v-if="wave.agent === 'developer-agent'">
                  <router-link
                    :to="{
                      name: ROUTE_NAMES.wave,
                      params: { projectId, waveId: wave.id },
                      hash: '#evidence',
                    }"
                    class="ib row-link"
                    :data-testid="`stage-evidence-${wave.id}`"
                  >
                    {{ $t('evidence.openOf', { wave: wave.id }) }}
                  </router-link>
                </template>
              </li>
            </ul>
          </section>

          <section class="card list-card" aria-labelledby="stage-artifacts-heading">
            <div class="list-head">
              <h3 id="stage-artifacts-heading">{{ $t('pages.project.artifacts') }}</h3>
              <span class="chip mute">{{ stage.artifacts.length }}</span>
            </div>
            <p v-if="stage.artifacts.length === 0" class="t2 list-empty">
              {{ $t('artifacts.empty') }}
            </p>
            <ul v-else class="plain-list" data-testid="stage-artifacts">
              <li v-for="artifact in stage.artifacts" :key="artifact.id" class="row">
                <router-link
                  :to="{
                    name: ROUTE_NAMES.artifact,
                    params: { projectId, artifactId: artifact.id },
                  }"
                  class="mono row-id"
                >
                  {{ artifact.id }}
                </router-link>
                <span class="row-main">
                  · <strong>{{ artifact.title }}</strong> ·
                  <span class="t2">{{
                    $t('artifacts.versionNumber', { n: artifact.version })
                  }}</span>
                </span>
                <StateLabel machine="artifact" :state="artifact.state" />
              </li>
            </ul>
          </section>

          <section class="card list-card" aria-labelledby="stage-findings-heading">
            <div class="list-head">
              <h3 id="stage-findings-heading">{{ $t('projects.stage.findings') }}</h3>
              <span class="chip mute">{{ stage.findings?.length ?? 0 }}</span>
            </div>
            <p v-if="!stage.findings?.length" class="t2 list-empty">
              {{ $t('projects.stage.noFindings') }}
            </p>
            <ul v-else class="plain-list" data-testid="stage-findings">
              <li v-for="finding in stage.findings" :key="finding.id" class="row">
                <router-link
                  :to="{
                    name: ROUTE_NAMES.finding,
                    params: { projectId, findingId: finding.id },
                  }"
                  class="mono row-id"
                >
                  {{ finding.id }}
                </router-link>
                <span class="row-main">
                  · {{ finding.title }} ·
                  <span class="t2">{{ $t(`findings.severity.${finding.severity}`) }}</span>
                </span>
                <StateLabel machine="finding" :state="finding.status" />
                <span v-if="finding.blocking" class="chip crit">
                  {{ $t('projects.stage.blocking') }}
                </span>
              </li>
            </ul>
            <div class="list-foot">
              <router-link :to="{ name: ROUTE_NAMES.projectFindings, params: { projectId } }">
                {{ $t('projects.stage.allFindings') }}
              </router-link>
            </div>
          </section>
        </div>

        <!-- Canvas side panel (.edp): the gate approval mode and the estimate of the stage. -->
        <div class="card edp stage-side">
          <!-- US-07 RN-4 / CA-8: the stage shows the gate mode in text and leads to the configuration. -->
          <div v-if="stage.approval?.mode" class="gate-box" data-testid="stage-approval-mode">
            <span class="gate-line">
              {{
                $t('projects.gateModes.stageMode', {
                  mode: $t(`projects.gateModes.modes.${stage.approval.mode}`),
                })
              }}
            </span>
            <router-link
              v-if="hasModeChoice"
              :to="{ name: ROUTE_NAMES.projectConfiguration, params: { projectId } }"
              data-testid="stage-approval-configuration"
            >
              {{ $t('projects.gateModes.toConfiguration') }}
            </router-link>
          </div>
          <!-- US-40 (ESP-07 3, ESP-08 3): estimate with its basis, estimated × performed. -->
          <StageEstimate
            :project-id="projectId"
            :stage="stage.id"
            :balance="balance"
            :revision="store.revision"
          />
        </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.stage {
  display: flex;
  flex-direction: column;
  gap: 20px;
  min-width: 0;
}

.stage-back {
  align-self: flex-start;
  display: inline-flex;
  align-items: center;
  min-height: 32px;
  font-size: 13px;
}

.stage-head {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: flex-end;
  gap: 16px;
}

.stage-title {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

/* The stage title reads as the artboard h1 (the page h1 is the project name). */
.stage-title h2 {
  font-size: 28px;
  line-height: 36px;
}

.stage-meta {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}

.meta-text {
  font-size: 13px;
}

.stage-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.note {
  margin: 0;
}

.note > svg {
  flex: none;
}

.note-text {
  flex: 1 1 400px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.note-list {
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.note-crit {
  flex-wrap: wrap;
  border-color: rgba(239, 68, 68, 0.45);
  background: rgba(239, 68, 68, 0.1);
}

.note-warn {
  flex-wrap: wrap;
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.1);
}

.stage-strip {
  margin: 0;
  padding: 14px 18px;
  display: flex;
  flex-wrap: wrap;
  gap: 10px 24px;
  align-items: center;
}

.strip-item {
  display: inline-flex;
  gap: 6px;
  align-items: baseline;
  font-size: 13px;
}

.strip-item dd {
  margin: 0;
  font-weight: 600;
}

.stage-card {
  padding: 18px 22px;
}

.stage-cols {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  align-items: flex-start;
}

.stage-main {
  flex: 3 1 520px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.list-card {
  overflow: hidden;
}

.list-head {
  padding: 16px 18px 10px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.list-head h3 {
  margin: 0;
  font-size: 18px;
  line-height: 24px;
  font-weight: 700;
}

.list-empty {
  margin: 0;
  padding: 0 18px 18px;
  font-size: 13px;
}

.list-card ul {
  margin: 0;
}

.row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  border-top: 1px solid rgba(255, 255, 255, 0.07);
  flex-wrap: wrap;
}

.row-id {
  flex: none;
  min-width: 64px;
}

.row-main {
  flex: 1 1 240px;
  min-width: 0;
}

.row-side {
  font-size: 13px;
}

.row-link {
  display: inline-flex;
  align-items: center;
  min-height: 44px;
  text-decoration: none;
  color: #e3e6fa;
}

.list-foot {
  padding: 12px 18px 16px;
  border-top: 1px solid rgba(255, 255, 255, 0.07);
  font-size: 13px;
}

.stage-side {
  flex: 2 1 360px;
  min-width: 0;
  padding: 20px 22px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.stage-side:empty {
  display: none;
}

.gate-box {
  padding: 12px;
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(140, 150, 255, 0.14);
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 13px;
}
</style>
