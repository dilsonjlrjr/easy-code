<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import type { ArtifactSummary } from '@/features/artifacts/api/artifactsApi'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import { hasProblemSlug, isApiError } from '@/shared/api/problemDetails'
import { redirectToSso } from '@/shared/session/sessionApi'
import { useSessionStore } from '@/shared/session/sessionStore'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { findingsApi, type Finding, type FindingDecision } from '../api/findingsApi'
import {
  DECISION_ACTIONS,
  JUSTIFICATION_MAX,
  STEP_UP_ACTIONS,
  buildDecision,
  savePendingFindingDecision,
  today,
  type DecisionForm,
  type DecisionFormErrors,
} from '../composables/findingRules'

/**
 * ESP-20 section 4: the approver's decision on a finding. Only the options the api lists are
 * offered; every decision needs a justification and a confirmation with its summary. Accepting a
 * risk, an extra round and a scope change are critical (ADR-19): when the api asks for a recent
 * authentication, the decision is kept, the browser goes to the SSO step-up and the page sends it
 * once back (ER-20-16). A conflict (409) keeps the text typed and shows the current state (ER-20-15).
 */
const props = defineProps<{
  projectId: string
  finding: Finding
  /** Requirements artifacts the scope change may start from (ESP-20 3, `starting_point_artifacts`). */
  requirementsArtifacts: ArtifactSummary[]
}>()
const emit = defineEmits<{ decided: [finding: Finding]; conflict: [] }>()

const { t } = useI18n()
const router = useRouter()
const session = useSessionStore()

const form = reactive<DecisionForm>({
  action: undefined,
  justification: '',
  validUntil: '',
  modifiedBy: undefined,
  startingPoints: [],
  discardReason: undefined,
  duplicateOf: '',
})
const errors = ref<DecisionFormErrors>({})
const confirmOpen = ref(false)
const pending = ref<FindingDecision>()
const sending = ref(false)
const reauthenticating = ref(false)
const failure = ref<unknown>()
const errorSummary = ref<HTMLElement>()
let idempotencyKey = newIdempotencyKey()

const options = computed(() =>
  DECISION_ACTIONS.filter((action) => props.finding.available_actions.includes(action)),
)
const renewing = computed(
  () => props.finding.status === 'accepted' && form.action === 'accept_risk',
)
const fieldErrors = computed(() =>
  Object.entries(errors.value).map(([field, key]) => ({ field, text: t(key) })),
)

watch(
  options,
  (next) => {
    if (form.action && !next.includes(form.action)) form.action = undefined
    if (!form.action && next.length === 1) form.action = next[0]
  },
  { immediate: true },
)

watch(
  () => form.action,
  () => {
    errors.value = {}
  },
)

function actionLabel(action: string): string {
  if (action === 'accept_risk' && props.finding.status === 'accepted')
    return t('findings.decision.actions.renew_risk')
  return t(`findings.decision.actions.${action}`)
}

function startingPointLabel(artifact: ArtifactSummary): string {
  return `${artifact.id} · ${artifact.title}`
}

/** Checks the form; with errors, the summary at the top takes the focus (ESP-20 section 8). */
async function review(): Promise<void> {
  failure.value = undefined
  const result = buildDecision(form)
  errors.value = result.errors
  if (!result.decision) {
    await nextTick()
    errorSummary.value?.focus()
    return
  }
  pending.value = result.decision
  confirmOpen.value = true
}

async function send(decision: FindingDecision, afterStepUp = false): Promise<void> {
  sending.value = true
  failure.value = undefined
  try {
    const decided = await findingsApi.decide(props.projectId, props.finding.id, decision, {
      idempotencyKey,
      csrfToken: session.csrfToken,
    })
    confirmOpen.value = false
    form.justification = ''
    form.validUntil = ''
    form.startingPoints = []
    form.duplicateOf = ''
    emit('decided', decided)
  } catch (error) {
    if (
      hasProblemSlug(error, 'reauthentication-required') &&
      !afterStepUp &&
      STEP_UP_ACTIONS.includes(decision.action)
    ) {
      savePendingFindingDecision({
        projectId: props.projectId,
        findingId: props.finding.id,
        decision,
      })
      reauthenticating.value = true
      redirectToSso(
        router.resolve({
          name: ROUTE_NAMES.finding,
          params: { projectId: props.projectId, findingId: props.finding.id },
        }).fullPath,
        { reauthenticate: true },
      )
      return
    }
    failure.value = error
    confirmOpen.value = false
    // US-33 CA-6: a validity in the past is pointed at the field.
    if (isApiError(error) && error.problem?.errors?.some((item) => item.field === 'valid_until'))
      errors.value = { ...errors.value, validUntil: 'findings.decision.errors.validUntil' }
    if (isApiError(error) && error.status === 409) emit('conflict')
  } finally {
    sending.value = false
    idempotencyKey = newIdempotencyKey()
  }
}

