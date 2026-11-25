<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { artifactsApi, type Artifact } from '@/features/artifacts/api/artifactsApi'
import { gatesApi, type CheckResult, type GateRound } from '@/features/gates/api/gatesApi'
import { useLoader } from '@/shared/composables/useLoader'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import type { StageViewProps } from '../stageViews'
import { opinionChecks, opinionViews, type OpinionStatus } from './opinions'

/**
 * ESP-13 "Pareceres" (canvas EtapaValidacao): three fixed cards, architect (lead), DBA and designer, with
 * the opinion, the reservations and the wave; without an opinion, the wave says "running" or "waiting
 * inputs". A rejected opinion says its effect (ER-13-5, ER-13-5a). With a round of gate.validation, the
 * checks about the opinions show their result (ER-13-4). Reservations are agent text (RNF-05).
 */
const props = defineProps<StageViewProps>()
const { t } = useI18n()

const validation = ref<Artifact>()
const round = ref<GateRound>()

const validationSummary = computed(() =>
  props.stage.artifacts.find((item) => item.type === 'VALIDACAO'),
)

const loader = useLoader(async () => {
  const gate = props.stage.gate
  const number = props.stage.current_round
  const [artifact, current] = await Promise.all([
    validationSummary.value
      ? artifactsApi.getArtifact(props.projectId, validationSummary.value.id)
      : undefined,
    gate && number ? gatesApi.getRound(props.projectId, gate, number) : undefined,
  ])
  validation.value = artifact
  round.value = current
})

watch(
  () => [
    props.projectId,
    props.revision,
    validationSummary.value?.version,
    props.stage.current_round,
  ],
  () => void loader.run(),
  { immediate: true },
)

const opinions = computed(() => opinionViews(validation.value?.header, props.stage.waves))
const checks = computed(() => opinionChecks(round.value?.checks))

const STATUS_TONES: Record<OpinionStatus, string> = {
  approved: 'ok',
  rejected: 'crit',
  running: 'info',
  waiting: 'mute',
}

function checkStatus(check: CheckResult): string {
  if (check.status === 'failed')
    return t(
      check.failure_severity === 'alert'
        ? 'gates.checkStatus.alert'
        : 'gates.checkStatus.failedBlocks',
    )
  return t(`gates.checkStatus.${check.status}`)
}
</script>

<template>
  <section
    class="opinions"
    aria-labelledby="validation-opinions-heading"
    data-testid="validation-opinions"
  >
    <div class="op-head">
      <h3 id="validation-opinions-heading">{{ $t('stageValidation.opinions') }}</h3>
      <p class="t2 op-intro">{{ $t('stageValidation.intro') }}</p>
    </div>

    <LoadingState v-if="loader.loading.value && !loader.loaded.value" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run()"
    />
    <template v-else>
      <ul class="op-grid">
        <li
          v-for="item in opinions"
          :key="item.participant"
          class="card op-card"
          :data-testid="`opinion-${item.participant}`"
        >
          <strong>{{ $t(`stageValidation.participants.${item.participant}`) }}</strong>
          <span class="chip op-chip" :class="STATUS_TONES[item.status]">
            {{ $t(`stageValidation.status.${item.status}`) }}
          </span>
          <span class="t2 op-text">{{ $t(`stageValidation.roles.${item.participant}`) }}</span>
          <div v-if="item.reservations.length" class="op-reservations">
            <span class="t2 op-text">{{ $t('stageValidation.reservations') }}</span>
            <ul class="op-list">
              <li v-for="(reservation, index) in item.reservations" :key="index" class="op-text">
                {{ reservation }}
              </li>
            </ul>
          </div>
          <p
            v-if="item.effect"
            class="note op-effect"
            :class="item.effect === 'forces_returned' ? 'note-crit' : 'note-warn'"
            :data-testid="`opinion-effect-${item.participant}`"
          >
            {{ $t(`stageValidation.effects.${item.effect}`) }}
          </p>
          <router-link
            v-if="item.wave"
            :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: item.wave } }"
            class="op-wave"
          >
            {{ $t('stageValidation.wave', { wave: item.wave }) }}
          </router-link>
        </li>
      </ul>

      <div
        v-if="round && checks.length"
        class="card op-checks"
        data-testid="validation-opinion-checks"
      >
        <h4 class="lb op-checks-title">
          {{ $t('stageValidation.checksTitle', { n: round.round }) }}
        </h4>
        <ul class="op-list">
          <li v-for="check in checks" :key="check.check" class="op-text">
            {{ $t('stageValidation.checkLine', { name: check.name, status: checkStatus(check) }) }}
            <ul v-if="check.evidence?.length" class="op-list">
              <li v-for="(evidence, index) in check.evidence" :key="index" class="t2 op-evidence">
                {{ evidence.message }}
              </li>
            </ul>
          </li>
        </ul>
      </div>
    </template>
  </section>
</template>

<style scoped>
.opinions {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
}

.op-head h3 {
  margin: 0;
  font-size: 18px;
  line-height: 24px;
  font-weight: 700;
}

.op-intro {
  margin: 4px 0 0;
  font-size: 13px;
}

.op-grid {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 14px;
}

.op-card {
  padding: 18px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}

.op-chip {
  align-self: flex-start;
}

.op-text {
  font-size: 13px;
  overflow-wrap: anywhere;
}

.op-reservations {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.op-list {
  margin: 0;
  padding-left: 18px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.op-effect {
  margin: 0;
  font-size: 13px;
}

.op-wave {
  align-self: flex-start;
  display: inline-flex;
  align-items: center;
  min-height: 44px;
  font-size: 13px;
}

.op-checks {
  padding: 14px 18px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.op-checks-title {
  margin: 0;
}

.op-evidence {
  font-size: 12px;
}
</style>
