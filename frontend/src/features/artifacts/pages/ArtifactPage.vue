<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { isApiError } from '@/shared/api/problemDetails'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import AppBreadcrumbs from '@/shared/ui/AppBreadcrumbs.vue'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import MarkdownContent from '@/shared/ui/MarkdownContent.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import { projectsApi, type Project } from '@/features/projects/api/projectsApi'
import {
  artifactsApi,
  type Artifact,
  type ArtifactAuthor,
  type ArtifactVersionSummary,
} from '../api/artifactsApi'
import ArtifactApprovalCard from '../components/ArtifactApprovalCard.vue'
import ArtifactAttachments from '../components/ArtifactAttachments.vue'
import ArtifactComparisonPanel from '../components/ArtifactComparisonPanel.vue'
import ArtifactTraceCard from '../components/ArtifactTraceCard.vue'
import ArtifactEditDialog from '../components/ArtifactEditDialog.vue'
import { awaitingAgentReview, canEditArtifact, isImposedAdr } from '../composables/artifactEditing'
import type { ArtifactEdit } from '../api/artifactsApi'
import { useArtifactEditNotice } from '../composables/useArtifactEditNotice'
import ArtifactStructuredView from '../components/ArtifactStructuredView.vue'
import QualityArtifactView from '../components/QualityArtifactView.vue'
import {
  QUALITY_TYPES,
  STRUCTURED_FIELDS,
  structuredArtifact,
} from '../composables/structuredArtifact'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const format = useFormat()
const projectId = String(route.params.projectId)
const artifactId = String(route.params.artifactId)

const project = ref<Project>()
const current = ref<Artifact>()
const versions = ref<ArtifactVersionSummary[]>([])
const shown = ref<Artifact>()
const downloadFailure = ref<unknown>()
/** US-53: the edit dialog, the file it starts from, and the notice after sending. */
const editOperation = ref<ArtifactEdit['operation']>('modify')
const editOpen = ref(false)
const editContent = ref('')
const editPreparing = ref(false)
const editFailure = ref<unknown>()

const requestedVersion = computed(() => {
  const value = Number(route.query.version)
  return Number.isInteger(value) && value >= 1 ? value : undefined
})
/** ESP-11 section 2: TELAS opens its prototype on the version shown. */
const prototypeRoute = computed(() =>
  shown.value?.type === 'TELAS'
    ? {
        name: ROUTE_NAMES.prototype,
        params: { projectId },
        query: { version: String(shown.value.version) },
      }
    : undefined,
)

const loader = useLoader(async () => {
  const [loadedProject, loadedArtifact, loadedVersions] = await Promise.all([
    projectsApi.getProject(projectId),
    artifactsApi.getArtifact(projectId, artifactId),
    artifactsApi.listVersions(projectId, artifactId),
  ])
  project.value = loadedProject
  current.value = loadedArtifact
  // ESP-21 3: versions newest first.
  versions.value = [...loadedVersions].sort((left, right) => right.version - left.version)
  await showVersion()
})

const versionLoader = useLoader(async () => {
  await showVersion()
})

/** US-26 CA-2: an older version shows its exact content from the repository, not a rebuild. */
async function showVersion(): Promise<void> {
  const version = requestedVersion.value
  if (!current.value || version === undefined || version === current.value.version) {
    shown.value = current.value
    return
  }
  shown.value = await artifactsApi.getVersion(projectId, artifactId, version)
}

const isOlderVersion = computed(
  () =>
    shown.value !== undefined &&
    current.value !== undefined &&
    shown.value.version !== current.value.version,
)
const shownVersionInfo = computed(() =>
  versions.value.find((item) => item.version === shown.value?.version),
)
/** CA-7: an approved artifact reopened by a scope change decision arrives with its finding. */
const scopeFinding = computed(() =>
  typeof route.query.finding === 'string' && route.query.finding ? route.query.finding : undefined,
)
const editable = computed(
  () =>
    !!current.value &&
    !!project.value &&
    !isOlderVersion.value &&
    canEditArtifact(current.value, project.value, scopeFinding.value),
)
const notFound = computed(() => isApiError(loader.error.value) && loader.error.value.status === 404)
/** US-20, US-21: DIC, PERSIST, TELAS and VALIDACAO show their structured content (ESP-09, ESP-10, ESP-13). */
const structured = computed(() =>
  shown.value ? structuredArtifact(shown.value.type, shown.value.header) : undefined,
)
const headerEntries = computed(() => {
  const skip = new Set(structured.value ? STRUCTURED_FIELDS[structured.value.type] : [])
  return Object.entries(shown.value?.header ?? {}).filter(([field]) => !skip.has(field))
})

