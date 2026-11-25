<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { waitForCommand } from '@/shared/commands/waitForCommand'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import { ApiError } from '@/shared/api/problemDetails'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import { projectsApi, type Attachment } from '@/features/projects/api/projectsApi'
import { useCurrentProjectStore } from '@/features/projects/stores/currentProjectStore'
import {
  artifactsApi,
  type Artifact,
  type ArtifactEditStatus,
} from '@/features/artifacts/api/artifactsApi'
import {
  awaitingAgentReview,
  canEditArtifact,
  followArtifactEdit,
} from '@/features/artifacts/composables/artifactEditing'
import { useArtifactEditNotice } from '@/features/artifacts/composables/useArtifactEditNotice'
import WaveQuestions from '@/features/waves/components/WaveQuestions.vue'
import { wavesApi, type Question } from '@/features/waves/api/wavesApi'
import type { StageViewProps } from '../stageViews'
import RequirementsEditor from './RequirementsEditor.vue'
import { requirementsApi } from './requirementsApi'
import {
  assumptions,
  citations,
  fieldsOf,
  itemEdit,
  KINDS,
  requirementItems,
  type Kind,
  type RequirementItem,
} from './requirementsView'

/**
 * ESP-07 (canvas EtapaRequisitos): the tabs Requisitos (stories, NFRs, rules by story, actors and
 * glossary, with the editor of US-53), Fontes (the business need and the project attachments, read-only
 * after the start: PD-12, CA-07-28) and Descoberta (the agent questions and the BACKLOG assumptions,
 * read-only: PL-19, CA-07-29). The exit criteria, waves and artifacts stay in StageDetail.
 */
const props = defineProps<StageViewProps>()
const { t } = useI18n()
const format = useFormat()
const route = useRoute()
const router = useRouter()
const store = useCurrentProjectStore()

const TABS = ['items', 'sources', 'discovery'] as const
type Tab = (typeof TABS)[number]

const tab = computed<Tab>({
  get: () => {
    const value = route.query.reqTab
    return typeof value === 'string' && (TABS as readonly string[]).includes(value)
      ? (value as Tab)
      : 'items'
  },
  set: (value) => void router.replace({ query: { ...route.query, reqTab: value } }),
})
const kind = ref<Kind>('stories')

function arrowTarget<T>(list: readonly T[], current: T, event: KeyboardEvent): T | undefined {
  const index = list.indexOf(current)
  const next =
    event.key === 'ArrowRight'
      ? (index + 1) % list.length
      : event.key === 'ArrowLeft'
        ? (index - 1 + list.length) % list.length
        : event.key === 'Home'
          ? 0
          : event.key === 'End'
            ? list.length - 1
            : undefined
  return next === undefined ? undefined : list[next]
}

function onTabKey(event: KeyboardEvent): void {
  const target = arrowTarget(TABS, tab.value, event)
  if (target === undefined) return
  event.preventDefault()
  tab.value = target
  void nextTick(() => document.getElementById(`req-tab-${target}`)?.focus())
}

function onKindKey(event: KeyboardEvent): void {
  const target = arrowTarget(KINDS, kind.value, event)
  if (target === undefined) return
  event.preventDefault()
  chooseKind(target)
  void nextTick(() => document.getElementById(`req-kind-${target}`)?.focus())
}

// ------------------------------------------------------------------ data

const artifacts = ref<Artifact[]>([])
const attachments = ref<Attachment[]>([])
const questions = ref<Array<{ wave: string; question: Question }>>([])

const requirementWaves = computed(() =>
  [...props.stage.waves]
    .filter((wave) => wave.agent === 'requirements-agent')
    .sort((left, right) => right.created_at.localeCompare(left.created_at)),
)

const loader = useLoader(async () => {
  const summaries = props.stage.artifacts.filter((artifact) =>
    ['VISAO', 'US', 'RNF', 'BACKLOG'].includes(artifact.type),
  )
  const [read, sent] = await Promise.all([
    Promise.all(summaries.map((summary) => artifactsApi.getArtifact(props.projectId, summary.id))),
    requirementsApi.listAttachments(props.projectId),
  ])
  artifacts.value = read
  attachments.value = sent
})

/** CA-07-2: the questions of the waves waiting for a person, and the last wave's answered ones. */
const questionsLoader = useLoader(async () => {
  const waves = requirementWaves.value.filter(
    (wave, index) => wave.state === 'awaiting_user' || index === 0,
  )
  const perWave = await Promise.all(
    waves.map(async (wave) =>
      (await wavesApi.listQuestions(props.projectId, wave.id)).map((question) => ({
        wave: wave.id,
        question,
      })),
    ),
  )
  questions.value = perWave.flat()
})

