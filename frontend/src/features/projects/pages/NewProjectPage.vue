<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { hasProblemSlug } from '@/shared/api/problemDetails'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useFormat } from '@/shared/format/useFormat'
import { useSessionStore } from '@/shared/session/sessionStore'
import AppBreadcrumbs from '@/shared/ui/AppBreadcrumbs.vue'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { constraintsAreValid, projectsApi, type ConstraintDraft } from '../api/projectsApi'
import BudgetFields from '../components/BudgetFields.vue'
import ConstraintsEditor from '../components/ConstraintsEditor.vue'
import {
  budgetPayload,
  draftFrom,
  hasBudgetErrors,
  validateBudget,
  type BudgetDraft,
} from '../composables/budgetForm'
import { parseModelKey, useModelOptions } from '../composables/useModelOptions'
import { NewProjectFlow, type FlowStep } from '../stores/newProjectFlow'
import { useProjectNoticesStore } from '../stores/projectNotices'

const { t } = useI18n()
const router = useRouter()
const format = useFormat()
const session = useSessionStore()
const notices = useProjectNoticesStore()
const { message, announce } = useAnnouncer()
const { options: modelOptions, loader: modelsLoader, providerName } = useModelOptions()

const STEPS = ['need', 'configuration', 'review'] as const
type WizardStep = (typeof STEPS)[number]
const NAME_MIN = 3
const NAME_MAX = 120
const NEED_MAX = 50_000
/** Phase 1 workflow (US-05 RN-1, CA-5): the stages that will be instantiated. */
const PHASE_ONE_STAGES = ['requirements', 'architecture', 'implementation'] as const
const ARTIFACT_LANGUAGES = ['pt-BR', 'en'] as const

const step = ref<WizardStep>('need')
const stepIndex = computed(() => STEPS.indexOf(step.value))
const form = reactive({
  name: '',
  businessNeed: '',
  attachments: [] as File[],
  reasoning: undefined as string | undefined,
  execution: undefined as string | undefined,
  artifactLanguage: 'pt-BR',
  acknowledged: false,
  constraints: [] as ConstraintDraft[],
})
const showErrors = ref(false)
/** US-39 CA-4, ESP-05 3.2: the stop limit is required; alerts 50% and 80% unless changed. */
const budget = ref<BudgetDraft>(draftFrom(null))
const showBudgetErrors = ref(false)
const nameConflict = ref(false)
const attachmentRefusal = ref<{ name: string; limit: number }>()
const submitting = ref(false)
const currentFlowStep = ref<FlowStep>()
const submitFailure = ref<unknown>()
const errorSummary = ref<HTMLElement>()
const fileInput = ref<HTMLInputElement>()
const flow = new NewProjectFlow(projectsApi, (flowStep) => {
  currentFlowStep.value = flowStep
  announce(t(`projects.new.progress.${flowStep}`))
})

const attachmentLimit = computed(() => session.user?.attachment_limit_bytes)

const nameError = computed(() => {
  const length = form.name.trim().length
  if (nameConflict.value) return t('projects.new.nameDuplicate')
  if (length < NAME_MIN || length > NAME_MAX) {
    return t('projects.new.nameRule', { min: NAME_MIN, max: NAME_MAX })
  }
  return undefined
})
const needError = computed(() => {
  if (form.businessNeed.trim() === '') return t('projects.new.needRequired')
  if (form.businessNeed.length > NEED_MAX) return t('projects.new.needTooLong', { max: NEED_MAX })
  return undefined
})
const stepOneErrors = computed(() =>
  [
    nameError.value ? { field: 'project-name', text: nameError.value } : undefined,
    needError.value ? { field: 'project-need', text: needError.value } : undefined,
  ].filter((item) => item !== undefined),
)
const modelPair = computed(() =>
  form.reasoning && form.execution
    ? { reasoning: parseModelKey(form.reasoning), execution: parseModelKey(form.execution) }
    : null,
)
const pairProviders = computed(() => {
  const pair = modelPair.value
  if (!pair) return ''
  return [...new Set([pair.reasoning.provider, pair.execution.provider])]
    .map(providerName)
    .join(', ')
})
const budgetErrors = computed(() => validateBudget(budget.value))
/**
 * The organization's currency (ER-05-7), from the session (`organization.currency`). An api build
 * without it falls back to the price of the chosen execution model, or else of any model.
 */
