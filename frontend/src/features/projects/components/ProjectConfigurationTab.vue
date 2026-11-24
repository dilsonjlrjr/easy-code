<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ModelPair, StageId } from '@/shared/api/contract'
import { hasProblemSlug } from '@/shared/api/problemDetails'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { useFormat } from '@/shared/format/useFormat'
import {
  constraintsAreValid,
  constraintsPayload,
  type Budget,
  type ConstraintDraft,
  type GateApprovalModeValue,
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
import ProjectDeletionSection from './ProjectDeletionSection.vue'
import ProjectGatesSection from './ProjectGatesSection.vue'
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

/**
 * US-07: the gate dialog shows the refusal itself, so this one throws; a concurrent change (412)
 * reloads the configuration so the dialog's message is read against the current mode.
 */
async function changeGateMode(gate: string, mode: GateApprovalModeValue): Promise<void> {
  try {
    const result = await currentProjectApi().modifyConfiguration(
      projectId.value,
      { gate_approval: { [gate]: mode } },
      etag.value,
    )
    apply(result.configuration, result.etag)
    announce(t('projects.gateModes.changed', { mode: t(`projects.gateModes.modes.${mode}`) }))
  } catch (error) {
    if (hasProblemSlug(error, 'failed-precondition')) await loader.run()
    throw error
  }
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
  <!-- Layout: canvas primitives of Projeto.dc.html (no own artboard): .card sections, h2, .kv, .note, .btn. -->
  <div class="configuration">
    <section aria-labelledby="configuration-heading" class="card block">
      <h2 id="configuration-heading">{{ $t('projects.configuration.modelPair') }}</h2>
      <p class="t2 help">{{ $t('projects.configuration.pairHelp') }}</p>
      <div role="status" class="sr-only">{{ message }}</div>

      <LoadingState v-if="loader.loading.value && !configuration" />
      <ProblemAlert
        v-else-if="loader.error.value && !configuration"
        :error="loader.error.value"
        retry
        @retry="loader.run"
      />
      <template v-else-if="configuration">
        <div v-if="concurrentChange" class="note note-warn" role="alert">
          <CanvasIcon name="alert" />
          <span>{{ $t('projects.configuration.concurrentChange') }}</span>
        </div>
        <p v-if="!configuration.model_pair" class="note">
          <span>{{ $t('projects.configuration.pairMissing') }}</span>
        </p>

        <dl class="kvs" data-testid="current-pair">
          <div class="kv">
            <dt class="t2">{{ $t('projects.configuration.reasoningModel') }}</dt>
            <dd>{{ describeRef(modelRefKey(configuration.model_pair?.reasoning)) }}</dd>
          </div>
          <div class="kv">
            <dt class="t2">{{ $t('projects.configuration.executionModel') }}</dt>
            <dd>{{ describeRef(modelRefKey(configuration.model_pair?.execution)) }}</dd>
          </div>
        </dl>

        <form v-if="canEdit" class="pair-form" @submit.prevent="saveProjectPair">
          <div v-if="options.length === 0 && !optionsLoader.loading.value" class="note note-warn">
            <CanvasIcon name="alert" />
            <span>{{ $t('projects.configuration.noModelAvailable') }}</span>
          </div>
          <v-select
            v-model="pairForm.reasoning"
            :items="options"
            item-title="title"
            item-value="value"
            :label="$t('projects.configuration.reasoningModel')"
            :loading="optionsLoader.loading.value"
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
          >
            <template #item="{ props: itemProps, item }">
              <v-list-item v-bind="itemProps" :subtitle="item.raw.subtitle" />
            </template>
          </v-select>
          <!-- CA-29-4: the running wave keeps its pair; the notice comes before saving. -->
          <p v-if="hasRunningWave" class="t2 help">
            {{ $t('projects.configuration.runningWaveKeepsPair') }}
          </p>
          <StartRefusal v-if="failure?.scope === 'project'" :error="failure.error" />
          <div class="actions">
            <button
              type="submit"
              class="btn btn-p"
              :disabled="saving === 'project' || !pairOf(pairForm)"
              :aria-busy="saving === 'project'"
            >
              {{ $t('projects.configuration.savePair') }}
            </button>
          </div>
        </form>
      </template>
    </section>

    <template v-if="configuration">
      <section aria-labelledby="stage-pairs-heading" class="card block">
        <h2 id="stage-pairs-heading">{{ $t('projects.configuration.byStage') }}</h2>
        <ul class="stage-pairs">
          <li
            v-for="stage in stages"
            :key="stage.id"
            class="stage-pair"
            :data-testid="`stage-pair-${stage.id}`"
          >
            <h3 class="stage-name">{{ stageName(stage) }}</h3>
            <p v-if="stage.required_capabilities?.length" class="t2 small">
              {{
                $t('projects.configuration.requires', {
                  capabilities: stage.required_capabilities
                    .map((c) => $t(`projects.capacities.${c}`))
                    .join(', '),
                })
              }}
            </p>
            <p class="small">
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
            <form
              v-if="canEdit && stageForms[stage.id]"
              class="stage-form"
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
              <StartRefusal v-if="failure?.scope === stage.id" :error="failure.error" />
              <div class="actions">
                <button
                  type="submit"
                  class="btn"
                  :disabled="saving === stage.id || !pairOf(stageForms[stage.id] ?? {})"
                  :aria-busy="saving === stage.id"
                >
                  {{ $t('projects.configuration.saveStagePair') }}
                </button>
                <button
                  v-if="configuration.pair_by_stage?.[stage.id]"
                  type="button"
                  class="btn"
                  @click="resetStagePair(stage.id)"
                >
                  {{ $t('projects.configuration.useProjectPair') }}
                </button>
              </div>
            </form>
          </li>
        </ul>
      </section>

      <ProjectGatesSection
        :configuration="configuration"
        :stages="stages"
        :can-edit="canEdit"
        :change="changeGateMode"
      />

      <ProjectBudgetSection
        :configuration="configuration"
        :can-edit="canEdit"
        :saving="saving === 'budget'"
        :failure="failure?.scope === 'budget' ? failure.error : undefined"
        :save="saveBudget"
      />

      <!-- US-54 / ESP-29: technical constraints, input of the Architecture stage. -->
      <section aria-labelledby="constraints-heading" class="card block" data-testid="constraints">
        <h2 id="constraints-heading">{{ $t('projects.constraints.title') }}</h2>
        <p class="t2 help">{{ $t('projects.constraints.help') }}</p>
        <p v-if="configuration.constraints" class="t2 small">
          {{
            $t('projects.constraints.version', {
              version: configuration.constraints.version,
              who: configuration.constraints.defined_by?.name ?? '',
              when: format.dateTime(configuration.constraints.defined_at),
            })
          }}
        </p>
        <template v-if="!editingConstraints">
          <p v-if="!configuration.constraints?.items.length" class="t2 small">
            {{ $t('projects.constraints.none') }}
          </p>
          <dl v-else class="constraints-list">
            <div v-for="item in configuration.constraints.items" :key="item.id" class="constraint">
              <dt class="constraint-title">
                <span class="mono">{{ item.id }}</span> ·
                {{ $t(`projects.constraints.categories.${item.category}`) }}
              </dt>
              <dd class="constraint-text">{{ item.description }}</dd>
            </div>
          </dl>
          <!-- CA-4 / CA-6: only the lead changes them, and not after the project ends. -->
          <div v-if="canEdit" class="actions">
            <button type="button" class="btn" @click="editingConstraints = true">
              {{ $t('projects.constraints.edit') }}
            </button>
          </div>
          <p v-else class="t2 small">{{ $t('projects.constraints.readOnly') }}</p>
        </template>
        <form v-else class="constraints-form" @submit.prevent="saveConstraints">
          <ConstraintsEditor v-model="constraintsForm" :disabled="saving === 'constraints'" />
          <p class="t2 small">{{ $t('projects.constraints.newVersionNotice') }}</p>
          <StartRefusal v-if="failure?.scope === 'constraints'" :error="failure.error" />
          <div class="actions">
            <button
              type="submit"
              class="btn btn-p"
              :disabled="saving === 'constraints' || !constraintsAreValid(constraintsForm)"
              :aria-busy="saving === 'constraints'"
            >
              {{ $t('projects.constraints.save') }}
            </button>
            <button type="button" class="btn" @click="cancelConstraints">
              {{ $t('common.cancel') }}
            </button>
          </div>
        </form>
      </section>

      <!-- US-04 / ESP-29 3.6: deleting an ended project. -->
      <ProjectDeletionSection v-if="store.project" :project="store.project" />
    </template>
  </div>
</template>

<style scoped>
.configuration {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.block {
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.help {
  margin: 0;
  font-size: 13px;
}

.small {
  margin: 0;
  font-size: 13px;
}

.note {
  margin: 0;
}

.note-warn {
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.1);
}

.note svg {
  flex: none;
}

.kvs {
  margin: 0;
}

.kvs dd {
  margin: 0;
  font-weight: 600;
  text-align: right;
}

.pair-form,
.stage-form,
.constraints-form {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 4px;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.stage-pairs {
  display: grid;
  gap: 10px;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  list-style: none;
  margin: 0;
  padding: 0;
}

.stage-pair {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 14px;
  border: 1px solid rgba(140, 150, 255, 0.16);
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.02);
}

.stage-name {
  margin: 0;
  font-size: 15px;
  font-weight: 700;
}

.constraints-list {
  margin: 0;
}

.constraint {
  padding: 10px 0;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.constraint-title {
  font-weight: 600;
}

.constraint-text {
  margin: 4px 0 0;
  white-space: pre-wrap;
  color: #a9b0d6;
}
</style>
