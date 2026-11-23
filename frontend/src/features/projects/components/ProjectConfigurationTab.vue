<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ModelPair, StageId } from '@/shared/api/contract'
import { hasProblemSlug } from '@/shared/api/problemDetails'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { useFormat } from '@/shared/format/useFormat'
import {
  constraintsAreValid,
  constraintsPayload,
  type Budget,
  type ConstraintDraft,
  type ProjectConfiguration,
  type StageSummary,
} from '../api/projectsApi'
import {
  missingCapabilities,
  modelRefKey,
  parseModelKey,
  useModelOptions,
  type ModelOption,
} from '../composables/useModelOptions'
import { currentProjectApi, useCurrentProjectStore } from '../stores/currentProjectStore'
import ConstraintsEditor from './ConstraintsEditor.vue'
import ProjectBudgetSection from './ProjectBudgetSection.vue'
import StartRefusal from './StartRefusal.vue'

const { t } = useI18n()
const store = useCurrentProjectStore()
const { message, announce } = useAnnouncer()
const { options, loader: optionsLoader, providerName } = useModelOptions()

const configuration = ref<ProjectConfiguration>()
const etag = ref('')
const pairForm = reactive({
  reasoning: undefined as string | undefined,
  execution: undefined as string | undefined,
})
const stageForms = reactive<Record<string, { reasoning?: string; execution?: string }>>({})
const format = useFormat()
/** US-54: the technical constraints, edited as a whole list (each save is a new version). */
const constraintsForm = ref<ConstraintDraft[]>([])
const editingConstraints = ref(false)
const saving = ref<string>()
const failure = ref<{ scope: string; error: unknown }>()
const concurrentChange = ref(false)

const projectId = computed(() => store.project?.id ?? '')
/** ER-29-1: only the lead changes the configuration; others read it. */
const canEdit = computed(
  () =>
    (store.project?.my_roles.includes('lead') ?? false) &&
    store.project?.state !== 'completed' &&
    store.project?.state !== 'canceled',
)
const hasRunningWave = computed(() =>
  store.waves.some((wave) => ['running', 'awaiting_user', 'pausing'].includes(wave.state)),
)
const stages = computed(() => store.workflow?.stages ?? [])

const loader = useLoader(async () => {
  const result = await currentProjectApi().getConfiguration(projectId.value)
  apply(result.configuration, result.etag)
})

function apply(next: ProjectConfiguration, nextEtag: string): void {
  configuration.value = next
  etag.value = nextEtag
  pairForm.reasoning = modelRefKey(next.model_pair?.reasoning)
  pairForm.execution = modelRefKey(next.model_pair?.execution)
  constraintsForm.value = (next.constraints?.items ?? []).map((item) => ({ ...item }))
  for (const stage of stages.value) {
    const pair = next.pair_by_stage?.[stage.id]
    stageForms[stage.id] = {
      reasoning: modelRefKey(pair?.reasoning),
      execution: modelRefKey(pair?.execution),
    }
  }
}

onMounted(() => {
  void Promise.all([loader.run(), optionsLoader.run()])
})

function optionsFor(stage: StageSummary): ModelOption[] {
  // ER-29-25: per-stage choices are filtered by the capabilities the stage requires.
  return options.value.filter(
    (option) => missingCapabilities(option.model, stage.required_capabilities).length === 0,
  )
}

function describeRef(key: string | undefined): string {
  if (!key) return t('projects.configuration.notDefined')
  const option = options.value.find((item) => item.value === key)
  const reference = parseModelKey(key)
  return option
    ? `${option.title} (${providerName(reference.provider)})`
    : `${providerName(reference.provider)}/${reference.model}`
}

function pairOf(form: { reasoning?: string; execution?: string }): ModelPair | undefined {
  if (!form.reasoning || !form.execution) return undefined
  return { reasoning: parseModelKey(form.reasoning), execution: parseModelKey(form.execution) }
}

async function save(
  scope: string,
  change: Parameters<ReturnType<typeof currentProjectApi>['modifyConfiguration']>[1],
): Promise<void> {
  saving.value = scope
  failure.value = undefined
  concurrentChange.value = false
  try {
    const result = await currentProjectApi().modifyConfiguration(
      projectId.value,
      change,
      etag.value,
    )
    apply(result.configuration, result.etag)
    announce(t('projects.configuration.saved'))
  } catch (error) {
    if (hasProblemSlug(error, 'failed-precondition')) {
      // CA-29-18: someone else changed it; show the current value without losing what was typed.
      concurrentChange.value = true
      const typed = { ...pairForm }
      const typedStages = JSON.parse(JSON.stringify(stageForms)) as typeof stageForms
      const typedConstraints = constraintsForm.value.map((item) => ({ ...item }))
      await loader.run()
      Object.assign(pairForm, typed)
      Object.assign(stageForms, typedStages)
      constraintsForm.value = typedConstraints
    } else {
      failure.value = { scope, error }
    }
  } finally {
    saving.value = undefined
  }
}

