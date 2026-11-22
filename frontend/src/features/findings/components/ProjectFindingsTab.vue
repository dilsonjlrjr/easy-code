<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { hasProblemSlug } from '@/shared/api/problemDetails'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import { useCurrentProjectStore } from '@/features/projects/stores/currentProjectStore'
import { SEVERITIES, findingsApi, type FindingSummary, type Severity } from '../api/findingsApi'
import {
  STATUS_SHORTCUTS,
  apiFilters,
  blockingText,
  isInternalCycle,
  readFindingQuery,
  writeFindingQuery,
  type FindingQuery,
  type StatusShortcut,
} from '../composables/findingRules'

/**
 * ESP-30: every finding of the project, filtered by status, severity and destination stage, with
 * the filters kept in the address (CA-30-3). The list only reads; decisions live on the finding
 * page (ESP-20). Status changes arrive on the project stream and refresh the rows (ER-30-11).
 */
const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const router = useRouter()
const format = useFormat()
const store = useCurrentProjectStore()
const { message, announce } = useAnnouncer()

const items = ref<FindingSummary[]>([])
const nextCursor = ref<string | null>(null)
const loadingMore = ref(false)
const moreFailure = ref<unknown>()
const autoUpdate = ref(true)

const projectId = computed(() => store.project?.id ?? '')
const isApprover = computed(() => store.project?.my_roles.includes('approver') ?? false)
const projectClosed = computed(() => ['completed', 'canceled'].includes(store.project?.state ?? ''))
const stageIds = computed(() => (store.workflow?.stages ?? []).map((stage) => stage.id))
const filters = computed<FindingQuery>(() => readFindingQuery(route.query, stageIds.value))
const hasActiveFilters = computed(
  () =>
    filters.value.shortcut !== 'open' ||
    filters.value.severity.length > 0 ||
    filters.value.destination !== undefined,
)

const stageOptions = computed(() =>
  (store.workflow?.stages ?? []).map((stage) => ({ value: stage.id, title: stageName(stage.id) })),
)

function stageName(stage: string): string {
  const key = `stages.${stage}`
  return i18n.te(key) ? t(key) : stage
}

const loader = useLoader(async () => {
  const page = await findingsApi.list(projectId.value, apiFilters(filters.value))
  items.value = page.items
  nextCursor.value = page.next_cursor
})

async function reload(announceCount: boolean): Promise<void> {
  await loader.run()
  // ER-30-12: an invalid filter (400) goes back to the default filter.
  if (hasProblemSlug(loader.error.value, 'validation')) {
    await router.replace({ name: ROUTE_NAMES.projectFindings, params: route.params })
    return
  }
  if (announceCount && !loader.error.value) {
    announce(t('findings.list.resultCount', { n: items.value.length }, items.value.length))
  }
}

watch(
  () => [projectId.value, route.fullPath],
  ([id], previous) => {
    if (!id || route.name !== ROUTE_NAMES.projectFindings) return
    void reload(previous !== undefined)
  },
  { immediate: true },
)

/** ER-30-11: a status change refreshes the first page; it is announced once and keeps the focus. */
watch(
  () => store.revision,
  () => {
    if (!autoUpdate.value || !projectId.value) return
    const before = new Map(items.value.map((item) => [item.id, item.status]))
    void findingsApi
      .list(projectId.value, apiFilters(filters.value))
      .then((page) => {
        const changed = page.items.filter((item) => before.get(item.id) !== item.status)
        items.value = page.items
        nextCursor.value = page.next_cursor
        for (const item of changed) {
          announce(t('findings.list.changed', { id: item.id, status: t(statusLabelKey(item)) }))
        }
      })
      .catch(() => undefined)
  },
)

function applyFilters(next: Partial<FindingQuery>): void {
  void router.push({
    name: ROUTE_NAMES.projectFindings,
    params: route.params,
    query: writeFindingQuery({ ...filters.value, ...next }),
  })
}

function chooseShortcut(shortcut: StatusShortcut | null): void {
  if (!shortcut) return
  applyFilters({ shortcut, status: [...STATUS_SHORTCUTS[shortcut]] })
}

function chooseSeverities(severity: Severity[]): void {
  applyFilters({ severity })
}

function chooseDestination(destination: string | null): void {
  applyFilters({
    destination: (destination ?? undefined) as FindingQuery['destination'],
  })
}

function clearFilters(): void {
  void router.push({ name: ROUTE_NAMES.projectFindings, params: route.params })
}

