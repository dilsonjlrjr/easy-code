<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ProviderId } from '@/shared/api/contract'
import { useFormat } from '@/shared/format/useFormat'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import type { Model, PricesPerMillion } from '../api/providersApi'
import {
  PRICE_SOURCES,
  convertDecimal,
  fetchPriceOffers,
  type PriceOffer,
  type PriceSourceId,
} from '../priceSources'
import {
  modelKey,
  useProvidersStore,
  type PriceImportItem,
  type PriceImportResult,
} from '../stores/providersStore'

/**
 * Fills the price table from a public price list (models.dev, LiteLLM or OpenRouter): the browser
 * reads the list, the administrator picks the models, and each pick becomes a new effective period
 * (and the model itself when it is not registered yet). Nothing is saved without the pick.
 */
const props = defineProps<{ defaultCurrency: string }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ imported: [count: number] }>()

const { t } = useI18n()
const format = useFormat()
const store = useProvidersStore()
const fieldId = useId()

const PRICE_TYPES: Array<keyof PricesPerMillion> = ['input', 'output', 'read_cache', 'write_cache']
const CURRENCY = /^[A-Z]{3}$/
const RATE = /^[0-9]+([.,][0-9]{1,8})?$/
/** The period starts a minute ahead, so a slow clock never makes it start in the past (RNF-16). */
const START_DELAY_MS = 60_000

const sourceId = ref<PriceSourceId>('models_dev')
const providerFilter = ref<ProviderId | 'all'>('all')
const search = ref('')
const hideDeprecated = ref(true)
const offers = ref<PriceOffer[]>([])
const fetchedAt = ref<Date>()
const fetching = ref(false)
const fetchFailed = ref(false)
const selected = ref(new Set<string>())
const currency = ref('')
const rate = ref('')
const importing = ref(false)
const results = ref<PriceImportResult[]>([])
let controller: AbortController | undefined

const source = computed(() => PRICE_SOURCES.find((item) => item.id === sourceId.value))
const registered = computed(
  () => new Map(store.models.map((model) => [modelKey(model.provider, model.model), model])),
)
const knownProviders = computed(() => new Set(store.providers.map((provider) => provider.id)))
const rateNumber = computed(() =>
  currency.value === 'USD' ? 1 : RATE.test(rate.value) ? Number(rate.value.replace(',', '.')) : 0,
)
const settingsValid = computed(() => CURRENCY.test(currency.value) && rateNumber.value > 0)
const visible = computed(() => {
  const text = search.value.trim().toLowerCase()
  return offers.value.filter(
    (offer) =>
      knownProviders.value.has(offer.provider) &&
      (providerFilter.value === 'all' || offer.provider === providerFilter.value) &&
      (!hideDeprecated.value || !offer.deprecated) &&
      (text === '' ||
        offer.model.toLowerCase().includes(text) ||
        offer.displayName.toLowerCase().includes(text)),
  )
})
const selectedOffers = computed(() =>
  offers.value.filter((offer) => selected.value.has(keyOf(offer))),
)
const allVisibleSelected = computed(
  () =>
    visible.value.length > 0 && visible.value.every((offer) => selected.value.has(keyOf(offer))),
)
const failures = computed(() => results.value.filter((result) => result.error !== undefined))
const successCount = computed(() => results.value.length - failures.value.length)

watch(open, (isOpen) => {
  if (isOpen) {
    currency.value = props.defaultCurrency
    rate.value = props.defaultCurrency === 'USD' ? '1' : ''
    results.value = []
    return
  }
  controller?.abort()
  fetching.value = false
})

watch(sourceId, () => {
  offers.value = []
  selected.value = new Set()
  fetchFailed.value = false
  results.value = []
})

function keyOf(offer: PriceOffer): string {
  return modelKey(offer.provider, offer.model)
}

function providerName(id: ProviderId): string {
  return store.providers.find((provider) => provider.id === id)?.name ?? id
}

function current(offer: PriceOffer): Model | undefined {
  return registered.value.get(keyOf(offer))
}

function converted(offer: PriceOffer): PricesPerMillion | undefined {
  if (!settingsValid.value) return undefined
  const out: Partial<PricesPerMillion> = {}
  for (const type of PRICE_TYPES) {
    const value = convertDecimal(offer.perMillionUsd[type], rateNumber.value)
    if (value === undefined) return undefined
    out[type] = value
  }
  return out as PricesPerMillion
}

