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
  inIdOrder,
  taskFromArtifact,
  taskListItem,
  type TaskListItem,
  type TaskView,
} from '../composables/taskPlan'

/**
 * The task plan (T) of the project. In the Architecture stage (ESP-08 "Tarefas", US-18 CA-1) it is
 * the plan; the selected T shows what it covers, follows and depends on. In the Implementation stage (ESP-14,
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
const tasks = ref<TaskListItem[]>([])
const selectedId = ref<string>()
const detail = ref<TaskView>()

// The list comes from the summaries (situation included); only the selected T is read in full.
const loader = useLoader(async () => {
  const summaries = await props.api.listArtifacts(props.projectId, { type: 'T' })
  tasks.value = inIdOrder(summaries.filter((summary) => summary.type === 'T').map(taskListItem))
  if (!tasks.value.some((task) => task.id === selectedId.value)) {
    selectedId.value = defaultTask(tasks.value)?.id
  }
})

const detailLoader = useLoader(async () => {
  const id = selectedId.value
  if (!id) return
  const view = taskFromArtifact(await props.api.getArtifact(props.projectId, id))
  if (selectedId.value === id) detail.value = view
})

watch(
  () => [props.projectId, props.revision],
  () => void loader.run().then(() => detailLoader.run()),
  { immediate: true },
)
watch(selectedId, (id, previous) => {
  if (previous !== undefined && id !== previous) void detailLoader.run()
})

const selected = computed(() =>
  detail.value && detail.value.id === selectedId.value ? detail.value : undefined,
)
const selectedWaves = computed(() =>
  selected.value ? props.waves.filter((wave) => wave.task === selected.value?.id) : [],
)
/** The evidence of a task is the one of its latest wave (one wave per task, retries included). */
const evidenceWave = computed(() => selectedWaves.value[selectedWaves.value.length - 1])

function situationText(task: { situation: TaskView['situation'] }): string {
  return t(`tasks.situation.${task.situation ?? 'unknown'}`)
}

/** Canvas EtapaImplementacao: the chip tone of each situation (the text always says it). */
const SITUATION_TONES: Record<string, string> = {
  concluida: 'ok',
  em_andamento: 'info',
  justificada: 'vio',
}