watch(
  () => [
    props.projectId,
    props.revision,
    props.stage.artifacts.map((a) => `${a.id}:${String(a.version)}`).join(),
  ],
  () => {
    void loader.run()
    void questionsLoader.run()
  },
  { immediate: true },
)

const items = computed(() => requirementItems(kind.value, artifacts.value))
const counts = computed(() =>
  Object.fromEntries(
    KINDS.map((value) => [value, requirementItems(value, artifacts.value).length]),
  ),
)
const totalItems = computed(() => KINDS.reduce((sum, value) => sum + (counts.value[value] ?? 0), 0))
const supposed = computed(() => assumptions(artifacts.value))
const openQuestions = computed(() => questions.value.filter((entry) => !entry.question.answer))
const tabCounts = computed<Record<Tab, number>>(() => ({
  items: totalItems.value,
  sources: attachments.value.length + (store.project?.business_need ? 1 : 0),
  discovery: openQuestions.value.length + supposed.value.length,
}))

const project = computed(() => store.project)
function editable(artifact: Artifact | undefined): boolean {
  return !!artifact && !!project.value && canEditArtifact(artifact, project.value)
}
/** Where a new item of the kind goes: stories and NFRs are new artifacts; rules a story; actors and terms the VISAO. */
const targets = computed(() => {
  if (kind.value === 'rules') return artifacts.value.filter((a) => a.type === 'US' && editable(a))
  if (kind.value === 'actors' || kind.value === 'terms')
    return artifacts.value.filter((a) => a.type === 'VISAO' && editable(a))
  return []
})
const canCreate = computed(() => {
  if (kind.value === 'stories' || kind.value === 'nfrs') {
    const roles = project.value?.my_roles ?? []
    return (
      !!project.value &&
      ['in_progress'].includes(project.value.state) &&
      roles.some((role) => ['member', 'approver', 'lead'].includes(role))
    )
  }
  return targets.value.length > 0
})

// ------------------------------------------------------------------ editor

const editing = ref<{ operation: 'create' | 'modify'; item?: RequirementItem; serial: number }>()
const confirming = ref<string>()
const deleting = ref<string>()
const deleteFailure = ref<unknown>()
let serial = 0

const {
  edit: trackedEdit,
  notice,
  message,
  track,
  dismiss,
} = useArtifactEditNotice(
  () => props.projectId,
  async () => {
    await loader.run()
  },
)

function chooseKind(next: Kind): void {
  kind.value = next
  editing.value = undefined
  confirming.value = undefined
}

function startCreate(): void {
  editing.value = { operation: 'create', serial: ++serial }
}

function startEdit(item: RequirementItem): void {
  confirming.value = undefined
  editing.value = { operation: 'modify', item, serial: ++serial }
}

function askDelete(item: RequirementItem): void {
  confirming.value = item.key
  void nextTick(() => document.getElementById(`req-confirm-${cssId(item.key)}`)?.focus())
}

function cancelDelete(item: RequirementItem): void {
  confirming.value = undefined
  void nextTick(() => document.getElementById(`req-delete-${cssId(item.key)}`)?.focus())
}

function cssId(key: string): string {
  return key.replace(/[^A-Za-z0-9_-]/g, '-')
}

async function onSubmitted(status: ArtifactEditStatus): Promise<void> {
  editing.value = undefined
  await track(status)
}

/** CA-07-27: removing an item is a new version without it, confirmed in line (PL-41). */
async function confirmDelete(item: RequirementItem): Promise<void> {
  const edit = itemEdit(
    'delete',
    item.kind,
    fieldsOf(item),
    t('stageRequirements.deleteReason', { item: item.label }),
    item,
  )
  if (!edit) return
  deleting.value = item.key
  deleteFailure.value = undefined
  try {
    const accepted = await artifactsApi.editArtifact(props.projectId, edit, newIdempotencyKey())
    const read = () => artifactsApi.getArtifactEdit(props.projectId, accepted.edit)
    const guarded = await followArtifactEdit(await read(), read, (state) => state === 'pending')
    if (guarded.state === 'refused') {
      const command = await projectsApi.getCommand(props.projectId, guarded.command)
      throw new ApiError(command.refusal?.status ?? 409, command.refusal)
    }
    confirming.value = undefined
    await track(guarded)
  } catch (error) {
    deleteFailure.value = error
  } finally {
    deleting.value = undefined
  }
}