/** CA-30-9: the next page is appended; the focus goes to its first row. */
async function loadMore(): Promise<void> {
  if (!nextCursor.value) return
  loadingMore.value = true
  moreFailure.value = undefined
  try {
    const page = await findingsApi.list(
      projectId.value,
      apiFilters(filters.value),
      nextCursor.value,
    )
    const known = new Set(items.value.map((item) => item.id))
    const added = page.items.filter((item) => !known.has(item.id))
    items.value = [...items.value, ...added]
    nextCursor.value = page.next_cursor
    await nextTick()
    const first = added[0]
    if (first) {
      document
        .querySelector<HTMLElement>(`[data-testid="finding-${CSS.escape(first.id)}"] a`)
        ?.focus()
    }
  } catch (error) {
    moreFailure.value = error
  } finally {
    loadingMore.value = false
  }
}

function statusLabelKey(finding: FindingSummary): string {
  return finding.status === 'accepted' && finding.acceptance_expired
    ? 'findings.status.accepted_expired'
    : `states.finding.${finding.status}`
}

function blocking(finding: FindingSummary): string {
  const text = blockingText(finding)
  if (!text) return ''
  return t(text.key, { stage: text.stage ? stageName(text.stage) : '' })
}

const activeFilterText = computed(() => {
  const parts: string[] = []
  const shortcut = filters.value.shortcut
  parts.push(
    shortcut
      ? t(`findings.list.shortcuts.${shortcut}`)
      : filters.value.status.map((status) => t(`states.finding.${status}`)).join(', '),
  )
  if (filters.value.severity.length > 0) {
    parts.push(filters.value.severity.map((item) => t(`findings.severity.${item}`)).join(', '))
  }
  if (filters.value.destination) {
    parts.push(t('findings.list.destinationIs', { stage: stageName(filters.value.destination) }))
  }
  return parts.join(' · ')
})

const emptyKey = computed(() => {
  if (
    filters.value.shortcut === 'all' &&
    !filters.value.severity.length &&
    !filters.value.destination
  )
    return 'findings.list.emptyProject'
  if (
    filters.value.shortcut === 'open' &&
    !filters.value.severity.length &&
    !filters.value.destination
  )
    return 'findings.list.emptyOpen'
  return 'findings.list.emptyFiltered'
})
</script>

