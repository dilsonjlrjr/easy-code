<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useFormat } from '@/shared/format/useFormat'
import StateLabel from '@/shared/ui/StateLabel.vue'
import RepositoryPanel from '@/features/repository/components/RepositoryPanel.vue'
import ProjectCostSummary from '@/features/costs/components/ProjectCostSummary.vue'
import type { ProjectConfiguration, StageSummary } from '../api/projectsApi'
import { parallelStages } from '../composables/parallelStages'
import { currentProjectApi, useCurrentProjectStore } from '../stores/currentProjectStore'
import ProjectWavesTab from './ProjectWavesTab.vue'

const i18n = useI18n()
const { t } = i18n
const format = useFormat()
const store = useCurrentProjectStore()

const stages = computed(
  () => store.workflow?.stages.filter((stage) => stage.type === 'stage') ?? [],
)
const activities = computed(
  () => store.workflow?.stages.filter((stage) => stage.type === 'parallel_activity') ?? [],
)

/** Canvas "Projeto": the stage that waits on the user's gate or runs is ringed. */
const RINGS: Record<string, string> = { awaiting_gate: 'ring-gate', running: 'ring-running' }

/** Canvas "Projeto": the technical validation sits between stages and is not numbered. */
function positionLabel(stage: StageSummary): string {
  if (stage.id === 'validation') return t('projects.workflow.between')
  const numbered = stages.value.filter((item) => item.id !== 'validation')
  return t('projects.workflow.position', { n: numbered.indexOf(stage) + 1 })
}

/** The footer of a stage card shows only when the stage has something to tell. */
function hasDetails(stage: StageSummary): boolean {
  return Boolean(
    (stage.state === 'reopened' && stage.reopened_by_finding) ||
    stage.reevaluation_pending ||
    parallelText(stage) ||
    (stage.state === 'blocked' && stage.depends_on.length > 0) ||
    wavesOf(stage).length > 0 ||
    (stage.gate && stage.current_round) ||
    stage.cost,
  )
}

/**
 * Canvas "Configuração" card: the technical constraints by category and the model pair. It reads
 * the project configuration; without it (no access, failure) the card stays out.
 */
const configuration = ref<ProjectConfiguration>()
const constraintRows = computed(() => {
  const rows = new Map<string, string[]>()
  for (const item of configuration.value?.constraints?.items ?? []) {
    const descriptions = rows.get(item.category) ?? []
    descriptions.push(item.description)
    rows.set(item.category, descriptions)
  }
  return [...rows].map(([category, descriptions]) => ({ category, text: descriptions.join(' · ') }))
})
watch(
  () => store.project?.id,
  (id) => {
    configuration.value = undefined
    if (!id) return
    currentProjectApi()
      .getConfiguration(id)
      .then((result) => {
        if (store.project?.id === id) configuration.value = result.configuration
      })
      .catch(() => undefined)
  },
  { immediate: true },
)

function stageName(stage: StageSummary): string {
  const key = `stages.${stage.id}`
  return i18n.te(key) ? t(key) : (stage.short_name ?? stage.name)
}

function wavesOf(stage: StageSummary) {
  return store.waves.filter((wave) => wave.stage === stage.id)
}

/** ESP-06 3.3: with the project `created`, stages have no domain state and read "Not started". */
function stageState(stage: StageSummary): string {
  return stage.state ?? 'not_started'
}

/** US-20 CA-3: stages that wait on the same stages run side by side (Data Modeling and Design). */
function parallelText(stage: StageSummary): string {
  return parallelStages(stages.value, stage.id).map(stageName).join(', ')
}

function waitingDependencies(stage: StageSummary): string {
  return stage.depends_on.map((id) => t(`stages.${id}`)).join(', ')
}
</script>