/** The list price is already the one in effect (same currency and the same four values). */
function sameAsCurrent(offer: PriceOffer): boolean {
  const price = current(offer)?.current_price
  const next = converted(offer)
  if (!price || !next || price.currency !== currency.value) return false
  return PRICE_TYPES.every((type) => Number(price.per_million_tokens[type]) === Number(next[type]))
}

async function fetchOffers(): Promise<void> {
  const chosen = source.value
  if (!chosen) return
  controller?.abort()
  controller = new AbortController()
  fetching.value = true
  fetchFailed.value = false
  results.value = []
  try {
    offers.value = await fetchPriceOffers(chosen, { signal: controller.signal })
    fetchedAt.value = new Date()
    // Starts with the registered models whose price changed; new models are picked by hand.
    selected.value = new Set(
      offers.value
        .filter((offer) => current(offer) && !offer.deprecated && !sameAsCurrent(offer))
        .map(keyOf),
    )
  } catch (error) {
    if (!(error instanceof DOMException && error.name === 'AbortError')) {
      offers.value = []
      fetchFailed.value = true
    }
  } finally {
    fetching.value = false
  }
}

function toggle(offer: PriceOffer): void {
  const next = new Set(selected.value)
  if (!next.delete(keyOf(offer))) next.add(keyOf(offer))
  selected.value = next
}

function toggleAllVisible(): void {
  const next = new Set(selected.value)
  const select = !allVisibleSelected.value
  for (const offer of visible.value) {
    if (select) next.add(keyOf(offer))
    else next.delete(keyOf(offer))
  }
  selected.value = next
}

async function importSelected(): Promise<void> {
  const chosen = source.value
  if (!chosen || !settingsValid.value || selectedOffers.value.length === 0) return
  importing.value = true
  const effectiveSince = new Date(Date.now() + START_DELAY_MS).toISOString()
  const consulted = format.dateTime((fetchedAt.value ?? new Date()).toISOString())
  const sourceText =
    currency.value === 'USD'
      ? t('settings.priceImport.sourceText', {
          source: chosen.name,
          url: chosen.homepage,
          when: consulted,
        })
      : t('settings.priceImport.sourceTextConverted', {
          source: chosen.name,
          url: chosen.homepage,
          when: consulted,
          rate: rate.value,
          currency: currency.value,
        })
  const items: PriceImportItem[] = []
  for (const offer of selectedOffers.value) {
    const prices = converted(offer)
    if (!prices) continue
    items.push({
      provider: offer.provider,
      model: offer.model,
      newModel: current(offer)
        ? undefined
        : { display_name: offer.displayName, capabilities: offer.capabilities, active: true },
      price: {
        effective_since: effectiveSince,
        currency: currency.value,
        per_million_tokens: prices,
        source: sourceText,
      },
    })
  }
  try {
    results.value = await store.importPrices(items)
    const done = new Set(
      results.value.filter((r) => r.error === undefined).map((r) => modelKey(r.provider, r.model)),
    )
    selected.value = new Set([...selected.value].filter((key) => !done.has(key)))
    if (done.size > 0) emit('imported', done.size)
  } finally {
    importing.value = false
  }
}
</script>