<template>
  <section aria-labelledby="findings-heading">
    <h2 id="findings-heading" class="text-h6 mb-4">{{ $t('pages.project.findings') }}</h2>
    <div role="status" class="sr-only">{{ message }}</div>

    <form class="filters mb-4" @submit.prevent>
      <fieldset class="plain-fieldset">
        <legend class="text-body-2 font-weight-medium mb-1">
          {{ $t('findings.list.statusFilter') }}
        </legend>
        <v-btn-toggle
          :model-value="filters.shortcut"
          density="comfortable"
          variant="outlined"
          divided
          mandatory
          :disabled="loader.loading.value"
          @update:model-value="chooseShortcut"
        >
          <v-btn
            v-for="shortcut in Object.keys(STATUS_SHORTCUTS)"
            :key="shortcut"
            :value="shortcut"
            :aria-pressed="filters.shortcut === shortcut"
          >
            {{ $t(`findings.list.shortcuts.${shortcut}`) }}
          </v-btn>
        </v-btn-toggle>
      </fieldset>
      <fieldset class="plain-fieldset">
        <legend class="text-body-2 font-weight-medium mb-1">
          {{ $t('findings.list.severityFilter') }}
        </legend>
        <div class="d-flex flex-wrap ga-2">
          <v-checkbox
            v-for="severity in SEVERITIES"
            :key="severity"
            :model-value="filters.severity"
            :value="severity"
            :label="$t(`findings.severity.${severity}`)"
            density="compact"
            hide-details
            :disabled="loader.loading.value"
            @update:model-value="(value) => chooseSeverities((value ?? []) as Severity[])"
          />
        </div>
      </fieldset>
      <v-select
        :model-value="filters.destination ?? null"
        :items="stageOptions"
        :label="$t('findings.list.destinationFilter')"
        density="compact"
        clearable
        hide-details
        class="destination-filter"
        :disabled="loader.loading.value"
        @update:model-value="chooseDestination"
      />
    </form>
    <p class="text-body-2 mb-2" data-testid="active-filters">
      {{ $t('findings.list.activeFilters', { filters: activeFilterText }) }}
      <v-btn v-if="hasActiveFilters" variant="text" size="small" @click="clearFilters">
        {{ $t('findings.list.clear') }}
      </v-btn>
    </p>
    <!-- WCAG 2.2.2: the automatic updates can be paused. -->
    <v-switch
      v-model="autoUpdate"
      :label="$t('findings.list.autoUpdate')"
      color="primary"
      density="compact"
      hide-details
      class="mb-2"
    />

    <LoadingState v-if="loader.loading.value && items.length === 0" type="table-row@3" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="() => reload(false)"
    />
    <div v-else-if="items.length === 0" data-testid="findings-empty">
      <p class="text-body-2">{{ $t(emptyKey) }}</p>
      <v-btn
        v-if="emptyKey === 'findings.list.emptyOpen'"
        variant="text"
        size="small"
        @click="chooseShortcut('all')"
      >
        {{ $t('findings.list.showAll') }}
      </v-btn>
      <v-btn
        v-else-if="emptyKey === 'findings.list.emptyFiltered'"
        variant="text"
        size="small"
        @click="clearFilters"
      >
        {{ $t('findings.list.clear') }}
      </v-btn>
    </div>
    <div v-else class="table-scroll">
      <v-table density="comfortable" data-testid="findings-table">
        <caption class="sr-only">
          {{
            $t('pages.project.findings')
          }}
        </caption>
        <thead>
          <tr>
            <th scope="col">{{ $t('findings.columns.finding') }}</th>
            <th scope="col">{{ $t('findings.columns.severity') }}</th>
            <th scope="col">{{ $t('findings.columns.category') }}</th>
            <th scope="col">{{ $t('findings.columns.status') }}</th>
            <th scope="col">{{ $t('findings.columns.blocking') }}</th>
            <th scope="col">{{ $t('findings.columns.origin') }}</th>
            <th scope="col">{{ $t('findings.columns.destination') }}</th>
            <th scope="col">{{ $t('findings.columns.cycles') }}</th>
            <th scope="col">{{ $t('findings.columns.openedAt') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="finding in items" :key="finding.id" :data-testid="`finding-${finding.id}`">
            <th scope="row" class="font-weight-regular">
              <router-link
                :to="{
                  name: ROUTE_NAMES.finding,
                  params: { projectId, findingId: finding.id },
                }"
              >
                <span class="monospace">{{ finding.id }}</span> · {{ finding.title }}
              </router-link>
            </th>
            <td>{{ $t(`findings.severity.${finding.severity}`) }}</td>
            <td>{{ finding.category }}</td>
            <td>
              <StateLabel
                machine="finding"
                :state="finding.status"
                :detail="
                  finding.status === 'accepted' && finding.acceptance_expired
                    ? $t('findings.expired')
                    : undefined
                "
              />
              <p v-if="finding.status === 'escalated'" class="text-body-2 mt-1">
                {{ $t('findings.list.waitingDecision') }}
                <router-link
                  v-if="isApprover && !projectClosed"
                  :to="{
                    name: ROUTE_NAMES.finding,
                    params: { projectId, findingId: finding.id },
                    hash: '#decision',
                  }"
                  :aria-label="$t('findings.list.decideNamed', { id: finding.id })"
                >
                  {{ $t('findings.list.decide') }}
                </router-link>
              </p>
              <p v-else-if="finding.status === 'awaiting_scope'" class="text-body-2 mt-1">
                {{ $t('findings.list.scopeInProgress') }}
              </p>
            </td>
            <td>{{ blocking(finding) }}</td>
            <td>
              {{ $t(`agents.${finding.author}`) }}
              <template v-if="finding.origin_stage">
                ·
                <router-link
                  :to="{
                    name: ROUTE_NAMES.stage,
                    params: { projectId, stageId: finding.origin_stage },
                  }"
                >
                  {{ stageName(finding.origin_stage) }}
                </router-link>
              </template>
            </td>
            <td>
              <router-link
                :to="{
                  name: ROUTE_NAMES.stage,
                  params: { projectId, stageId: finding.destination_stage },
                }"
              >
                {{ stageName(finding.destination_stage) }}
              </router-link>
              <template v-if="isInternalCycle(finding)">
                · {{ $t('findings.internalCycle') }}</template
              >
            </td>
            <td>{{ $t('findings.cyclesOf', { n: finding.cycles ?? 0 }) }}</td>
            <td>{{ finding.opened_at ? format.dateTime(finding.opened_at) : '' }}</td>
          </tr>
        </tbody>
      </v-table>
    </div>
    <div v-if="nextCursor" class="mt-4">
      <v-btn variant="outlined" :loading="loadingMore" @click="loadMore">
        {{ $t('findings.list.loadMore') }}
      </v-btn>
      <ProblemAlert v-if="moreFailure" :error="moreFailure" class="mt-2" />
    </div>
  </section>
</template>

<style scoped>
.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  align-items: flex-end;
}

.plain-fieldset {
  border: 0;
  margin: 0;
  padding: 0;
}

.destination-filter {
  min-width: 240px;
  max-width: 320px;
}
</style>