/** Called by the page once back from the step-up, with the decision kept before leaving. */
async function resume(decision: FindingDecision): Promise<void> {
  form.action = decision.action
  form.justification = decision.body.justification
  await send(decision, true)
}

defineExpose({ resume })
</script>

<template>
  <!-- Layout: canvas "Achado" (Achado.dc.html), the "Decisão" card. -->
  <section id="decision" aria-labelledby="decision-heading" class="card decision" tabindex="-1">
    <h2 id="decision-heading">{{ $t('findings.decision.heading') }}</h2>
    <p v-if="reauthenticating" class="note decision-note" role="status">
      {{ $t('findings.decision.reauthenticating') }}
    </p>
    <ProblemAlert
      v-if="failure && !confirmOpen"
      :error="failure"
      data-testid="finding-decision-failure"
    />
    <div
      v-if="fieldErrors.length > 0"
      ref="errorSummary"
      tabindex="-1"
      role="alert"
      class="note note-crit error-summary"
      data-testid="decision-errors"
    >
      <div>
        <p class="error-summary-title">{{ $t('findings.decision.errorSummary') }}</p>
        <ul>
          <li v-for="item in fieldErrors" :key="item.field">{{ item.text }}</li>
        </ul>
      </div>
    </div>

    <form class="decision-form" novalidate @submit.prevent="review">
      <fieldset
        class="decision-group"
        :aria-describedby="errors.action ? 'decision-action-error' : undefined"
      >
        <legend class="sr-only">{{ $t('findings.decision.option') }}</legend>
        <label
          v-for="action in options"
          :key="action"
          :for="`decision-action-${action}`"
          :class="['op', { on: form.action === action }]"
        >
          <input
            :id="`decision-action-${action}`"
            v-model="form.action"
            type="radio"
            class="op-input"
            name="finding-decision-action"
            :value="action"
            :aria-labelledby="`decision-name-${action}`"
            :aria-describedby="`decision-help-${action}`"
            :aria-invalid="errors.action ? 'true' : undefined"
          />
          <span class="rd" aria-hidden="true"></span>
          <span class="op-text">
            <strong :id="`decision-name-${action}`">{{ actionLabel(action) }}</strong>
            <span :id="`decision-help-${action}`" class="t2 op-help">
              {{ $t(`findings.decision.help.${action}`) }}
            </span>
          </span>
        </label>
        <p v-if="errors.action" id="decision-action-error" class="field-error">
          {{ $t(errors.action) }}
        </p>
      </fieldset>

      <div aria-live="polite" class="decision-extra">
        <template v-if="form.action === 'accept_risk'">
          <div class="field">
            <label for="decision-valid-until" class="field">
              <span class="field-label">{{ $t('findings.decision.validUntil') }}</span>
              <input
                id="decision-valid-until"
                v-model="form.validUntil"
                type="date"
                class="fld date-field"
                :min="today()"
                :aria-invalid="errors.validUntil ? 'true' : undefined"
                :aria-describedby="
                  errors.validUntil
                    ? 'decision-valid-until-error decision-valid-until-hint'
                    : 'decision-valid-until-hint'
                "
              />
            </label>
            <p id="decision-valid-until-hint" class="t2 field-hint">
              {{ $t('findings.decision.validUntilHint') }}
            </p>
            <p v-if="errors.validUntil" id="decision-valid-until-error" class="field-error">
              {{ $t(errors.validUntil) }}
            </p>
          </div>
          <!-- ER-20-9 / CA-20-13: separation of duties is said before confirming. -->
          <p class="note note-warn" role="alert" data-testid="risk-separation">
            {{ $t('findings.decision.separationWarning') }}
          </p>
        </template>
        <template v-if="form.action === 'decide_scope_change'">
          <fieldset
            class="decision-group"
            :aria-describedby="errors.modifiedBy ? 'decision-modified-by-error' : undefined"
          >
            <legend class="field-label">{{ $t('findings.decision.modifiedBy') }}</legend>
            <label
              v-for="who in ['requirements-agent', 'human'] as const"
              :key="who"
              :for="`decision-modified-by-${who}`"
              :class="['op', 'op-compact', { on: form.modifiedBy === who }]"
            >
              <input
                :id="`decision-modified-by-${who}`"
                v-model="form.modifiedBy"
                type="radio"
                class="op-input"
                name="finding-decision-modified-by"
                :value="who"
                :aria-invalid="errors.modifiedBy ? 'true' : undefined"
              />
              <span class="rd" aria-hidden="true"></span>
              <span class="op-text">{{ $t(`findings.modifiedBy.${who}`) }}</span>
            </label>
            <p v-if="errors.modifiedBy" id="decision-modified-by-error" class="field-error">
              {{ $t(errors.modifiedBy) }}
            </p>
          </fieldset>
          <v-select
            v-model="form.startingPoints"
            :items="requirementsArtifacts"
            :item-title="startingPointLabel"
            item-value="id"
            multiple
            chips
            :label="$t('findings.decision.startingPoints')"
            :error-messages="errors.startingPoints ? [$t(errors.startingPoints)] : []"
            :no-data-text="$t('findings.decision.noRequirementsArtifacts')"
          />
        </template>
        <template v-if="form.action === 'discard'">
          <fieldset
            class="decision-group"
            :aria-describedby="errors.discardReason ? 'decision-discard-error' : undefined"
          >
            <legend class="field-label">{{ $t('findings.decision.discardReason') }}</legend>
            <label
              v-for="reason in ['no_evidence', 'duplicate'] as const"
              :key="reason"
              :for="`decision-discard-${reason}`"
              :class="['op', 'op-compact', { on: form.discardReason === reason }]"
            >
              <input
                :id="`decision-discard-${reason}`"
                v-model="form.discardReason"
                type="radio"
                class="op-input"
                name="finding-decision-discard-reason"
                :value="reason"
                :aria-invalid="errors.discardReason ? 'true' : undefined"
              />
              <span class="rd" aria-hidden="true"></span>
              <span class="op-text">{{ $t(`findings.discardReasons.${reason}`) }}</span>
            </label>
            <p v-if="errors.discardReason" id="decision-discard-error" class="field-error">
              {{ $t(errors.discardReason) }}
            </p>
          </fieldset>
          <div v-if="form.discardReason === 'duplicate'" class="field">
            <label for="decision-duplicate-of" class="field">
              <span class="field-label">{{ $t('findings.decision.duplicateOf') }}</span>
              <input
                id="decision-duplicate-of"
                v-model="form.duplicateOf"
                type="text"
                class="fld"
                :aria-invalid="errors.duplicateOf ? 'true' : undefined"
                :aria-describedby="errors.duplicateOf ? 'decision-duplicate-of-error' : undefined"
              />
            </label>
            <p v-if="errors.duplicateOf" id="decision-duplicate-of-error" class="field-error">
              {{ $t(errors.duplicateOf) }}
            </p>
          </div>
        </template>
      </div>

      <div class="field">
        <label for="decision-justification" class="field">
          <span class="field-label">{{ $t('findings.decision.justification') }}</span>
          <textarea
            id="decision-justification"
            v-model="form.justification"
            class="fld"
            rows="3"
            :aria-invalid="errors.justification ? 'true' : undefined"
            :aria-describedby="
              errors.justification
                ? 'decision-justification-error decision-justification-hint'
                : 'decision-justification-hint'
            "
          ></textarea>
        </label>
        <div class="field-foot">
          <p id="decision-justification-hint" class="t2 field-hint">
            {{ $t('findings.decision.justificationHint') }}
          </p>
          <span class="t2 field-hint field-counter">
            {{ form.justification.length }} / {{ JUSTIFICATION_MAX }}
          </span>
        </div>
        <p v-if="errors.justification" id="decision-justification-error" class="field-error">
          {{ $t(errors.justification) }}
        </p>
      </div>
      <button
        type="submit"
        class="btn btn-p"
        :disabled="options.length === 0 || sending || reauthenticating"
      >
        {{ $t('findings.decision.review') }}
      </button>
    </form>
    <p class="t2 decision-footnote">{{ $t('findings.decision.footnote') }}</p>

    <v-dialog v-model="confirmOpen" max-width="560" :persistent="sending">
      <v-card v-if="pending">
        <div class="dialog-body">
          <h2>{{ $t('findings.decision.confirmTitle', { id: finding.id }) }}</h2>
          <dl class="dialog-summary" data-testid="decision-summary">
            <div class="kv">
              <dt class="t2">{{ $t('findings.decision.option') }}</dt>
              <dd>{{ actionLabel(pending.action) }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('findings.decision.justification') }}</dt>
              <dd class="pre-wrap">{{ pending.body.justification }}</dd>
            </div>
            <div v-if="pending.action === 'accept_risk'" class="kv">
              <dt class="t2">{{ $t('findings.decision.validUntil') }}</dt>
              <dd>{{ pending.body.valid_until ?? $t('findings.noDeadline') }}</dd>
            </div>
            <template v-if="pending.action === 'decide_scope_change'">
              <div class="kv">
                <dt class="t2">{{ $t('findings.decision.modifiedBy') }}</dt>
                <dd>{{ $t(`findings.modifiedBy.${pending.body.modified_by}`) }}</dd>
              </div>
              <div class="kv">
                <dt class="t2">{{ $t('findings.decision.startingPoints') }}</dt>
                <dd>{{ pending.body.starting_point_artifacts.join(', ') }}</dd>
              </div>
            </template>
            <div v-if="pending.action === 'discard'" class="kv">
              <dt class="t2">{{ $t('findings.decision.discardReason') }}</dt>
              <dd>
                {{ $t(`findings.discardReasons.${pending.body.reason}`) }}
                <template v-if="pending.body.duplicate_of">
                  · {{ pending.body.duplicate_of }}</template
                >
              </dd>
            </div>
          </dl>
          <p class="dialog-text">{{ $t(`findings.decision.effect.${pending.action}`) }}</p>
          <p v-if="pending.action === 'accept_risk'" class="note note-warn" role="alert">
            {{ $t('findings.decision.separationWarning') }}
          </p>
          <p v-if="renewing" class="t2 dialog-text">{{ $t('findings.decision.renewNotice') }}</p>
          <div class="dialog-actions">
            <button type="button" class="btn" :disabled="sending" @click="confirmOpen = false">
              {{ $t('common.cancel') }}
            </button>
            <button
              type="button"
              class="btn btn-p"
              :disabled="sending"
              :aria-busy="sending"
              @click="send(pending)"
            >
              {{ $t('findings.decision.confirm') }}
            </button>
          </div>
        </div>
      </v-card>
    </v-dialog>
  </section>
