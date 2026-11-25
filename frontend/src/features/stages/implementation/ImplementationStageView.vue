<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useLoader } from '@/shared/composables/useLoader'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import type { ChipTone } from '@/shared/ui/stateFamilies'
import { wavesApi, type SandboxExecution } from '@/features/waves/api/wavesApi'
import type { WaveSummary } from '@/features/projects/api/projectsApi'
import type { StageViewProps } from '../stageViews'
import {
  checkSituations,
  latestDeveloperWave,
  reworkItems,
  type CheckSituation,
  type ReworkItem,
} from './implementationView'
import { reworkApi } from './reworkApi'

/**
 * ESP-14 (canvas EtapaImplementacao): the build, tests and lint situation of the newest Developer
 * wave (US-41, the "build verde · N testes passando · lint limpo" chips) with the link to its
 * evidence, and the rework list of the internal cycle (US-32): each finding sent to the
 * Implementation with its emitter, severity, status, cycle "N of 3" and the rework and revalidation
 * waves. The task plan and the repository stay where StageDetail renders them.
 */
const props = defineProps<StageViewProps>()
const { t } = useI18n()

const wave = computed(() => latestDeveloperWave(props.stage.waves))
const executions = ref<SandboxExecution[]>([])
const items = ref<ReworkItem[]>([])

const checksLoader = useLoader(async () => {
  const current = wave.value
  executions.value = current
    ? await wavesApi.listSandboxExecutions(props.projectId, current.id)
    : []
})

const reworkLoader = useLoader(async () => {
  const [findings, rework, revalidation] = await Promise.all([
    reworkApi.findings(props.projectId),
    reworkApi.waves(props.projectId, 'rework'),
    reworkApi.waves(props.projectId, 'revalidation'),
  ])
  items.value = reworkItems(findings, [...rework, ...revalidation])
})

watch(
  () => [props.projectId, props.revision, wave.value?.id],
  () => {
    void checksLoader.run()
    void reworkLoader.run()
  },
  { immediate: true },
)

const situations = computed(() => checkSituations(executions.value))

function situationText(situation: CheckSituation): string {
  const category = t(`evidence.categories.${situation.category}`)
  const execution = situation.execution
  const code = execution?.exit_code ?? t('evidence.noExitCode')
  switch (situation.outcome) {
    case 'not_run':
      return t('stageImplementation.checks.notRun', { category })
    case 'limit':
      return t('stageImplementation.checks.limit', {
        category,
        reason: t(`evidence.endReasons.${execution?.end_reason ?? 'time_limit'}`),
      })
    case 'aborted':
      return t('stageImplementation.checks.aborted', { category })
    case 'executor_error':
      return t('stageImplementation.checks.executorError', { category })
    case 'failed':
      if (situation.category === 'tests' && execution?.tests) {
        return t('stageImplementation.checks.testsFailing', {
          failed: execution.tests.failed,
          total: execution.tests.total,
        })
      }
      return t('stageImplementation.checks.failed', { category, code })
    case 'succeeded':
      if (situation.category === 'tests' && execution?.tests) {
        return t('stageImplementation.checks.testsPassing', { n: execution.tests.passed })
      }
      if (situation.category === 'lint') return t('stageImplementation.checks.lintClean')
      return t('stageImplementation.checks.green', { category })
  }
  return category
}

function situationTone(situation: CheckSituation): ChipTone {
  if (situation.outcome === 'succeeded') return 'ok'
  if (situation.outcome === 'not_run' || situation.outcome === 'aborted') return 'mute'
  if (situation.outcome === 'limit') return 'warn'
  return 'crit'
}

function waveRoute(target: WaveSummary, hash?: string) {
  return {
    name: ROUTE_NAMES.wave,
    params: { projectId: props.projectId, waveId: target.id },
    ...(hash ? { hash } : {}),
  }
}

function agentName(agent: string): string {
  return t(`agents.${agent}`)
}
</script>

