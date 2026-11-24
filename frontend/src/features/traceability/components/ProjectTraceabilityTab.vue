<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
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
const uid = `traceability-${useId()}`

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

/** CA-24-4: at least one priority stays chosen; none falls back to Must. */
function togglePriority(priority: Priority, checked: boolean): void {
  const others = priorities.value.filter((item) => item !== priority)
  const next = checked ? [...others, priority] : others
  setQuery({ priority: next.length ? next : ['must'] })
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
  <!-- Layout: canvas primitives of Projeto.dc.html (.tabf switch, .fld fields, .tbl table); no own artboard. -->
  <section class="card tab-card" aria-labelledby="traceability-heading">
    <h2 id="traceability-heading">{{ $t('pages.project.traceability') }}</h2>
    <div role="status" class="sr-only">{{ message }}</div>
    <nav :aria-label="$t('traceability.views')" class="views">
      <button
        type="button"
        class="tabf"
        :class="{ on: view === 'links' }"
        :aria-current="view === 'links' ? 'page' : undefined"
        @click="go({ view: 'links' })"
      >
        {{ $t('traceability.links.title') }}
      </button>
      <button
        type="button"
        class="tabf"
        :class="{ on: view === 'coverage' }"
        :aria-current="view === 'coverage' ? 'page' : undefined"
        @click="go({ view: 'coverage' })"
      >
        {{ $t('traceability.coverage.title') }}
      </button>
    </nav>

    <ProblemAlert
      v-if="artifactsLoader.error.value"
      :error="artifactsLoader.error.value"
      retry
      @retry="artifactsLoader.run"
    />

    <!-- ESP-24 3.1: links of one artifact. -->
    <template v-if="view === 'links'">
      <div class="params">
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
        <label :for="`${uid}-depth`" class="field small-field">
          <span class="field-label">{{ $t('traceability.links.depth') }}</span>
          <select
            :id="`${uid}-depth`"
            class="fld"
            :value="depth"
            :disabled="viewLoader.loading.value"
            @change="setQuery({ depth: Number(($event.target as HTMLSelectElement).value) })"
          >
            <option v-for="value in [1, 2, 3, 4, 5]" :key="value" :value="value">
              {{ value }}
            </option>
          </select>
        </label>
        <label :for="`${uid}-direction`" class="field small-field">
          <span class="field-label">{{ $t('traceability.links.direction') }}</span>
          <select
            :id="`${uid}-direction`"
            class="fld"
            :value="direction"
            :disabled="viewLoader.loading.value"
            @change="setQuery({ direction: ($event.target as HTMLSelectElement).value })"
          >
            <option v-for="value in DIRECTIONS" :key="value" :value="value">
              {{ $t(`traceability.directions.${value}`) }}
            </option>
          </select>
        </label>
      </div>
      <LoadingState v-if="viewLoader.loading.value" />
      <p v-else-if="rootNotFound" class="note note-warn" role="alert">
        {{ $t('traceability.links.rootNotFound') }}
      </p>
      <ProblemAlert
        v-else-if="viewLoader.error.value"
        :error="viewLoader.error.value"
        retry
        @retry="run"
      />
      <p v-else-if="!root" class="t2 plain-text">{{ $t('traceability.links.noRoot') }}</p>
      <template v-else-if="graph">
        <p class="trace-root" data-testid="trace-root">
          <span class="chip mute mono">{{ graph.root }}</span>
          <template v-if="rootArtifact">
            <span class="trace-root-title">{{ rootArtifact.title }}</span>
            <span class="t2">· {{ artifactState(rootArtifact.state) }}</span>
          </template>
        </p>
        <p v-if="groups.length === 0" class="t2 plain-text">
          {{ $t('traceability.links.empty') }}
        </p>
        <div v-else class="groups">
          <section
            v-for="group in groups"
            :key="group.group"
            :aria-labelledby="`trace-group-${group.group}`"
            class="group"
            :data-testid="`trace-group-${group.group}`"
          >
            <h3 :id="`trace-group-${group.group}`" class="lb">
              {{
                $t('traceability.links.group', {
                  group: $t(`traceability.groups.${group.group}`),
                  n: group.nodes.length,
                })
              }}
            </h3>
            <ul class="plain-list node-list">
              <li
                v-for="node in group.nodes"
                :key="node.id"
                class="node"
                :data-testid="`trace-node-${node.id}`"
              >
                <router-link v-if="nodeRoute(node)" :to="nodeRoute(node)!" class="mono">
                  {{ node.id }}
                </router-link>
                <span v-else class="mono">{{ node.id }}</span>
                <template v-if="!node.exists">
                  · <strong class="broken">{{ $t('traceability.links.broken') }}</strong></template
                >
                <template v-if="node.title"> · {{ node.title }}</template>
                <span v-if="node.state" class="t2"> · {{ artifactState(node.state) }}</span>
                <span v-if="node.rules.length" class="t2">
                  · {{ $t('traceability.links.rules', { rules: node.rules.join(', ') }) }}
                </span>
              </li>
            </ul>
          </section>
        </div>
      </template>
    </template>

    <!-- ESP-24 3.2: coverage matrix. -->
    <template v-else>
      <fieldset class="plain-fieldset">
        <legend class="field-label">{{ $t('traceability.coverage.priority') }}</legend>
        <div class="checks">
          <label
            v-for="priority in PRIORITIES"
            :key="priority"
            class="check"
            :class="{ disabled: viewLoader.loading.value }"
            :for="`${uid}-priority-${priority}`"
          >
            <input
              :id="`${uid}-priority-${priority}`"
              type="checkbox"
              :value="priority"
              :checked="priorities.includes(priority)"
              :disabled="viewLoader.loading.value"
              @change="togglePriority(priority, ($event.target as HTMLInputElement).checked)"
            />
            {{ $t(`traceability.priorities.${priority}`) }}
          </label>
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
        <p class="plain-text summary" data-testid="coverage-summary">
          {{ $t('traceability.coverage.summary', counts) }}
        </p>
        <p v-if="notExecuted" class="note">
          {{ $t('traceability.coverage.notExecuted') }}
        </p>
        <p v-if="rows.length === 0" class="t2 plain-text">
          {{ $t('traceability.coverage.empty') }}
        </p>
        <div v-else class="table-scroll">
          <table class="tbl coverage-table" data-testid="coverage-table">
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
                <th scope="row" class="row-head">
                  <router-link
                    :to="{
                      name: ROUTE_NAMES.traceability,
                      params: { projectId, view: 'links' },
                      query: { root: row.us },
                    }"
                    class="mono"
                    :aria-label="$t('traceability.coverage.openLinks', { us: row.us })"
                  >
                    {{ row.us }}
                  </router-link>
                </th>
                <td>{{ $t(`traceability.priorities.${row.priority}`) }}</td>
                <td>
                  <span class="chip" :class="row.status === 'gap' ? 'crit' : 'ok'">
                    {{ $t(`traceability.coverage.statuses.${row.status}`) }}
                  </span>
                </td>
                <td>
                  <ul class="plain-list criteria">
                    <li v-for="criterion in row.criteria" :key="criterion.ca">
                      <span class="mono">{{ criterion.ca }}</span>:
                      <template v-if="criterion.cts.length === 0">
                        {{ $t('traceability.coverage.noScenario') }}
                      </template>
                      <template v-for="(ct, index) in criterion.cts" :key="ct.ct">
                        <router-link
                          :to="{
                            name: ROUTE_NAMES.artifact,
                            params: { projectId, artifactId: ct.ct },
                          }"
                          class="mono"
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
          </table>
        </div>
      </template>
    </template>
  </section>