const breadcrumbs = computed(() => [
  { title: t('pages.projects.title'), to: { name: ROUTE_NAMES.projects } },
  { title: project.value?.name ?? '', to: { name: ROUTE_NAMES.project, params: { projectId } } },
  {
    title: t('pages.project.artifacts'),
    to: { name: ROUTE_NAMES.projectArtifacts, params: { projectId } },
  },
  { title: artifactId },
])

watch(
  () => route.query.version,
  () => {
    if (current.value) void versionLoader.run()
  },
)
void loader.run()

function chooseVersion(version: number): void {
  const query = { ...route.query }
  if (version === current.value?.version) delete query.version
  else query.version = String(version)
  void router.replace({ query })
}

function authorText(author: ArtifactAuthor | undefined): string {
  if (!author) return ''
  if (author.type === 'human' && awaitingAgentReview(author)) {
    return t('artifactEdit.awaitingReview', { person: author.edited_by?.name ?? '' })
  }
  if (author.type === 'human') {
    return t('artifacts.editedBy', {
      person: author.edited_by?.name ?? '',
      agent: author.reviewed_by ? t(`agents.${author.reviewed_by}`) : '',
    })
  }
  if (author.type === 'orchestrator') return t('agents.orchestrator')
  return author.agent ? t(`agents.${author.agent}`) : ''
}

function headerValue(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
    return String(value)
  }
  return JSON.stringify(value, null, 2)
}

function isScalar(value: unknown): boolean {
  return value === null || ['string', 'number', 'boolean', 'undefined'].includes(typeof value)
}

/** US-53 CA-1: a change starts from the current Markdown file; a removal needs no text. */
async function startEdit(operation: 'modify' | 'delete'): Promise<void> {
  if (!current.value) return
  editFailure.value = undefined
  editOperation.value = operation
  if (operation === 'modify') {
    editPreparing.value = true
    try {
      editContent.value = await artifactsApi.getVersionFile(
        projectId,
        artifactId,
        current.value.version,
      )
    } catch (error) {
      editFailure.value = error
      return
    } finally {
      editPreparing.value = false
    }
  }
  editOpen.value = true
}

const {
  edit: trackedEdit,
  notice: editNotice,
  message,
  track: editSubmitted,
  dismiss: dismissEditNotice,
} = useArtifactEditNotice(() => projectId, loader.run)

async function downloadVersion(): Promise<void> {
  if (!shown.value) return
  downloadFailure.value = undefined
  try {
    const markdown = await artifactsApi.getVersionFile(projectId, artifactId, shown.value.version)
    const url = URL.createObjectURL(new Blob([markdown], { type: 'text/markdown' }))
    const link = document.createElement('a')
    link.href = url
    link.download = `${artifactId}-v${String(shown.value.version)}.md`
    link.click()
    URL.revokeObjectURL(url)
  } catch (error) {
    downloadFailure.value = error
  }
}
</script>

