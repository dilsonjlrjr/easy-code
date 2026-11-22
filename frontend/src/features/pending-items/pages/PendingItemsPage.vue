<script setup lang="ts">
import { mdiAccountQuestionOutline, mdiCurrencyUsd, mdiFlagOutline, mdiGate } from '@mdi/js'
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
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import {
  gateRoundOf,
  noticesUrl,
  oldestFirst,
  SHOWN_TYPES,
  pendingItemsApi,
  waveOfQuestion,
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
const TYPE_ICONS: Record<PendingItemType, string> = {
  gate: mdiGate,
  question: mdiAccountQuestionOutline,
  finding_escalated: mdiFlagOutline,
  budget: mdiCurrencyUsd,
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

/** ESP-19 section 2: each item leads to where it is decided or answered. */
function destination(item: PendingItem): RouteLocationRaw {
  const projectId = item.project.id
  if (item.target.type === 'gate_round') {
    const gate = gateRoundOf(item.target.id)
    if (gate) return { name: ROUTE_NAMES.gate, params: { projectId, ...gate } }
  }
  if (item.target.type === 'question') {
    const waveId = waveOfQuestion(item.target.id)
    if (waveId) return { name: ROUTE_NAMES.wave, params: { projectId, waveId } }
  }
  // US-32 CA-4: the escalated finding opens on its decision (ESP-20).
  if (item.target.type === 'finding')
    return {
      name: ROUTE_NAMES.finding,
      params: { projectId, findingId: item.target.id },
      hash: '#decision',
    }
  if (item.type === 'budget')
    return { name: ROUTE_NAMES.projectConfiguration, params: { projectId } }
  return { name: ROUTE_NAMES.project, params: { projectId } }
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
  <v-container max-width="1200">
    <div class="d-flex flex-wrap align-center ga-2 mb-2">
      <h1 class="text-h4">{{ $t('pages.pendingItems.title') }}</h1>
      <v-spacer />
      <LiveConnectionStatus :status="liveStatus" />
    </div>
    <p class="text-body-1 text-medium-emphasis mb-4">{{ $t('pages.pendingItems.description') }}</p>
    <div aria-live="polite" role="status" class="sr-only">{{ message }}</div>

    <!-- ESP-19 section 8: toggle buttons with the pressed state and the count in the name. -->
    <div
      role="group"
      :aria-label="$t('pendingItems.filterLabel')"
      class="d-flex flex-wrap ga-2 mb-4"
    >
      <v-btn
        v-for="value in FILTERS"
        :key="value"
        :variant="filter === value ? 'flat' : 'outlined'"
        :color="filter === value ? 'primary' : undefined"
        :aria-pressed="filter === value"
        size="small"
        @click="filter = value"
      >
        {{ $t(`pendingItems.filters.${value}`) }} ({{ counts[value] }})
      </v-btn>
    </div>

    <h2 ref="listHeading" tabindex="-1" class="sr-only">{{ $t('pendingItems.listHeading') }}</h2>
    <LoadingState v-if="loader.loading.value && items.length === 0" type="list-item-two-line@3" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <p v-else-if="items.length === 0" data-testid="pending-empty">{{ emptyText }}</p>
    <ul v-else class="plain-list" data-testid="pending-list">
      <li v-for="item in items" :key="itemKey(item)" :data-item-key="itemKey(item)" class="mb-3">
        <v-card variant="outlined" class="pa-4">
          <p class="text-body-2 d-flex align-center ga-1">
            <v-icon :icon="TYPE_ICONS[item.type]" size="small" aria-hidden="true" />
            <span class="font-weight-medium">{{ $t(`pendingItems.types.${item.type}`) }}</span>
            · {{ item.project.name }}
          </p>
          <h3 class="text-subtitle-1 font-weight-medium">{{ itemTitle(item) }}</h3>
          <p class="text-body-2">
            <template v-if="item.stage">{{ $t(`stages.${item.stage}`) }} · </template>
            <template v-if="item.agent">{{ $t(`agents.${item.agent}`) }} · </template>
            <span class="monospace">{{ item.target.id }}</span> ·
            <span :title="format.dateTime(item.opened_at)">
              {{ $t('pendingItems.waiting', { age: format.age(item.opened_at) }) }}
            </span>
          </p>
          <p v-if="summaryText(item)" class="text-body-2">{{ summaryText(item) }}</p>
          <router-link
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
        </v-card>
      </li>
    </ul>

    <div v-if="nextCursor && !loader.loading.value" class="mt-4">
      <v-btn variant="outlined" :loading="loadingMore" @click="loadMore">
        {{ $t('common.loadMore') }}
      </v-btn>
      <ProblemAlert v-if="moreFailure" :error="moreFailure" class="mt-2" />
    </div>

    <p class="text-body-2 text-medium-emphasis mt-6">{{ $t('pendingItems.localClientNotice') }}</p>
  </v-container>
</template>