</template>

<style scoped>
.tab-card {
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.views {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.tabf {
  min-height: 44px;
}

.params {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 16px;
}

.root-field {
  flex: 1 1 260px;
  min-width: 260px;
  max-width: 420px;
}

.field {
  display: flex;
  flex-direction: column;
}

.small-field {
  min-width: 160px;
  max-width: 200px;
}

.field-label {
  display: block;
  margin-bottom: 6px;
  padding: 0;
  font-size: 13px;
  font-weight: 500;
  color: #c7cceb;
}

.plain-fieldset {
  border: 0;
  margin: 0;
  padding: 0;
  min-width: 0;
}

.checks {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
}

.check {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 44px;
  cursor: pointer;
}

.check input {
  width: 18px;
  height: 18px;
  margin: 0;
  accent-color: #7c3aed;
  cursor: pointer;
}

.check.disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.note {
  margin: 0;
}

.note-warn {
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.1);
}

.plain-text {
  margin: 0;
}

.summary {
  font-weight: 600;
}

.trace-root {
  margin: 0;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.trace-root-title {
  font-weight: 700;
  font-size: 15px;
}

.groups {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 10px;
}

.group {
  padding: 14px 16px;
  border-radius: 12px;
  border: 1px solid rgba(140, 150, 255, 0.16);
  background: rgba(10, 13, 40, 0.35);
}

.group h3 {
  margin: 0 0 6px;
}

.node-list {
  margin: 0;
}

.node {
  padding: 8px 0;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  font-size: 13px;
}

.broken {
  color: #fca5a5;
}

.coverage-table {
  min-width: 720px;
}

.row-head {
  font-weight: 400;
  padding: 10px 10px 10px 0;
  border-bottom: 1px solid rgba(140, 150, 255, 0.08);
  vertical-align: top;
}

.criteria {
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
</style>
