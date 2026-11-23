<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
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
  <section aria-labelledby="artifacts-heading">
    <div class="d-flex flex-wrap align-center ga-2 mb-4">
      <h2 id="artifacts-heading" class="text-h6">{{ $t('pages.project.artifacts') }}</h2>
      <v-spacer />
      <v-btn
        v-if="canCreate"
        color="primary"
        variant="tonal"
        data-testid="create-artifact"
        @click="createOpen = true"
      >
        {{ $t('artifactEdit.create') }}
      </v-btn>
    </div>
    <div role="status" class="sr-only">{{ message }}</div>
    <v-alert
      v-if="notice"
      type="info"
      variant="tonal"
      class="mb-4"
      closable
      data-testid="artifact-edit-notice"
      @click:close="dismiss"
    >
      {{ notice }}
    </v-alert>
    <LoadingState v-if="loader.loading.value" type="table-row@3" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <p v-else-if="artifacts.length === 0">{{ $t('artifacts.empty') }}</p>
    <div v-else class="table-scroll">
      <v-table density="comfortable">
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
            <td>
              <router-link
                :to="{ name: ROUTE_NAMES.artifact, params: { projectId, artifactId: artifact.id } }"
                class="monospace"
              >
                {{ artifact.id }}
              </router-link>
            </td>
            <td>{{ artifact.title }}</td>
            <td><StateLabel machine="artifact" :state="artifact.state" /></td>
            <td>{{ artifact.version }}</td>
            <td class="monospace">{{ artifact.wave }}</td>
            <td>{{ format.dateTime(artifact.updated_at) }}</td>
          </tr>
        </tbody>
      </v-table>
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