<template>
  <!-- Layout: canvas "Artefato" (Artefato.dc.html). -->
  <LoadingState v-if="loader.loading.value && !current" />
  <template v-else-if="loader.error.value && !current">
    <h1>{{ $t('pages.artifact.title') }}</h1>
    <div v-if="notFound" class="note note-warn" role="alert">
      <CanvasIcon name="alert" />
      <span class="note-text">
        <span>{{ $t('artifacts.notFound') }}</span>
        <router-link :to="{ name: ROUTE_NAMES.projects }">{{
          $t('common.goProjects')
        }}</router-link>
      </span>
    </div>
    <ProblemAlert v-else :error="loader.error.value" retry @retry="loader.run" />
  </template>

  <template v-else-if="shown && current">
    <AppBreadcrumbs :items="breadcrumbs" />
    <div class="art-head">
      <div class="art-title">
        <h1>{{ shown.id }} · {{ shown.title }}</h1>
        <span class="art-meta">
          <StateLabel machine="artifact" :state="current.state" />
          <span class="t2 art-meta-text">
            <span>{{ $t('artifacts.type') }}: {{ shown.type }}</span> ·
            <span>{{ $t('artifacts.author') }}: {{ authorText(shown.author) }}</span> ·
            <span>
              {{ $t('artifacts.wave') }}:
              <!-- US-26 CA-3: the wave that produced it. -->
              <router-link
                :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: shown.wave } }"
                class="mono"
                data-testid="origin-wave"
                >{{ shown.wave }}</router-link
              ></span
            >
            · <span>{{ $t('artifacts.version') }}: {{ shown.version }}</span>
            <template v-if="shown.path">
              ·
              <span :title="$t('artifacts.path')">{{ shown.path }}</span>
            </template>
          </span>
        </span>
      </div>
      <div class="art-actions">
        <div
          v-if="versions.length > 1"
          class="seg2"
          role="group"
          :aria-label="$t('artifacts.version')"
        >
          <button
            v-for="item in versions"
            :key="item.version"
            type="button"
            :class="{ on: item.version === shown.version }"
            :aria-pressed="item.version === shown.version ? 'true' : 'false'"
            @click="chooseVersion(item.version)"
          >
            {{ $t('artifacts.versionShort', { n: item.version }) }}
          </button>
        </div>
        <router-link
          v-if="prototypeRoute"
          class="btn"
          :to="prototypeRoute"
          data-testid="artifact-open-prototype"
        >
          {{ $t('designStage.openPrototype') }}
        </router-link>
        <button type="button" class="btn" @click="downloadVersion">
          {{ $t('artifacts.download') }}
        </button>
        <!-- US-53: edit or remove the item; the stage agent validates before it counts. -->
        <div v-if="editable" class="art-edit" data-testid="artifact-edit-actions">
          <button
            type="button"
            class="btn"
            :disabled="editPreparing"
            :aria-busy="editPreparing ? 'true' : undefined"
            data-testid="edit-artifact"
            @click="startEdit('modify')"
          >
            {{ $t('artifactEdit.modify') }}
          </button>
          <button
            type="button"
            class="btn btn-d"
            data-testid="delete-artifact"
            @click="startEdit('delete')"
          >
            {{ $t('artifactEdit.delete') }}
          </button>
        </div>
      </div>
    </div>

    <div v-if="isOlderVersion" class="note" role="alert" data-testid="older-version">
      <span class="note-text">
        <span>
          {{
            $t('artifacts.olderVersion', {
              n: shown.version,
              total: current.version,
            })
          }}
          <template v-if="shownVersionInfo?.approved === false">
            · {{ $t('artifacts.notApproved') }}</template
          >
          <template v-else-if="shownVersionInfo?.approved">
            · {{ $t('artifacts.approvedVersion') }}</template
          >
        </span>
      </span>
      <button type="button" class="btn note-action" @click="chooseVersion(current.version)">
        {{ $t('artifacts.goToCurrent') }}
      </button>
    </div>
    <p v-if="current.state === 'draft'" class="t2 art-line">
      {{ $t('artifacts.draftNotice') }}
    </p>
    <!-- ESP-08 3: an imposed ADR says who imposed it. -->
    <p
      v-if="isImposedAdr(current.header) && current.author.type === 'human'"
      class="t2 art-line"
      data-testid="imposed-adr"
    >
      {{ $t('artifactEdit.imposedBy', { person: current.author.edited_by?.name ?? '' }) }}
    </p>
    <div role="status" class="sr-only">{{ message }}</div>
    <div v-if="editNotice" class="note" role="alert" data-testid="artifact-edit-notice">
      <span class="note-text">
        <span>{{ editNotice }}</span>
        <!-- RN-1: the agent's review wave; when it refuses, its reason stays in the wave (CA-2). -->
        <router-link
          v-if="trackedEdit?.review_wave"
          :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: trackedEdit.review_wave } }"
          class="mono"
          data-testid="edit-review-wave"
        >
          {{ $t('artifactEdit.reviewWave', { wave: trackedEdit.review_wave }) }}
        </router-link>
      </span>
      <button
        type="button"
        class="ibtn note-close"
        :aria-label="$t('common.close')"
        @click="dismissEditNotice"
      >
        <CanvasIcon name="close" :size="18" />
      </button>
    </div>
    <ProblemAlert v-if="editFailure" :error="editFailure" />
    <ProblemAlert v-if="downloadFailure" :error="downloadFailure" />
    <ArtifactEditDialog
      v-if="editable"
      v-model="editOpen"
      :project-id="projectId"
      :operation="editOperation"
      :artifact-id="current.id"
      :base-version="current.version"
      :initial-content="editContent"
      :finding="scopeFinding"
      @submitted="editSubmitted"
    />

    <div class="art-cols">
      <div class="art-main">
        <article class="card art-body" aria-labelledby="artifact-body-heading">
          <h2 id="artifact-body-heading" class="sr-only">{{ $t('artifacts.body') }}</h2>
          <LoadingState v-if="versionLoader.loading.value" type="paragraph" />
          <MarkdownContent v-else :source="shown.body" data-testid="artifact-body" />
          <p class="t2 art-sanitized">{{ $t('artifacts.sanitizedNotice') }}</p>
          <ProblemAlert v-if="versionLoader.error.value" :error="versionLoader.error.value" />
        </article>

        <!-- US-22, US-23: CT, REL-QA, AMEACAS and REL-SEG (ESP-15, ESP-16). -->
        <QualityArtifactView
          v-if="structured && QUALITY_TYPES.includes(structured.type)"
          :project-id="projectId"
          :artifact="structured"
        />
        <ArtifactStructuredView
          v-else-if="structured"
          :project-id="projectId"
          :artifact="structured"
        />

        <section class="card art-card" aria-labelledby="artifact-header-heading">
          <h2 id="artifact-header-heading">{{ $t('artifacts.header') }}</h2>
          <dl class="art-fields">
            <div v-for="[field, value] in headerEntries" :key="field" class="art-field">
              <dt class="mono">{{ field }}</dt>
              <dd>
                <span v-if="isScalar(value)">{{ headerValue(value) }}</span>
                <pre v-else class="mono header-value">{{ headerValue(value) }}</pre>
              </dd>
            </div>
          </dl>
        </section>

        <!-- PL-24, CA-21-16: the other files of the version. -->
        <ArtifactAttachments
          :project-id="projectId"
          :artifact-id="artifactId"
          :version="shown.version"
          :attachments="shown.attachments ?? []"
        />

        <!-- US-27: any two versions side by side (ESP-21 4, Phase 2). -->
        <ArtifactComparisonPanel
          :project-id="projectId"
          :artifact-id="artifactId"
          :versions="versions"
        />
      </div>

      <aside class="art-side">
        <section class="card side-card" aria-labelledby="versions-heading">
          <h2 id="versions-heading">{{ $t('artifacts.versions') }}</h2>
          <p v-if="versions.length <= 1" class="t2 side-text">
            {{ $t('artifacts.singleVersion') }}
          </p>
          <ol class="version-list" data-testid="version-list">
            <li v-for="item in versions" :key="item.version" class="version">
              <span class="version-top">
                <button
                  type="button"
                  class="version-btn"
                  :class="{ on: item.version === shown.version }"
                  :aria-current="item.version === shown.version ? 'true' : undefined"
                  @click="chooseVersion(item.version)"
                >
                  {{
                    item.version === current.version
                      ? $t('artifacts.versionCurrent', { n: item.version })
                      : $t('artifacts.versionNumber', { n: item.version })
                  }}
                </button>
                <span class="t2">{{ format.date(item.data) }}</span>
              </span>
              <span class="version-line">
                <router-link
                  :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: item.wave } }"
                  class="mono"
                >
                  {{ item.wave }}
                </router-link>
                · <span class="t2">{{ item.description }}</span>
              </span>
              <!-- ER-21-7: the finding that drove a rework version, with a link. -->
              <span v-if="item.finding" class="version-line t2">
                {{ $t('artifacts.drivenBy') }}
                <router-link
                  :to="{
                    name: ROUTE_NAMES.finding,
                    params: { projectId, findingId: item.finding },
                  }"
                  class="mono"
                  :data-testid="`version-finding-${item.version}`"
                >
                  {{ item.finding }}
                </router-link>
              </span>
            </li>
          </ol>
        </section>

        <!-- ESP-21 3: the chain from requirement to test and "Depende deste" (US-28). -->
        <ArtifactTraceCard
          :project-id="projectId"
          :artifact-id="artifactId"
          :version="current.version"
        />

        <!-- ESP-21 3 / CA-21-4: the gate and the round of the approval; CA-6: approved changes only by a finding. -->
        <ArtifactApprovalCard v-if="!isOlderVersion" :project-id="projectId" :artifact="current" />
        <p
          v-if="!editable && current.state === 'approved' && !isOlderVersion"
          class="t2 side-note"
          data-testid="artifact-approved-notice"
        >
          {{ $t('artifactEdit.approvedNotice') }}
        </p>
      </aside>
    </div>
  </template>
