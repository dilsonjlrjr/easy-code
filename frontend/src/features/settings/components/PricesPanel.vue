<script setup lang="ts">
import { computed, nextTick, onMounted, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import type { Model, ModelPrice, PricesPerMillion } from '../api/providersApi'
import { modelKey, useProvidersStore } from '../stores/providersStore'
import PriceDialog from './PriceDialog.vue'

const { t } = useI18n()
const format = useFormat()
const store = useProvidersStore()
const { message, announce } = useAnnouncer()
const selectedKey = ref<string>()
const dialogOpen = ref(false)
const correcting = ref<ModelPrice>()
const fieldId = useId()

const PRICE_TYPES: Array<keyof PricesPerMillion> = ['input', 'output', 'read_cache', 'write_cache']
const FALLBACK_CURRENCY = 'BRL'

const modelsLoader = useLoader(async () => {
  await Promise.all([store.loadModels(), store.loadProviders()])
})
const selectedModel = computed(() =>
  store.models.find((model) => modelKey(model.provider, model.model) === selectedKey.value),
)
const pricesLoader = useLoader(async () => {
  const model = selectedModel.value
  if (model) await store.loadPrices(model.provider, model.model)
})
const periods = computed(() => (selectedKey.value ? (store.prices[selectedKey.value] ?? []) : []))
const modelOptions = computed(() =>
  store.models.map((model) => ({
    title: `${model.display_name} (${model.model})`,
    value: modelKey(model.provider, model.model),
  })),
)
/** New periods default to the currency already used by the table (ER-23-7: organization currency). */
const defaultCurrency = computed(
  () =>
    periods.value[0]?.currency ??
    store.models.find((model) => model.current_price)?.current_price?.currency ??
    FALLBACK_CURRENCY,
)

onMounted(async () => {
  await modelsLoader.run()
  selectedKey.value ??= modelOptions.value[0]?.value
})

watch(selectedKey, () => {
  void pricesLoader.run()
})

/** PC-28: the history of one model opens below the matrix, with the focus on its heading. */
async function showPeriods(model: Model): Promise<void> {
  selectedKey.value = modelKey(model.provider, model.model)
  await nextTick()
  document.getElementById(`${fieldId}-periods`)?.focus()
}

function openNewPeriod(): void {
  correcting.value = undefined
  dialogOpen.value = true
}

function openCorrection(period: ModelPrice): void {
  correcting.value = period
  dialogOpen.value = true
}

function onSaved(): void {
  announce(t('settings.prices.savedAnnouncement'))
}
</script>

<template>
  <!-- Layout: canvas "Configurações" (Configuracoes.dc.html), tab "Tabela de preços". -->
  <section class="card panel" aria-labelledby="prices-heading">
    <h2 id="prices-heading">{{ $t('settings.prices.title') }}</h2>
    <p class="t2 panel-help">{{ $t('settings.prices.costRule') }}</p>
    <div role="status" class="sr-only">{{ message }}</div>

    <LoadingState v-if="modelsLoader.loading.value" type="table-row@3" />
    <ProblemAlert
      v-else-if="modelsLoader.error.value"
      :error="modelsLoader.error.value"
      retry
      @retry="modelsLoader.run"
    />
    <p v-else-if="store.models.length === 0" class="empty">
      {{ $t('settings.prices.noModels') }}
    </p>
    <template v-else>
      <!-- ESP-23 3 / US-37 (PC-28): model × current price, each model with its own period. -->
      <h3 class="block-title">{{ $t('settings.prices.currentTitle') }}</h3>
      <div class="table-scroll">
        <table class="tbl" data-testid="current-prices">
          <caption class="sr-only">
            {{
              $t('settings.prices.currentCaption')
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('settings.models.model') }}</th>
              <th scope="col">{{ $t('settings.prices.effectiveSince') }}</th>
              <th v-for="priceType in PRICE_TYPES" :key="priceType" scope="col">
                {{
                  $t('settings.prices.perMillionColumn', {
                    type: $t(`settings.prices.types.${priceType}`),
                  })
                }}
              </th>
              <th scope="col">
                <span class="sr-only">{{ $t('common.actions') }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="model in store.models"
              :key="modelKey(model.provider, model.model)"
              :data-testid="`current-price-${model.model}`"
            >
              <th scope="row" class="model-cell">
                {{ model.display_name }}
                <span class="t2 mono model-id">{{ model.model }}</span>
              </th>
              <template v-if="model.current_price">
                <td>{{ format.dateTime(model.current_price.effective_since) }}</td>
                <td v-for="priceType in PRICE_TYPES" :key="priceType">
                  {{
                    format.amount(
                      model.current_price.per_million_tokens[priceType],
                      model.current_price.currency,
                    )
                  }}
                </td>
              </template>
              <td v-else :colspan="PRICE_TYPES.length + 1">
                <span class="chip warn">{{ $t('settings.prices.noCurrentPrice') }}</span>
              </td>
              <td class="row-actions">
                <button
                  type="button"
                  class="btn"
                  :aria-label="
                    $t('settings.prices.showPeriodsNamed', { model: model.display_name })
                  "
                  @click="showPeriods(model)"
                >
                  {{ $t('settings.prices.showPeriods') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <h3 :id="`${fieldId}-periods`" class="block-title" tabindex="-1">
        {{ $t('settings.prices.periodsTitle', { model: selectedModel?.display_name ?? '' }) }}
      </h3>
      <div class="toolbar">
        <div class="model-field">
          <label :for="`${fieldId}-model`" class="control">
            <span class="field-label">{{ $t('settings.models.model') }}</span>
            <select :id="`${fieldId}-model`" v-model="selectedKey" class="fld">
              <option v-for="option in modelOptions" :key="option.value" :value="option.value">
                {{ option.title }}
              </option>
            </select>
          </label>
        </div>
        <button type="button" class="btn btn-p" :disabled="!selectedModel" @click="openNewPeriod">
          {{ $t('settings.prices.newPeriod') }}
        </button>
      </div>

      <LoadingState v-if="pricesLoader.loading.value" type="table-row@3" />
      <ProblemAlert
        v-else-if="pricesLoader.error.value"
        :error="pricesLoader.error.value"
        retry
        @retry="pricesLoader.run"
      />
      <p v-else-if="periods.length === 0" class="empty">
        <span class="chip warn">{{ $t('settings.prices.noCurrentPrice') }}</span>
      </p>
      <div v-else class="table-scroll">
        <table class="tbl" data-testid="price-periods">
          <caption class="sr-only">
            {{
              $t('settings.prices.tableCaption', { model: selectedModel?.display_name ?? '' })
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('settings.prices.effectiveSince') }}</th>
              <th scope="col">{{ $t('settings.prices.tableVersion') }}</th>
              <th v-for="priceType in PRICE_TYPES" :key="priceType" scope="col">
                {{ $t(`settings.prices.types.${priceType}`) }}
              </th>
              <th scope="col">{{ $t('settings.prices.source') }}</th>
              <th scope="col">{{ $t('settings.prices.usage') }}</th>
              <th scope="col">{{ $t('settings.prices.recorded') }}</th>
              <th scope="col">
                <span class="sr-only">{{ $t('common.actions') }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="period in periods"
              :key="period.id"
              :data-testid="`price-period-${period.id}`"
            >
              <td>{{ format.dateTime(period.effective_since) }}</td>
              <td class="mono">{{ period.table_version ?? period.id }}</td>
              <td v-for="priceType in PRICE_TYPES" :key="priceType">
                {{ format.amount(period.per_million_tokens[priceType], period.currency) }}
              </td>
              <td>{{ period.source }}</td>
              <td>
                <span class="chip" :class="period.used ? 'info' : 'mute'">
                  {{ period.used ? $t('settings.prices.used') : $t('settings.prices.unused') }}
                </span>
              </td>
              <td>
                {{ period.recorded_by?.name ?? '' }}
                <div class="t2">{{ format.dateTime(period.recorded_at) }}</div>
              </td>
              <td>
                <!-- ER-23-6: a used period is immutable; only unused ones can be corrected. -->
                <button
                  v-if="!period.used"
                  type="button"
                  class="btn"
                  @click="openCorrection(period)"
                >
                  {{ $t('settings.prices.correct') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <PriceDialog
      v-model="dialogOpen"
      :model="selectedModel"
      :correcting="correcting"
      :default-currency="defaultCurrency"
      @saved="onSaved"
    />
  </section>
</template>

<style scoped>
.panel {
  padding: 18px 22px;
}

.panel-help {
  margin: 4px 0 8px;
  font-size: 13px;
}

.empty {
  margin: 0;
}

.block-title {
  margin: 14px 0 6px;
  font-size: 14px;
  font-weight: 600;
}

.model-cell {
  text-align: left;
  font-weight: 600;
}

.model-id {
  display: block;
  font-size: 12px;
  font-weight: 400;
}

.row-actions {
  white-space: nowrap;
}

.toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 8px 16px;
  margin: 6px 0 14px;
}

.model-field {
  flex: 0 1 480px;
  min-width: 260px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.control {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.field-label {
  font-weight: 600;
}

.tbl {
  min-width: 620px;
}

.tbl th {
  padding: 8px 12px 8px 0;
}

.tbl td {
  padding: 12px 12px 12px 0;
  vertical-align: middle;
}
</style>
