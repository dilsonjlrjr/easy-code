<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import type { WaveSummary } from '@/features/projects/api/projectsApi'
import { agentsApi, type AgentConfiguration } from '../api/agentsApi'

/**
 * ESP-22 3.5 / US-25 CA-4: the configuration versions of the agent (newest first, with date and
 * author) and its waves, each with the configuration version it used (RNF-15), by cursor pages.
 */
const props = defineProps<{ projectId: string; agent: string; revision: number }>()

const i18n = useI18n()
const { t } = i18n
const format = useFormat()
const versions = ref<AgentConfiguration[]>([])
const waves = ref<WaveSummary[]>([])
const nextCursor = ref<string | null>(null)
const loadingMore = ref(false)
const moreFailure = ref<unknown>()

const loader = useLoader(async () => {
  const [loadedVersions, page] = await Promise.all([
    agentsApi.listConfigurationVersions(props.projectId, props.agent),
    agentsApi.listAgentWaves(props.projectId, props.agent),
  ])
  versions.value = [...loadedVersions].sort((left, right) => right.version - left.version)
  waves.value = page.items
  nextCursor.value = page.next_cursor
})

watch(
  () => [props.agent, props.revision],
  () => void loader.run(),
  { immediate: true },
)

async function loadMore(): Promise<void> {
  if (!nextCursor.value) return
  loadingMore.value = true
  moreFailure.value = undefined
  try {
    const page = await agentsApi.listAgentWaves(props.projectId, props.agent, nextCursor.value)
    waves.value = [...waves.value, ...page.items]
    nextCursor.value = page.next_cursor
  } catch (error) {
    moreFailure.value = error
  } finally {
    loadingMore.value = false
  }
}

function stageName(id: string): string {
  const key = `stages.${id}`
  return i18n.te(key) ? t(key) : id
}
</script>

<template>
  <section aria-labelledby="history-heading" class="mb-6" data-testid="agent-history">
    <h3 id="history-heading" class="text-h6 mb-2">{{ $t('agentsPage.history.title') }}</h3>
    <LoadingState v-if="loader.loading.value && !loader.loaded.value" type="list-item@2" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else>
      <h4 class="text-subtitle-1 font-weight-medium mb-1">
        {{ $t('agentsPage.history.versions') }}
      </h4>
      <ol class="plain-list mb-4" data-testid="agent-versions">
        <li v-for="item in versions" :key="item.version" class="text-body-2 mb-1">
          <span class="font-weight-medium">
            {{ $t('agentsPage.configurationVersion', { n: item.version }) }}
          </span>
          · {{ $t('agentsPage.instructionsVersion', { v: item.instructions_version }) }} ·
          {{ format.dateTime(item.created_at) }}
          <template v-if="item.created_by"> · {{ item.created_by.name }}</template>
        </li>
      </ol>

      <h4 class="text-subtitle-1 font-weight-medium mb-1">{{ $t('agentsPage.history.waves') }}</h4>
      <p v-if="waves.length === 0" class="text-body-2">{{ $t('agentsPage.history.noWaves') }}</p>
      <div v-else class="table-scroll">
        <v-table density="compact" data-testid="agent-waves">
          <thead>
            <tr>
              <th scope="col">{{ $t('agentsPage.history.wave') }}</th>
              <th scope="col">{{ $t('agentsPage.stages') }}</th>
              <th scope="col">{{ $t('agentsPage.history.state') }}</th>
              <th scope="col">{{ $t('agentsPage.history.configuration') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="wave in waves" :key="wave.id">
              <td>
                <router-link
                  :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: wave.id } }"
                  class="monospace"
                >
                  {{ wave.id }}
                </router-link>
              </td>
              <td>{{ stageName(wave.stage) }}</td>
              <td><StateLabel machine="wave" :state="wave.state" /></td>
              <td>
                <template v-if="wave.agent_configuration_version">
                  {{
                    $t('agentsPage.configurationVersion', { n: wave.agent_configuration_version })
                  }}
                </template>
                <template v-else>{{ $t('agentsPage.history.unknownVersion') }}</template>
              </td>
            </tr>
          </tbody>
        </v-table>
      </div>
      <v-btn
        v-if="nextCursor"
        variant="outlined"
        size="small"
        class="mt-2"
        :loading="loadingMore"
        @click="loadMore"
      >
        {{ $t('agentsPage.history.loadMore') }}
      </v-btn>
      <ProblemAlert v-if="moreFailure" :error="moreFailure" class="mt-2" />
    </template>
  </section>
</template>