function authorChip(artifact: Artifact): { tone: string; text: string } {
  if (artifact.author.type === 'human') {
    return awaitingAgentReview(artifact.author)
      ? { tone: 'o-voce', text: t('stageRequirements.author.awaiting') }
      : {
          tone: 'o-voce',
          text: t('stageRequirements.author.human', {
            person: artifact.author.edited_by?.name ?? '',
          }),
        }
  }
  return { tone: 'o-agente', text: t('stageRequirements.author.agent') }
}

// ------------------------------------------------------------------ discovery

const canAnswer = computed(
  () =>
    !!project.value &&
    project.value.state === 'in_progress' &&
    project.value.my_roles.some((role) => ['member', 'approver', 'lead'].includes(role)),
)

function questionsOf(wave: string): Question[] {
  return questions.value.filter((entry) => entry.wave === wave).map((entry) => entry.question)
}
const questionWaves = computed(() => [...new Set(questions.value.map((entry) => entry.wave))])

/** US-10 CA-1: the answer is a command; the wave runs again from where it stopped. */
function submitFor(wave: string) {
  return async (
    question: Question,
    answers: Array<{ number: number; text: string }>,
    key: string,
  ): Promise<void> => {
    const accepted = await wavesApi.answerQuestion(props.projectId, wave, question.id, answers, key)
    const finished = await waitForCommand(accepted, (id) =>
      projectsApi.getCommand(props.projectId, id),
    )
    if (finished.state === 'refused') {
      throw new ApiError(finished.refusal?.status ?? 409, finished.refusal)
    }
    await questionsLoader.run()
  }
}
</script>