const currency = computed(() => {
  const chosen = modelOptions.value.find((option) => option.value === form.execution)
  return (
    session.user?.organization?.currency ??
    chosen?.model.current_price?.currency ??
    modelOptions.value.find((option) => option.model.current_price)?.model.current_price?.currency
  )
})
const canSubmit = computed(
  () =>
    form.acknowledged &&
    stepOneErrors.value.length === 0 &&
    !hasBudgetErrors(budgetErrors.value) &&
    constraintsAreValid(form.constraints) &&
    !submitting.value,
)

function modelTitle(key: string | undefined): string {
  if (!key) return t('projects.configuration.notDefined')
  return modelOptions.value.find((option) => option.value === key)?.title ?? key
}

// Nothing is preselected: the pair is an explicit choice (ESP-05 3.2, no "organization default").
onMounted(() => {
  void modelsLoader.run()
})

async function goTo(next: WizardStep): Promise<void> {
  if (stepIndex.value === 0 && STEPS.indexOf(next) > 0 && stepOneErrors.value.length > 0) {
    showErrors.value = true
    await nextTick()
    errorSummary.value?.focus()
    return
  }
  // CA-05-8: leaving step 2 without a valid stop limit points the field out with the reason.
  if (step.value === 'configuration' && next === 'review' && hasBudgetErrors(budgetErrors.value)) {
    showBudgetErrors.value = true
    await nextTick()
    document.getElementById('new-project-budget-stop-limit')?.focus()
    return
  }
  step.value = next
}

function chooseFiles(event: Event): void {
  const input = event.target as HTMLInputElement
  attachmentRefusal.value = undefined
  const limit = attachmentLimit.value
  for (const file of Array.from(input.files ?? [])) {
    // US-03 CA-4 / CA-05-16: refused at once, before the project exists; the form keeps its data.
    if (limit !== undefined && file.size > limit) {
      attachmentRefusal.value = { name: file.name, limit }
      continue
    }
    form.attachments.push(file)
  }
  input.value = ''
}

function removeAttachment(index: number): void {
  form.attachments.splice(index, 1)
}

async function submit(): Promise<void> {
  if (!canSubmit.value) return
  submitting.value = true
  submitFailure.value = undefined
  nameConflict.value = false
  const outcome = await flow.run({
    name: form.name,
    businessNeed: form.businessNeed,
    attachments: [...form.attachments],
    modelPair: modelPair.value,
    artifactLanguage: form.artifactLanguage,
    constraints: form.constraints,
    budget: currency.value ? budgetPayload(budget.value, currency.value) : undefined,
  })
  submitting.value = false
  currentFlowStep.value = undefined

  if (outcome.kind === 'not-created') {
    if (hasProblemSlug(outcome.error, 'duplicate-name')) {
      // CA-05-5: the name field is pointed out, nothing else is lost.
      nameConflict.value = true
      showErrors.value = true
      step.value = 'need'
      await nextTick()
      errorSummary.value?.focus()
      return
    }
    submitFailure.value = outcome.error
    return
  }
  if (outcome.kind === 'partial') {
    notices.rememberStartFailure(outcome.project.id, outcome.error)
  }
  await router.push({ name: ROUTE_NAMES.project, params: { projectId: outcome.project.id } })
}

const breadcrumbs = computed(() => [
  { title: t('pages.projects.title'), to: { name: ROUTE_NAMES.projects } },
  { title: t('pages.newProject.title') },
])
</script>

