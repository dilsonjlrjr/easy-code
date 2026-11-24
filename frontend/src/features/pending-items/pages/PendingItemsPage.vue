<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter, type RouteLocationRaw } from 'vue-router'
import { LOGIN_REASONS, ROUTE_NAMES } from '@/app/router/routeNames'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { createEventStream, type CloudEvent, type StreamStatus } from '@/shared/events/eventStream'
import { useFormat } from '@/shared/format/useFormat'
import { useSessionStore } from '@/shared/session/sessionStore'
import LiveConnectionStatus from '@/shared/ui/LiveConnectionStatus.vue'
import AppBreadcrumbs from '@/shared/ui/AppBreadcrumbs.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import {
  noticesUrl,
  oldestFirst,
  SHOWN_TYPES,
  pendingDestination,
  pendingItemsApi,
  type PendingItem,
  type PendingItemType,
} from '../api/pendingItemsApi'

/**
 * ESP-19: one box with what waits for the user in every project where they have a role (US-48).
 * The page does not decide anything: each item leads to the screen where it is decided. Items come
 * and go live through `watchNotices`.
 */
type Filter = 'all' | PendingItemType
const FILTERS: readonly Filter[] = ['all', ...SHOWN_TYPES]
const RELOAD_DELAY_MS = 300
const RELEVANT_GROUPS = ['gate.', 'wave.question_', 'finding.', 'project.budget_']
// Canvas "Pendências": each type has a letter sign, a sign tone and a chip tone.
const TYPE_SIGNS: Record<PendingItemType, string> = {
  gate: 'G',
  question: '?',
  finding_escalated: '!',
  budget: '$',
}
const TYPE_TONES: Record<PendingItemType, string> = {
  gate: 'warn',
  question: 'vio',
  finding_escalated: 'crit',
  budget: 'info',
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const format = useFormat()
const session = useSessionStore()
const { message, announce } = useAnnouncer()

const initialType = route.query.type
const filter = ref<Filter>(
  typeof initialType === 'string' && (SHOWN_TYPES as string[]).includes(initialType)
    ? (initialType as PendingItemType)
    : 'all',
)
const items = ref<PendingItem[]>([])
const nextCursor = ref<string | null>(null)
const counts = ref<Record<Filter, number>>({
  all: 0,
  gate: 0,
  question: 0,
  finding_escalated: 0,
  budget: 0,
})
const loadingMore = ref(false)
const moreFailure = ref<unknown>()
const liveStatus = ref<StreamStatus>()
const listHeading = ref<HTMLElement>()
let reloadTimer: ReturnType<typeof setTimeout> | undefined

async function readCounts(): Promise<void> {
  // PL-15: each filter shows the box total, not the size of the page loaded.
  const totals = await Promise.all(
    FILTERS.map((value) =>
      pendingItemsApi.list({ type: value === 'all' ? undefined : value, limit: 1 }),
    ),
  )
  FILTERS.forEach((value, index) => {
    counts.value[value] = totals[index]?.total ?? 0
  })
}

async function readItems(): Promise<void> {
  const page = await pendingItemsApi.list({
    type: filter.value === 'all' ? undefined : filter.value,
  })
  items.value = oldestFirst(page.items)
  nextCursor.value = page.next_cursor
}

const loader = useLoader(async () => {
  await Promise.all([readItems(), readCounts()])
})

function itemKey(item: PendingItem): string {
  return `${item.project.id}/${item.target.type}/${item.target.id}`
}

/** ER-19-4: a decided or new item changes the list without reloading; the change is announced. */
async function reloadLive(): Promise<void> {
  const before = new Set(items.value.map(itemKey))
  const focusedKey = (document.activeElement as HTMLElement | null)
    ?.closest('[data-item-key]')
    ?.getAttribute('data-item-key')
  try {
    await Promise.all([readItems(), readCounts()])
  } catch {
    return
  }
  const after = new Set(items.value.map(itemKey))
  const added = [...after].filter((key) => !before.has(key)).length
  const removed = [...before].filter((key) => !after.has(key)).length
  if (added || removed) announce(t('pendingItems.liveChange', { added, removed }))
  // ESP-19 section 6: when the focused item leaves, the focus goes to the list heading.
  if (focusedKey && !after.has(focusedKey)) {
    await nextTick()
    listHeading.value?.focus()
  }
}

function scheduleReload(): void {
  if (reloadTimer !== undefined) clearTimeout(reloadTimer)
  reloadTimer = setTimeout(() => {
    reloadTimer = undefined
    void reloadLive()
  }, RELOAD_DELAY_MS)
}

function handleEvent(event: CloudEvent): void {
  const type = event.type.replace(/^br\.easycode\./, '')
  if (RELEVANT_GROUPS.some((group) => type.startsWith(group))) scheduleReload()
}

// ER-19-8: the stream opens before the REST read; replayed events only trigger a reload.
const stream = createEventStream({
  url: noticesUrl(),
  order: 'position',
  onEvent: handleEvent,
  onGap: async () => {
    await reloadLive()
    return undefined
  },
  onStatus: (status) => {
    liveStatus.value = status
  },
  onEnded: (reason) => {
    if (reason === 'session_expired' || reason === 'session_revoked') {
      session.markSignedOut()
      void router.push({
        name: ROUTE_NAMES.login,
        query: {
          reason:
            reason === 'session_expired' ? LOGIN_REASONS.sessionExpired : LOGIN_REASONS.signedOut,
          return: route.fullPath,
        },
      })
    }
  },
})

onMounted(() => {
  stream.start()
  void loader.run()
})

onBeforeUnmount(() => {
  stream.stop()
  if (reloadTimer !== undefined) clearTimeout(reloadTimer)
})

watch(filter, (value) => {
  void router.replace({ query: value === 'all' ? {} : { type: value } })
  void loader.run()
})

async function loadMore(): Promise<void> {
  if (!nextCursor.value) return
  loadingMore.value = true
  moreFailure.value = undefined
  try {
    const page = await pendingItemsApi.list({
      type: filter.value === 'all' ? undefined : filter.value,
      cursor: nextCursor.value,
    })
    const known = new Set(items.value.map(itemKey))
    items.value = oldestFirst([
      ...items.value,
      ...page.items.filter((item) => !known.has(itemKey(item))),
    ])
    nextCursor.value = page.next_cursor
  } catch (error) {
    moreFailure.value = error
  } finally {
    loadingMore.value = false
  }
}

function destination(item: PendingItem): RouteLocationRaw {
  return pendingDestination(item)
}

function actionLabel(item: PendingItem): string {
  return t(`pendingItems.actions.${item.type}`)
}

function itemTitle(item: PendingItem): string {
  return item.title ?? `${t(`pendingItems.types.${item.type}`)} ${item.target.id}`
}

function summaryText(item: PendingItem): string | undefined {
  const summary = item.summary
  if (!summary) return undefined
  if (item.type === 'question' && summary.questions !== undefined)
    return t('pendingItems.questionCount', { count: summary.questions })
  if (item.type === 'gate' && summary.failed_checks !== undefined)
    return t('pendingItems.failedChecks', { count: summary.failed_checks })
  if (item.type === 'budget' && summary.budget_percentage !== undefined)
    return t('pendingItems.budgetPercentage', { percentage: summary.budget_percentage })
  return undefined
}

const emptyText = computed(() =>
  filter.value === 'all' ? t('pendingItems.empty') : t('pendingItems.emptyFilter'),
)
</script>

<template>
  <!-- Layout: canvas "Pendências" (Pendencias.dc.html). -->
  <AppBreadcrumbs :items="[{ title: $t('pages.pendingItems.title') }]" />
  <div class="pending-head">
    <div>
      <h1>{{ $t('pages.pendingItems.title') }}</h1>
      <p class="t2 pending-description">{{ $t('pages.pendingItems.description') }}</p>
    </div>
    <LiveConnectionStatus :status="liveStatus" />
  </div>
  <div aria-live="polite" role="status" class="sr-only">{{ message }}</div>

  <!-- ESP-19 section 8: toggle buttons with the pressed state and the count in the name. -->
  <div role="group" :aria-label="$t('pendingItems.filterLabel')" class="pending-filters">
    <button
      v-for="value in FILTERS"
      :key="value"
      type="button"
      :class="['tabf', { on: filter === value }]"
      :aria-pressed="filter === value"
      @click="filter = value"
    >
      {{ $t(`pendingItems.filters.${value}`) }}
      <!-- The brackets are in the name ("Todos (3)") but not on screen, as in the canvas pill. -->
      <span class="cnt"
        ><span class="paren">(</span>{{ counts[value] }}<span class="paren">)</span></span
      >
    </button>
  </div>

  <section class="card pending-card" aria-labelledby="pending-list-heading">
    <h2 id="pending-list-heading" ref="listHeading" tabindex="-1" class="sr-only">
      {{ $t('pendingItems.listHeading') }}
    </h2>
    <div v-if="loader.loading.value && items.length === 0" class="pending-pad">
      <LoadingState type="list-item-two-line@3" />
    </div>
    <div v-else-if="loader.error.value" class="pending-pad">
      <ProblemAlert :error="loader.error.value" retry @retry="loader.run" />
    </div>
    <p v-else-if="items.length === 0" class="t2 pending-pad" data-testid="pending-empty">
      {{ emptyText }}
    </p>
    <ul v-else class="plain-list" data-testid="pending-list">
      <li v-for="item in items" :key="itemKey(item)" :data-item-key="itemKey(item)" class="pr">
        <span :class="['pi', `pi-${TYPE_TONES[item.type]}`]" aria-hidden="true">
          {{ TYPE_SIGNS[item.type] }}
        </span>
        <span class="pending-text">
          <h3 class="pending-title">{{ itemTitle(item) }}</h3>
          <span class="t2 pending-detail">
            <span class="sr-only">{{ $t(`pendingItems.types.${item.type}`) }} · </span
            >{{ item.project.name }}
            <template v-if="item.stage"> · {{ $t(`stages.${item.stage}`) }}</template>
            <template v-if="item.agent"> · {{ $t(`agents.${item.agent}`) }}</template>
            · <span class="mono">{{ item.target.id }}</span>
            <template v-if="summaryText(item)"> · {{ summaryText(item) }}</template>
          </span>
        </span>
        <span class="pending-meta">
          <span :class="['chip', TYPE_TONES[item.type]]" aria-hidden="true">
            {{ $t(`pendingItems.types.${item.type}`) }}
          </span>
          <span class="t2 pending-age" :title="format.dateTime(item.opened_at)">
            {{ $t('pendingItems.waiting', { age: format.age(item.opened_at) }) }}
          </span>
        </span>
        <router-link
          class="btn"
          :to="destination(item)"
          :aria-label="
            $t('pendingItems.itemLink', {
              action: actionLabel(item),
              type: $t(`pendingItems.types.${item.type}`),
              title: itemTitle(item),
              project: item.project.name,
              age: format.dateTime(item.opened_at),
            })
          "
        >
          {{ actionLabel(item) }}
        </router-link>
      </li>
    </ul>
  </section>

  <div v-if="nextCursor && !loader.loading.value" class="pending-more">
    <button
      type="button"
      class="btn"
      :disabled="loadingMore"
      :aria-busy="loadingMore"
      @click="loadMore"
    >
      {{ $t('common.loadMore') }}
    </button>
    <ProblemAlert v-if="moreFailure" :error="moreFailure" />
  </div>

  <p class="t2 pending-notice">{{ $t('pendingItems.localClientNotice') }}</p>
</template>

<style scoped>
.pending-head {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  flex-wrap: wrap;
  justify-content: space-between;
}
.pending-description {
  margin: 4px 0 0;
}
.pending-filters {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.pending-card {
  overflow: hidden;
}
.pending-pad {
  margin: 0;
  padding: 20px;
}
.plain-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.pr {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 18px 20px;
  color: #f1f3ff;
  flex-wrap: wrap;
  border-top: 1px solid rgba(255, 255, 255, 0.06);
}
.pi {
  width: 44px;
  height: 44px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
}
.pi-warn {
  background: rgba(234, 179, 8, 0.18);
  color: #fcd34d;
}
.pi-vio {
  background: rgba(168, 85, 247, 0.22);
  color: #e9d5ff;
}
.pi-crit {
  background: rgba(239, 68, 68, 0.18);
  color: #fca5a5;
}
.pi-info {
  background: rgba(99, 102, 241, 0.22);
  color: #c7d2fe;
}
.pending-text {
  flex: 1 1 320px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.pending-title {
  margin: 0;
  font-weight: 700;
  font-size: 15px;
  line-height: 20px;
}
.pending-detail {
  font-size: 13px;
}
.pending-meta {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
}
.pending-age {
  font-size: 12px;
}
.pending-more {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
}
.pending-notice {
  margin: 0;
  font-size: 13px;
}

.paren {
  font-size: 0;
}
</style>