<template>
  <v-dialog
    v-model="open"
    max-width="1040"
    scrollable
    :persistent="importing"
    aria-labelledby="price-import-title"
  >
    <v-card data-testid="price-import">
      <div class="dialog-body">
        <h2 id="price-import-title">{{ $t('settings.priceImport.title') }}</h2>
        <p class="t2 help">{{ $t('settings.priceImport.help') }}</p>

        <fieldset class="sources">
          <legend class="lb">{{ $t('settings.priceImport.source') }}</legend>
          <div class="source-options">
            <label
              v-for="item in PRICE_SOURCES"
              :key="item.id"
              :for="`${fieldId}-${item.id}`"
              class="op"
              :class="{ on: sourceId === item.id }"
            >
              <input
                :id="`${fieldId}-${item.id}`"
                v-model="sourceId"
                type="radio"
                :name="`${fieldId}-source`"
                :value="item.id"
                :disabled="fetching || importing"
              />
              <span class="source-text">
                <strong>{{ item.name }}</strong>
                <span class="t2">{{ $t(`settings.priceImport.sources.${item.id}`) }}</span>
              </span>
            </label>
          </div>
        </fieldset>

        <div class="toolbar">
          <button
            type="button"
            class="btn btn-p"
            :disabled="fetching || importing"
            :aria-busy="fetching ? 'true' : undefined"
            data-testid="price-import-fetch"
            @click="fetchOffers"
          >
            {{ fetching ? $t('settings.priceImport.fetching') : $t('settings.priceImport.fetch') }}
          </button>
          <a
            v-if="source"
            :href="source.homepage"
            target="_blank"
            rel="noopener noreferrer"
            class="t2"
          >
            {{ $t('settings.priceImport.openSource', { source: source.name }) }}
          </a>
        </div>
        <v-progress-linear v-if="fetching" indeterminate color="#7C3AED" />
        <v-alert v-if="fetchFailed" type="error" variant="tonal" role="alert">
          {{ $t('settings.priceImport.fetchFailed', { source: source?.name ?? '' }) }}
        </v-alert>

        <template v-if="offers.length > 0">
          <div class="filters">
            <label :for="`${fieldId}-provider`" class="control">
              <span class="field-label">{{ $t('settings.models.provider') }}</span>
              <select :id="`${fieldId}-provider`" v-model="providerFilter" class="fld">
                <option value="all">{{ $t('settings.priceImport.allProviders') }}</option>
                <option v-for="provider in store.providers" :key="provider.id" :value="provider.id">
                  {{ provider.name }}
                </option>
              </select>
            </label>
            <label :for="`${fieldId}-search`" class="control grow">
              <span class="field-label">{{ $t('settings.priceImport.search') }}</span>
              <input
                :id="`${fieldId}-search`"
                v-model="search"
                class="fld"
                type="search"
                autocomplete="off"
              />
            </label>
            <label :for="`${fieldId}-deprecated`" class="check">
              <input :id="`${fieldId}-deprecated`" v-model="hideDeprecated" type="checkbox" />
              {{ $t('settings.priceImport.hideDeprecated') }}
            </label>
          </div>

          <p class="t2 count" role="status">
            {{
              $t('settings.priceImport.count', {
                shown: visible.length,
                total: offers.length,
                selected: selectedOffers.length,
              })
            }}
          </p>
          <div class="offers-scroll">
            <table class="tbl" data-testid="price-offers">
              <caption class="sr-only">
                {{
                  $t('settings.priceImport.caption', { source: source?.name ?? '' })
                }}
              </caption>
              <thead>
                <tr>
                  <th scope="col" class="pick">
                    <input
                      type="checkbox"
                      :checked="allVisibleSelected"
                      :aria-label="$t('settings.priceImport.selectAll')"
                      @change="toggleAllVisible"
                    />
                  </th>
                  <th scope="col">{{ $t('settings.models.model') }}</th>
                  <th scope="col">{{ $t('settings.priceImport.state') }}</th>
                  <th v-for="type in PRICE_TYPES" :key="type" scope="col" class="num">
                    {{ $t(`settings.prices.types.${type}`) }}
                  </th>
                  <th scope="col" class="num">{{ $t('settings.models.contextTokens') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="offer in visible"
                  :key="keyOf(offer)"
                  :data-testid="`price-offer-${offer.provider}-${offer.model}`"
                >
                  <td class="pick">
                    <input
                      type="checkbox"
                      :checked="selected.has(keyOf(offer))"
                      :aria-label="$t('settings.priceImport.selectNamed', { model: offer.model })"
                      @change="toggle(offer)"
                    />
                  </td>
                  <td>
                    <strong>{{ offer.displayName }}</strong>
                    <div class="t2">
                      {{ providerName(offer.provider) }} ·
                      <span class="mono">{{ offer.model }}</span>
                    </div>
                  </td>
                  <td>
                    <span v-if="!current(offer)" class="chip info">
                      {{ $t('settings.priceImport.newModel') }}
                    </span>
                    <span v-else-if="sameAsCurrent(offer)" class="chip mute">
                      {{ $t('settings.priceImport.samePrice') }}
                    </span>
                    <span v-else class="chip ok">{{ $t('settings.priceImport.registered') }}</span>
                    <span v-if="offer.deprecated" class="chip warn">
                      {{ $t('settings.priceImport.deprecated') }}
                    </span>
                  </td>
                  <td v-for="type in PRICE_TYPES" :key="type" class="num">
                    {{ format.amount(offer.perMillionUsd[type], 'USD') }}
                    <div v-if="currency !== 'USD' && converted(offer)" class="t2">
                      {{ format.amount(converted(offer)?.[type] ?? '0', currency) }}
                    </div>
                  </td>
                  <td class="num">{{ format.integer(offer.capabilities.context_tokens) }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <div class="settings">
            <label :for="`${fieldId}-currency`" class="control">
              <span class="field-label">{{ $t('settings.prices.currency') }}</span>
              <input
                :id="`${fieldId}-currency`"
                v-model="currency"
                class="fld"
                maxlength="3"
                autocomplete="off"
                :aria-describedby="`${fieldId}-currency-hint`"
                @input="currency = currency.toUpperCase()"
              />
              <span :id="`${fieldId}-currency-hint`" class="t2 hint">
                {{ $t('settings.prices.currencyHint') }}
              </span>
            </label>
            <label v-if="currency !== 'USD'" :for="`${fieldId}-rate`" class="control">
              <span class="field-label">
                {{ $t('settings.priceImport.rate', { currency: currency || '…' }) }}
              </span>
              <input
                :id="`${fieldId}-rate`"
                v-model="rate"
                class="fld"
                inputmode="decimal"
                autocomplete="off"
                :aria-invalid="rate !== '' && rateNumber <= 0 ? 'true' : undefined"
                :aria-describedby="`${fieldId}-rate-hint`"
              />
              <span :id="`${fieldId}-rate-hint`" class="t2 hint">
                {{ $t('settings.priceImport.rateHint') }}
              </span>
            </label>
          </div>
          <p class="note">{{ $t('settings.priceImport.effectNote') }}</p>
        </template>

        <div v-if="results.length > 0" aria-live="polite" data-testid="price-import-results">
          <p v-if="successCount > 0" class="ok-text">
            {{ $t('settings.priceImport.imported', successCount) }}
          </p>
          <div
            v-for="result in failures"
            :key="`${result.provider}/${result.model}`"
            class="failure"
          >
            <p>{{ $t('settings.priceImport.failedModel', { model: result.model }) }}</p>
            <ProblemAlert :error="result.error" />
          </div>
        </div>
      </div>
      <div class="dialog-actions">
        <button type="button" class="btn" :disabled="importing" @click="open = false">
          {{ $t('common.close') }}
        </button>
        <button
          type="button"
          class="btn btn-p"
          :disabled="importing || !settingsValid || selectedOffers.length === 0"
          :aria-busy="importing ? 'true' : undefined"
          data-testid="price-import-confirm"
          @click="importSelected"
        >
          {{ $t('settings.priceImport.confirm', selectedOffers.length) }}
        </button>
      </div>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.dialog-body {
  padding: 22px 24px 8px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.dialog-body h2,
.dialog-body p {
  margin: 0;
}

.help {
  font-size: 13px;
}

.sources {
  border: 0;
  margin: 0;
  padding: 0;
}

.sources legend {
  padding: 0;
  margin-bottom: 8px;
}

.source-options {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.source-options .op {
  width: auto;
  flex: 1 1 220px;
  align-items: flex-start;
  gap: 10px;
  box-sizing: border-box;
}

.source-options .op:has(input:focus-visible) {
  outline: 2px solid #c4b5fd;
  outline-offset: 2px;
}

.source-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-size: 13px;
}

input[type='radio'],
input[type='checkbox'] {
  width: 18px;
  height: 18px;
  margin: 2px 0 0;
  flex: none;
  accent-color: #7c3aed;
  cursor: pointer;
}

.toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 16px;
}

.toolbar a {
  color: #c4b5fd;
}

.filters,
.settings {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 8px 16px;
}

.control {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 180px;
}

.control.grow {
  flex: 1 1 240px;
}

.field-label {
  font-weight: 600;
}

.hint {
  font-size: 12px;
}

.check {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 44px;
  cursor: pointer;
}

.count {
  font-size: 13px;
}

.offers-scroll {
  max-height: 380px;
  overflow: auto;
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 10px;
}

.tbl {
  min-width: 860px;
}

.tbl thead th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: #161a33;
}

.tbl th,
.tbl td {
  padding: 8px 10px;
  vertical-align: middle;
}

.pick {
  width: 36px;
}

.num {
  text-align: right;
  white-space: nowrap;
}

.ok-text {
  color: #86efac;
  font-weight: 600;
}

.failure {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.dialog-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 24px 22px;
}
</style>
