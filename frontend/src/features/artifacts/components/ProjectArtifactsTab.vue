<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import { useCurrentProjectStore } from '@/features/projects/stores/currentProjectStore'
import { artifactsApi, type ArtifactSummary } from '../api/artifactsApi'
import ArtifactEditDialog from '../components/ArtifactEditDialog.vue'
import { useArtifactEditNotice } from '../composables/useArtifactEditNotice'

const store = useCurrentProjectStore()
const format = useFormat()
const artifacts = ref<ArtifactSummary[]>([])
const projectId = computed(() => store.project?.id ?? '')
const createOpen = ref(false)
/** US-53 CA-1, CA-5: any project role creates an item or an imposed ADR while the project runs. */
const canCreate = computed(
  () =>
    !!store.project &&
    !['completed', 'canceled'].includes(store.project.state) &&
    store.project.my_roles.length > 0,
)

const loader = useLoader(async () => {
  artifacts.value = await artifactsApi.listArtifacts(projectId.value)
})
const {
  notice,
  message,
  track: submitted,
  dismiss,
} = useArtifactEditNotice(() => projectId.value, loader.run)

onMounted(() => {
  void loader.run()
})
</script>

<template>
  <!-- Layout: canvas primitives of Projeto.dc.html ("Ondas" card and table); no own artboard. -->
  <section class="card tab-card" aria-labelledby="artifacts-heading">
    <div class="card-head">
      <h2 id="artifacts-heading">{{ $t('pages.project.artifacts') }}</h2>
      <button
        v-if="canCreate"
        type="button"
        class="btn btn-p"
        data-testid="create-artifact"
        @click="createOpen = true"
      >
        <CanvasIcon name="plus" :size="16" :stroke-width="2.2" />
        {{ $t('artifactEdit.create') }}
      </button>
    </div>
    <div role="status" class="sr-only">{{ message }}</div>
    <div v-if="notice" class="note" data-testid="artifact-edit-notice">
      <span class="note-text">{{ notice }}</span>
      <button type="button" class="ib note-close" @click="dismiss">
        {{ $t('common.close') }}
      </button>
    </div>
    <LoadingState v-if="loader.loading.value" type="table-row@3" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <p v-else-if="artifacts.length === 0" class="t2 card-empty">{{ $t('artifacts.empty') }}</p>
    <div v-else class="table-scroll">
      <table class="tbl">
        <thead>
          <tr>
            <th scope="col">{{ $t('artifacts.id') }}</th>
            <th scope="col">{{ $t('artifacts.titleColumn') }}</th>
            <th scope="col">{{ $t('artifacts.state') }}</th>
            <th scope="col">{{ $t('artifacts.version') }}</th>
            <th scope="col">{{ $t('artifacts.wave') }}</th>
            <th scope="col">{{ $t('artifacts.updatedAt') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="artifact in artifacts" :key="artifact.id">
            <td class="mono">
              <router-link
                :to="{ name: ROUTE_NAMES.artifact, params: { projectId, artifactId: artifact.id } }"
              >
                {{ artifact.id }}
              </router-link>
            </td>
            <td>{{ artifact.title }}</td>
            <td><StateLabel machine="artifact" :state="artifact.state" /></td>
            <td>{{ artifact.version }}</td>
            <td class="mono">{{ artifact.wave }}</td>
            <td>{{ format.dateTime(artifact.updated_at) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <ArtifactEditDialog
      v-if="canCreate"
      v-model="createOpen"
      :project-id="projectId"
      operation="create"
      @submitted="submitted"
    />
  </section>
</template>

<style scoped>
.tab-card {
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.card-head {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.note-text {
  flex: 1;
}

.note-close {
  flex: none;
  min-height: 44px;
  margin: -12px -8px -12px 0;
  border-color: transparent;
}

.card-empty {
  margin: 0;
}
</style>
