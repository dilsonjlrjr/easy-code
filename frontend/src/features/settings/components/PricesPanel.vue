<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import type { ModelPrice, PricesPerMillion } from '../api/providersApi'
import { modelKey, useProvidersStore } from '../stores/providersStore'
import PriceDialog from './PriceDialog.vue'

const { t } = useI18n()
const format = useFormat()
const store = useProvidersStore()
const { message, announce } = useAnnouncer()
const selectedKey = ref<string>()
const dialogOpen = ref(false)
const correcting = ref<ModelPrice>()

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
  <section aria-labelledby="prices-heading">
    <h2 id="prices-heading" class="text-h6 mb-2">{{ $t('settings.prices.title') }}</h2>
    <p class="text-body-2 text-medium-emphasis mb-4">{{ $t('settings.prices.costRule') }}</p>
    <div role="status" class="sr-only">{{ message }}</div>

    <LoadingState v-if="modelsLoader.loading.value" type="table-row@3" />
    <ProblemAlert
      v-else-if="modelsLoader.error.value"
      :error="modelsLoader.error.value"
      retry
      @retry="modelsLoader.run"
    />
    <p v-else-if="store.models.length === 0" class="text-body-2">
      {{ $t('settings.prices.noModels') }}
    </p>
    <template v-else>
      <div class="d-flex flex-wrap align-center ga-4 mb-4">
        <v-select
          v-model="selectedKey"
          :items="modelOptions"
          :label="$t('settings.models.model')"
          hide-details
          class="model-select"
        />
        <v-btn color="primary" variant="tonal" :disabled="!selectedModel" @click="openNewPeriod">
          {{ $t('settings.prices.newPeriod') }}
        </v-btn>
      </div>

      <LoadingState v-if="pricesLoader.loading.value" type="table-row@3" />
      <ProblemAlert
        v-else-if="pricesLoader.error.value"
        :error="pricesLoader.error.value"
        retry
        @retry="pricesLoader.run"
      />
      <p v-else-if="periods.length === 0" class="font-weight-medium">
        {{ $t('settings.prices.noCurrentPrice') }}
      </p>
      <div v-else class="table-scroll">
        <v-table density="comfortable">
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
              <td class="monospace">{{ period.table_version ?? period.id }}</td>
              <td v-for="priceType in PRICE_TYPES" :key="priceType">
                {{ format.amount(period.per_million_tokens[priceType], period.currency) }}
              </td>
              <td>{{ period.source }}</td>
              <td>{{ period.used ? $t('settings.prices.used') : $t('settings.prices.unused') }}</td>
              <td>
                {{ period.recorded_by?.name ?? '' }}
                <div class="text-body-2">{{ format.dateTime(period.recorded_at) }}</div>
              </td>
              <td>
                <!-- ER-23-6: a used period is immutable; only unused ones can be corrected. -->
                <v-btn
                  v-if="!period.used"
                  variant="text"
                  size="small"
                  @click="openCorrection(period)"
                >
                  {{ $t('settings.prices.correct') }}
                </v-btn>
              </td>
            </tr>
          </tbody>
        </v-table>
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
.model-select {
  min-width: 260px;
  max-width: 480px;
}
</style>
