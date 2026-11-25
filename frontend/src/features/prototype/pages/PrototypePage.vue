<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import {
  artifactsApi,
  type Artifact,
  type ArtifactVersionSummary,
} from '@/features/artifacts/api/artifactsApi'
import { projectsApi, type Project } from '@/features/projects/api/projectsApi'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import { isApiError } from '@/shared/api/problemDetails'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import type { CloudEvent, EventClass } from '@/shared/events/eventStream'
import { useProjectEventStream } from '@/shared/events/useProjectEventStream'
import { useSessionStore } from '@/shared/session/sessionStore'
import AppBreadcrumbs from '@/shared/ui/AppBreadcrumbs.vue'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { prototypeApi, type AnnotatedRoundSummary, type ContentUrl } from '../api/prototypeApi'
import AnnotationsCard from '../components/AnnotationsCard.vue'
import PrototypeFrame, { type FramePin } from '../components/PrototypeFrame.vue'
import { useAnnotationDrafts } from '../composables/annotationDrafts'
import {
  displayPath,
  pagePath,
  screenMap,
  servedStories,
  withState,
  type ViewerWidth,
} from '../composables/prototypeModel'

/**
 * ESP-11: the prototype of the Designer, screen by screen, from the content origin only (ADR-19). The page
 * asks the api for a signed URL of the screen page and shows it in `iframe sandbox="allow-scripts"`; it never
 * reads the prototype. With an open gate.design round the approver may annotate the screens (US-57) and send
 * the annotations with the rejection of the gate (PD-25 option a).
 */
const TELAS = 'TELAS'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()
const live = useProjectEventStream()
const { message, announce } = useAnnouncer()

const project = ref<Project>()
const artifact = ref<Artifact>()
const versions = ref<ArtifactVersionSummary[]>([])
const rounds = ref<AnnotatedRoundSummary[]>([])
const content = ref<ContentUrl>()
const contentMissing = ref(false)
const contentFailure = ref<unknown>()
const loadingContent = ref(false)
const newVersion = ref(false)
const width = ref<ViewerWidth>('wide')
const annotating = ref(false)
const pending = ref<{ x: number; y: number } | null | undefined>(undefined)
let contentRequest = 0

const projectId = computed(() => String(route.params.projectId))
const requestedVersion = computed(() => {
  const value = Number(route.query.version)
  return Number.isInteger(value) && value > 0 ? value : undefined
})
const latestVersion = computed(() => versions.value[0]?.version)
const map = computed(() => screenMap(artifact.value?.header))
const screen = computed(
  () => map.value.screens.find((item) => item.id === route.query.screen) ?? map.value.screens[0],
)
const state = computed(() => {
  const value = typeof route.query.state === 'string' ? route.query.state : undefined
  return value && screen.value?.states.includes(value) ? value : undefined
})
const page = computed(() =>
  screen.value ? pagePath(map.value.prototype, screen.value.file) : undefined,
)
const frameUrl = computed(() =>
  content.value ? withState(content.value.url, state.value) : undefined,
)
const notFound = computed(() => isApiError(loader.error.value) && loader.error.value.status === 404)
const stories = computed(() => (screen.value ? servedStories(screen.value) : []))

const breadcrumbs = computed(() => [
  { title: t('pages.projects.title'), to: { name: ROUTE_NAMES.projects } },
  {
    title: project.value?.name ?? '',
    to: { name: ROUTE_NAMES.project, params: { projectId: projectId.value } },
  },
  {
    title: t('stages.design'),
    to: { name: ROUTE_NAMES.stage, params: { projectId: projectId.value, stageId: 'design' } },
  },
  { title: t('prototype.breadcrumb') },
])

// ------------------------------------------------------------------ loading

const loader = useLoader(async () => {
  project.value = await projectsApi.getProject(projectId.value)
  await loadArtifact()
  live.open({
    projectId: projectId.value,
    since: project.value.last_sequence,
    query: { class: 'domain' },
    onEvent: handleEvent,
    reload: async () => {
      await loadRounds()
      return undefined
    },
  })
})