<template>
  <!-- Layout: canvas "Projeto" (Projeto.dc.html): stages, parallel activities, waves, cost, configuration. -->
  <div class="workflow">
    <section aria-labelledby="pipeline-heading" class="stages">
      <div class="stages-head">
        <h2 id="pipeline-heading">{{ $t('projects.workflow.title') }}</h2>
        <span class="t2 stages-hint">
          <template v-if="store.project?.summary">
            {{
              $t('projects.workflow.progress', {
                approved: store.project.summary.approved_stages,
                total: store.project.summary.stages_total || stages.length,
              })
            }}
            ·
          </template>
          {{ $t('projects.workflow.hint') }}
        </span>
      </div>
      <!-- ESP-06 section 8: the pipeline is an ordered list; each item says position, name, agent, state. -->
      <ol class="pipeline">
        <li
          v-for="stage in stages"
          :key="stage.id"
          class="card stage-card"
          :class="RINGS[stageState(stage)]"
          :data-testid="`stage-${stage.id}`"
        >
          <div class="et">
            <span class="et-head">
              <span class="t2 et-position">{{ positionLabel(stage) }}</span>
              <!-- US-49 CA-2: choosing a stage opens its detail; the whole card is the target. -->
              <h3 class="et-name">
                <router-link
                  class="et-link"
                  :to="{
                    name: ROUTE_NAMES.stage,
                    params: { projectId: store.project?.id ?? '', stageId: stage.id },
                  }"
                >
                  {{ stageName(stage) }}
                </router-link>
              </h3>
              <span class="t2 et-agent">{{ $t(`agents.${stage.agent}`) }}</span>
            </span>
            <StateLabel class="et-state" machine="stage" :state="stageState(stage)" />
            <div v-if="hasDetails(stage)" class="t2 et-info">
              <!-- US-34 / ESP-06 3: the finding that reopened it and the reevaluation mark. -->
              <p v-if="stage.state === 'reopened' && stage.reopened_by_finding">
                {{ $t('projects.stage.reopenedBy') }}
                <router-link
                  :to="{
                    name: ROUTE_NAMES.finding,
                    params: {
                      projectId: store.project?.id ?? '',
                      findingId: stage.reopened_by_finding,
                    },
                  }"
                  class="mono et-inner"
                >
                  {{ stage.reopened_by_finding }}
                </router-link>
              </p>
              <p v-if="stage.reevaluation_pending">
                {{ $t('projects.stage.reevaluationPending') }}
              </p>
              <p v-if="parallelText(stage)">
                {{ $t('projects.workflow.parallelWith', { stages: parallelText(stage) }) }}
              </p>
              <p v-if="stage.state === 'blocked' && stage.depends_on.length > 0">
                {{ $t('projects.workflow.waitingFor', { stages: waitingDependencies(stage) }) }}
              </p>
              <ul v-if="wavesOf(stage).length > 0" class="stage-waves">
                <li v-for="wave in wavesOf(stage)" :key="wave.id">
                  <router-link
                    :to="{
                      name: ROUTE_NAMES.wave,
                      params: { projectId: store.project?.id ?? '', waveId: wave.id },
                    }"
                    class="mono et-inner"
                  >
                    {{ wave.id }}
                  </router-link>
                  · {{ $t(`states.wave.${wave.state}`) }}
                </li>
              </ul>
              <!-- ESP-12 section 2: the gate is reached from its stage in the workflow. -->
              <p v-if="stage.gate && stage.current_round">
                <router-link
                  :to="{
                    name: ROUTE_NAMES.gate,
                    params: {
                      projectId: store.project?.id ?? '',
                      gateId: stage.gate,
                      round: stage.current_round,
                    },
                  }"
                  class="et-inner"
                >
                  {{ $t('projects.stage.openGate', { round: stage.current_round }) }}
                </router-link>
              </p>
              <p v-if="stage.cost">
                {{ $t('projects.workflow.cost', { cost: format.money(stage.cost) }) }}
              </p>
            </div>
          </div>
        </li>
      </ol>

      <template v-if="activities.length > 0">
        <h3 class="sr-only">{{ $t('projects.workflow.activities') }}</h3>
        <ul class="activities">
          <li
            v-for="activity in activities"
            :key="activity.id"
            class="card activity"
            :data-testid="`activity-${activity.id}`"
          >
            <StateLabel machine="stage" :state="stageState(activity)" />
            <span class="activity-text">
              <strong>
                {{ $t('projects.workflow.activity') }} ·
                <router-link
                  class="activity-link"
                  :to="{
                    name: ROUTE_NAMES.stage,
                    params: { projectId: store.project?.id ?? '', stageId: activity.id },
                  }"
                >
                  {{ stageName(activity) }}
                </router-link>
              </strong>
              <span class="t2 activity-detail">
                {{ $t(`agents.${activity.agent}`) }}
                <!-- US-22 CA-1 / US-23 CA-1: the activity is approved together with a stage gate. -->
                <template v-if="activity.approved_in_gate">
                  ·
                  {{ $t('projects.workflow.approvedInGate', { gate: activity.approved_in_gate }) }}
                </template>
              </span>
            </span>
          </li>
        </ul>
      </template>
    </section>

    <div class="workflow-row">
      <div class="waves-slot">
        <ProjectWavesTab />
      </div>
      <aside class="workflow-side">
        <!-- ESP-06 3.6: cost and time, with the estimate of the next stage (US-38, US-40). -->
        <ProjectCostSummary v-if="store.project" />

        <section
          v-if="configuration && store.project"
          class="card side-card"
          aria-labelledby="workflow-configuration-heading"
          data-testid="workflow-configuration"
        >
          <h2 id="workflow-configuration-heading">{{ $t('pages.project.configuration') }}</h2>
          <div v-for="row in constraintRows" :key="row.category" class="kv">
            <span class="t2">{{ $t(`projects.constraints.categories.${row.category}`) }}</span>
            <span class="kv-value">{{ row.text }}</span>
          </div>
          <div class="kv">
            <span class="t2">{{ $t('projects.workflow.modelPair') }}</span>
            <span class="kv-value">
              <template v-if="configuration.model_pair">
                {{ configuration.model_pair.reasoning.model }} ·
                {{ configuration.model_pair.execution.model }}
              </template>
              <template v-else>{{ $t('projects.configuration.notDefined') }}</template>
            </span>
          </div>
          <router-link
            class="card-link"
            :to="{ name: ROUTE_NAMES.projectAgents, params: { projectId: store.project.id } }"
          >
            {{ $t('projects.workflow.agentSkills') }}
          </router-link>
        </section>

        <!-- ESP-06: the clone address of the project repository, for whoever has a role (US-42). -->
        <RepositoryPanel v-if="store.project" :project-id="store.project.id" />
      </aside>
    </div>
  </div>
