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
  <!-- Look: canvas "Configurações" (Configuracoes.dc.html) primitives inside the dialog. -->
  <v-dialog v-model="open" max-width="600" aria-labelledby="price-dialog-title">
    <v-card>
      <div class="dialog-head">
        <h2 id="price-dialog-title">
          {{
            isCorrection
              ? $t('settings.prices.correctTitle', { model: model?.display_name ?? '' })
              : $t('settings.prices.newTitle', { model: model?.display_name ?? '' })
          }}
        </h2>
      </div>

      <form v-if="step === 'form'" @submit.prevent="step = 'confirm'">
        <div class="dialog-body">
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
          />
          <fieldset class="price-fields">
            <legend class="lb">{{ $t('settings.prices.perMillion') }}</legend>
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
        </div>
        <div class="dialog-actions">
          <button type="button" class="btn" @click="open = false">{{ $t('common.cancel') }}</button>
          <button type="submit" class="btn btn-p" :disabled="!valid">
            {{ $t('common.continue') }}
          </button>
        </div>
      </form>

      <template v-else>
        <div class="dialog-body">
          <!-- ESP-23 section 4: confirmation shows model, date and values. -->
          <p class="intro">{{ $t('settings.prices.confirmIntro') }}</p>
          <dl class="summary">
            <div class="kv">
              <dt class="t2">{{ $t('settings.models.model') }}</dt>
              <dd>{{ model?.display_name }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('settings.prices.effectiveSince') }}</dt>
              <dd>{{ format.dateTime(effectiveSinceIso) }}</dd>
            </div>
            <div v-for="priceType in PRICE_TYPES" :key="priceType" class="kv">
              <dt class="t2">{{ $t(`settings.prices.types.${priceType}`) }}</dt>
              <dd>{{ format.amount(form.prices[priceType], form.currency) }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('settings.prices.source') }}</dt>
              <dd>{{ form.source }}</dd>
            </div>
          </dl>
          <p class="note rule">{{ $t('settings.prices.costRule') }}</p>
          <v-alert v-if="periodInUse" type="error" variant="tonal" role="alert">
            {{ $t('settings.prices.periodInUse') }}
          </v-alert>
          <ProblemAlert v-else-if="failure" :error="failure" />
        </div>
        <div class="dialog-actions">
          <button type="button" class="btn" :disabled="saving" @click="step = 'form'">
            {{ $t('common.back') }}
          </button>
          <button
            type="button"
            class="btn btn-p"
            :disabled="saving"
            :aria-busy="saving ? 'true' : undefined"
            @click="confirm"
          >
            {{
              isCorrection
                ? $t('settings.prices.confirmCorrection')
                : $t('settings.prices.confirmNew')
            }}
          </button>
        </div>
      </template>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.dialog-head {
  padding: 22px 24px 8px;
}

.dialog-body {
  padding: 4px 24px 8px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.price-fields {
  border: 0;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.price-fields legend {
  padding: 0;
  margin-bottom: 8px;
}

.intro {
  margin: 0;
}

.summary {
  margin: 0;
}

.summary dd {
  margin: 0;
  text-align: right;
}

.rule {
  margin: 0;
  font-size: 13px;
}

.dialog-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 24px 22px;
}
</style>
