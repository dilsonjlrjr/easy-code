<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter, type RouteLocationRaw } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { artifactsApi, type ArtifactSummary } from '@/features/artifacts/api/artifactsApi'
import { useCurrentProjectStore } from '@/features/projects/stores/currentProjectStore'
import { hasProblemSlug, isApiError } from '@/shared/api/problemDetails'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import {
  DIRECTIONS,
  PRIORITIES,
  traceabilityApi,
  type CoverageMatrix,
  type Direction,
  type Priority,
  type Traceability,
} from '../api/traceabilityApi'
import { coverageRows, groupNodes, qaNotExecuted } from '../composables/traceabilityView'

/**
 * ESP-24: the traceability of one artifact (US → T, ADR, screens, CT, commits, ACH; US-28 CA-1) and
 * the coverage matrix US × CT (CA-2). Both read only; the view, the root and the parameters stay in
 * the address so the link can be shared and "back" works.
 */
const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const router = useRouter()
const store = useCurrentProjectStore()
const { message, announce } = useAnnouncer()

const artifacts = ref<ArtifactSummary[]>([])
const graph = ref<Traceability>()
const matrix = ref<CoverageMatrix>()

const projectId = computed(() => store.project?.id ?? '')
const view = computed(() => (route.params.view === 'coverage' ? 'coverage' : 'links'))
const root = computed(() => {
  const value = typeof route.query.root === 'string' ? route.query.root : ''
  return value || artifacts.value.find((item) => item.type === 'US')?.id || ''
})
const depth = computed(() => {
  const value = Number(route.query.depth)
  return Number.isInteger(value) && value >= 1 && value <= 5 ? value : 3
})
const direction = computed<Direction>(() =>
  DIRECTIONS.includes(route.query.direction as Direction)
    ? (route.query.direction as Direction)
    : 'both',
)
const priorities = computed<Priority[]>(() => {
  const raw = route.query.priority
  const values = (Array.isArray(raw) ? raw : raw ? [raw] : []).filter((item): item is Priority =>
    PRIORITIES.includes(item as Priority),
  )
  return values.length > 0 ? values : ['must']
})

const rootArtifact = computed(() => artifacts.value.find((item) => item.id === root.value))
const groups = computed(() => (graph.value ? groupNodes(graph.value) : []))
const rows = computed(() => (matrix.value ? coverageRows(matrix.value) : []))
const counts = computed(() => ({
  total: rows.value.length,
  covered: rows.value.filter((row) => row.status === 'covered').length,
  gaps: rows.value.filter((row) => row.status === 'gap').length,
}))
const notExecuted = computed(() => (matrix.value ? qaNotExecuted(matrix.value) : false))

const artifactsLoader = useLoader(async () => {
  artifacts.value = await artifactsApi.listArtifacts(projectId.value)
})

const viewLoader = useLoader(async () => {
  if (view.value === 'coverage') {
    matrix.value = await traceabilityApi.coverage(projectId.value, priorities.value)
    announce(t('traceability.coverage.summary', counts.value))
  } else if (root.value) {
    graph.value = await traceabilityApi.links(
      projectId.value,
      root.value,
      depth.value,
      direction.value,
    )
  } else {
    graph.value = undefined
  }
})

async function run(): Promise<void> {
  await viewLoader.run()
  // ESP-24 6: an invalid parameter (400) brings the defaults back.
  if (hasProblemSlug(viewLoader.error.value, 'validation')) {
    await router.replace({ name: ROUTE_NAMES.traceability, params: route.params })
  }
}

watch(
  projectId,
  async (id) => {
    if (!id) return
    await artifactsLoader.run()
    await run()
  },
  { immediate: true },
)
watch(
  () => route.fullPath,
  () => {
    if (projectId.value && route.name === ROUTE_NAMES.traceability) void run()
  },
)

function go(next: { view?: string; query?: Record<string, unknown> }): void {
  void router.push({
    name: ROUTE_NAMES.traceability,
    params: { projectId: projectId.value, view: next.view ?? view.value },
    query: next.query ?? route.query,
  } as RouteLocationRaw)
}

function setQuery(change: Record<string, unknown>): void {
  go({ query: { ...route.query, ...change } })
}

function nodeRoute(node: {
  id: string
  type: string
  exists: boolean
}): RouteLocationRaw | undefined {
  if (!node.exists || node.type === 'commit' || node.type === 'test') return undefined
  if (node.type === 'ACH')
    return { name: ROUTE_NAMES.finding, params: { projectId: projectId.value, findingId: node.id } }
  return { name: ROUTE_NAMES.artifact, params: { projectId: projectId.value, artifactId: node.id } }
}

