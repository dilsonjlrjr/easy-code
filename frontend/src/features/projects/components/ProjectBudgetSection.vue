<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import { useSessionStore } from '@/shared/session/sessionStore'
import ConfirmActionDialog from '@/shared/ui/ConfirmActionDialog.vue'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import type { Budget, ProjectConfiguration, ProjectCosts } from '../api/projectsApi'
import {
  budgetPayload,
  draftFrom,
  hasBudgetErrors,
  limitAtOrBelowSpend,
  validateBudget,
  type BudgetDraft,
} from '../composables/budgetForm'
import { currentProjectApi, useCurrentProjectStore } from '../stores/currentProjectStore'
import BudgetFields from './BudgetFields.vue'

/**
 * ESP-29 3.3 (US-39): stop limit, alerts, the organization's ceiling and what was spent. Only the
 * lead edits (ER-29-1); a limit at or below the spend is accepted after a warning and pauses the
 * project by budget (CA-5, ER-29-21). Raising the limit of a project paused by budget does not
 * resume it: the page says to resume it next (ER-29-11). The figures follow the project stream.
 */
const props = defineProps<{
  configuration: ProjectConfiguration
  canEdit: boolean
  saving: boolean
  failure: unknown
  /** Saves `budget`; it never throws (the tab shows the failure). Resolves true when saved. */
  save: (budget: Budget) => Promise<boolean>
}>()
const session = useSessionStore()

const format = useFormat()
const store = useCurrentProjectStore()

const costs = ref<ProjectCosts>()
const draft = ref<BudgetDraft>(draftFrom(props.configuration.budget))
const tried = ref(false)
const editing = ref(false)
const warnOpen = ref(false)

const project = computed(() => store.project)
const loader = useLoader(async () => {
  if (!project.value) return
  costs.value = await currentProjectApi().getCosts(project.value.id)
})

// The project stream reloads the store; the spend and the balance follow it (ESP-29 section 6).
watch(
  () => store.revision,
  () => void loader.run(),
  { immediate: true },
)
watch(
  () => props.configuration.budget,
  (budget) => {
    if (!editing.value) draft.value = draftFrom(budget)
  },
)

const ceiling = computed(() => props.configuration.stop_limit_ceiling ?? null)
const spend = computed(() => costs.value?.total.total ?? project.value?.summary?.accumulated_cost)
/**
 * The organization's currency (ER-29-12): the session's, else the limit's, the ceiling's or the
 * spend's on api builds without `organization.currency`.
 */
const currency = computed(
  () =>
    session.user?.organization?.currency ??
    props.configuration.budget?.stop_limit.currency ??
    ceiling.value?.currency ??
    spend.value?.currency,
)
const errors = computed(() => validateBudget(draft.value, ceiling.value))
const payload = computed(() =>
  currency.value ? budgetPayload(draft.value, currency.value, ceiling.value) : undefined,
)
const pausedByBudget = computed(
  () => project.value?.state === 'paused' && project.value.pause_reason === 'budget',
)
/** ER-29-11: the limit is already above the spend; resuming is the next step, by a person. */
const readyToResume = computed(
  () =>
    pausedByBudget.value &&
    !!props.configuration.budget &&
    !limitAtOrBelowSpend(props.configuration.budget, spend.value),
)
const usedPercentage = computed(() => costs.value?.budget?.used_percentage)

function startEditing(): void {
  draft.value = draftFrom(props.configuration.budget)
  tried.value = false
  editing.value = true
}

function cancelEditing(): void {
  editing.value = false
  draft.value = draftFrom(props.configuration.budget)
}

async function send(): Promise<boolean> {
  if (!payload.value) return false
  const saved = await props.save(payload.value)
  if (saved) {
    editing.value = false
    void loader.run()
  }
  return saved
}

function submit(): void {
  tried.value = true
  if (hasBudgetErrors(errors.value) || !payload.value) return
  // CA-5 / ER-29-21: warn before saving a limit that pauses the project.
  if (project.value?.state === 'in_progress' && limitAtOrBelowSpend(payload.value, spend.value)) {
    warnOpen.value = true
    return
  }
  void send()
}

/** The dialog shows the refusal itself, so it gets the api error (412 is handled by the tab). */
async function confirmLowLimit(): Promise<void> {
  if (await send()) return
  const failure = props.failure
  throw failure instanceof Error ? failure : new Error('budget not saved')
}
</script>