/** US-39: the budget section saves through the same `If-Match` flow as the other sections. */
async function saveBudget(budget: Budget): Promise<boolean> {
  await save('budget', { budget })
  // ER-29-21: a lower limit may pause the project; the header and the state follow at once.
  if (!failure.value && !concurrentChange.value) {
    await store.load(projectId.value).catch(() => undefined)
    return true
  }
  return false
}

async function saveConstraints(): Promise<void> {
  if (!constraintsAreValid(constraintsForm.value)) return
  await save('constraints', { constraints: constraintsPayload(constraintsForm.value) })
  if (!failure.value && !concurrentChange.value) editingConstraints.value = false
}

function cancelConstraints(): void {
  constraintsForm.value = (configuration.value?.constraints?.items ?? []).map((item) => ({
    ...item,
  }))
  editingConstraints.value = false
}

function saveProjectPair(): void {
  const pair = pairOf(pairForm)
  if (pair) void save('project', { model_pair: pair })
}

function saveStagePair(stage: StageId): void {
  const form = stageForms[stage]
  const pair = form ? pairOf(form) : undefined
  if (pair) void save(stage, { pair_by_stage: { [stage]: pair } })
}

function resetStagePair(stage: StageId): void {
  void save(stage, { pair_by_stage: { [stage]: null } })
}

function stageName(stage: StageSummary): string {
  return t(`stages.${stage.id}`)
}
</script>