<template>
  <!-- Layout: canvas "Novo projeto" (NovoProjeto.dc.html). -->
  <AppBreadcrumbs :items="breadcrumbs" />
  <div>
    <h1>{{ $t('pages.newProject.title') }}</h1>
    <p class="t2 new-description">{{ $t('pages.newProject.description') }}</p>
  </div>

  <!-- ESP-05 section 8: step indicator with aria-current="step" and "passo N de 3" in text. -->
  <nav :aria-label="$t('projects.new.stepsLabel')">
    <ol class="wizard-steps">
      <li
        v-for="(item, index) in STEPS"
        :key="item"
        class="wizard-step"
        :aria-current="item === step ? 'step' : undefined"
        :class="{ 'wizard-step--current': item === step, 'wizard-step--done': index < stepIndex }"
      >
        <span class="wizard-step-number" aria-hidden="true">
          <CanvasIcon v-if="index < stepIndex" name="check" :size="14" :stroke-width="3" />
          <template v-else>{{ index + 1 }}</template>
        </span>
        <span class="wizard-step-text">
          <span class="wizard-step-name">{{ $t(`projects.new.steps.${item}`) }}</span>
          <span class="t2 wizard-step-of">{{
            $t('projects.new.stepOf', { n: index + 1, total: STEPS.length })
          }}</span>
          <span v-if="index < stepIndex" class="sr-only">{{ $t('projects.new.stepDone') }}</span>
        </span>
      </li>
    </ol>
  </nav>

  <div role="status" class="sr-only">{{ message }}</div>

  <section class="card new-card">
    <div
      v-if="showErrors && step === 'need' && stepOneErrors.length > 0"
      ref="errorSummary"
      tabindex="-1"
      class="note note-crit error-summary"
    >
      <div>
        <p class="error-summary-title">{{ $t('projects.new.errorSummary') }}</p>
        <ul class="error-summary-list">
          <li v-for="error in stepOneErrors" :key="error.field">
            <a :href="`#${error.field}`">{{ error.text }}</a>
          </li>
        </ul>
      </div>
    </div>

    <!-- Step 1: need and sources -->
    <section v-show="step === 'need'" class="new-step" aria-labelledby="step-need-heading">
      <h2 id="step-need-heading">{{ $t('projects.new.steps.need') }}</h2>
      <div class="fl">
        <label for="project-name" class="fl-control">
          <span class="fl-label">{{ $t('projects.new.name') }}</span>
          <input
            id="project-name"
            v-model="form.name"
            class="fld"
            type="text"
            autocomplete="off"
            :aria-invalid="showErrors && !!nameError"
            :aria-describedby="
              showErrors && nameError
                ? 'project-name-error project-name-counter'
                : 'project-name-counter'
            "
            @input="nameConflict = false"
          />
        </label>
        <span class="fld-foot">
          <span
            v-if="showErrors && nameError"
            id="project-name-error"
            class="fld-error"
            role="alert"
          >
            {{ nameError }}
          </span>
          <span id="project-name-counter" class="t2 fld-counter">
            {{ form.name.length }} / {{ NAME_MAX }}
          </span>
        </span>
      </div>
      <div class="fl">
        <label for="project-need" class="fl-control">
          <span class="fl-label">{{ $t('projects.new.need') }}</span>
          <textarea
            id="project-need"
            v-model="form.businessNeed"
            class="fld"
            rows="5"
            :aria-invalid="showErrors && !!needError"
            :aria-describedby="
              showErrors && needError ? 'project-need-error project-need-hint' : 'project-need-hint'
            "
          />
        </label>
        <span v-if="showErrors && needError" id="project-need-error" class="fld-error" role="alert">
          {{ needError }}
        </span>
        <span id="project-need-hint" class="t2 fld-hint">{{ $t('projects.new.needHint') }}</span>
      </div>

      <div class="fl">
        <h3 class="fl-title">{{ $t('projects.new.attachments') }}</h3>
        <span class="t2 fld-hint">
          {{
            attachmentLimit === undefined
              ? $t('projects.new.attachmentsHelp')
              : $t('projects.new.attachmentsLimit', { limit: format.bytes(attachmentLimit) })
          }}
        </span>
        <input
          id="project-attachments"
          ref="fileInput"
          type="file"
          multiple
          class="sr-only"
          :aria-label="$t('projects.new.addAttachment')"
          @change="chooseFiles"
        />
        <ul v-if="form.attachments.length > 0" class="attachment-list">
          <li v-for="(file, index) in form.attachments" :key="`${file.name}-${index}`">
            <span class="chip mute">
              {{ file.name }}
              <span class="attachment-size">({{ format.bytes(file.size) }})</span>
            </span>
            <button
              type="button"
              class="btn btn-d attachment-remove"
              :aria-label="$t('projects.new.removeAttachment', { name: file.name })"
              @click="removeAttachment(index)"
            >
              {{ $t('common.remove') }}
            </button>
          </li>
        </ul>
        <div class="source-actions">
          <button type="button" class="btn source-button" @click="fileInput?.click()">
            {{ $t('projects.new.addAttachment') }}
          </button>
        </div>
        <p
          v-if="attachmentRefusal"
          class="note note-crit attachment-refusal"
          role="alert"
          data-testid="attachment-refusal"
        >
          {{
            $t('projects.new.attachmentTooLarge', {
              name: attachmentRefusal.name,
              limit: format.bytes(attachmentRefusal.limit),
            })
          }}
        </p>
      </div>
    </section>

    <!-- Step 2: configuration -->
    <section
      v-show="step === 'configuration'"
      class="new-step new-step--configuration"
      aria-labelledby="step-configuration-heading"
    >
      <h2 id="step-configuration-heading">{{ $t('projects.new.steps.configuration') }}</h2>
      <div class="config-block">
        <div>
          <h3 class="block-title">{{ $t('projects.configuration.modelPair') }}</h3>
          <p class="t2 block-help">{{ $t('projects.configuration.pairHelp') }}</p>
        </div>
        <LoadingState v-if="modelsLoader.loading.value" type="list-item-two-line@2" />
        <ProblemAlert
          v-else-if="modelsLoader.error.value"
          :error="modelsLoader.error.value"
          retry
          @retry="modelsLoader.run"
        />
        <p v-else-if="modelOptions.length === 0" class="note note-warn">
          {{ $t('projects.configuration.noModelAvailable') }}
        </p>
        <div class="field-grid">
          <template
            v-if="
              !modelsLoader.loading.value && !modelsLoader.error.value && modelOptions.length > 0
            "
          >
            <v-select
              v-model="form.reasoning"
              :items="modelOptions"
              item-title="title"
              item-value="value"
              :label="$t('projects.configuration.reasoningModel')"
              :hint="$t('projects.configuration.reasoningHint')"
              persistent-hint
              clearable
            >
              <template #item="{ props: itemProps, item }">
                <v-list-item v-bind="itemProps" :subtitle="item.raw.subtitle" />
              </template>
            </v-select>
            <v-select
              v-model="form.execution"
              :items="modelOptions"
              item-title="title"
              item-value="value"
              :label="$t('projects.configuration.executionModel')"
              :hint="$t('projects.configuration.executionHint')"
              persistent-hint
              clearable
            >
              <template #item="{ props: itemProps, item }">
                <v-list-item v-bind="itemProps" :subtitle="item.raw.subtitle" />
              </template>
            </v-select>
          </template>
          <div class="fl">
            <label for="new-project-artifact-language" class="fl-control">
              <span id="new-project-artifact-language-label" class="fl-label">{{
                $t('projects.configuration.artifactLanguage')
              }}</span>
              <select
                id="new-project-artifact-language"
                v-model="form.artifactLanguage"
                aria-labelledby="new-project-artifact-language-label"
                class="fld"
              >
                <option v-for="value in ARTIFACT_LANGUAGES" :key="value" :value="value">
                  {{ $t(`language.names.${value}`) }}
                </option>
              </select>
            </label>
          </div>
        </div>
      </div>
      <!-- ESP-05 3.2 / US-39: the stop limit (required) and the alerts. -->
      <div class="config-block">
        <h3 class="block-title">{{ $t('projects.budget.title') }}</h3>
        <BudgetFields
          v-model="budget"
          id-prefix="new-project-budget"
          :currency="currency"
          :errors="budgetErrors"
          :show-errors="showBudgetErrors"
        />
      </div>
      <!-- ESP-05 3.2 / US-54 CA-1: optional technical constraints, version 1 of the project. -->
      <div class="config-block">
        <div>
          <h3 class="block-title">{{ $t('projects.constraints.title') }}</h3>
          <p class="t2 block-help">{{ $t('projects.constraints.help') }}</p>
        </div>
        <ConstraintsEditor v-model="form.constraints" />
      </div>
    </section>

    <!-- Step 3: review and start -->
    <section v-show="step === 'review'" class="new-step" aria-labelledby="step-review-heading">
      <h2 id="step-review-heading">{{ $t('projects.new.steps.review') }}</h2>
      <dl class="review-list">
        <dt class="t2">{{ $t('projects.new.name') }}</dt>
        <dd class="review-name">{{ form.name }}</dd>
        <dt class="t2">{{ $t('projects.new.attachments') }}</dt>
        <dd>{{ $t('projects.new.attachmentCount', { count: form.attachments.length }) }}</dd>
        <dt class="t2">{{ $t('projects.configuration.reasoningModel') }}</dt>
        <dd>{{ modelTitle(form.reasoning) }}</dd>
        <dt class="t2">{{ $t('projects.configuration.executionModel') }}</dt>
        <dd>{{ modelTitle(form.execution) }}</dd>
        <dt class="t2">{{ $t('projects.configuration.artifactLanguage') }}</dt>
        <dd>{{ $t(`language.names.${form.artifactLanguage}`) }}</dd>
        <dt class="t2">{{ $t('projects.constraints.title') }}</dt>
        <dd>{{ $t('projects.constraints.count', { count: form.constraints.length }) }}</dd>
        <dt class="t2">{{ $t('projects.budget.stopLimit') }}</dt>
        <dd data-testid="review-stop-limit">
          {{
            currency
              ? format.amount(budgetPayload(budget, currency)?.stop_limit.value ?? '', currency)
              : budget.limitText
          }}
        </dd>
        <dt class="t2">{{ $t('projects.budget.alerts') }}</dt>
        <dd>{{ budget.alerts.map((value) => format.percent(value)).join(', ') }}</dd>
        <dt class="t2">{{ $t('projects.new.workflow') }}</dt>
        <dd>
          <ol class="stage-flow">
            <li v-for="stage in PHASE_ONE_STAGES" :key="stage">{{ $t(`stages.${stage}`) }}</li>
          </ol>
        </dd>
        <dt class="t2">{{ $t('projects.gateModes.title') }}</dt>
        <dd>{{ $t('projects.new.gatesHuman') }}</dd>
      </dl>
      <!-- US-03 CA-3: native checkbox with the full label; nothing is created without it. -->
      <label
        for="new-project-acknowledgment"
        class="acknowledgment"
        data-testid="provider-acknowledgment"
      >
        <input
          id="new-project-acknowledgment"
          v-model="form.acknowledged"
          type="checkbox"
          class="acknowledgment-box"
        />
        <span>
          {{
            pairProviders
              ? $t('projects.new.acknowledgmentWithProvider', { provider: pairProviders })
              : $t('projects.new.acknowledgment')
          }}
        </span>
      </label>
      <ProblemAlert v-if="submitFailure" :error="submitFailure" />
      <div v-if="submitting" role="status" class="t2 submit-progress">
        <v-progress-linear indeterminate color="#7C3AED" bg-color="#3B3F7A" rounded />
        {{ currentFlowStep ? $t(`projects.new.progress.${currentFlowStep}`) : '' }}
      </div>
    </section>

    <div class="new-actions">
      <div class="new-actions-group">
        <button v-if="submitting" type="button" class="btn" disabled>
          {{ $t('common.cancel') }}
        </button>
        <router-link v-else class="btn" :to="{ name: ROUTE_NAMES.projects }">
          {{ $t('common.cancel') }}
        </router-link>
        <button
          v-if="stepIndex > 0"
          type="button"
          class="btn"
          :disabled="submitting"
          @click="goTo(STEPS[stepIndex - 1] ?? 'need')"
        >
          {{ $t('common.back') }}
        </button>
      </div>
      <button
        v-if="step !== 'review'"
        type="button"
        class="btn btn-p"
        @click="goTo(STEPS[stepIndex + 1] ?? 'review')"
      >
        {{ $t('common.continue') }}
      </button>
      <button
        v-else
        type="button"
        class="btn btn-p"
        :disabled="!canSubmit"
        :aria-busy="submitting"
        @click="submit"
      >
        <CanvasIcon name="play" :size="16" :stroke-width="2" />
        {{ $t('projects.new.createAndStart') }}
      </button>
    </div>
  </section>