<template>
  <div class="impl-view">
    <!-- Look: canvas EtapaImplementacao, current task card (`.ck` chips of build, tests and lint). -->
    <section class="card impl-card" aria-labelledby="impl-checks-heading" data-testid="impl-checks">
      <div class="impl-head">
        <h3 id="impl-checks-heading">{{ $t('stageImplementation.checks.title') }}</h3>
        <span v-if="wave" class="t2 impl-sub">
          {{ $t('stageImplementation.checks.ofWave', { wave: wave.id }) }}
          <template v-if="wave.task"> · {{ $t('waves.task', { task: wave.task }) }}</template>
        </span>
      </div>
      <p v-if="!wave" class="t2 impl-text">{{ $t('stageImplementation.checks.noWave') }}</p>
      <LoadingState v-else-if="checksLoader.loading.value && !checksLoader.loaded.value" />
      <ProblemAlert
        v-else-if="checksLoader.error.value"
        :error="checksLoader.error.value"
        retry
        @retry="checksLoader.run()"
      />
      <template v-else>
        <p v-if="executions.length === 0" class="t2 impl-text" data-testid="impl-checks-empty">
          {{ $t('evidence.none') }}
        </p>
        <ul class="ck-list" :aria-label="$t('stageImplementation.checks.title')">
          <li
            v-for="situation in situations"
            :key="situation.category"
            class="chip"
            :class="situationTone(situation)"
            :data-testid="`impl-check-${situation.category}`"
          >
            {{ situationText(situation) }}
          </li>
        </ul>
        <div>
          <router-link :to="waveRoute(wave, '#evidence')" class="btn" data-testid="impl-evidence">
            {{ $t('evidence.openOf', { wave: wave.id }) }}
          </router-link>
        </div>
      </template>
    </section>

    <!-- Look: canvas EtapaImplementacao "Retrabalho por achados (ciclo interno)" (`.li` rows). -->
    <section class="card impl-card" aria-labelledby="impl-rework-heading" data-testid="impl-rework">
      <div class="impl-head">
        <h3 id="impl-rework-heading">
          {{ $t('stageImplementation.rework.title') }}
          <span class="chip mute">{{ items.length }}</span>
        </h3>
      </div>
      <p class="t2 impl-text">{{ $t('stageImplementation.rework.intro') }}</p>
      <LoadingState v-if="reworkLoader.loading.value && !reworkLoader.loaded.value" />
      <ProblemAlert
        v-else-if="reworkLoader.error.value"
        :error="reworkLoader.error.value"
        retry
        @retry="reworkLoader.run()"
      />
      <p v-else-if="items.length === 0" class="t2 impl-text" data-testid="impl-rework-empty">
        {{ $t('stageImplementation.rework.empty') }}
      </p>
      <ul v-else class="rework-list">
        <li
          v-for="item in items"
          :key="item.finding.id"
          class="li rework-item"
          :data-testid="`rework-${item.finding.id}`"
        >
          <div class="rework-main">
            <span>
              <router-link
                :to="{
                  name: ROUTE_NAMES.finding,
                  params: { projectId: props.projectId, findingId: item.finding.id },
                }"
                class="mono"
              >
                {{ item.finding.id }}
              </router-link>
              · {{ agentName(item.finding.author) }} ·
              {{ $t(`findings.severity.${item.finding.severity}`) }} ·
              {{ item.finding.title }}
            </span>
            <span class="t2 rework-meta">
              {{ $t('stageImplementation.rework.cycle', { n: item.finding.cycles ?? 0 }) }}
            </span>
            <ul v-if="item.waves.length" class="rework-waves">
              <li v-for="target in item.waves" :key="target.id" class="rework-wave">
                <router-link :to="waveRoute(target)" class="mono">{{ target.id }}</router-link>
                <span class="t2">· {{ $t(`waves.purpose.${target.purpose}`) }} ·</span>
                <StateLabel machine="wave" :state="target.state" />
              </li>
            </ul>
            <span v-else class="t2 rework-meta">{{
              $t('stageImplementation.rework.noWaves')
            }}</span>
          </div>
          <div class="rework-side">
            <StateLabel machine="finding" :state="item.finding.status" />
            <router-link
              v-if="item.finding.status === 'escalated'"
              :to="{ name: ROUTE_NAMES.pendingItems }"
              class="ib rework-pending"
            >
              {{ $t('stageImplementation.rework.pending') }}
            </router-link>
          </div>
        </li>
      </ul>
    </section>
  </div>
</template>

<style scoped>
.impl-view {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.impl-card {
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.impl-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.impl-head h3 {
  margin: 0;
  font-size: 18px;
  line-height: 24px;
  font-weight: 700;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.impl-sub,
.impl-text {
  margin: 0;
  font-size: 13px;
}

.ck-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.rework-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.rework-item {
  flex-wrap: wrap;
  align-items: flex-start;
}

.rework-main {
  flex: 1 1 320px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.rework-meta {
  font-size: 12px;
}

.rework-waves {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 13px;
}

.rework-wave {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.rework-side {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.rework-pending {
  display: inline-flex;
  align-items: center;
  min-height: 44px;
  text-decoration: none;
}
</style>