<template>
  <!-- Layout: canvas EtapaRequisitos (tabs, list + editor, Fontes, Descoberta). -->
  <section class="req" aria-labelledby="req-heading" data-testid="requirements-view">
    <h3 id="req-heading" class="sr-only">{{ $t('stageRequirements.heading') }}</h3>
    <div class="tabs" role="tablist" :aria-label="$t('stageRequirements.tabsLabel')">
      <button
        v-for="value in TABS"
        :id="`req-tab-${value}`"
        :key="value"
        type="button"
        role="tab"
        class="tab"
        :class="{ on: tab === value }"
        :aria-selected="tab === value ? 'true' : 'false'"
        :aria-controls="`req-panel-${value}`"
        :tabindex="tab === value ? 0 : -1"
        @click="tab = value"
        @keydown="onTabKey"
      >
        {{ $t(`stageRequirements.tabs.${value}`) }}
        <span class="chip mute">{{ tabCounts[value] }}</span>
      </button>
    </div>

    <div role="status" class="sr-only">{{ message }}</div>
    <div v-if="notice" class="note" role="alert" data-testid="requirements-edit-notice">
      <span class="note-text">
        <span>{{ notice }}</span>
        <router-link
          v-if="trackedEdit?.review_wave"
          :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: trackedEdit.review_wave } }"
          class="mono"
        >
          {{ $t('artifactEdit.reviewWave', { wave: trackedEdit.review_wave }) }}
        </router-link>
      </span>
      <button type="button" class="ibtn" :aria-label="$t('common.close')" @click="dismiss">
        <CanvasIcon name="close" :size="18" />
      </button>
    </div>

    <LoadingState v-if="loader.loading.value && artifacts.length === 0" type="paragraph" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />

    <!-- Requisitos -->
    <div
      v-if="tab === 'items'"
      id="req-panel-items"
      role="tabpanel"
      aria-labelledby="req-tab-items"
      class="req-cols"
    >
      <section class="card req-list" :aria-label="$t('stageRequirements.listLabel')">
        <div class="req-list-head">
          <span class="lb">{{ $t('stageRequirements.show') }}</span>
          <div role="tablist" class="req-kinds" :aria-label="$t('stageRequirements.kindsLabel')">
            <button
              v-for="value in KINDS"
              :id="`req-kind-${value}`"
              :key="value"
              type="button"
              role="tab"
              class="tabf"
              :class="{ on: kind === value }"
              :aria-selected="kind === value ? 'true' : 'false'"
              :tabindex="kind === value ? 0 : -1"
              @click="chooseKind(value)"
              @keydown="onKindKey"
            >
              {{ $t(`stageRequirements.kinds.${value}`) }}
              <span class="cnt">{{ counts[value] }}</span>
            </button>
          </div>
          <button
            v-if="canCreate"
            type="button"
            class="btn btn-p"
            data-testid="requirements-new"
            @click="startCreate"
          >
            {{ $t(`stageRequirements.editor.new.${kind}`) }}
          </button>
        </div>
        <ul v-if="items.length > 0" class="plain-list" data-testid="requirements-items">
          <li v-for="item in items" :key="item.key" class="li req-item">
            <span class="mono t2 req-id">{{ item.label }}</span>
            <span class="req-main">
              <router-link
                v-if="item.kind === 'stories' || item.kind === 'nfrs'"
                :to="{
                  name: ROUTE_NAMES.artifact,
                  params: { projectId, artifactId: item.artifact.id },
                }"
              >
                {{ item.story ? $t('stageRequirements.storyTitle', item.story) : item.title }}
              </router-link>
              <span v-else>{{ item.title }}</span>
              <span class="req-meta">
                <span class="chip" :class="authorChip(item.artifact).tone">{{
                  authorChip(item.artifact).text
                }}</span>
                <span class="t2">{{ $t(item.detail.key, item.detail.values ?? {}) }}</span>
                <span v-if="item.needsAttention" class="chip warn">{{
                  $t('stageRequirements.needsAdjustment')
                }}</span>
                <StateLabel machine="artifact" :state="item.artifact.state" />
              </span>
              <span v-if="item.origin" class="t2 req-origin">
                {{ $t('stageRequirements.origin', { origin: item.origin }) }}
              </span>
            </span>
            <span v-if="confirming === item.key" class="req-confirm">
              <span>{{ $t('stageRequirements.confirmDelete') }}</span>
              <button
                :id="`req-confirm-${cssId(item.key)}`"
                type="button"
                class="ib d"
                :disabled="deleting === item.key"
                :aria-busy="deleting === item.key ? 'true' : undefined"
                @click="confirmDelete(item)"
              >
                {{ $t('stageRequirements.yesDelete') }}
              </button>
              <button type="button" class="ib" @click="cancelDelete(item)">
                {{ $t('stageRequirements.no') }}
              </button>
            </span>
            <span v-else-if="editable(item.artifact)" class="req-actions">
              <button
                type="button"
                class="ib"
                :aria-label="$t('stageRequirements.editItem', { item: item.label })"
                @click="startEdit(item)"
              >
                {{ $t('common.edit') }}
              </button>
              <button
                :id="`req-delete-${cssId(item.key)}`"
                type="button"
                class="ib d"
                :aria-label="$t('stageRequirements.deleteItem', { item: item.label })"
                @click="askDelete(item)"
              >
                {{ $t('stageRequirements.delete') }}
              </button>
            </span>
          </li>
        </ul>
        <p v-else class="t2 req-empty">{{ $t('stageRequirements.empty') }}</p>
        <ProblemAlert v-if="deleteFailure" :error="deleteFailure" />
      </section>

      <section class="card req-editor" aria-labelledby="req-editor-heading">
        <template v-if="!editing">
          <h4 id="req-editor-heading">{{ $t('stageRequirements.editor.title') }}</h4>
          <p class="t2">{{ $t('stageRequirements.editor.help') }}</p>
        </template>
        <template v-else>
          <span id="req-editor-heading" class="sr-only">{{
            $t('stageRequirements.editor.title')
          }}</span>
          <RequirementsEditor
            :key="editing.serial"
            :project-id="projectId"
            :kind="kind"
            :operation="editing.operation"
            :item="editing.item"
            :targets="targets"
            @submitted="onSubmitted"
            @cancel="editing = undefined"
          />
        </template>
      </section>
    </div>

    <!-- Fontes (read-only after the start, CA-07-28) -->
    <section
      v-else-if="tab === 'sources'"
      id="req-panel-sources"
      role="tabpanel"
      aria-labelledby="req-tab-sources"
      class="card req-sources"
    >
      <p class="t2">{{ $t('stageRequirements.sources.help') }}</p>
      <ul class="plain-list" data-testid="requirements-sources">
        <li v-if="store.project?.business_need" class="li src">
          <span class="chip mute">{{ $t('stageRequirements.sources.text') }}</span>
          <span class="src-main">
            <span>{{ $t('stageRequirements.sources.need') }}</span>
            <span class="t2 src-need">{{ store.project.business_need }}</span>
            <span class="t2">{{
              $t('stageRequirements.sources.writtenBy', {
                person: store.project.created_by?.name ?? '',
              })
            }}</span>
          </span>
        </li>
        <li v-for="attachment in attachments" :key="attachment.id" class="li src">
          <span class="chip mute mono">{{ attachment.media_type }}</span>
          <span class="src-main">
            <span class="mono">{{ attachment.file_name }}</span>
            <span class="t2">
              {{
                $t('stageRequirements.sources.attachmentDetail', {
                  size: format.bytes(attachment.size_bytes),
                  person: attachment.submitted_by.name,
                  when: format.dateTime(attachment.submitted_at),
                })
              }}
            </span>
          </span>
          <span class="t2">{{
            $t('stageRequirements.sources.cited', { n: citations(attachment.file_name, artifacts) })
          }}</span>
        </li>
      </ul>
      <p v-if="attachments.length === 0" class="t2">
        {{ $t('stageRequirements.sources.noAttachments') }}
      </p>
      <p class="note">
        <span class="note-text">{{ $t('stageRequirements.sources.readOnly') }}</span>
      </p>
    </section>

    <!-- Descoberta -->
    <div
      v-else
      id="req-panel-discovery"
      role="tabpanel"
      aria-labelledby="req-tab-discovery"
      class="req-discovery"
    >
      <section class="card req-card" aria-labelledby="req-questions-heading">
        <h4 id="req-questions-heading">{{ $t('stageRequirements.discovery.questions') }}</h4>
        <p class="t2">{{ $t('stageRequirements.discovery.questionsHelp') }}</p>
        <LoadingState
          v-if="questionsLoader.loading.value && questions.length === 0"
          type="paragraph"
        />
        <ProblemAlert
          v-else-if="questionsLoader.error.value"
          :error="questionsLoader.error.value"
        />
        <p v-else-if="questions.length === 0" class="t2">
          {{ $t('stageRequirements.discovery.noQuestions') }}
        </p>
        <div v-for="wave in questionWaves" :key="wave" class="req-wave-questions">
          <router-link
            :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: wave } }"
            class="mono"
          >
            {{ $t('stageRequirements.discovery.ofWave', { wave }) }}
          </router-link>
          <WaveQuestions
            :questions="questionsOf(wave)"
            :can-answer="canAnswer"
            :focus-on-open="false"
            :submit="submitFor(wave)"
          />
        </div>
      </section>
      <section class="card req-card" aria-labelledby="req-assumptions-heading">
        <h4 id="req-assumptions-heading">{{ $t('stageRequirements.discovery.assumptions') }}</h4>
        <p class="t2">{{ $t('stageRequirements.discovery.assumptionsHelp') }}</p>
        <ul v-if="supposed.length > 0" class="plain-list" data-testid="requirements-assumptions">
          <li v-for="item in supposed" :key="item.id" class="li">
            <span class="mono t2">{{ item.id }}</span>
            <span class="src-main">
              <span>{{ item.descricao }}</span>
              <span class="t2">{{
                $t('stageRequirements.discovery.because', { reason: item.motivo })
              }}</span>
            </span>
          </li>
        </ul>
        <p v-else class="t2">{{ $t('stageRequirements.discovery.noAssumptions') }}</p>
      </section>
    </div>
  </section>