async function loadArtifact(): Promise<void> {
  versions.value = await artifactsApi.listVersions(projectId.value, TELAS)
  artifact.value =
    requestedVersion.value === undefined
      ? await artifactsApi.getArtifact(projectId.value, TELAS)
      : await artifactsApi.getVersion(projectId.value, TELAS, requestedVersion.value)
  newVersion.value = false
  await loadRounds()
}

async function loadRounds(): Promise<void> {
  try {
    rounds.value = await prototypeApi.listDesignRounds(projectId.value)
  } catch {
    rounds.value = []
  }
}

/** CA-11-7 / ER-11-9: a new signed URL on every screen or version change and on "recarregar". */
async function loadContent(): Promise<void> {
  const request = ++contentRequest
  content.value = undefined
  contentMissing.value = false
  contentFailure.value = undefined
  if (!artifact.value || !page.value) {
    contentMissing.value = !!artifact.value && !!screen.value
    return
  }
  loadingContent.value = true
  try {
    const issued = await prototypeApi.contentUrl(
      projectId.value,
      TELAS,
      artifact.value.version,
      page.value,
    )
    if (request === contentRequest) content.value = issued
  } catch (error) {
    if (request !== contentRequest) return
    // CA-11-5: the screen has no file in this version.
    if (isApiError(error) && error.status === 404) contentMissing.value = true
    else contentFailure.value = error
  } finally {
    if (request === contentRequest) loadingContent.value = false
  }
}

function expired(): boolean {
  return !content.value || Date.parse(content.value.expires_at) <= Date.now()
}

watch([projectId, requestedVersion], () => void loader.run(), { immediate: true })
watch([() => artifact.value?.version, () => screen.value?.id], () => void loadContent())
watch(state, () => {
  if (content.value && expired()) void loadContent()
})
watch(
  () => screen.value?.id,
  (id, previous) => {
    pending.value = undefined
    if (id && previous && screen.value) {
      // ESP-11 section 8: the screen change is announced in the status region.
      announce(t('prototype.screenShown', { id, name: screen.value.name }))
    }
  },
)

/** CA-11-9: a new TELAS version shows a notice; the area stays on the version shown. */
function handleEvent(event: CloudEvent, eventClass: EventClass): void {
  if (eventClass !== 'domain') return
  const type = event.type.replace(/^br\.easycode\./, '')
  if (type.startsWith('gate.')) void loadRounds()
  if (!type.startsWith('artifact.') || event.data.artifact !== TELAS) return
  const version = Number(event.data.version)
  if (Number.isInteger(version) && version !== artifact.value?.version) newVersion.value = true
}

onBeforeUnmount(() => {
  live.close()
})

// ------------------------------------------------------------------ navigation

function setQuery(change: Record<string, string | undefined>): void {
  void router.replace({ query: { ...route.query, ...change } })
}

function pickScreen(id: string): void {
  setQuery({ screen: id, state: undefined })
}

function pickState(value: string | undefined): void {
  setQuery({ state: value })
}

function pickVersion(event: Event): void {
  const value = (event.target as HTMLSelectElement).value
  setQuery({ version: value === String(latestVersion.value) ? undefined : value })
}

function loadLatest(): void {
  if (requestedVersion.value !== undefined) setQuery({ version: undefined })
  else void loader.run()
}

function stateLabel(value: string): string {
  return value.replace(/_/g, ' ')
}

// ------------------------------------------------------------------ annotations (US-57)

