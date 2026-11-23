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
 * The organization's currency (ER-05-7): the contract gives it only through money values, so the
 * price of the chosen execution model, or else of any model, names it. Without a price the budget
 * is not sent and the start is refused listing the budget (US-05 CA-2).
 */
const currency = computed(() => {
  const chosen = modelOptions.value.find((option) => option.value === form.execution)
  return (
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
  <v-container max-width="960">
    <AppBreadcrumbs :items="breadcrumbs" />
    <h1 class="text-h4 mb-4">{{ $t('pages.newProject.title') }}</h1>

    <!-- ESP-05 section 8: step indicator with aria-current="step" and "passo N de 3" in text. -->
    <nav :aria-label="$t('projects.new.stepsLabel')" class="mb-6">
      <ol class="wizard-steps">
        <li
          v-for="(item, index) in STEPS"
          :key="item"
          :aria-current="item === step ? 'step' : undefined"
          :class="{ 'wizard-step--current': item === step, 'wizard-step--done': index < stepIndex }"
        >
          <span class="text-body-2">{{
            $t('projects.new.stepOf', { n: index + 1, total: STEPS.length })
          }}</span>
          <span class="font-weight-medium">{{ $t(`projects.new.steps.${item}`) }}</span>
          <span v-if="index < stepIndex" class="sr-only">{{ $t('projects.new.stepDone') }}</span>
        </li>
      </ol>
    </nav>

    <div role="status" class="sr-only">{{ message }}</div>

    <div
      v-if="showErrors && step === 'need' && stepOneErrors.length > 0"
      ref="errorSummary"
      tabindex="-1"
      class="mb-4"
    >
      <v-alert type="error" variant="tonal">
        <p class="font-weight-medium">{{ $t('projects.new.errorSummary') }}</p>
        <ul class="ml-4">
          <li v-for="error in stepOneErrors" :key="error.field">
            <a :href="`#${error.field}`">{{ error.text }}</a>
          </li>
        </ul>
      </v-alert>
    </div>

    <!-- Step 1: need and sources -->
    <section v-show="step === 'need'" aria-labelledby="step-need-heading">
      <h2 id="step-need-heading" class="text-h6 mb-4">{{ $t('projects.new.steps.need') }}</h2>
      <v-text-field
        id="project-name"
        v-model="form.name"
        :label="$t('projects.new.name')"
        :counter="NAME_MAX"
        :error-messages="showErrors && nameError ? [nameError] : []"
        autocomplete="off"
        @update:model-value="nameConflict = false"
      />
      <v-textarea
        id="project-need"
        v-model="form.businessNeed"
        :label="$t('projects.new.need')"
        :hint="$t('projects.new.needHint')"
        persistent-hint
        rows="6"
        auto-grow
        :error-messages="showErrors && needError ? [needError] : []"
        class="mb-4"
      />

      <h3 class="text-subtitle-1 font-weight-medium mt-4">{{ $t('projects.new.attachments') }}</h3>
      <p class="text-body-2 text-medium-emphasis">
        {{
          attachmentLimit === undefined
            ? $t('projects.new.attachmentsHelp')
            : $t('projects.new.attachmentsLimit', { limit: format.bytes(attachmentLimit) })
        }}
      </p>
      <input
        id="project-attachments"
        ref="fileInput"
        type="file"
        multiple
        class="sr-only"
        :aria-label="$t('projects.new.addAttachment')"
        @change="chooseFiles"
      />
      <v-btn variant="tonal" class="mt-2" @click="fileInput?.click()">
        {{ $t('projects.new.addAttachment') }}
      </v-btn>
      <v-alert
        v-if="attachmentRefusal"
        type="error"
        variant="tonal"
        role="alert"
        class="mt-2"
        data-testid="attachment-refusal"
      >
        {{
          $t('projects.new.attachmentTooLarge', {
            name: attachmentRefusal.name,
            limit: format.bytes(attachmentRefusal.limit),
          })
        }}
      </v-alert>
      <ul v-if="form.attachments.length > 0" class="attachment-list mt-2">
        <li v-for="(file, index) in form.attachments" :key="`${file.name}-${index}`">
          <span>{{ file.name }}</span>
          <span class="text-body-2 text-medium-emphasis">({{ format.bytes(file.size) }})</span>
          <v-btn
            variant="text"
            size="small"
            :aria-label="$t('projects.new.removeAttachment', { name: file.name })"
            @click="removeAttachment(index)"
          >
            {{ $t('common.remove') }}
          </v-btn>
        </li>
      </ul>
    </section>

    <!-- Step 2: configuration -->
    <section v-show="step === 'configuration'" aria-labelledby="step-configuration-heading">
      <h2 id="step-configuration-heading" class="text-h6 mb-2">
        {{ $t('projects.new.steps.configuration') }}
      </h2>
      <h3 class="text-subtitle-1 font-weight-medium">
        {{ $t('projects.configuration.modelPair') }}
      </h3>
      <p class="text-body-2 text-medium-emphasis mb-4">
        {{ $t('projects.configuration.pairHelp') }}
      </p>
      <LoadingState v-if="modelsLoader.loading.value" type="list-item-two-line@2" />
      <ProblemAlert
        v-else-if="modelsLoader.error.value"
        :error="modelsLoader.error.value"
        retry
        @retry="modelsLoader.run"
      />
      <v-alert v-else-if="modelOptions.length === 0" type="warning" variant="tonal" class="mb-4">
        {{ $t('projects.configuration.noModelAvailable') }}
      </v-alert>
      <template v-else>
        <v-select
          v-model="form.reasoning"
          :items="modelOptions"
          item-title="title"
          item-value="value"
          :label="$t('projects.configuration.reasoningModel')"
          :hint="$t('projects.configuration.reasoningHint')"
          persistent-hint
          clearable
          class="mb-4"
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
          class="mb-4"
        >
          <template #item="{ props: itemProps, item }">
            <v-list-item v-bind="itemProps" :subtitle="item.raw.subtitle" />
          </template>
        </v-select>
      </template>
      <v-select
        v-model="form.artifactLanguage"
        :items="
          ARTIFACT_LANGUAGES.map((value) => ({ value, title: $t(`language.names.${value}`) }))
        "
        :label="$t('projects.configuration.artifactLanguage')"
      />
      <!-- ESP-05 3.2 / US-39: the stop limit (required) and the alerts. -->
      <h3 class="text-subtitle-1 font-weight-medium mt-4 mb-2">
        {{ $t('projects.budget.title') }}
      </h3>
      <BudgetFields
        v-model="budget"
        id-prefix="new-project-budget"
        :currency="currency"
        :errors="budgetErrors"
        :show-errors="showBudgetErrors"
      />
      <!-- ESP-05 3.2 / US-54 CA-1: optional technical constraints, version 1 of the project. -->
      <h3 class="text-subtitle-1 font-weight-medium mt-4">
        {{ $t('projects.constraints.title') }}
      </h3>
      <p class="text-body-2 text-medium-emphasis mb-2">{{ $t('projects.constraints.help') }}</p>
      <ConstraintsEditor v-model="form.constraints" />
    </section>

    <!-- Step 3: review and start -->
    <section v-show="step === 'review'" aria-labelledby="step-review-heading">
      <h2 id="step-review-heading" class="text-h6 mb-4">{{ $t('projects.new.steps.review') }}</h2>
      <dl class="summary-list mb-4">
        <dt>{{ $t('projects.new.name') }}</dt>
        <dd>{{ form.name }}</dd>
        <dt>{{ $t('projects.new.attachments') }}</dt>
        <dd>{{ $t('projects.new.attachmentCount', { count: form.attachments.length }) }}</dd>
        <dt>{{ $t('projects.configuration.reasoningModel') }}</dt>
        <dd>{{ modelTitle(form.reasoning) }}</dd>
        <dt>{{ $t('projects.configuration.executionModel') }}</dt>
        <dd>{{ modelTitle(form.execution) }}</dd>
        <dt>{{ $t('projects.configuration.artifactLanguage') }}</dt>
        <dd>{{ $t(`language.names.${form.artifactLanguage}`) }}</dd>
        <dt>{{ $t('projects.constraints.title') }}</dt>
        <dd>{{ $t('projects.constraints.count', { count: form.constraints.length }) }}</dd>
        <dt>{{ $t('projects.budget.stopLimit') }}</dt>
        <dd data-testid="review-stop-limit">
          {{
            currency
              ? format.amount(budgetPayload(budget, currency)?.stop_limit.value ?? '', currency)
              : budget.limitText
          }}
        </dd>
        <dt>{{ $t('projects.budget.alerts') }}</dt>
        <dd>{{ budget.alerts.map((value) => format.percent(value)).join(', ') }}</dd>
      </dl>
      <h3 class="text-subtitle-1 font-weight-medium">{{ $t('projects.new.workflow') }}</h3>
      <ol class="ml-6 mb-2">
        <li v-for="stage in PHASE_ONE_STAGES" :key="stage">{{ $t(`stages.${stage}`) }}</li>
      </ol>
      <p class="text-body-2 text-medium-emphasis mb-4">{{ $t('projects.new.gatesHuman') }}</p>
      <!-- US-03 CA-3: native checkbox with the full label; nothing is created without it. -->
      <v-checkbox v-model="form.acknowledged" data-testid="provider-acknowledgment">
        <template #label>
          {{
            pairProviders
              ? $t('projects.new.acknowledgmentWithProvider', { provider: pairProviders })
              : $t('projects.new.acknowledgment')
          }}
        </template>
      </v-checkbox>
      <ProblemAlert v-if="submitFailure" :error="submitFailure" class="mb-4" />
      <div v-if="submitting" role="status" class="text-body-2 mb-2">
        <v-progress-linear indeterminate class="mb-1" />
        {{ currentFlowStep ? $t(`projects.new.progress.${currentFlowStep}`) : '' }}
      </div>
    </section>

    <div class="d-flex flex-wrap ga-2 mt-6">
      <v-btn variant="text" :to="{ name: ROUTE_NAMES.projects }" :disabled="submitting">
        {{ $t('common.cancel') }}
      </v-btn>
      <v-spacer />
      <v-btn
        v-if="stepIndex > 0"
        variant="outlined"
        :disabled="submitting"
        @click="goTo(STEPS[stepIndex - 1] ?? 'need')"
      >
        {{ $t('common.back') }}
      </v-btn>
      <v-btn
        v-if="step !== 'review'"
        color="primary"
        variant="flat"
        @click="goTo(STEPS[stepIndex + 1] ?? 'review')"
      >
        {{ $t('common.continue') }}
      </v-btn>
      <v-btn
        v-else
        color="primary"
        variant="flat"
        :disabled="!canSubmit"
        :loading="submitting"
        @click="submit"
      >
        {{ $t('projects.new.createAndStart') }}
      </v-btn>
    </div>
  </v-container>
</template>

<style scoped>
.wizard-steps {
  display: flex;
  flex-wrap: wrap;
  gap: 1rem;
  list-style: none;
  padding: 0;
  margin: 0;
}

.wizard-steps li {
  display: flex;
  flex-direction: column;
  min-width: 160px;
  padding: 0.5rem 0.75rem;
  border-bottom: 3px solid rgb(var(--v-theme-on-surface), 0.2);
}

.wizard-step--current {
  border-bottom-color: rgb(var(--v-theme-primary)) !important;
}

.wizard-step--done {
  border-bottom-color: rgb(var(--v-theme-success)) !important;
}

.attachment-list {
  list-style: none;
  padding: 0;
}

.attachment-list li {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}
</style>