function artifactState(state: string | undefined): string {
  if (!state) return ''
  const key = `states.artifact.${state}`
  return i18n.te(key) ? t(key) : state
}

function rootLabel(item: ArtifactSummary): string {
  return `${item.id} · ${item.title}`
}

const rootNotFound = computed(
  () => isApiError(viewLoader.error.value) && viewLoader.error.value.status === 404,
)
</script>

<template>
  <section aria-labelledby="traceability-heading">
    <h2 id="traceability-heading" class="text-h6 mb-2">{{ $t('pages.project.traceability') }}</h2>
    <div role="status" class="sr-only">{{ message }}</div>
    <nav :aria-label="$t('traceability.views')" class="d-flex ga-2 mb-4">
      <v-btn
        :variant="view === 'links' ? 'flat' : 'outlined'"
        :color="view === 'links' ? 'primary' : undefined"
        :aria-current="view === 'links' ? 'page' : undefined"
        @click="go({ view: 'links' })"
      >
        {{ $t('traceability.links.title') }}
      </v-btn>
      <v-btn
        :variant="view === 'coverage' ? 'flat' : 'outlined'"
        :color="view === 'coverage' ? 'primary' : undefined"
        :aria-current="view === 'coverage' ? 'page' : undefined"
        @click="go({ view: 'coverage' })"
      >
        {{ $t('traceability.coverage.title') }}
      </v-btn>
    </nav>

    <ProblemAlert
      v-if="artifactsLoader.error.value"
      :error="artifactsLoader.error.value"
      retry
      @retry="artifactsLoader.run"
    />

    <!-- ESP-24 3.1: links of one artifact. -->
    <template v-if="view === 'links'">
      <div class="params mb-4">
        <v-autocomplete
          :model-value="root || null"
          :items="artifacts"
          :item-title="rootLabel"
          item-value="id"
          :label="$t('traceability.links.root')"
          density="compact"
          hide-details
          class="root-field"
          :disabled="viewLoader.loading.value"
          @update:model-value="(value) => value && setQuery({ root: value })"
        />
        <v-select
          :model-value="depth"
          :items="[1, 2, 3, 4, 5]"
          :label="$t('traceability.links.depth')"
          density="compact"
          hide-details
          class="small-field"
          :disabled="viewLoader.loading.value"
          @update:model-value="(value) => setQuery({ depth: value })"
        />
        <v-select
          :model-value="direction"
          :items="
            DIRECTIONS.map((value) => ({ value, title: $t(`traceability.directions.${value}`) }))
          "
          :label="$t('traceability.links.direction')"
          density="compact"
          hide-details
          class="small-field"
          :disabled="viewLoader.loading.value"
          @update:model-value="(value) => setQuery({ direction: value })"
        />
      </div>
      <LoadingState v-if="viewLoader.loading.value" />
      <v-alert v-else-if="rootNotFound" type="warning" variant="tonal" role="alert">
        {{ $t('traceability.links.rootNotFound') }}
      </v-alert>
      <ProblemAlert
        v-else-if="viewLoader.error.value"
        :error="viewLoader.error.value"
        retry
        @retry="run"
      />
      <p v-else-if="!root" class="text-body-2">{{ $t('traceability.links.noRoot') }}</p>
      <template v-else-if="graph">
        <p class="text-body-2 mb-4" data-testid="trace-root">
          <span class="monospace">{{ graph.root }}</span>
          <template v-if="rootArtifact">
            · {{ rootArtifact.title }} · {{ artifactState(rootArtifact.state) }}
          </template>
        </p>
        <p v-if="groups.length === 0" class="text-body-2">{{ $t('traceability.links.empty') }}</p>
        <section
          v-for="group in groups"
          :key="group.group"
          :aria-labelledby="`trace-group-${group.group}`"
          class="mb-4"
          :data-testid="`trace-group-${group.group}`"
        >
          <h3 :id="`trace-group-${group.group}`" class="text-subtitle-1 font-weight-medium">
            {{
              $t('traceability.links.group', {
                group: $t(`traceability.groups.${group.group}`),
                n: group.nodes.length,
              })
            }}
          </h3>
          <ul class="plain-list">
            <li
              v-for="node in group.nodes"
              :key="node.id"
              class="text-body-2 mb-1"
              :data-testid="`trace-node-${node.id}`"
            >
              <router-link v-if="nodeRoute(node)" :to="nodeRoute(node)!" class="monospace">
                {{ node.id }}
              </router-link>
              <span v-else class="monospace">{{ node.id }}</span>
              <template v-if="!node.exists">
                · <strong>{{ $t('traceability.links.broken') }}</strong></template
              >
              <template v-if="node.title"> · {{ node.title }}</template>
              <template v-if="node.state"> · {{ artifactState(node.state) }}</template>
              <template v-if="node.rules.length">
                · {{ $t('traceability.links.rules', { rules: node.rules.join(', ') }) }}
              </template>
            </li>
          </ul>
        </section>
      </template>
    </template>

    <!-- ESP-24 3.2: coverage matrix. -->
    <template v-else>
      <fieldset class="plain-fieldset mb-4">
        <legend class="text-body-2 font-weight-medium mb-1">
          {{ $t('traceability.coverage.priority') }}
        </legend>
        <div class="d-flex flex-wrap ga-2">
          <v-checkbox
            v-for="priority in PRIORITIES"
            :key="priority"
            :model-value="priorities"
            :value="priority"
            :label="$t(`traceability.priorities.${priority}`)"
            density="compact"
            hide-details
            :disabled="viewLoader.loading.value"
            @update:model-value="
              (value) => setQuery({ priority: value?.length ? value : ['must'] })
            "
          />
        </div>
      </fieldset>
      <LoadingState v-if="viewLoader.loading.value" />
      <ProblemAlert
        v-else-if="viewLoader.error.value"
        :error="viewLoader.error.value"
        retry
        @retry="run"
      />
      <template v-else-if="matrix">
        <p class="text-body-2 mb-2" data-testid="coverage-summary">
          {{ $t('traceability.coverage.summary', counts) }}
        </p>
        <v-alert v-if="notExecuted" type="info" variant="tonal" class="mb-4">
          {{ $t('traceability.coverage.notExecuted') }}
        </v-alert>
        <p v-if="rows.length === 0" class="text-body-2">{{ $t('traceability.coverage.empty') }}</p>
        <div v-else class="table-scroll">
          <v-table density="comfortable" data-testid="coverage-table">
            <caption class="sr-only">
              {{
                $t('traceability.coverage.title')
              }}
            </caption>
            <thead>
              <tr>
                <th scope="col">{{ $t('traceability.coverage.us') }}</th>
                <th scope="col">{{ $t('traceability.coverage.priority') }}</th>
                <th scope="col">{{ $t('traceability.coverage.status') }}</th>
                <th scope="col">{{ $t('traceability.coverage.criteria') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in rows" :key="row.us" :data-testid="`coverage-${row.us}`">
                <th scope="row" class="font-weight-regular">
                  <router-link
                    :to="{
                      name: ROUTE_NAMES.traceability,
                      params: { projectId, view: 'links' },
                      query: { root: row.us },
                    }"
                    class="monospace"
                    :aria-label="$t('traceability.coverage.openLinks', { us: row.us })"
                  >
                    {{ row.us }}
                  </router-link>
                </th>
                <td>{{ $t(`traceability.priorities.${row.priority}`) }}</td>
                <td :class="row.status === 'gap' ? 'text-error font-weight-medium' : ''">
                  {{ $t(`traceability.coverage.statuses.${row.status}`) }}
                </td>
                <td>
                  <ul class="plain-list">
                    <li v-for="criterion in row.criteria" :key="criterion.ca" class="text-body-2">
                      <span class="monospace">{{ criterion.ca }}</span>:
                      <template v-if="criterion.cts.length === 0">
                        {{ $t('traceability.coverage.noScenario') }}
                      </template>
                      <template v-for="(ct, index) in criterion.cts" :key="ct.ct">
                        <router-link
                          :to="{
                            name: ROUTE_NAMES.artifact,
                            params: { projectId, artifactId: ct.ct },
                          }"
                          class="monospace"
                          >{{ ct.ct }}</router-link
                        >
                        {{ $t(`traceability.results.${ct.result}`)
                        }}<span v-if="index < criterion.cts.length - 1">, </span>
                      </template>
                    </li>
                  </ul>
                </td>
              </tr>
            </tbody>
          </v-table>
        </div>
      </template>
    </template>
  </section>
</template>

<style scoped>
.params {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
}

.root-field {
  min-width: 260px;
  max-width: 420px;
}

.small-field {
  min-width: 160px;
  max-width: 200px;
}

.plain-fieldset {
  border: 0;
  margin: 0;
  padding: 0;
}
</style>
