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
  <!-- Look: canvas "Agentes e skills" (Agentes.dc.html) card, with the canvas `.kv` rows and `.tbl`. -->
  <section class="card panel" aria-labelledby="history-heading" data-testid="agent-history">
    <h2 id="history-heading" class="panel-title">{{ $t('agentsPage.history.title') }}</h2>
    <LoadingState v-if="loader.loading.value && !loader.loaded.value" type="list-item@2" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else>
      <h3 class="lb sub-label">{{ $t('agentsPage.history.versions') }}</h3>
      <ol class="version-list" data-testid="agent-versions">
        <li v-for="item in versions" :key="item.version" class="kv">
          <span>
            <strong>{{ $t('agentsPage.configurationVersion', { n: item.version }) }}</strong>
            · {{ $t('agentsPage.instructionsVersion', { v: item.instructions_version }) }}
          </span>
          <span class="t2 kv-value">
            {{ format.dateTime(item.created_at) }}
            <template v-if="item.created_by"> · {{ item.created_by.name }}</template>
          </span>
        </li>
      </ol>

      <h3 class="lb sub-label">{{ $t('agentsPage.history.waves') }}</h3>
      <p v-if="waves.length === 0" class="empty">{{ $t('agentsPage.history.noWaves') }}</p>
      <div v-else class="table-scroll">
        <table class="tbl" data-testid="agent-waves">
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
                  class="mono"
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
        </table>
      </div>
      <button
        v-if="nextCursor"
        type="button"
        class="btn more"
        :disabled="loadingMore"
        :aria-busy="loadingMore ? 'true' : undefined"
        @click="loadMore"
      >
        {{ $t('agentsPage.history.loadMore') }}
      </button>
      <ProblemAlert v-if="moreFailure" :error="moreFailure" class="more" />
    </template>
  </section>
</template>

<style scoped>
.panel {
  padding: 18px 22px;
}

.panel-title {
  margin-bottom: 4px;
}

.sub-label {
  display: block;
  margin: 14px 0 6px;
}

.version-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.kv-value {
  text-align: right;
}

.empty {
  margin: 0;
}

.more {
  margin-top: 12px;
}
</style>
