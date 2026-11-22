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
  <section id="decision" aria-labelledby="decision-heading" class="decision mb-6" tabindex="-1">
    <h2 id="decision-heading" class="text-h6 mb-2">{{ $t('findings.decision.heading') }}</h2>
    <v-alert v-if="reauthenticating" type="info" variant="tonal" role="status" class="mb-4">
      {{ $t('findings.decision.reauthenticating') }}
    </v-alert>
    <ProblemAlert
      v-if="failure && !confirmOpen"
      :error="failure"
      class="mb-4"
      data-testid="finding-decision-failure"
    />
    <div
      v-if="fieldErrors.length > 0"
      ref="errorSummary"
      tabindex="-1"
      role="alert"
      class="error-summary mb-4"
      data-testid="decision-errors"
    >
      <p class="text-body-2 font-weight-medium">{{ $t('findings.decision.errorSummary') }}</p>
      <ul>
        <li v-for="item in fieldErrors" :key="item.field" class="text-body-2">{{ item.text }}</li>
      </ul>
    </div>

    <v-form @submit.prevent="review">
      <v-radio-group
        v-model="form.action"
        :label="$t('findings.decision.option')"
        :error-messages="errors.action ? [$t(errors.action)] : []"
        hide-details="auto"
      >
        <v-radio
          v-for="action in options"
          :key="action"
          :value="action"
          :label="actionLabel(action)"
          :aria-describedby="`decision-help-${action}`"
        />
      </v-radio-group>
      <p
        v-for="action in options"
        v-show="form.action === action"
        :id="`decision-help-${action}`"
        :key="`help-${action}`"
        class="text-body-2 text-medium-emphasis mb-2"
      >
        {{ $t(`findings.decision.help.${action}`) }}
      </p>

      <div aria-live="polite">
        <template v-if="form.action === 'accept_risk'">
          <v-text-field
            v-model="form.validUntil"
            type="date"
            :min="today()"
            :label="$t('findings.decision.validUntil')"
            :hint="$t('findings.decision.validUntilHint')"
            persistent-hint
            :error-messages="errors.validUntil ? [$t(errors.validUntil)] : []"
            class="mb-2 date-field"
          />
          <!-- ER-20-9 / CA-20-13: separation of duties is said before confirming. -->
          <v-alert type="warning" variant="tonal" class="mb-4" data-testid="risk-separation">
            {{ $t('findings.decision.separationWarning') }}
          </v-alert>
        </template>
        <template v-if="form.action === 'decide_scope_change'">
          <v-radio-group
            v-model="form.modifiedBy"
            :label="$t('findings.decision.modifiedBy')"
            :error-messages="errors.modifiedBy ? [$t(errors.modifiedBy)] : []"
            hide-details="auto"
          >
            <v-radio
              value="requirements-agent"
              :label="$t('findings.modifiedBy.requirements-agent')"
            />
            <v-radio value="human" :label="$t('findings.modifiedBy.human')" />
          </v-radio-group>
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
            class="mb-2"
          />
        </template>
        <template v-if="form.action === 'discard'">
          <v-radio-group
            v-model="form.discardReason"
            :label="$t('findings.decision.discardReason')"
            :error-messages="errors.discardReason ? [$t(errors.discardReason)] : []"
            hide-details="auto"
          >
            <v-radio value="no_evidence" :label="$t('findings.discardReasons.no_evidence')" />
            <v-radio value="duplicate" :label="$t('findings.discardReasons.duplicate')" />
          </v-radio-group>
          <v-text-field
            v-if="form.discardReason === 'duplicate'"
            v-model="form.duplicateOf"
            :label="$t('findings.decision.duplicateOf')"
            :error-messages="errors.duplicateOf ? [$t(errors.duplicateOf)] : []"
            class="mb-2"
          />
        </template>
      </div>

      <v-textarea
        v-model="form.justification"
        :label="$t('findings.decision.justification')"
        :hint="$t('findings.decision.justificationHint')"
        persistent-hint
        :error-messages="errors.justification ? [$t(errors.justification)] : []"
        :counter="JUSTIFICATION_MAX"
        rows="3"
        auto-grow
        class="mt-2"
      />
      <v-btn
        type="submit"
        color="primary"
        variant="flat"
        class="mt-4"
        :disabled="options.length === 0 || sending || reauthenticating"
      >
        {{ $t('findings.decision.review') }}
      </v-btn>
    </v-form>

    <v-dialog v-model="confirmOpen" max-width="560" :persistent="sending">
      <v-card v-if="pending">
        <v-card-title>
          <h2 class="text-h6">{{ $t('findings.decision.confirmTitle', { id: finding.id }) }}</h2>
        </v-card-title>
        <v-card-text>
          <dl class="summary-list" data-testid="decision-summary">
            <dt>{{ $t('findings.decision.option') }}</dt>
            <dd>{{ actionLabel(pending.action) }}</dd>
            <dt>{{ $t('findings.decision.justification') }}</dt>
            <dd class="pre-wrap">{{ pending.body.justification }}</dd>
            <template v-if="pending.action === 'accept_risk'">
              <dt>{{ $t('findings.decision.validUntil') }}</dt>
              <dd>
                {{ pending.body.valid_until ?? $t('findings.noDeadline') }}
              </dd>
            </template>
            <template v-if="pending.action === 'decide_scope_change'">
              <dt>{{ $t('findings.decision.modifiedBy') }}</dt>
              <dd>{{ $t(`findings.modifiedBy.${pending.body.modified_by}`) }}</dd>
              <dt>{{ $t('findings.decision.startingPoints') }}</dt>
              <dd>{{ pending.body.starting_point_artifacts.join(', ') }}</dd>
            </template>
            <template v-if="pending.action === 'discard'">
              <dt>{{ $t('findings.decision.discardReason') }}</dt>
              <dd>
                {{ $t(`findings.discardReasons.${pending.body.reason}`) }}
                <template v-if="pending.body.duplicate_of">
                  · {{ pending.body.duplicate_of }}</template
                >
              </dd>
            </template>
          </dl>
          <p class="text-body-2 mt-4">{{ $t(`findings.decision.effect.${pending.action}`) }}</p>
          <v-alert
            v-if="pending.action === 'accept_risk'"
            type="warning"
            variant="tonal"
            class="mt-4"
          >
            {{ $t('findings.decision.separationWarning') }}
          </v-alert>
          <p v-if="renewing" class="text-body-2 mt-2">{{ $t('findings.decision.renewNotice') }}</p>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="sending" @click="confirmOpen = false">
            {{ $t('common.cancel') }}
          </v-btn>
          <v-btn color="primary" variant="flat" :loading="sending" @click="send(pending)">
            {{ $t('findings.decision.confirm') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </section>
</template>

<style scoped>
.pre-wrap {
  white-space: pre-wrap;
}

.date-field {
  max-width: 280px;
}

.error-summary {
  border-left: 4px solid rgb(var(--v-theme-error));
  padding: 8px 12px;
}
</style>
