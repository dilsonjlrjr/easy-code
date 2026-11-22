<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useFormat } from '@/shared/format/useFormat'
import StateLabel from '@/shared/ui/StateLabel.vue'
import type { StageSummary } from '../api/projectsApi'
import { useCurrentProjectStore } from '../stores/currentProjectStore'

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

function waitingDependencies(stage: StageSummary): string {
  return stage.depends_on.map((id) => t(`stages.${id}`)).join(', ')
}
</script>

<template>
  <section aria-labelledby="pipeline-heading">
    <h2 id="pipeline-heading" class="text-h6 mb-2">{{ $t('projects.workflow.title') }}</h2>
    <p v-if="store.project?.summary" class="text-body-2 mb-4">
      {{
        $t('projects.workflow.progress', {
          approved: store.project.summary.approved_stages,
          total: store.project.summary.stages_total || stages.length,
        })
      }}
    </p>
    <!-- ESP-06 section 8: the pipeline is an ordered list; each item says position, name, agent, state. -->
    <ol class="pipeline">
      <li v-for="(stage, index) in stages" :key="stage.id" :data-testid="`stage-${stage.id}`">
        <v-card variant="outlined" class="pa-4 h-100">
          <p class="text-body-2 text-medium-emphasis">
            {{ $t('projects.workflow.position', { n: index + 1 }) }}
          </p>
          <h3 class="text-subtitle-1 font-weight-medium">{{ stageName(stage) }}</h3>
          <p class="text-body-2 mb-2">{{ $t(`agents.${stage.agent}`) }}</p>
          <StateLabel machine="stage" :state="stageState(stage)" />
          <p
            v-if="stage.state === 'blocked' && stage.depends_on.length > 0"
            class="text-body-2 mt-2"
          >
            {{ $t('projects.workflow.waitingFor', { stages: waitingDependencies(stage) }) }}
          </p>
          <ul v-if="wavesOf(stage).length > 0" class="stage-waves mt-2">
            <li v-for="wave in wavesOf(stage)" :key="wave.id" class="text-body-2">
              <router-link
                :to="{
                  name: ROUTE_NAMES.wave,
                  params: { projectId: store.project?.id ?? '', waveId: wave.id },
                }"
                class="monospace"
              >
                {{ wave.id }}
              </router-link>
              · {{ $t(`states.wave.${wave.state}`) }}
            </li>
          </ul>
          <p v-if="stage.cost" class="text-body-2 mt-2">
            {{ $t('projects.workflow.cost', { cost: format.money(stage.cost) }) }}
          </p>
        </v-card>
      </li>
    </ol>

    <template v-if="activities.length > 0">
      <h2 class="text-h6 mt-6 mb-2">{{ $t('projects.workflow.activities') }}</h2>
      <ul class="pipeline">
        <li v-for="activity in activities" :key="activity.id">
          <v-card variant="outlined" class="pa-4">
            <h3 class="text-subtitle-1 font-weight-medium">{{ stageName(activity) }}</h3>
            <StateLabel machine="stage" :state="stageState(activity)" />
          </v-card>
        </li>
      </ul>
    </template>
  </section>
</template>

<style scoped>
.pipeline {
  display: grid;
  gap: 1rem;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  list-style: none;
  padding: 0;
}

.stage-waves {
  list-style: none;
  padding: 0;
}
</style>