<template>
  <!-- Layout: canvas primitives of Projeto.dc.html (no own artboard): .card, h2, .kv rows, .note. -->
  <section aria-labelledby="budget-heading" class="card budget" data-testid="budget">
    <h2 id="budget-heading">{{ $t('projects.budget.title') }}</h2>
    <p class="t2 help">{{ $t('projects.budget.stopRule') }}</p>

    <div
      v-if="pausedByBudget"
      class="note note-crit"
      role="alert"
      data-testid="budget-section-paused"
    >
      <CanvasIcon name="alert" />
      <span class="note-text">
        <span>{{ $t('projects.budget.paused') }}</span>
        <span v-if="readyToResume" data-testid="budget-ready-to-resume">
          {{ $t('projects.budget.readyToResume') }}
        </span>
        <span v-else>{{ $t('projects.budget.raiseToResume') }}</span>
      </span>
    </div>

    <dl class="kvs" data-testid="budget-current">
      <div class="kv">
        <dt class="t2">{{ $t('projects.budget.stopLimit') }}</dt>
        <dd>
          {{
            configuration.budget
              ? format.money(configuration.budget.stop_limit)
              : $t('projects.budget.notDefined')
          }}
        </dd>
      </div>
      <div class="kv">
        <dt class="t2">{{ $t('projects.budget.alerts') }}</dt>
        <dd>
          {{
            (configuration.budget?.percentage_alert_limits ?? [])
              .map((value) => format.percent(value))
              .join(', ') || $t('projects.budget.noAlerts')
          }}
        </dd>
      </div>
      <div v-if="ceiling" class="kv">
        <dt class="t2">{{ $t('projects.budget.ceilingLabel') }}</dt>
        <dd>{{ format.money(ceiling) }}</dd>
      </div>
      <div class="kv">
        <dt class="t2">{{ $t('projects.budget.spent') }}</dt>
        <dd data-testid="budget-spent">
          {{ format.money(spend) }}
          <template v-if="usedPercentage !== undefined">
            ·
            {{
              $t('projects.budget.usedPercentage', { percentage: format.percent(usedPercentage) })
            }}
          </template>
        </dd>
      </div>
      <div v-if="costs?.budget?.balance" class="kv">
        <dt class="t2">{{ $t('projects.budget.balance') }}</dt>
        <dd>{{ format.money(costs.budget.balance) }}</dd>
      </div>
    </dl>
    <ProblemAlert v-if="loader.error.value" :error="loader.error.value" retry @retry="loader.run" />

    <template v-if="canEdit">
      <div v-if="!editing" class="actions">
        <button type="button" class="btn" @click="startEditing">
          {{ $t('projects.budget.edit') }}
        </button>
      </div>
      <form v-else class="edit" @submit.prevent="submit">
        <BudgetFields
          v-model="draft"
          id-prefix="configuration-budget"
          :currency="currency"
          :ceiling="ceiling"
          :errors="errors"
          :show-errors="tried"
          :disabled="saving"
        />
        <ProblemAlert v-if="failure && !warnOpen" :error="failure" />
        <div class="actions">
          <button type="submit" class="btn btn-p" :disabled="saving" :aria-busy="saving">
            {{ $t('projects.budget.save') }}
          </button>
          <button type="button" class="btn" :disabled="saving" @click="cancelEditing">
            {{ $t('common.cancel') }}
          </button>
        </div>
      </form>
    </template>
    <p v-else class="t2 help foot">{{ $t('projects.budget.readOnly') }}</p>

    <ConfirmActionDialog
      v-model="warnOpen"
      :title="$t('projects.budget.lowLimitTitle')"
      :lines="[
        $t('projects.budget.lowLimitEffect', { spend: format.money(spend) }),
        $t('projects.budget.lowLimitResume'),
      ]"
      :keep-label="$t('projects.lifecycle.keep')"
      :confirm-label="$t('projects.budget.lowLimitConfirm')"
      confirm-color="warning"
      test-id="low-limit-dialog"
      :submit="confirmLowLimit"
    />
  </section>
</template>

<style scoped>
.budget {
  padding: 18px 22px;
}

.help {
  margin: 4px 0 10px;
  font-size: 13px;
}

.foot {
  margin: 12px 0 0;
}

.note {
  margin-bottom: 10px;
}

.note-crit {
  border-color: rgba(239, 68, 68, 0.45);
  background: rgba(239, 68, 68, 0.1);
}

.note svg {
  flex: none;
}

.note-text {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.kvs {
  margin: 0;
}

.kvs dd {
  margin: 0;
  font-weight: 600;
  text-align: right;
}

.edit {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 12px;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}

.edit .actions {
  margin-top: 0;
}
</style>
