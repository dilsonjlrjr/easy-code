<script setup lang="ts">
import { computed, nextTick, ref, useId, watch } from 'vue'
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
const destinationId = `findings-destination-${useId()}`

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

function toggleSeverity(severity: Severity, checked: boolean): void {
  const others = filters.value.severity.filter((item) => item !== severity)
  chooseSeverities(checked ? [...others, severity] : others)
}

/** Canvas chip tones for the severity; the text names the severity, the color only reinforces it. */
const SEVERITY_TONES: Record<Severity, string> = {
  critical: 'crit',
  high: 'crit',
  medium: 'warn',
  low: 'mute',
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
  <!-- Layout: canvas primitives of Projeto.dc.html (filters .tabf/.fld, table .tbl); no own artboard. -->
  <section class="card tab-card" aria-labelledby="findings-heading">
    <h2 id="findings-heading">{{ $t('pages.project.findings') }}</h2>
    <div role="status" class="sr-only">{{ message }}</div>

    <form class="filters" @submit.prevent>
      <fieldset class="plain-fieldset">
        <legend class="field-label">{{ $t('findings.list.statusFilter') }}</legend>
        <div class="chips-row">
          <button
            v-for="shortcut in Object.keys(STATUS_SHORTCUTS) as StatusShortcut[]"
            :key="shortcut"
            type="button"
            class="tabf"
            :class="{ on: filters.shortcut === shortcut }"
            :aria-pressed="filters.shortcut === shortcut"
            :disabled="loader.loading.value"
            @click="chooseShortcut(shortcut)"
          >
            {{ $t(`findings.list.shortcuts.${shortcut}`) }}
          </button>
        </div>
      </fieldset>
      <fieldset class="plain-fieldset">
        <legend class="field-label">{{ $t('findings.list.severityFilter') }}</legend>
        <div class="chips-row">
          <label
            v-for="severity in SEVERITIES"
            :key="severity"
            class="check"
            :class="{ disabled: loader.loading.value }"
            :for="`${destinationId}-${severity}`"
          >
            <input
              :id="`${destinationId}-${severity}`"
              type="checkbox"
              :value="severity"
              :checked="filters.severity.includes(severity)"
              :disabled="loader.loading.value"
              @change="toggleSeverity(severity, ($event.target as HTMLInputElement).checked)"
            />
            {{ $t(`findings.severity.${severity}`) }}
          </label>
        </div>
      </fieldset>
      <div class="field destination-filter">
        <label :for="destinationId">
          <span class="field-label">{{ $t('findings.list.destinationFilter') }}</span>
          <select
            :id="destinationId"
            class="fld"
            :value="filters.destination ?? ''"
            :disabled="loader.loading.value"
            @change="chooseDestination(($event.target as HTMLSelectElement).value || null)"
          >
            <option value="">{{ $t('findings.list.anyDestination') }}</option>
            <option v-for="option in stageOptions" :key="option.value" :value="option.value">
              {{ option.title }}
            </option>
          </select>
        </label>
      </div>
    </form>
    <p class="t2 active-filters" data-testid="active-filters">
      {{ $t('findings.list.activeFilters', { filters: activeFilterText }) }}
      <button v-if="hasActiveFilters" type="button" class="ib" @click="clearFilters">
        {{ $t('findings.list.clear') }}
      </button>
    </p>
    <!-- WCAG 2.2.2: the automatic updates can be paused. -->
    <label class="check switch" :for="`${destinationId}-auto`">
      <input :id="`${destinationId}-auto`" v-model="autoUpdate" type="checkbox" role="switch" />
      {{ $t('findings.list.autoUpdate') }}
    </label>

    <LoadingState v-if="loader.loading.value && items.length === 0" type="table-row@3" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="() => reload(false)"
    />
    <div v-else-if="items.length === 0" class="empty" data-testid="findings-empty">
      <p class="t2">{{ $t(emptyKey) }}</p>
      <button
        v-if="emptyKey === 'findings.list.emptyOpen'"
        type="button"
        class="ib"
        @click="chooseShortcut('all')"
      >
        {{ $t('findings.list.showAll') }}
      </button>
      <button
        v-else-if="emptyKey === 'findings.list.emptyFiltered'"
        type="button"
        class="ib"
        @click="clearFilters"
      >
        {{ $t('findings.list.clear') }}
      </button>
    </div>
    <div v-else class="table-scroll">
      <table class="tbl findings-table" data-testid="findings-table">
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
            <th scope="row" class="row-head">
              <router-link
                :to="{
                  name: ROUTE_NAMES.finding,
                  params: { projectId, findingId: finding.id },
                }"
              >
                <span class="mono">{{ finding.id }}</span> · {{ finding.title }}
              </router-link>
            </th>
            <td>
              <span class="chip" :class="SEVERITY_TONES[finding.severity]">
                {{ $t(`findings.severity.${finding.severity}`) }}
              </span>
            </td>
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
              <p v-if="finding.status === 'escalated'" class="cell-note">
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
              <p v-else-if="finding.status === 'awaiting_scope'" class="cell-note">
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
      </table>
    </div>
    <div v-if="nextCursor" class="more">
      <button
        type="button"
        class="btn"
        :disabled="loadingMore"
        :aria-busy="loadingMore ? 'true' : undefined"
        @click="loadMore"
      >
        {{ $t('findings.list.loadMore') }}
      </button>
      <ProblemAlert v-if="moreFailure" :error="moreFailure" />
    </div>
  </section>
</template>

<style scoped>
.tab-card {
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 16px 24px;
  align-items: flex-end;
}

.plain-fieldset {
  border: 0;
  margin: 0;
  padding: 0;
  min-width: 0;
}

.field-label {
  display: block;
  margin-bottom: 6px;
  padding: 0;
  font-size: 13px;
  font-weight: 500;
  color: #c7cceb;
}

.chips-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.tabf {
  min-height: 44px;
}

.tabf[disabled] {
  opacity: 0.45;
  cursor: not-allowed;
}

.check {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 44px;
  padding-right: 8px;
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

.switch {
  align-self: flex-start;
}

.destination-filter {
  min-width: 240px;
  max-width: 320px;
  flex: 1 1 240px;
}

.active-filters {
  margin: 0;
  font-size: 13px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.empty {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.empty p {
  margin: 0;
}

.findings-table {
  min-width: 960px;
}

.row-head {
  font-weight: 400;
  color: #f1f3ff;
  padding: 10px 10px 10px 0;
  border-bottom: 1px solid rgba(140, 150, 255, 0.08);
  vertical-align: top;
}

.cell-note {
  margin: 4px 0 0;
  font-size: 12px;
  color: #a9b0d6;
}

.more {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
}
</style>
