<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import { hasProblemSlug } from '@/shared/api/problemDetails'
import { useFormat } from '@/shared/format/useFormat'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import type { Model, ModelPrice, PricesPerMillion } from '../api/providersApi'
import { useProvidersStore } from '../stores/providersStore'

const props = defineProps<{
  model: Model | undefined
  /** Unused period being corrected; without it the dialog records a new period. */
  correcting?: ModelPrice
  defaultCurrency: string
}>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

const store = useProvidersStore()
const format = useFormat()
const step = ref<'form' | 'confirm'>('form')
const saving = ref(false)
const failure = ref<unknown>()
let idempotencyKey = newIdempotencyKey()

const PRICE_TYPES: Array<keyof PricesPerMillion> = ['input', 'output', 'read_cache', 'write_cache']
const DECIMAL = /^[0-9]+(\.[0-9]{1,10})?$/
const CURRENCY = /^[A-Z]{3}$/

const form = reactive<{
  effectiveSince: string
  currency: string
  source: string
  prices: PricesPerMillion
}>({
  effectiveSince: '',
  currency: '',
  source: '',
  prices: { input: '', output: '', read_cache: '', write_cache: '' },
})

const isCorrection = computed(() => props.correcting !== undefined)
const valid = computed(
  () =>
    form.effectiveSince !== '' &&
    CURRENCY.test(form.currency) &&
    form.source.trim() !== '' &&
    PRICE_TYPES.every((type) => DECIMAL.test(form.prices[type])),
)
const periodInUse = computed(() => hasProblemSlug(failure.value, 'effective-period-in-use'))

function toLocalInput(iso: string): string {
  const date = new Date(iso)
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${String(date.getFullYear())}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

watch(open, (isOpen) => {
  if (!isOpen) return
  step.value = 'form'
  failure.value = undefined
  idempotencyKey = newIdempotencyKey()
  const base = props.correcting
  form.effectiveSince = base
    ? toLocalInput(base.effective_since)
    : toLocalInput(new Date().toISOString())
  form.currency = base?.currency ?? props.defaultCurrency
  form.source = base?.source ?? ''
  form.prices = base
    ? { ...base.per_million_tokens }
    : { input: '', output: '', read_cache: '', write_cache: '' }
})

const effectiveSinceIso = computed(() =>
  form.effectiveSince === '' ? '' : new Date(form.effectiveSince).toISOString(),
)

async function confirm(): Promise<void> {
  const model = props.model
  if (!model || !valid.value) return
  saving.value = true
  failure.value = undefined
  try {
    if (props.correcting) {
      await store.correctPrice(model.provider, model.model, props.correcting, {
        effective_since: effectiveSinceIso.value,
        per_million_tokens: { ...form.prices },
        source: form.source.trim(),
      })
    } else {
      await store.recordPrice(
        model.provider,
        model.model,
        {
          effective_since: effectiveSinceIso.value,
          currency: form.currency,
          per_million_tokens: { ...form.prices },
          source: form.source.trim(),
        },
        idempotencyKey,
      )
    }
    open.value = false
    emit('saved')
  } catch (error) {
    failure.value = error
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <v-dialog v-model="open" max-width="600">
    <v-card>
      <v-card-title>
        <h2 class="text-h6">
          {{
            isCorrection
              ? $t('settings.prices.correctTitle', { model: model?.display_name ?? '' })
              : $t('settings.prices.newTitle', { model: model?.display_name ?? '' })
          }}
        </h2>
      </v-card-title>

      <v-form v-if="step === 'form'" @submit.prevent="step = 'confirm'">
        <v-card-text>
          <v-text-field
            v-model="form.effectiveSince"
            type="datetime-local"
            :label="$t('settings.prices.effectiveSince')"
          />
          <v-text-field
            v-model="form.currency"
            :label="$t('settings.prices.currency')"
            maxlength="3"
            :disabled="isCorrection"
            :hint="$t('settings.prices.currencyHint')"
            persistent-hint
            class="mb-2"
          />
          <fieldset class="price-fields">
            <legend class="text-subtitle-2 mb-2">{{ $t('settings.prices.perMillion') }}</legend>
            <v-text-field
              v-for="priceType in PRICE_TYPES"
              :key="priceType"
              v-model="form.prices[priceType]"
              inputmode="decimal"
              :label="$t(`settings.prices.types.${priceType}`)"
              :rules="[(value: string) => DECIMAL.test(value) || $t('settings.prices.decimalRule')]"
            />
          </fieldset>
          <v-text-field
            v-model="form.source"
            :label="$t('settings.prices.source')"
            :hint="$t('settings.prices.sourceHint')"
            persistent-hint
            maxlength="500"
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="open = false">{{ $t('common.cancel') }}</v-btn>
          <v-btn type="submit" color="primary" variant="flat" :disabled="!valid">
            {{ $t('common.continue') }}
          </v-btn>
        </v-card-actions>
      </v-form>

      <template v-else>
        <v-card-text>
          <!-- ESP-23 section 4: confirmation shows model, date and values. -->
          <p class="mb-2">{{ $t('settings.prices.confirmIntro') }}</p>
          <dl class="summary-list">
            <dt>{{ $t('settings.models.model') }}</dt>
            <dd>{{ model?.display_name }}</dd>
            <dt>{{ $t('settings.prices.effectiveSince') }}</dt>
            <dd>{{ format.dateTime(effectiveSinceIso) }}</dd>
            <template v-for="priceType in PRICE_TYPES" :key="priceType">
              <dt>{{ $t(`settings.prices.types.${priceType}`) }}</dt>
              <dd>{{ format.amount(form.prices[priceType], form.currency) }}</dd>
            </template>
            <dt>{{ $t('settings.prices.source') }}</dt>
            <dd>{{ form.source }}</dd>
          </dl>
          <p class="text-body-2 mt-3">{{ $t('settings.prices.costRule') }}</p>
          <v-alert v-if="periodInUse" type="error" variant="tonal" role="alert" class="mt-2">
            {{ $t('settings.prices.periodInUse') }}
          </v-alert>
          <ProblemAlert v-else-if="failure" :error="failure" class="mt-2" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="saving" @click="step = 'form'">
            {{ $t('common.back') }}
          </v-btn>
          <v-btn color="primary" variant="flat" :loading="saving" @click="confirm">
            {{
              isCorrection
                ? $t('settings.prices.confirmCorrection')
                : $t('settings.prices.confirmNew')
            }}
          </v-btn>
        </v-card-actions>
      </template>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.price-fields {
  border: 0;
  margin: 0;
  padding: 0;
}
</style>
