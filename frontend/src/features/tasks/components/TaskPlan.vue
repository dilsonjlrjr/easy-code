<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useLoader } from '@/shared/composables/useLoader'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { artifactsApi, type ArtifactsApi } from '@/features/artifacts/api/artifactsApi'
import type { WaveSummary } from '@/features/projects/api/projectsApi'
import {
  defaultTask,
  hasDeviation,
  inPlanOrder,
  taskFromArtifact,
  type TaskView,
} from '../composables/taskPlan'

/**
 * The task plan (T) of the project. In the Architecture stage (ESP-08 "Tarefas", US-18 CA-1) it is
 * the plan with what each T covers, follows and depends on. In the Implementation stage (ESP-14,
 * US-19) it adds the situation, the waves of the selected T (ER-14-18, CA-14-24), its commits, tests
 * and deviations, and the link to the build and test evidence of its wave (PT-48).
 */
const props = withDefaults(
  defineProps<{
    projectId: string
    waves: WaveSummary[]
    showExecution?: boolean
    revision?: number
    api?: Pick<ArtifactsApi, 'listArtifacts' | 'getArtifact'>
  }>(),
  { showExecution: false, revision: 0, api: () => artifactsApi },
)

const i18n = useI18n()
const { t } = i18n
const tasks = ref<TaskView[]>([])
const selectedId = ref<string>()

const loader = useLoader(async () => {
  const summaries = await props.api.listArtifacts(props.projectId, { type: 'T' })
  // The situation and the developer record are in the T header (no field in ArtifactSummary).
  const artifacts = await Promise.all(
    summaries
      .filter((summary) => summary.type === 'T')
      .map((summary) => props.api.getArtifact(props.projectId, summary.id)),
  )
  tasks.value = inPlanOrder(artifacts.map(taskFromArtifact))
  if (!tasks.value.some((task) => task.id === selectedId.value)) {
    selectedId.value = defaultTask(tasks.value)?.id
  }
})

watch(
  () => [props.projectId, props.revision],
  () => void loader.run(),
  { immediate: true },
)

const selected = computed(() => tasks.value.find((task) => task.id === selectedId.value))
const selectedWaves = computed(() =>
  selected.value ? props.waves.filter((wave) => wave.task === selected.value?.id) : [],
)
/** The evidence of a task is the one of its latest wave (one wave per task, retries included). */
const evidenceWave = computed(() => selectedWaves.value[selectedWaves.value.length - 1])

function situationText(task: TaskView): string {
  return t(`tasks.situation.${task.situation ?? 'unknown'}`)
}

function sizeText(task: TaskView): string {
  if (!task.size) return ''
  const key = `tasks.size.${task.size}`
  return t('tasks.sizeLabel', { size: i18n.te(key) ? t(key) : task.size })
}

function natureText(task: TaskView): string {
  if (!task.nature) return ''
  const key = `tasks.nature.${task.nature}`
  return i18n.te(key) ? t(key) : task.nature
}

function artifactLink(artifactId: string) {
  return { name: ROUTE_NAMES.artifact, params: { projectId: props.projectId, artifactId } }
}
</script>