</template>

<style scoped>
.art-head {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: flex-end;
  gap: 16px;
}

.art-title {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.art-meta {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}

.art-meta-text {
  font-size: 13px;
  overflow-wrap: anywhere;
}

.art-actions,
.art-edit {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}

/* Canvas "Artefato": version segmented control. */
.seg2 {
  display: inline-flex;
  max-width: 100%;
  overflow-x: auto;
  border: 1px solid rgba(140, 150, 255, 0.28);
  border-radius: 10px;
}

.seg2 button {
  min-height: 42px;
  padding: 0 14px;
  border: 0;
  border-right: 1px solid rgba(140, 150, 255, 0.2);
  background: transparent;
  font: inherit;
  color: #c7cceb;
  cursor: pointer;
}

.seg2 button:last-child {
  border-right: 0;
}

.seg2 button.on {
  background: #4f46e5;
  color: #ffffff;
  font-weight: 600;
}

.note {
  align-items: center;
}

.note-warn {
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.1);
}

.note-warn > svg {
  flex: none;
  color: #fcd34d;
}

.note-text {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.note-action {
  flex: none;
}

.note-close {
  flex: none;
  margin: -8px -8px -8px 0;
}

.art-line {
  margin: 0;
  font-size: 13px;
}

.art-cols {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  align-items: flex-start;
}

.art-main {
  flex: 3 1 520px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.art-body {
  padding: 26px 30px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  line-height: 22px;
}

.art-sanitized {
  margin: 4px 0 0;
  font-size: 12px;
}

.art-card {
  padding: 18px 22px;
}

.art-card h2 {
  margin-bottom: 4px;
}

.art-fields {
  margin: 0;
}

.art-field {
  display: grid;
  grid-template-columns: minmax(140px, max-content) 1fr;
  gap: 12px;
  padding: 10px 0;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.art-field dt {
  color: #a9b0d6;
}

.art-field dd {
  margin: 0;
  min-width: 0;
  overflow-wrap: anywhere;
}

.header-value {
  margin: 0;
  white-space: pre-wrap;
}

.art-side {
  flex: 2 1 300px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.side-card {
  padding: 18px 22px;
}

.side-card h2 {
  margin-bottom: 4px;
}

.side-text {
  margin: 0;
  font-size: 13px;
}

.side-note {
  margin: -12px 0 0;
  font-size: 13px;
}

.version-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.version {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 6px 0 10px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.version-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.version-btn {
  min-height: 44px;
  margin-left: -8px;
  padding: 0 8px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: #f1f3ff;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.version-btn:hover {
  background: rgba(255, 255, 255, 0.07);
  color: #ffffff;
}

.version-btn.on {
  background: rgba(99, 102, 241, 0.16);
  font-weight: 600;
}

.version-line {
  font-size: 13px;
  overflow-wrap: anywhere;
}
</style>