const drafts = useAnnotationDrafts(
  () => projectId.value,
  () => artifact.value?.version,
)
const openRound = computed(() => rounds.value.find((item) => item.state === 'awaiting_human'))
const isApprover = computed(() => project.value?.my_roles.includes('approver') ?? false)
/** RN-3: only who decides gate.design annotates, on the version the open round decides (the latest). */
const canAnnotate = computed(
  () =>
    isApprover.value &&
    !!openRound.value &&
    artifact.value?.version === latestVersion.value &&
    artifact.value?.state === 'in_review',
)
/** CA-3: the annotations of the decided rounds about the version shown, read only. */
const recorded = computed(() =>
  rounds.value
    .map((item) => ({
      round: item.round,
      items: (item.decision?.annotations ?? []).filter(
        (note) => note.version === artifact.value?.version,
      ),
    }))
    .filter((group) => group.items.length > 0),
)
const pins = computed<FramePin[]>(() => {
  const id = screen.value?.id
  const out: FramePin[] = []
  drafts.list().forEach((draft, index) => {
    if (draft.screen === id && draft.x !== null && draft.y !== null)
      out.push({
        key: `d${String(draft.id)}`,
        number: index + 1,
        x: draft.x,
        y: draft.y,
        recorded: false,
      })
  })
  for (const group of recorded.value)
    for (const note of group.items)
      if (note.screen === id && note.x !== null && note.y !== null)
        out.push({
          key: `r${String(group.round)}-${String(note.number)}`,
          number: note.number,
          x: note.x,
          y: note.y,
          recorded: true,
        })
  if (pending.value)
    out.push({
      key: 'pending',
      number: drafts.list().length + 1,
      ...pending.value,
      recorded: false,
    })
  return out
})

function screenName(id: string): string {
  return map.value.screens.find((item) => item.id === id)?.name ?? ''
}

function toggleAnnotating(): void {
  annotating.value = !annotating.value
  pending.value = undefined
}

function save(comment: string): void {
  if (!screen.value || pending.value === undefined) return
  const added = drafts.add({
    screen: screen.value.id,
    state: state.value ?? null,
    width: width.value,
    x: pending.value?.x ?? null,
    y: pending.value?.y ?? null,
    comment,
  })
  pending.value = undefined
  if (added) announce(t('prototype.annotations.added', { n: drafts.list().length }))
}

// The rejection with the annotations ("Enviar ao Designer").
const sendDialog = ref(false)
const reason = ref('')
const reasonError = ref(false)
const reasonField = ref<HTMLTextAreaElement>()
const sending = ref(false)
const sendFailure = ref<unknown>()
const sentRound = ref<number>()
let sendKey = newIdempotencyKey()

function openSend(): void {
  reason.value = ''
  reasonError.value = false
  sendFailure.value = undefined
  sendDialog.value = true
  void nextTick(() => setTimeout(() => reasonField.value?.focus(), 50))
}

async function send(): Promise<void> {
  const current = openRound.value
  if (!current) return
  if (reason.value.trim() === '') {
    reasonError.value = true
    reasonField.value?.focus()
    return
  }
  sending.value = true
  sendFailure.value = undefined
  try {
    await prototypeApi.rejectDesign(
      projectId.value,
      current.round,
      { reason: reason.value.trim(), annotations: drafts.payload() },
      { idempotencyKey: sendKey, csrfToken: session.csrfToken },
    )
    drafts.clear()
    annotating.value = false
    sendDialog.value = false
    sentRound.value = current.round
    announce(t('prototype.annotations.sent', { round: current.round }))
    await loadRounds()
  } catch (error) {
    sendFailure.value = error
  } finally {
    sending.value = false
    sendKey = newIdempotencyKey()
  }
}
</script>