</template>

<style scoped>
.req {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.req-cols {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  align-items: flex-start;
}

.req-list {
  flex: 3 1 480px;
  min-width: 0;
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.req-editor {
  flex: 2 1 340px;
  min-width: 0;
  padding: 18px 22px;
}

.req-editor h4,
.req-card h4 {
  margin: 0 0 4px;
  font-size: 18px;
  line-height: 24px;
  font-weight: 700;
}

.req-list-head {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  justify-content: space-between;
}

.req-kinds {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.plain-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.req-item {
  display: grid;
  grid-template-columns: minmax(64px, max-content) 1fr auto;
  gap: 12px;
  align-items: start;
}

.req-main,
.src-main {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  overflow-wrap: anywhere;
}

.req-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  font-size: 13px;
}

.req-origin {
  font-size: 12px;
}

.req-actions,
.req-confirm {
  display: flex;
  gap: 6px;
  align-items: center;
  flex-wrap: wrap;
}

.req-empty {
  margin: 0;
}

.req-sources,
.req-card {
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.src {
  display: grid;
  grid-template-columns: minmax(90px, max-content) 1fr auto;
  gap: 12px;
  align-items: start;
}

.src-need {
  white-space: pre-wrap;
  max-height: 160px;
  overflow: auto;
}

.req-discovery {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.req-wave-questions {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

@media (max-width: 640px) {
  .req-item,
  .src {
    grid-template-columns: 1fr;
  }
}
</style>