</template>

<style scoped>
.new-description {
  margin: 4px 0 0;
}

.wizard-steps {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.wizard-step {
  flex: 1 1 200px;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border-radius: 12px;
  background: transparent;
  border: 1px solid rgba(140, 150, 255, 0.16);
}

.wizard-step--current {
  background: rgba(99, 102, 241, 0.16);
  border-color: rgba(129, 140, 248, 0.5);
}

.wizard-step-number {
  width: 28px;
  height: 28px;
  border-radius: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  font-size: 13px;
  flex: none;
  background: rgba(255, 255, 255, 0.14);
  color: #ffffff;
}

.wizard-step--current .wizard-step-number,
.wizard-step--done .wizard-step-number {
  background: #6366f1;
}

.wizard-step-text {
  display: flex;
  flex-direction: column;
}

.wizard-step-name {
  font-weight: 600;
}

.wizard-step-of {
  font-size: 12px;
}

.new-card {
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 20px;
  max-width: 980px;
}

.new-step {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.new-step--configuration {
  gap: 22px;
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

.fl-label {
  font-weight: 600;
  font-size: 13px;
}

.fl-title {
  margin: 0;
  font-weight: 600;
  font-size: 13px;
  line-height: 20px;
}

.fld[aria-invalid='true'] {
  border-color: #fca5a5;
}

.fld-foot {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}

.fld-counter {
  margin-left: auto;
  font-size: 12px;
  white-space: nowrap;
}

.fld-hint {
  font-size: 12px;
}

.fld-error {
  color: #fca5a5;
  font-size: 12px;
}

.note {
  margin: 0;
}

.note-crit {
  border-color: rgba(239, 68, 68, 0.45);
  background: rgba(239, 68, 68, 0.12);
}

.note-warn {
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.1);
}

.error-summary-title {
  margin: 0;
  font-weight: 600;
}

.error-summary-list {
  margin: 4px 0 0;
  padding-left: 18px;
}

.attachment-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.attachment-list li {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.attachment-size {
  font-weight: 400;
}

.attachment-remove,
.source-button {
  min-height: 40px;
}

.source-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.config-block {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.block-title {
  margin: 0;
  font-size: 16px;
  line-height: 24px;
  font-weight: 700;
}

.block-help {
  margin: 2px 0 0;
  font-size: 13px;
}

.field-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 16px;
  align-items: start;
}

.review-list {
  margin: 0;
  display: grid;
  grid-template-columns: minmax(160px, 220px) 1fr;
  gap: 10px 16px;
}

.review-list dd {
  margin: 0;
}

.review-name {
  font-weight: 600;
}

.stage-flow {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-wrap: wrap;
}

.stage-flow li + li::before {
  content: '·';
  margin: 0 6px;
}

.acknowledgment {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  cursor: pointer;
}

.acknowledgment-box {
  width: 18px;
  height: 18px;
  margin: 1px 0 0;
  flex: none;
  accent-color: #7c3aed;
  cursor: pointer;
}

.submit-progress {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 13px;
}

.new-actions {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  padding-top: 16px;
}

.new-actions-group {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}
</style>