function situationTone(task: { situation: TaskView['situation'] }): string {
  return SITUATION_TONES[task.situation ?? ''] ?? 'mute'
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
  <!-- Layout: canvas EtapaImplementacao, "Tarefas" card with the selected task in the side panel (.edp). -->
  <section aria-labelledby="tasks-heading" class="task-plan" data-testid="task-plan">
    <div class="card tp-list">
      <div class="tp-head">
        <h3 id="tasks-heading">{{ $t('tasks.title') }}</h3>
        <span v-if="tasks.length" class="chip mute">{{ tasks.length }}</span>
      </div>
      <div v-if="loader.loading.value && tasks.length === 0" class="tp-pad">
        <LoadingState />
      </div>
      <div v-else-if="loader.error.value && tasks.length === 0" class="tp-pad">
        <ProblemAlert :error="loader.error.value" retry @retry="loader.run" />
      </div>
      <p v-else-if="tasks.length === 0" class="t2 tp-pad tp-empty">{{ $t('tasks.none') }}</p>
      <ol v-else class="task-list">
        <li v-for="task in tasks" :key="task.id">
          <button
            type="button"
            class="tk"
            :class="{ on: task.id === selectedId }"
            :aria-current="task.id === selectedId ? 'true' : undefined"
            :data-testid="`task-${task.id}`"
            @click="selectedId = task.id"
          >
            <span class="mono t2 tk-id">{{ task.id }}</span>
            <span class="tk-main"><span class="sr-only"> · </span>{{ task.title }}</span>
            <span v-if="showExecution" class="chip" :class="situationTone(task)"
              ><span class="sr-only"> · </span>{{ situationText(task) }}</span
            >
          </button>
        </li>
      </ol>
    </div>

    <div v-if="tasks.length > 0" class="card edp tp-detail">
      <LoadingState v-if="detailLoader.loading.value && !selected" />
      <ProblemAlert
        v-else-if="detailLoader.error.value && !selected"
        :error="detailLoader.error.value"
        retry
        @retry="detailLoader.run"
      />
      <article
        v-else-if="selected"
        aria-labelledby="task-detail-heading"
        class="task-detail"
        data-testid="task-detail"
      >
        <h4 id="task-detail-heading">
          {{ $t('tasks.selected', { task: selected.id }) }} ·
          <router-link :to="artifactLink(selected.id)">{{ selected.title }}</router-link>
        </h4>
        <p class="t2 td-line">
          {{ situationText(selected) }}
          <template v-if="selected.nature"> · {{ natureText(selected) }}</template>
          <template v-if="selected.size"> · {{ sizeText(selected) }}</template>
          · {{ $t(`states.artifact.${selected.state}`) }}
        </p>
        <p v-if="selected.justification" class="td-line">
          {{ $t('tasks.justification', { text: selected.justification }) }}
        </p>
        <dl class="td-list">
          <div class="kv">
            <dt class="t2">{{ $t('tasks.covers') }}</dt>
            <dd>
              <template v-if="selected.covers.length">
                <span v-for="(item, index) in selected.covers" :key="item.us">
                  <template v-if="index > 0">, </template>
                  <router-link :to="artifactLink(item.us)" class="mono">{{ item.us }}</router-link>
                  <template v-if="item.criteria.length"> ({{ item.criteria.join(', ') }})</template>
                </span>
              </template>
              <template v-else>{{ $t('tasks.nothing') }}</template>
            </dd>
          </div>
          <div class="kv">
            <dt class="t2">{{ $t('tasks.follows') }}</dt>
            <dd>
              <template v-if="selected.follows.length">
                <span v-for="(adr, index) in selected.follows" :key="adr">
                  <template v-if="index > 0">, </template>
                  <router-link :to="artifactLink(adr)" class="mono">{{ adr }}</router-link>
                </span>
              </template>
              <template v-else>{{ $t('tasks.nothing') }}</template>
            </dd>
          </div>
          <div class="kv">
            <dt class="t2">{{ $t('tasks.dependsOn') }}</dt>
            <dd>
              <template v-if="selected.dependsOn.length">
                <span class="mono">{{ selected.dependsOn.join(', ') }}</span>
              </template>
              <template v-else>{{ $t('tasks.nothing') }}</template>
            </dd>
          </div>
          <template v-if="showExecution">
            <div class="kv">
              <dt class="t2">{{ $t('tasks.fileScope') }}</dt>
              <dd class="mono">{{ selected.fileScope.join(', ') }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('tasks.commits') }}</dt>
              <dd data-testid="task-commits">
                <span v-if="selected.devRecord?.commits.length" class="mono">{{
                  selected.devRecord.commits.map((sha) => sha.slice(0, 12)).join(', ')
                }}</span>
                <template v-else>{{ $t('tasks.notRecorded') }}</template>
              </dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('tasks.testsCreated') }}</dt>
              <dd>
                <span v-if="selected.devRecord?.tests.length" class="mono">{{
                  selected.devRecord.tests.join(', ')
                }}</span>
                <template v-else>{{ $t('tasks.notRecorded') }}</template>
              </dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('tasks.deviations') }}</dt>
              <dd :class="{ deviation: hasDeviation(selected) }" data-testid="task-deviations">
                {{
                  selected.devRecord?.deviations && hasDeviation(selected)
                    ? selected.devRecord.deviations
                    : $t('tasks.noDeviation')
                }}
              </dd>
            </div>
          </template>
        </dl>

        <template v-if="showExecution">
          <h5 class="lb td-sub">{{ $t('tasks.waves') }}</h5>
          <p v-if="selectedWaves.length === 0" class="t2 td-line">{{ $t('tasks.noWaves') }}</p>
          <ul v-else class="plain-list td-waves" data-testid="task-waves">
            <li v-for="wave in selectedWaves" :key="wave.id" class="td-wave">
              <router-link
                :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: wave.id } }"
                class="mono"
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
            class="btn td-evidence"
            data-testid="task-evidence-link"
          >
            {{ $t('evidence.openOf', { wave: evidenceWave.id }) }}
          </router-link>
        </template>
      </article>
    </div>
  </section>
</template>

<style scoped>
.task-plan {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  align-items: flex-start;
}

.tp-list {
  flex: 3 1 520px;
  min-width: 0;
  overflow: hidden;
}

.tp-head {
  padding: 16px 18px 10px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.tp-head h3 {
  margin: 0;
  font-size: 18px;
  line-height: 24px;
  font-weight: 700;
}

.tp-pad {
  padding: 0 18px 18px;
}

.tp-empty {
  margin: 0;
  font-size: 13px;
}

.task-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.tk {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  min-height: 44px;
  padding: 12px 14px;
  border: 0;
  border-top: 1px solid rgba(255, 255, 255, 0.07);
  background: transparent;
  color: #f1f3ff;
  font: inherit;
  text-align: left;
  cursor: pointer;
  box-sizing: border-box;
  flex-wrap: wrap;
}

.tk:hover {
  background: rgba(255, 255, 255, 0.03);
}

/* The selected task: canvas list selection (.it.on) plus a weight change, not color alone. */
.tk.on {
  background: rgba(99, 102, 241, 0.12);
  box-shadow: inset 3px 0 0 #7c3aed;
}

.tk.on .tk-main {
  font-weight: 700;
}

.tk-id {
  width: 44px;
  flex: none;
}

.tk-main {
  flex: 1 1 240px;
  min-width: 0;
  font-weight: 600;
}

.tp-detail {
  flex: 2 1 360px;
  min-width: 0;
  padding: 20px 22px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.task-detail {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.task-detail h4 {
  margin: 0;
  font-size: 18px;
  line-height: 24px;
  font-weight: 700;
}

.td-line {
  margin: 0;
  font-size: 13px;
}

.td-list {
  margin: 0;
}

.td-list dt {
  flex: none;
}

.td-list dd {
  margin: 0;
  text-align: right;
  overflow-wrap: anywhere;
}

.deviation {
  font-weight: 700;
}

.td-sub {
  margin: 6px 0 0;
}

.td-waves {
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 13px;
}

.td-evidence {
  align-self: flex-start;
}
</style>
