<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Money } from '@/shared/api/contract'
import { useFormat } from '@/shared/format/useFormat'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
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

const limitMessage = computed(() =>
  props.showErrors && props.errors.limit
    ? t(`projects.budget.errors.${props.errors.limit}`, { ceiling: format.money(props.ceiling) })
    : undefined,
)
const limitDescription = computed(
  () =>
    [
      limitMessage.value ? `${props.idPrefix}-stop-limit-error` : undefined,
      props.ceiling ? `${props.idPrefix}-stop-limit-hint` : undefined,
    ]
      .filter(Boolean)
      .join(' ') || undefined,
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
  <!-- Look: canvas "Novo projeto", the budget fields of step 2 (`.fl` label over `.fld`). -->
  <div class="budget">
    <div class="budget-grid">
      <div class="fl">
        <label :for="`${idPrefix}-stop-limit`" class="fl-control">
          <span class="fl-label">{{
            currency
              ? $t('projects.budget.stopLimitIn', { currency })
              : $t('projects.budget.stopLimit')
          }}</span>
          <input
            :id="`${idPrefix}-stop-limit`"
            :value="draft.limitText"
            class="fld"
            type="text"
            inputmode="decimal"
            autocomplete="off"
            :disabled="disabled"
            :aria-invalid="!!limitMessage"
            :aria-describedby="limitDescription"
            @input="setLimit(($event.target as HTMLInputElement).value)"
          />
        </label>
        <span
          v-if="limitMessage"
          :id="`${idPrefix}-stop-limit-error`"
          class="fld-error"
          role="alert"
          >{{ limitMessage }}</span
        >
        <span v-if="ceiling" :id="`${idPrefix}-stop-limit-hint`" class="t2 fld-hint">
          {{ $t('projects.budget.ceiling', { ceiling: format.money(ceiling) }) }}
        </span>
      </div>
      <fieldset class="alerts" :aria-describedby="`${idPrefix}-alerts-help`">
        <legend class="alerts-legend">{{ $t('projects.budget.alerts') }}</legend>
        <p :id="`${idPrefix}-alerts-help`" class="t2 fld-hint alerts-help">
          {{ $t('projects.budget.alertsHelp') }}
        </p>
        <ul class="alert-list">
          <li v-for="(alert, index) in draft.alerts" :key="index" class="alert-item">
            <label :for="`${idPrefix}-alert-${index}`" class="alert-control">
              <span class="t2 alert-label">{{
                $t('projects.budget.alertLabel', { n: index + 1 })
              }}</span>
              <input
                :id="`${idPrefix}-alert-${index}`"
                :value="Number.isFinite(alert) ? String(alert) : ''"
                class="fld alert-field"
                type="number"
                min="1"
                max="99"
                :disabled="disabled"
                :aria-invalid="showErrors && !!errors.alerts"
                @input="setAlert(index, ($event.target as HTMLInputElement).value)"
              />
            </label>
            <span class="alert-row">
              <span class="t2" aria-hidden="true">%</span>
              <button
                type="button"
                class="ibtn"
                :disabled="disabled"
                :aria-label="$t('projects.budget.removeAlert', { percentage: alert })"
                @click="removeAlert(index)"
              >
                <CanvasIcon name="close" :size="18" />
              </button>
            </span>
          </li>
        </ul>
        <p v-if="alertsMessage" class="fld-error alerts-error" role="alert">
          {{ alertsMessage }}
        </p>
        <button type="button" class="btn add-alert" :disabled="disabled" @click="addAlert">
          <CanvasIcon name="plus" :size="16" :stroke-width="2.2" />
          {{ $t('projects.budget.addAlert') }}
        </button>
      </fieldset>
    </div>
    <p class="t2 fld-hint stop-rule">{{ $t('projects.budget.stopRule') }}</p>
  </div>
</template>

<style scoped>
.budget {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.budget-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 16px;
  align-items: start;
}

.fl {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.fl-control {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.fl-label,
.alerts-legend {
  font-weight: 600;
  font-size: 13px;
}

.fld[aria-invalid='true'] {
  border-color: #fca5a5;
}

.fld-hint {
  font-size: 12px;
}

.fld-error {
  color: #fca5a5;
  font-size: 12px;
}

.alerts {
  border: 0;
  margin: 0;
  padding: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  align-items: flex-start;
}

.alerts-legend {
  padding: 0;
  margin-bottom: 6px;
}

.alerts-help,
.alerts-error,
.stop-rule {
  margin: 0;
}

.alert-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
}

.alert-item {
  display: flex;
  align-items: flex-end;
  gap: 6px;
}

.alert-control {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.alert-label {
  font-size: 12px;
}

.alert-row {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: 44px;
}

.alert-field {
  width: 88px;
}

.add-alert {
  min-height: 40px;
}
</style>