<template>
  <section aria-labelledby="configuration-heading">
    <h2 id="configuration-heading" class="text-h6 mb-2">
      {{ $t('projects.configuration.modelPair') }}
    </h2>
    <p class="text-body-2 text-medium-emphasis mb-4">{{ $t('projects.configuration.pairHelp') }}</p>
    <div role="status" class="sr-only">{{ message }}</div>

    <LoadingState v-if="loader.loading.value && !configuration" />
    <ProblemAlert
      v-else-if="loader.error.value && !configuration"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else-if="configuration">
      <v-alert v-if="concurrentChange" type="warning" variant="tonal" role="alert" class="mb-4">
        {{ $t('projects.configuration.concurrentChange') }}
      </v-alert>
      <v-alert v-if="!configuration.model_pair" type="info" variant="tonal" class="mb-4">
        {{ $t('projects.configuration.pairMissing') }}
      </v-alert>

      <dl class="summary-list mb-4" data-testid="current-pair">
        <dt>{{ $t('projects.configuration.reasoningModel') }}</dt>
        <dd>{{ describeRef(modelRefKey(configuration.model_pair?.reasoning)) }}</dd>
        <dt>{{ $t('projects.configuration.executionModel') }}</dt>
        <dd>{{ describeRef(modelRefKey(configuration.model_pair?.execution)) }}</dd>
      </dl>

      <v-form v-if="canEdit" class="mb-8" @submit.prevent="saveProjectPair">
        <v-alert
          v-if="options.length === 0 && !optionsLoader.loading.value"
          type="warning"
          variant="tonal"
          class="mb-4"
        >
          {{ $t('projects.configuration.noModelAvailable') }}
        </v-alert>
        <v-select
          v-model="pairForm.reasoning"
          :items="options"
          item-title="title"
          item-value="value"
          :label="$t('projects.configuration.reasoningModel')"
          :loading="optionsLoader.loading.value"
          class="mb-2"
        >
          <template #item="{ props: itemProps, item }">
            <v-list-item v-bind="itemProps" :subtitle="item.raw.subtitle" />
          </template>
        </v-select>
        <v-select
          v-model="pairForm.execution"
          :items="options"
          item-title="title"
          item-value="value"
          :label="$t('projects.configuration.executionModel')"
          :loading="optionsLoader.loading.value"
          class="mb-2"
        >
          <template #item="{ props: itemProps, item }">
            <v-list-item v-bind="itemProps" :subtitle="item.raw.subtitle" />
          </template>
        </v-select>
        <!-- CA-29-4: the running wave keeps its pair; the notice comes before saving. -->
        <p v-if="hasRunningWave" class="text-body-2 mb-2">
          {{ $t('projects.configuration.runningWaveKeepsPair') }}
        </p>
        <StartRefusal v-if="failure?.scope === 'project'" :error="failure.error" class="mb-2" />
        <v-btn
          type="submit"
          color="primary"
          variant="flat"
          :loading="saving === 'project'"
          :disabled="!pairOf(pairForm)"
        >
          {{ $t('projects.configuration.savePair') }}
        </v-btn>
      </v-form>

      <h3 class="text-subtitle-1 font-weight-medium mb-2">
        {{ $t('projects.configuration.byStage') }}
      </h3>
      <ul class="stage-pairs">
        <li v-for="stage in stages" :key="stage.id" :data-testid="`stage-pair-${stage.id}`">
          <v-card variant="outlined" class="pa-4">
            <h4 class="text-subtitle-2">{{ stageName(stage) }}</h4>
            <p v-if="stage.required_capabilities?.length" class="text-body-2">
              {{
                $t('projects.configuration.requires', {
                  capabilities: stage.required_capabilities
                    .map((c) => $t(`projects.capacities.${c}`))
                    .join(', '),
                })
              }}
            </p>
            <p class="text-body-2">
              <template v-if="configuration.pair_by_stage?.[stage.id]">
                {{
                  $t('projects.configuration.stagePair', {
                    reasoning: describeRef(
                      modelRefKey(configuration.pair_by_stage[stage.id]?.reasoning),
                    ),
                    execution: describeRef(
                      modelRefKey(configuration.pair_by_stage[stage.id]?.execution),
                    ),
                  })
                }}
              </template>
              <template v-else>{{ $t('projects.configuration.usesProjectPair') }}</template>
            </p>
            <v-form
              v-if="canEdit && stageForms[stage.id]"
              class="mt-2"
              @submit.prevent="saveStagePair(stage.id)"
            >
              <v-select
                v-model="stageForms[stage.id]!.reasoning"
                :items="optionsFor(stage)"
                item-title="title"
                item-value="value"
                :label="$t('projects.configuration.reasoningModel')"
                density="compact"
              />
              <v-select
                v-model="stageForms[stage.id]!.execution"
                :items="optionsFor(stage)"
                item-title="title"
                item-value="value"
                :label="$t('projects.configuration.executionModel')"
                density="compact"
              />
              <StartRefusal
                v-if="failure?.scope === stage.id"
                :error="failure.error"
                class="mb-2"
              />
              <div class="d-flex flex-wrap ga-2">
                <v-btn
                  type="submit"
                  variant="tonal"
                  :loading="saving === stage.id"
                  :disabled="!pairOf(stageForms[stage.id] ?? {})"
                >
                  {{ $t('projects.configuration.saveStagePair') }}
                </v-btn>
                <v-btn
                  v-if="configuration.pair_by_stage?.[stage.id]"
                  variant="text"
                  @click="resetStagePair(stage.id)"
                >
                  {{ $t('projects.configuration.useProjectPair') }}
                </v-btn>
              </div>
            </v-form>
          </v-card>
        </li>
      </ul>

      <ProjectBudgetSection
        :configuration="configuration"
        :can-edit="canEdit"
        :saving="saving === 'budget'"
        :failure="failure?.scope === 'budget' ? failure.error : undefined"
        :save="saveBudget"
      />

      <!-- US-54 / ESP-29: technical constraints, input of the Architecture stage. -->
      <section aria-labelledby="constraints-heading" class="mt-8" data-testid="constraints">
        <h3 id="constraints-heading" class="text-subtitle-1 font-weight-medium mb-1">
          {{ $t('projects.constraints.title') }}
        </h3>
        <p class="text-body-2 text-medium-emphasis mb-2">{{ $t('projects.constraints.help') }}</p>
        <p v-if="configuration.constraints" class="text-body-2 mb-2">
          {{
            $t('projects.constraints.version', {
              version: configuration.constraints.version,
              who: configuration.constraints.defined_by?.name ?? '',
              when: format.dateTime(configuration.constraints.defined_at),
            })
          }}
        </p>
        <template v-if="!editingConstraints">
          <p v-if="!configuration.constraints?.items.length" class="text-body-2 mb-2">
            {{ $t('projects.constraints.none') }}
          </p>
          <dl v-else class="constraints-list mb-2">
            <template v-for="item in configuration.constraints.items" :key="item.id">
              <dt class="font-weight-medium">
                <span class="monospace">{{ item.id }}</span> ·
                {{ $t(`projects.constraints.categories.${item.category}`) }}
              </dt>
              <dd class="constraint-text mb-2">{{ item.description }}</dd>
            </template>
          </dl>
          <!-- CA-4 / CA-6: only the lead changes them, and not after the project ends. -->
          <v-btn v-if="canEdit" variant="tonal" @click="editingConstraints = true">
            {{ $t('projects.constraints.edit') }}
          </v-btn>
          <p v-else class="text-body-2 text-medium-emphasis">
            {{ $t('projects.constraints.readOnly') }}
          </p>
        </template>
        <v-form v-else @submit.prevent="saveConstraints">
          <ConstraintsEditor v-model="constraintsForm" :disabled="saving === 'constraints'" />
          <p class="text-body-2 mt-2">{{ $t('projects.constraints.newVersionNotice') }}</p>
          <StartRefusal
            v-if="failure?.scope === 'constraints'"
            :error="failure.error"
            class="my-2"
          />
          <div class="d-flex flex-wrap ga-2 mt-2">
            <v-btn
              type="submit"
              color="primary"
              variant="flat"
              :loading="saving === 'constraints'"
              :disabled="!constraintsAreValid(constraintsForm)"
            >
              {{ $t('projects.constraints.save') }}
            </v-btn>
            <v-btn variant="text" @click="cancelConstraints">{{ $t('common.cancel') }}</v-btn>
          </div>
        </v-form>
      </section>
    </template>
  </section>
</template>

<style scoped>
.constraint-text {
  white-space: pre-wrap;
}

.stage-pairs {
  display: grid;
  gap: 1rem;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  list-style: none;
  padding: 0;
}
</style>