<template>
  <LoadingState v-if="loader.loading.value && !artifact" />
  <template v-else-if="loader.error.value && !artifact">
    <div v-if="notFound" class="note note-warn" role="alert">
      <CanvasIcon name="alert" />
      <span>{{ $t('problems.not-found') }}</span>
    </div>
    <ProblemAlert v-else :error="loader.error.value" retry @retry="loader.run" />
  </template>
  <template v-else-if="project && artifact">
    <AppBreadcrumbs :items="breadcrumbs" />
    <div role="status" class="sr-only">{{ message }}</div>

    <!-- Layout and values: canvas Prototipo artboard. -->
    <div class="page-head">
      <div class="head-title">
        <h1>{{ $t('prototype.title') }}</h1>
        <span class="head-row">
          <span class="chip mono mute" data-testid="prototype-version">
            {{
              $t('prototype.header', {
                state: $t(`states.artifact.${artifact.state}`),
                version: artifact.version,
                wave: artifact.wave,
              })
            }}
          </span>
          <span class="t2 head-text mono">{{ map.prototype }}</span>
        </span>
      </div>
      <div class="head-actions">
        <router-link
          class="btn"
          :to="{ name: ROUTE_NAMES.stage, params: { projectId, stageId: 'design' } }"
        >
          {{ $t('prototype.backToDesign') }}
        </router-link>
        <a
          v-if="content"
          class="btn"
          :href="frameUrl"
          target="_blank"
          rel="noopener noreferrer"
          data-testid="prototype-new-tab"
        >
          {{ $t('prototype.openInNewTab') }}
        </a>
      </div>
    </div>

    <div class="note">
      <CanvasIcon name="alert" />
      <span>
        <strong>{{ $t('prototype.isolation.title') }}</strong>
        {{ $t('prototype.isolation.text') }}
      </span>
    </div>
    <p v-if="artifact.state === 'draft'" class="note note-warn">
      <CanvasIcon name="alert" />
      <span>{{ $t('prototype.mayChange') }}</span>
    </p>
    <p v-if="artifact.state === 'replaced'" class="note note-warn">
      <CanvasIcon name="alert" />
      <span>
        {{ $t('prototype.replaced') }}
        <button type="button" class="link-button" @click="loadLatest">
          {{ $t('prototype.openCurrent') }}
        </button>
      </span>
    </p>
    <div v-if="newVersion" class="note" role="status" data-testid="prototype-new-version">
      <CanvasIcon name="bolt" />
      <span class="note-row">
        {{ $t('prototype.newVersion') }}
        <button type="button" class="ib" @click="loadLatest">{{ $t('prototype.loadNew') }}</button>
      </span>
    </div>
    <div v-if="sentRound" class="note note-ok" role="status" data-testid="annotations-sent">
      <CanvasIcon name="check" />
      <span>
        {{ $t('prototype.annotations.sent', { round: sentRound }) }}
        <router-link
          :to="{
            name: ROUTE_NAMES.gate,
            params: { projectId, gateId: 'gate.design', round: sentRound },
          }"
        >
          {{ $t('prototype.annotations.openGate') }}
        </router-link>
      </span>
    </div>

    <p v-if="map.screens.length === 0" class="note" data-testid="prototype-empty">
      <CanvasIcon name="document" />
      <span>
        {{ $t('prototype.empty') }}
        <router-link :to="{ name: ROUTE_NAMES.stage, params: { projectId, stageId: 'design' } }">
          {{ $t('prototype.backToDesign') }}
        </router-link>
      </span>
    </p>

    <div v-else class="viewer">
      <section class="card screens" aria-labelledby="prototype-screens-heading">
        <h2 id="prototype-screens-heading" class="lb screens-title">
          {{ $t('prototype.screens') }}
        </h2>
        <label v-if="versions.length > 1" for="prototype-version" class="version-field">
          <span class="lb">{{ $t('prototype.version') }}</span>
          <select
            id="prototype-version"
            class="fld"
            :value="String(artifact.version)"
            data-testid="prototype-version-select"
            @change="pickVersion"
          >
            <option v-for="item in versions" :key="item.version" :value="String(item.version)">
              {{
                $t('prototype.versionOption', {
                  n: item.version,
                  date: item.data,
                  wave: item.wave,
                })
              }}
            </option>
          </select>
        </label>
        <ul class="plain-list screen-list">
          <li v-for="item in map.screens" :key="item.id">
            <button
              type="button"
              class="tlb"
              :class="{ on: item.id === screen?.id }"
              :aria-pressed="item.id === screen?.id"
              :data-testid="`prototype-screen-${item.id}`"
              @click="pickScreen(item.id)"
            >
              <span class="mono t2 tlb-id">{{ item.id }}</span>
              <span class="tlb-text">
                <span class="tlb-name">{{ item.name }}</span>
                <span class="t2 tlb-sub">
                  {{
                    pagePath(map.prototype, item.file)
                      ? displayPath(map.prototype, item.file)
                      : $t('prototype.noFile')
                  }}
                </span>
              </span>
            </button>
          </li>
        </ul>
      </section>

      <div v-if="screen" class="stage-area">
        <div class="controls">
          <span
            v-if="screen.states.length > 0"
            class="control-group"
            role="group"
            :aria-label="$t('prototype.state')"
          >
            <span class="lb control-label" aria-hidden="true">{{ $t('prototype.state') }}</span>
            <button
              type="button"
              class="ib"
              :class="{ on: !state }"
              :aria-pressed="!state"
              @click="pickState(undefined)"
            >
              {{ $t('prototype.defaultState') }}
            </button>
            <button
              v-for="item in screen.states"
              :key="item"
              type="button"
              class="ib"
              :class="{ on: state === item }"
              :aria-pressed="state === item"
              :data-testid="`prototype-state-${item}`"
              @click="pickState(item)"
            >
              {{ stateLabel(item) }}
            </button>
          </span>
          <span class="control-group" role="group" :aria-label="$t('prototype.width')">
            <span class="lb control-label" aria-hidden="true">{{ $t('prototype.width') }}</span>
            <button
              v-for="item in ['wide', 'narrow'] as const"
              :key="item"
              type="button"
              class="ib"
              :class="{ on: width === item }"
              :aria-pressed="width === item"
              :data-testid="`prototype-width-${item}`"
              @click="width = item"
            >
              {{ $t(`prototype.widths.${item}`) }}
            </button>
          </span>
          <button
            v-if="canAnnotate"
            type="button"
            class="ib"
            :class="{ on: annotating }"
            :aria-pressed="annotating"
            data-testid="prototype-annotate"
            @click="toggleAnnotating"
          >
            <CanvasIcon name="pin" :size="16" />
            {{ $t('prototype.annotations.toggle') }}
          </button>
          <button
            type="button"
            class="ib"
            :disabled="loadingContent"
            data-testid="prototype-reload"
            @click="loadContent"
          >
            {{ $t('prototype.reload') }}
          </button>
        </div>

        <LoadingState v-if="loadingContent" />
        <p v-else-if="contentMissing" class="note note-warn" data-testid="prototype-missing">
          <CanvasIcon name="alert" />
          <span>{{ $t('prototype.missing') }}</span>
        </p>
        <div v-else-if="contentFailure" class="note note-crit" role="alert">
          <CanvasIcon name="alert" />
          <span class="note-row">
            {{ $t('prototype.loadFailed') }}
            <button type="button" class="ib" @click="loadContent">
              {{ $t('prototype.reload') }}
            </button>
          </span>
        </div>
        <PrototypeFrame
          v-else-if="frameUrl"
          :url="frameUrl"
          :title="$t('prototype.frameTitle', { id: screen.id, name: screen.name })"
          :path="displayPath(map.prototype, screen.file)"
          :label="`${screen.id} · ${state ? stateLabel(state) : $t('prototype.defaultState')}`"
          :width="width"
          :annotating="annotating && canAnnotate"
          :pins="pins"
          @mark="pending = $event"
        />
        <span class="t2 hint">
          {{ annotating ? $t('prototype.annotations.hint') : $t('prototype.linksHint') }}
        </span>
      </div>

      <aside v-if="screen" class="side">
        <section class="card side-card" aria-labelledby="prototype-serves-heading">
          <h2 id="prototype-serves-heading" class="side-title">{{ $t('prototype.serves') }}</h2>
          <p v-if="stories.length === 0" class="t2 fine">{{ $t('prototype.servesNone') }}</p>
          <div v-for="story in stories" :key="story.us" class="kv" data-testid="prototype-serves">
            <router-link
              class="mono"
              :to="{ name: ROUTE_NAMES.artifact, params: { projectId, artifactId: story.us } }"
            >
              {{ story.us }}
            </router-link>
            <span class="t2 kv-value">{{ story.criteria.join(', ') }}</span>
          </div>
        </section>
        <AnnotationsCard
          :can-annotate="canAnnotate"
          :annotating="annotating"
          :pending="pending"
          :drafts="drafts.list()"
          :recorded="recorded"
          :screen-name="screenName"
          @save="save"
          @cancel="pending = undefined"
          @without-point="pending = null"
          @update="drafts.update"
          @remove="drafts.remove"
          @send="openSend"
        />
      </aside>
    </div>
  </template>

  <v-dialog
    v-model="sendDialog"
    max-width="460"
    :persistent="sending"
    aria-labelledby="prototype-send-title"
  >
    <v-card class="dlg-card">
      <h2 id="prototype-send-title">{{ $t('prototype.annotations.sendTitle') }}</h2>
      <p class="t2 fine">
        {{ $t('prototype.annotations.sendText', { n: drafts.payload().length }) }}
      </p>
      <form class="dlg-form" @submit.prevent="send">
        <label for="prototype-send-reason" class="field">
          <span class="lb">{{ $t('gates.reasonRequired') }}</span>
          <textarea
            id="prototype-send-reason"
            ref="reasonField"
            v-model="reason"
            class="fld note-text"
            rows="4"
            maxlength="10000"
            aria-required="true"
            :aria-invalid="reasonError || undefined"
            :aria-describedby="reasonError ? 'prototype-send-reason-error' : undefined"
            data-testid="annotation-reason"
            @input="reasonError = false"
          ></textarea>
        </label>
        <span v-if="reasonError" id="prototype-send-reason-error" class="field-error" role="alert">
          {{ $t('gates.reasonMissing') }}
        </span>
        <ProblemAlert v-if="sendFailure" :error="sendFailure" />
        <div class="dlg-actions">
          <button type="button" class="btn" :disabled="sending" @click="sendDialog = false">
            {{ $t('common.cancel') }}
          </button>
          <button
            type="submit"
            class="btn btn-p"
            :disabled="sending"
            :aria-busy="sending || undefined"
            data-testid="annotation-confirm"
          >
            {{ $t('prototype.annotations.send') }}
          </button>
        </div>
      </form>
    </v-card>
  </v-dialog>
