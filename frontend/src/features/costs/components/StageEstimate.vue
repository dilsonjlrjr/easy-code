<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import type { Money, StageId } from '@/shared/api/contract'
import { isApiError } from '@/shared/api/problemDetails'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { costsApi, type Estimate } from '../api/costsApi'
import { estimateExceedsBalance } from '../composables/costView'

/**
 * US-40 (ESP-07 3, ESP-08 3, ESP-06 3.6): the cost estimate of a stage with the basis of the
 * calculation (input size, price table version, historical average); "no history" when the stage
 * never ran (CA-2); estimated × performed side by side after a concluded wave (CA-3).
 */
const props = withDefaults(
  defineProps<{
    projectId: string
    stage: StageId
    /** Budget balance; an estimate above it pauses the project instead of starting the wave. */
    balance?: Money | null
    /** Re-reads when the page reloads after a live event. */
    revision?: number
    headingLevel?: 2 | 3 | 4
  }>(),
  { balance: undefined, revision: 0, headingLevel: 3 },
)

const format = useFormat()
const estimate = ref<Estimate>()
const loader = useLoader(async () => {
  estimate.value = await costsApi.getStageEstimate(props.projectId, props.stage)
})
/** A stage without an estimate (the orchestrator's Release) answers 404: the block stays out. */
const absent = computed(() => isApiError(loader.error.value) && loader.error.value.status === 404)
const exceeds = computed(() => estimateExceedsBalance(estimate.value, props.balance))
const headingTag = computed(() => `h${String(props.headingLevel)}`)
const headingId = useId()

watch(
  () => [props.projectId, props.stage, props.revision],
  () => void loader.run(),
  { immediate: true },
)
</script>

<template>
  <!-- Look: canvas primitives (block title and label/value grid as in "Novo projeto", `.tbl`, `.note`). -->
  <section
    v-if="!absent"
    class="estimate"
    data-testid="stage-estimate"
    :aria-labelledby="headingId"
  >
    <component :is="headingTag" :id="headingId" class="estimate-title">
      {{ $t('estimate.title') }}
    </component>
    <LoadingState v-if="loader.loading.value && !estimate" type="paragraph" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else-if="estimate">
      <p class="estimate-value">
        <strong>{{ $t('estimate.estimated', { value: format.money(estimate.estimated) }) }}</strong>
        <span v-if="estimate.no_history" class="chip mute" data-testid="estimate-no-history">
          {{ $t('estimate.noHistory') }}
        </span>
      </p>
      <p class="t2 estimate-formula">
        {{ estimate.no_history ? $t('estimate.formulaNoHistory') : $t('estimate.formula') }}
      </p>
      <dl class="estimate-basis" data-testid="estimate-basis">
        <dt class="t2">{{ $t('estimate.inputSize') }}</dt>
        <dd>
          {{ $t('estimate.tokens', { n: format.integer(estimate.basis.input_size_tokens) }) }}
        </dd>
        <dt class="t2">{{ $t('estimate.priceTable') }}</dt>
        <dd>
          {{ $t('estimate.priceTableVersion', { version: estimate.basis.price_table_version }) }}
        </dd>
        <template v-if="estimate.basis.input_price_per_million">
          <dt class="t2">{{ $t('estimate.inputPrice') }}</dt>
          <dd>
            {{
              $t('estimate.perMillion', {
                price: format.money(estimate.basis.input_price_per_million),
              })
            }}
          </dd>
        </template>
        <template v-if="!estimate.no_history && estimate.basis.historical_average">
          <dt class="t2">{{ $t('estimate.historicalAverage') }}</dt>
          <dd>
            {{ format.money(estimate.basis.historical_average) }}
            <template v-if="estimate.basis.considered_waves !== undefined">
              ·
              {{ $t('estimate.consideredWaves', { n: estimate.basis.considered_waves }) }}
            </template>
          </dd>
        </template>
      </dl>
      <!-- CA-3: estimated × performed side by side once a wave of the stage concluded. -->
      <table
        v-if="estimate.performed"
        class="tbl estimate-comparison"
        data-testid="estimate-performed"
      >
        <caption class="sr-only">
          {{
            $t('estimate.comparisonCaption')
          }}
        </caption>
        <thead>
          <tr>
            <th scope="col">{{ $t('estimate.estimatedColumn') }}</th>
            <th scope="col">{{ $t('estimate.performedColumn') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>{{ format.money(estimate.estimated) }}</td>
            <td>{{ format.money(estimate.performed) }}</td>
          </tr>
        </tbody>
      </table>
      <p v-if="exceeds" class="note note-warn" data-testid="estimate-exceeds-balance">
        {{
          $t('estimate.exceedsBalance', {
            estimate: format.money(estimate.estimated),
            balance: format.money(balance),
          })
        }}
      </p>
    </template>
  </section>
</template>

<style scoped>
.estimate {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.estimate-title {
  margin: 0;
  font-size: 16px;
  line-height: 24px;
  font-weight: 700;
}

.estimate-value {
  margin: 0;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.estimate-formula {
  margin: 0;
  font-size: 13px;
}

.estimate-basis {
  margin: 0;
  display: grid;
  grid-template-columns: minmax(160px, 220px) 1fr;
  gap: 10px 16px;
  font-size: 13px;
}

.estimate-basis dd {
  margin: 0;
}

.estimate-comparison {
  width: auto;
  font-size: 13px;
}

.estimate-comparison th,
.estimate-comparison td {
  padding-right: 24px;
}

.note {
  margin: 0;
}

.note-warn {
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.1);
}
</style>