</template>

<style scoped>
.decision {
  padding: 22px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.pre-wrap {
  white-space: pre-wrap;
}
.decision-form {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.decision-group {
  margin: 0;
  padding: 0;
  border: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.op {
  position: relative;
}
.op:hover {
  background: rgba(255, 255, 255, 0.05);
}
.op:has(.op-input:focus-visible) {
  outline: 2px solid #c4b5fd;
  outline-offset: 2px;
}
.op-input {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  margin: 0;
  opacity: 0;
  cursor: pointer;
}
.op-compact {
  min-height: 44px;
  padding: 10px 14px;
  align-items: center;
}
.op-compact .rd {
  margin-top: 0;
}
.op-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.op-help {
  font-size: 13px;
}
.decision-extra {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.decision-extra:empty {
  display: none;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.field-label {
  padding: 0;
  font-weight: 600;
  font-size: 13px;
}
.field-hint {
  margin: 0;
  font-size: 12px;
}
.field-foot {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}
.field-counter {
  flex: none;
}
.field-error {
  margin: 0;
  font-size: 12px;
  color: #fca5a5;
}
.fld[aria-invalid='true'] {
  border-color: rgba(239, 68, 68, 0.7);
}
.date-field {
  max-width: 280px;
}
.note {
  margin: 0;
}
.note-crit {
  border-color: rgba(239, 68, 68, 0.45);
  background: rgba(239, 68, 68, 0.1);
}
.note-warn {
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.12);
}
.error-summary-title {
  margin: 0;
  font-weight: 600;
}
.error-summary ul {
  margin: 4px 0 0;
  padding-left: 18px;
}
.decision-footnote {
  margin: 0;
  font-size: 12px;
}
.dialog-body {
  padding: 22px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.dialog-summary {
  margin: 0;
}
.dialog-summary dd {
  margin: 0;
  text-align: right;
}
.dialog-text {
  margin: 0;
}
.dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}
</style>