</template>

<style scoped>
/* Values copied from the canvas Prototipo artboard. */
.page-head {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: flex-end;
  gap: 16px;
}

.head-title {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.head-row,
.head-actions,
.note-row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}

.head-text {
  font-size: 13px;
}

.note {
  margin: 0;
}

.note-warn {
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.1);
}

.note-crit {
  border-color: rgba(239, 68, 68, 0.45);
  background: rgba(239, 68, 68, 0.1);
}

.note-ok {
  border-color: rgba(34, 197, 94, 0.45);
  background: rgba(34, 197, 94, 0.12);
}

.link-button {
  background: none;
  border: 0;
  padding: 0;
  font: inherit;
  color: #a78bfa;
  text-decoration: underline;
  cursor: pointer;
  min-height: 24px;
}

.viewer {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  align-items: flex-start;
}

.screens {
  flex: 1 1 230px;
  min-width: 0;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.screens-title {
  margin: 0;
  padding: 6px 8px;
}

.version-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 0 8px 8px;
}

.screen-list {
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.tlb {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  min-height: 48px;
  padding: 8px 12px;
  border: 1px solid transparent;
  border-radius: 10px;
  background: transparent;
  color: #f1f3ff;
  font: inherit;
  text-align: left;
  cursor: pointer;
  box-sizing: border-box;
}

.tlb:hover {
  background: rgba(255, 255, 255, 0.05);
}

.tlb.on {
  background: rgba(99, 102, 241, 0.16);
  border-color: rgba(129, 140, 248, 0.45);
}

.tlb-id {
  font-size: 12px;
  width: 38px;
  flex: none;
}

.tlb-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.tlb-name {
  font-weight: 600;
}

.tlb-sub {
  font-size: 12px;
  overflow-wrap: anywhere;
}

.stage-area {
  flex: 999 1 560px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.controls {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
  align-items: center;
}

.control-group {
  display: flex;
  gap: 6px;
  align-items: center;
  flex-wrap: wrap;
}

.control-label {
  margin-right: 4px;
}

.controls .ib {
  min-height: 44px;
}

.hint,
.fine {
  margin: 0;
  font-size: 12px;
}

.side {
  flex: 1 1 280px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.side-card {
  padding: 16px 18px;
}

.side-title {
  margin: 0 0 4px;
  font-size: 16px;
  line-height: 24px;
  font-weight: 700;
}

.kv-value {
  text-align: right;
}

.dlg-card {
  padding: 20px 22px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.dlg-card h2 {
  margin: 0;
  font-size: 18px;
}

.dlg-form,
.field {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.note-text {
  min-height: 88px;
  padding: 8px 12px;
  resize: vertical;
}

.field-error {
  color: #fca5a5;
  font-size: 13px;
}

.dlg-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}
</style>
