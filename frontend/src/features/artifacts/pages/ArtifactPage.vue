<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { isApiError } from '@/shared/api/problemDetails'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import AppBreadcrumbs from '@/shared/ui/AppBreadcrumbs.vue'
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
import ArtifactComparisonPanel from '../components/ArtifactComparisonPanel.vue'
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
  <v-container max-width="1200">
    <LoadingState v-if="loader.loading.value && !current" />
    <template v-else-if="loader.error.value && !current">
      <h1 class="text-h4 mb-4">{{ $t('pages.artifact.title') }}</h1>
      <v-alert v-if="notFound" type="warning" variant="tonal" role="alert">
        <p>{{ $t('artifacts.notFound') }}</p>
        <router-link :to="{ name: ROUTE_NAMES.projects }">{{
          $t('common.goProjects')
        }}</router-link>
      </v-alert>
      <ProblemAlert v-else :error="loader.error.value" retry @retry="loader.run" />
    </template>

    <template v-else-if="shown && current">
      <AppBreadcrumbs :items="breadcrumbs" />
      <div class="d-flex flex-wrap align-center ga-3 mb-2">
        <h1 class="text-h4">
          <span class="monospace">{{ shown.id }}</span> · {{ shown.title }}
        </h1>
        <StateLabel machine="artifact" :state="current.state" />
      </div>

      <v-alert
        v-if="isOlderVersion"
        type="info"
        variant="tonal"
        class="mb-4"
        data-testid="older-version"
      >
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
        <div class="mt-2">
          <v-btn variant="outlined" size="small" @click="chooseVersion(current.version)">
            {{ $t('artifacts.goToCurrent') }}
          </v-btn>
        </div>
      </v-alert>
      <p v-if="current.state === 'draft'" class="text-body-2 mb-2">
        {{ $t('artifacts.draftNotice') }}
      </p>
      <!-- ESP-08 3: an imposed ADR says who imposed it. -->
      <p
        v-if="isImposedAdr(current.header) && current.author.type === 'human'"
        class="text-body-2 mb-2"
        data-testid="imposed-adr"
      >
        {{ $t('artifactEdit.imposedBy', { person: current.author.edited_by?.name ?? '' }) }}
      </p>
      <div role="status" class="sr-only">{{ message }}</div>
      <v-alert
        v-if="editNotice"
        type="info"
        variant="tonal"
        class="mb-4"
        closable
        data-testid="artifact-edit-notice"
        @click:close="dismissEditNotice"
      >
        {{ editNotice }}
        <!-- RN-1: the agent's review wave; when it refuses, its reason stays in the wave (CA-2). -->
        <router-link
          v-if="trackedEdit?.review_wave"
          :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: trackedEdit.review_wave } }"
          class="d-block monospace"
          data-testid="edit-review-wave"
        >
          {{ $t('artifactEdit.reviewWave', { wave: trackedEdit.review_wave }) }}
        </router-link>
      </v-alert>
      <!-- US-53: edit or remove the item; the stage agent validates before it counts. -->
      <div v-if="editable" class="d-flex flex-wrap ga-2 mb-4" data-testid="artifact-edit-actions">
        <v-btn
          color="primary"
          variant="tonal"
          :loading="editPreparing"
          data-testid="edit-artifact"
          @click="startEdit('modify')"
        >
          {{ $t('artifactEdit.modify') }}
        </v-btn>
        <v-btn
          color="error"
          variant="outlined"
          data-testid="delete-artifact"
          @click="startEdit('delete')"
        >
          {{ $t('artifactEdit.delete') }}
        </v-btn>
      </div>
      <!-- CA-6: an approved artifact only changes through a finding or a scope change. -->
      <p
        v-else-if="current.state === 'approved' && !isOlderVersion"
        class="text-body-2 mb-4"
        data-testid="artifact-approved-notice"
      >
        {{ $t('artifactEdit.approvedNotice') }}
      </p>
      <ProblemAlert v-if="editFailure" :error="editFailure" class="mb-4" />
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

      <div class="artifact-layout">
        <div>
          <section aria-labelledby="artifact-meta-heading" class="mb-6">
            <h2 id="artifact-meta-heading" class="text-h6 mb-2">{{ $t('artifacts.metadata') }}</h2>
            <dl class="summary-list">
              <dt>{{ $t('artifacts.type') }}</dt>
              <dd>{{ shown.type }}</dd>
              <dt>{{ $t('artifacts.version') }}</dt>
              <dd>{{ shown.version }}</dd>
              <dt>{{ $t('artifacts.author') }}</dt>
              <dd>{{ authorText(shown.author) }}</dd>
              <dt>{{ $t('artifacts.wave') }}</dt>
              <dd>
                <!-- US-26 CA-3: the wave that produced it. -->
                <router-link
                  :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: shown.wave } }"
                  class="monospace"
                  data-testid="origin-wave"
                >
                  {{ shown.wave }}
                </router-link>
              </dd>
              <template v-if="shown.path">
                <dt>{{ $t('artifacts.path') }}</dt>
                <dd class="monospace">{{ shown.path }}</dd>
              </template>
            </dl>
          </section>

          <section aria-labelledby="artifact-header-heading" class="mb-6">
            <h2 id="artifact-header-heading" class="text-h6 mb-2">{{ $t('artifacts.header') }}</h2>
            <dl class="summary-list">
              <template v-for="[field, value] in headerEntries" :key="field">
                <dt class="monospace">{{ field }}</dt>
                <dd>
                  <span v-if="isScalar(value)">{{ headerValue(value) }}</span>
                  <pre v-else class="header-value">{{ headerValue(value) }}</pre>
                </dd>
              </template>
            </dl>
          </section>

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
          <!-- ESP-24 2: the full traceability with this artifact as root (US-28). -->
          <router-link
            :to="{
              name: ROUTE_NAMES.traceability,
              params: { projectId, view: 'links' },
              query: { root: artifactId },
            }"
            class="d-inline-block text-body-2 mb-6"
            data-testid="artifact-traceability"
          >
            {{ $t('artifacts.traceabilityLink') }}
          </router-link>

          <section aria-labelledby="artifact-body-heading" class="mb-6">
            <h2 id="artifact-body-heading" class="text-h6 mb-1">{{ $t('artifacts.body') }}</h2>
            <p class="text-body-2 text-medium-emphasis mb-3">
              {{ $t('artifacts.sanitizedNotice') }}
            </p>
            <LoadingState v-if="versionLoader.loading.value" type="paragraph" />
            <MarkdownContent v-else :source="shown.body" data-testid="artifact-body" />
            <v-btn variant="outlined" size="small" class="mt-4" @click="downloadVersion">
              {{ $t('artifacts.download') }}
            </v-btn>
            <ProblemAlert v-if="downloadFailure" :error="downloadFailure" class="mt-2" />
            <ProblemAlert
              v-if="versionLoader.error.value"
              :error="versionLoader.error.value"
              class="mt-2"
            />
          </section>

          <!-- US-27: any two versions side by side (ESP-21 4, Phase 2). -->
          <ArtifactComparisonPanel
            :project-id="projectId"
            :artifact-id="artifactId"
            :versions="versions"
          />
        </div>

        <aside aria-labelledby="versions-heading">
          <h2 id="versions-heading" class="text-h6 mb-2">{{ $t('artifacts.versions') }}</h2>
          <p v-if="versions.length <= 1" class="text-body-2 mb-2">
            {{ $t('artifacts.singleVersion') }}
          </p>
          <ol class="version-list" data-testid="version-list">
            <li v-for="item in versions" :key="item.version" class="mb-3">
              <v-btn
                variant="text"
                size="small"
                class="px-1"
                :aria-current="item.version === shown.version ? 'true' : undefined"
                :class="{ 'font-weight-bold': item.version === shown.version }"
                @click="chooseVersion(item.version)"
              >
                {{
                  item.version === current.version
                    ? $t('artifacts.versionCurrent', { n: item.version })
                    : $t('artifacts.versionNumber', { n: item.version })
                }}
              </v-btn>
              <p class="text-body-2">
                {{ format.date(item.data) }} ·
                <router-link
                  :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: item.wave } }"
                  class="monospace"
                >
                  {{ item.wave }}
                </router-link>
              </p>
              <p class="text-body-2">{{ item.description }}</p>
              <!-- ER-21-7: the finding that drove a rework version, with a link. -->
              <p v-if="item.finding" class="text-body-2">
                {{ $t('artifacts.drivenBy') }}
                <router-link
                  :to="{
                    name: ROUTE_NAMES.finding,
                    params: { projectId, findingId: item.finding },
                  }"
                  class="monospace"
                  :data-testid="`version-finding-${item.version}`"
                >
                  {{ item.finding }}
                </router-link>
              </p>
            </li>
          </ol>
        </aside>
      </div>
    </template>
  </v-container>
</template>

<style scoped>
.artifact-layout {
  display: grid;
  gap: 2rem;
  grid-template-columns: minmax(0, 1fr);
}

@media (min-width: 960px) {
  .artifact-layout {
    grid-template-columns: minmax(0, 3fr) minmax(220px, 1fr);
  }
}

.version-list {
  list-style: none;
  padding: 0;
}

.header-value {
  white-space: pre-wrap;
  font-family: monospace;
  font-size: 0.85rem;
}
</style>