<template>
  <section aria-labelledby="tasks-heading" class="mb-6" data-testid="task-plan">
    <h3 id="tasks-heading" class="text-subtitle-1 font-weight-medium mb-2">
      {{ $t('tasks.title') }}
    </h3>
    <LoadingState v-if="loader.loading.value && tasks.length === 0" />
    <ProblemAlert
      v-else-if="loader.error.value && tasks.length === 0"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <p v-else-if="tasks.length === 0" class="text-body-2">{{ $t('tasks.none') }}</p>
    <template v-else>
      <ol class="task-list mb-4">
        <li v-for="task in tasks" :key="task.id">
          <button
            type="button"
            class="task-button text-body-2"
            :aria-current="task.id === selectedId ? 'true' : undefined"
            :data-testid="`task-${task.id}`"
            @click="selectedId = task.id"
          >
            <span class="monospace">{{ task.id }}</span> · {{ task.title }}
            <template v-if="showExecution"> · {{ situationText(task) }}</template>
            <template v-else-if="task.size"> · {{ sizeText(task) }}</template>
          </button>
        </li>
      </ol>

      <article
        v-if="selected"
        aria-labelledby="task-detail-heading"
        class="task-detail"
        data-testid="task-detail"
      >
        <h4 id="task-detail-heading" class="text-subtitle-2 font-weight-medium mb-1">
          {{ $t('tasks.selected', { task: selected.id }) }} ·
          <router-link :to="artifactLink(selected.id)">{{ selected.title }}</router-link>
        </h4>
        <p class="text-body-2 mb-2">
          {{ situationText(selected) }}
          <template v-if="selected.nature"> · {{ natureText(selected) }}</template>
          <template v-if="selected.size"> · {{ sizeText(selected) }}</template>
          · {{ $t(`states.artifact.${selected.state}`) }}
        </p>
        <p v-if="selected.justification" class="text-body-2 mb-2">
          {{ $t('tasks.justification', { text: selected.justification }) }}
        </p>
        <dl class="summary-list">
          <dt>{{ $t('tasks.covers') }}</dt>
          <dd>
            <template v-if="selected.covers.length">
              <span v-for="(item, index) in selected.covers" :key="item.us">
                <template v-if="index > 0">, </template>
                <router-link :to="artifactLink(item.us)" class="monospace">{{
                  item.us
                }}</router-link>
                <template v-if="item.criteria.length"> ({{ item.criteria.join(', ') }})</template>
              </span>
            </template>
            <template v-else>{{ $t('tasks.nothing') }}</template>
          </dd>
          <dt>{{ $t('tasks.follows') }}</dt>
          <dd>
            <template v-if="selected.follows.length">
              <span v-for="(adr, index) in selected.follows" :key="adr">
                <template v-if="index > 0">, </template>
                <router-link :to="artifactLink(adr)" class="monospace">{{ adr }}</router-link>
              </span>
            </template>
            <template v-else>{{ $t('tasks.nothing') }}</template>
          </dd>
          <dt>{{ $t('tasks.dependsOn') }}</dt>
          <dd>
            <template v-if="selected.dependsOn.length">
              <span class="monospace">{{ selected.dependsOn.join(', ') }}</span>
            </template>
            <template v-else>{{ $t('tasks.nothing') }}</template>
          </dd>
          <template v-if="showExecution">
            <dt>{{ $t('tasks.fileScope') }}</dt>
            <dd class="monospace">{{ selected.fileScope.join(', ') }}</dd>
            <dt>{{ $t('tasks.commits') }}</dt>
            <dd data-testid="task-commits">
              <span v-if="selected.devRecord?.commits.length" class="monospace">{{
                selected.devRecord.commits.map((sha) => sha.slice(0, 12)).join(', ')
              }}</span>
              <template v-else>{{ $t('tasks.notRecorded') }}</template>
            </dd>
            <dt>{{ $t('tasks.testsCreated') }}</dt>
            <dd>
              <span v-if="selected.devRecord?.tests.length" class="monospace">{{
                selected.devRecord.tests.join(', ')
              }}</span>
              <template v-else>{{ $t('tasks.notRecorded') }}</template>
            </dd>
            <dt>{{ $t('tasks.deviations') }}</dt>
            <dd
              :class="{ 'font-weight-bold': hasDeviation(selected) }"
              data-testid="task-deviations"
            >
              {{
                selected.devRecord?.deviations && hasDeviation(selected)
                  ? selected.devRecord.deviations
                  : $t('tasks.noDeviation')
              }}
            </dd>
          </template>
        </dl>

        <template v-if="showExecution">
          <h5 class="text-subtitle-2 font-weight-medium mt-3 mb-1">{{ $t('tasks.waves') }}</h5>
          <p v-if="selectedWaves.length === 0" class="text-body-2">{{ $t('tasks.noWaves') }}</p>
          <ul v-else class="plain-list" data-testid="task-waves">
            <li v-for="wave in selectedWaves" :key="wave.id" class="text-body-2 mb-1">
              <router-link
                :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: wave.id } }"
                class="monospace"
              >
                {{ wave.id }}
              </router-link>
              · {{ $t(`waves.purpose.${wave.purpose}`) }} · {{ $t(`states.wave.${wave.state}`) }}
            </li>
          </ul>
          <router-link
            v-if="evidenceWave"
            :to="{
              name: ROUTE_NAMES.wave,
              params: { projectId, waveId: evidenceWave.id },
              hash: '#evidence',
            }"
            class="d-inline-block mt-2 text-body-2"
            data-testid="task-evidence-link"
          >
            {{ $t('evidence.openOf', { wave: evidenceWave.id }) }}
          </router-link>
        </template>
      </article>
    </template>
  </section>
</template>

<style scoped>
.task-list {
  list-style: none;
  padding: 0;
}

.task-button {
  display: block;
  width: 100%;
  min-height: 2rem;
  text-align: start;
  padding: 0.25rem 0.5rem;
  border-left: 3px solid transparent;
}

.task-button[aria-current='true'] {
  border-left-color: rgb(var(--v-theme-primary));
  font-weight: 600;
}

.task-button:focus-visible {
  outline: 2px solid rgb(var(--v-theme-primary));
}

.task-detail {
  border: 1px solid rgb(var(--v-theme-on-surface), 0.2);
  border-radius: 4px;
  padding: 0.75rem;
}
</style>