</template>

<style scoped>
.workflow {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.stages {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.stages-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  align-items: baseline;
}

.stages-hint {
  font-size: 13px;
}

.pipeline {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
  gap: 10px;
}

.stage-card {
  overflow: hidden;
  position: relative;
}

.stage-card.ring-gate {
  box-shadow:
    inset 0 0 0 2px #f59e0b,
    0 8px 30px rgba(0, 0, 0, 0.25);
}

.stage-card.ring-running {
  box-shadow:
    inset 0 0 0 2px #6366f1,
    0 8px 30px rgba(0, 0, 0, 0.25);
}

.et {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px;
  color: #f1f3ff;
  height: 100%;
  box-sizing: border-box;
}

.stage-card:hover .et {
  background: rgba(255, 255, 255, 0.04);
}

.et-head {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.et-position {
  font-size: 12px;
  font-weight: 600;
}

.et-name {
  margin: 0;
  font-weight: 700;
  font-size: 15px;
  line-height: 20px;
}

.et-link {
  color: #f1f3ff;
  text-decoration: none;
}

.et-link:hover {
  color: #ffffff;
}

/* The stage link covers the whole card (canvas `a.et`); its name stays the stage name. */
.et-link::after {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: 14px;
}

.et-link:focus-visible {
  outline: none;
}

.et-link:focus-visible::after {
  outline: 2px solid #c4b5fd;
  outline-offset: -2px;
}

.et-agent {
  font-size: 12px;
}

.et-state {
  align-self: flex-start;
}

.et-info {
  font-size: 12px;
  line-height: 16px;
  margin-top: auto;
  padding-top: 8px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.et-info p {
  margin: 0;
}

.et-info .mono {
  font-size: 12px;
}

/* Links inside the card sit above the card-wide stage link. */
.et-inner {
  position: relative;
  z-index: 1;
}

.stage-waves {
  list-style: none;
  margin: 0;
  padding: 0;
}

.activities {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
  gap: 10px;
}

.activity {
  padding: 14px 16px;
  display: flex;
  gap: 12px;
  align-items: center;
  border-style: dashed;
}

.activity-text {
  flex: 1;
  display: flex;
  flex-direction: column;
}

.activity-link {
  color: #f1f3ff;
  text-decoration: none;
}

.activity-link:hover {
  color: #ffffff;
  text-decoration: underline;
}

.activity-detail {
  font-size: 13px;
}

.workflow-row {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  align-items: flex-start;
}

.waves-slot {
  flex: 3 1 520px;
  min-width: 0;
}

.workflow-side {
  flex: 2 1 300px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.side-card {
  padding: 18px 22px;
}

.kv-value {
  font-weight: 600;
  text-align: right;
}

.card-link {
  display: inline-flex;
  min-height: 44px;
  align-items: center;
  font-weight: 500;
  text-decoration: none;
}
</style>
