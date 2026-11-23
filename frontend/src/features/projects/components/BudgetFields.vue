<script setup lang="ts">
import { mdiClose, mdiPlus } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Money } from '@/shared/api/contract'
import { useFormat } from '@/shared/format/useFormat'
import type { BudgetDraft, BudgetErrors } from '../composables/budgetForm'

/**
 * Stop limit and alert percentages (US-39, ESP-05 3.2, ESP-29 3.3). The stop is always on: there
 * is no switch to turn it off (RN-1). Each alert is a labelled numeric field with a remove action
 * named by its value (ESP-29 section 8).
 */
const props = defineProps<{
  currency?: string
  ceiling?: Money | null
  errors: BudgetErrors
  /** Show the errors (after the first save attempt or once the field was touched). */
  showErrors: boolean
  disabled?: boolean
  idPrefix: string
}>()
const draft = defineModel<BudgetDraft>({ required: true })

const { t } = useI18n()
const format = useFormat()

const limitMessages = computed(() =>
  props.showErrors && props.errors.limit
    ? [
        t(`projects.budget.errors.${props.errors.limit}`, {
          ceiling: format.money(props.ceiling),
        }),
      ]
    : [],
)
const alertsMessage = computed(() =>
  props.showErrors && props.errors.alerts
    ? t(`projects.budget.errors.${props.errors.alerts}`)
    : undefined,
)

function setLimit(value: string): void {
  draft.value = { ...draft.value, limitText: value }
}

function setAlert(index: number, value: string): void {
  const alerts = [...draft.value.alerts]
  alerts[index] = value === '' ? Number.NaN : Number(value)
  draft.value = { ...draft.value, alerts }
}

function removeAlert(index: number): void {
  draft.value = { ...draft.value, alerts: draft.value.alerts.filter((_, at) => at !== index) }
}

function addAlert(): void {
  const next = Math.min(99, Math.max(0, ...draft.value.alerts.filter(Number.isFinite)) + 10)
  draft.value = { ...draft.value, alerts: [...draft.value.alerts, next] }
}
</script>

<template>
  <div>
    <v-text-field
      :id="`${idPrefix}-stop-limit`"
      :model-value="draft.limitText"
      :label="
        currency ? $t('projects.budget.stopLimitIn', { currency }) : $t('projects.budget.stopLimit')
      "
      :hint="ceiling ? $t('projects.budget.ceiling', { ceiling: format.money(ceiling) }) : ''"
      :persistent-hint="!!ceiling"
      :error-messages="limitMessages"
      :disabled="disabled"
      inputmode="decimal"
      autocomplete="off"
      class="mb-2"
      @update:model-value="setLimit"
    />
    <fieldset class="plain-fieldset" :aria-describedby="`${idPrefix}-alerts-help`">
      <legend class="text-body-1 mb-1">{{ $t('projects.budget.alerts') }}</legend>
      <p :id="`${idPrefix}-alerts-help`" class="text-body-2 text-medium-emphasis mb-2">
        {{ $t('projects.budget.alertsHelp') }}
      </p>
      <ul class="alert-list">
        <li v-for="(alert, index) in draft.alerts" :key="index" class="d-flex align-center ga-2">
          <v-text-field
            :model-value="Number.isFinite(alert) ? String(alert) : ''"
            :label="$t('projects.budget.alertLabel', { n: index + 1 })"
            type="number"
            min="1"
            max="99"
            suffix="%"
            density="compact"
            hide-details
            :disabled="disabled"
            class="alert-field"
            @update:model-value="setAlert(index, $event)"
          />
          <v-btn
            :icon="mdiClose"
            variant="text"
            size="small"
            :disabled="disabled"
            :aria-label="$t('projects.budget.removeAlert', { percentage: alert })"
            @click="removeAlert(index)"
          />
        </li>
      </ul>
      <p v-if="alertsMessage" class="text-error text-body-2 mt-1" role="alert">
        {{ alertsMessage }}
      </p>
      <v-btn
        :prepend-icon="mdiPlus"
        variant="text"
        size="small"
        class="mt-1"
        :disabled="disabled"
        @click="addAlert"
      >
        {{ $t('projects.budget.addAlert') }}
      </v-btn>
    </fieldset>
    <p class="text-body-2 text-medium-emphasis mt-2">{{ $t('projects.budget.stopRule') }}</p>
  </div>
</template>

<style scoped>
.plain-fieldset {
  border: 0;
  margin: 0;
  padding: 0;
}

.alert-list {
  display: grid;
  gap: 0.5rem;
  list-style: none;
  padding: 0;
}

.alert-field {
  max-width: 12rem;
}
</style>
