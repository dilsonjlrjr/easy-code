<script setup lang="ts">
import { mdiContentCopy } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import type { Page } from '@/shared/api/contract'
import { isApiError } from '@/shared/api/problemDetails'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { auditApi, type AuditRecord } from '../api/auditApi'
import {
  ORGANIZATION_AUDIT_ACTIONS,
  PROJECT_AUDIT_ACTIONS,
  actionText,
  actorText,
  apiQueryOf,
  appendPage,
  filtersFromQuery,
  hasFilters,
  periodIsValid,
  queryFromFilters,
  targetLocation,
  type AuditFilters,
  type AuditScope,
} from '../composables/auditView'

/**
 * ESP-27 (US-35, RNF-13): who decided what, when and from where, in order. Filters by action and
 * period live in the address; "load more" follows the cursor. The trail only reads: there is no
 * change or delete action anywhere (CA-3).
 */
const props = defineProps<{
  scope: AuditScope
  /** The project of a project trail; absent in the organization trail. */
  projectId?: string
}>()

const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const router = useRouter()
const format = useFormat()
const { message, announce } = useAnnouncer()

const applied = computed(() => filtersFromQuery(route.query))
const draft = ref<AuditFilters>({ ...applied.value, actions: [...applied.value.actions] })
const records = ref<AuditRecord[]>([])
const nextCursor = ref<string | null>(null)
const loadingMore = ref(false)
const moreFailure = ref<unknown>()
const rowRefs = ref<Record<string, HTMLElement | null>>({})

const actionOptions = computed(() =>
  (props.scope === 'project' ? PROJECT_AUDIT_ACTIONS : ORGANIZATION_AUDIT_ACTIONS).map(
    (action) => ({ value: action, title: actionText(action, t, (key) => i18n.te(key)) }),
  ),
)
const draftPeriodValid = computed(() => periodIsValid(draft.value))

function list(cursor?: string): Promise<Page<AuditRecord>> {
  const query = apiQueryOf(applied.value, cursor)
  return props.scope === 'project' && props.projectId
    ? auditApi.listProjectAudit(props.projectId, query)
    : auditApi.listOrganizationAudit(query)
}

const loader = useLoader(async () => {
  const page = await list()
  records.value = page.items
  nextCursor.value = page.next_cursor
})

const forbidden = computed(
  () => isApiError(loader.error.value) && loader.error.value.status === 403,
)
const notFound = computed(() => isApiError(loader.error.value) && loader.error.value.status === 404)
const invalidPeriod = computed(
  () => isApiError(loader.error.value) && loader.error.value.status === 400,
)
const filtered = computed(() => hasFilters(applied.value))

function apply(): void {
  if (!draftPeriodValid.value) return
  void router.replace({ query: queryFromFilters(draft.value) })
}

function clearFilters(): void {
  draft.value = { actions: [], from: '', until: '', order: applied.value.order }
  void router.replace({ query: queryFromFilters(draft.value) })
}

/** CA-27-9: newest first by default; the user can turn it to oldest first. */
function toggleOrder(): void {
  const order = applied.value.order === 'desc' ? 'asc' : 'desc'
  draft.value = { ...draft.value, order }
  void router.replace({ query: queryFromFilters({ ...applied.value, order }) })
}

/** CA-27-8 / ESP-27 8: next page after the shown ones; focus on the first new record, announced. */
async function loadMore(): Promise<void> {
  if (!nextCursor.value) return
  loadingMore.value = true
  moreFailure.value = undefined
  try {
    const page = await list(nextCursor.value)
    const before = records.value.length
    records.value = appendPage(records.value, page.items)
    nextCursor.value = page.next_cursor
    const added = records.value.length - before
    announce(t('audit.loadedMore', { n: added }))
    const first = records.value[before]
    if (first) {
      await nextTick()
      rowRefs.value[first.id]?.focus()
    }
  } catch (error) {
    moreFailure.value = error
  } finally {
    loadingMore.value = false
  }
}

async function copyCorrelation(id: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(id)
    announce(t('audit.correlationCopied'))
  } catch {
    announce(t('audit.correlationCopyFailed'))
  }
}

function originText(origin: AuditRecord['origin']): string {
  return origin ? t(`audit.origins.${origin}`) : '—'
}

function targetTypeText(type: string): string {
  const key = `audit.targetTypes.${type.replace(/s$/, '')}`
  return i18n.te(key) ? t(key) : type
}

function setRowRef(id: string, element: unknown): void {
  rowRefs.value[id] = element instanceof HTMLElement ? element : null
}

watch(
  () => [props.projectId, route.query],
  () => {
    draft.value = { ...applied.value, actions: [...applied.value.actions] }
    void loader.run()
  },
  { immediate: true, deep: true },
)
</script>

<template>
  <div data-testid="audit-trail">
    <div role="status" class="sr-only">{{ message }}</div>
    <!-- ER-27-8: a member without approver or lead, or a non-administrator, gets "no permission". -->
    <v-alert
      v-if="forbidden"
      type="warning"
      variant="tonal"
      role="alert"
      data-testid="audit-forbidden"
    >
      {{ $t('audit.forbidden') }}
    </v-alert>
    <v-alert v-else-if="notFound" type="warning" variant="tonal" role="alert">
      {{ $t('problems.not-found') }}
    </v-alert>
    <template v-else>
      <form class="audit-filters mb-4" data-testid="audit-filters" @submit.prevent="apply">
        <v-select
          v-model="draft.actions"
          :items="actionOptions"
          :label="$t('audit.filters.actions')"
          :disabled="loader.loading.value"
          multiple
          chips
          closable-chips
          clearable
          density="comfortable"
          hide-details
          class="audit-actions"
        />
        <v-text-field
          v-model="draft.from"
          type="date"
          :label="$t('audit.filters.from')"
          :disabled="loader.loading.value"
          density="comfortable"
          hide-details="auto"
        />
        <v-text-field
          v-model="draft.until"
          type="date"
          :label="$t('audit.filters.until')"
          :disabled="loader.loading.value"
          :error-messages="
            draftPeriodValid && !invalidPeriod ? [] : [$t('audit.filters.invalidPeriod')]
          "
          density="comfortable"
          hide-details="auto"
          data-testid="audit-until"
        />
        <div class="d-flex flex-wrap align-center ga-2">
          <v-btn type="submit" color="primary" variant="flat" :disabled="!draftPeriodValid">
            {{ $t('audit.filters.apply') }}
          </v-btn>
          <v-btn v-if="filtered" variant="outlined" @click="clearFilters">
            {{ $t('audit.filters.clear') }}
          </v-btn>
          <v-btn variant="text" data-testid="audit-order" @click="toggleOrder">
            {{ applied.order === 'desc' ? $t('audit.order.toAsc') : $t('audit.order.toDesc') }}
          </v-btn>
        </div>
      </form>
      <p class="text-body-2 text-medium-emphasis mb-2">
        {{ applied.order === 'desc' ? $t('audit.order.desc') : $t('audit.order.asc') }}
        · {{ $t('audit.readOnly') }}
      </p>

      <LoadingState v-if="loader.loading.value && records.length === 0" type="table" />
      <ProblemAlert
        v-else-if="loader.error.value && !invalidPeriod"
        :error="loader.error.value"
        retry
        @retry="loader.run"
      />
      <template v-else-if="records.length === 0 && !loader.loading.value">
        <p v-if="filtered" data-testid="audit-empty">
          {{ $t('audit.emptyFiltered') }}
          <v-btn variant="text" size="small" @click="clearFilters">
            {{ $t('audit.filters.clear') }}
          </v-btn>
        </p>
        <p v-else data-testid="audit-empty">{{ $t('audit.empty') }}</p>
      </template>
      <template v-else>
        <div class="table-scroll">
          <v-table density="comfortable" data-testid="audit-records">
            <caption class="sr-only">
              {{
                $t('audit.caption')
              }}
            </caption>
            <thead>
              <tr>
                <th scope="col">{{ $t('audit.columns.actor') }}</th>
                <th scope="col">{{ $t('audit.columns.when') }}</th>
                <th scope="col">{{ $t('audit.columns.action') }}</th>
                <th scope="col">{{ $t('audit.columns.origin') }}</th>
                <th scope="col">{{ $t('audit.columns.target') }}</th>
                <th v-if="scope === 'organization'" scope="col">
                  {{ $t('audit.columns.project') }}
                </th>
                <th scope="col">{{ $t('audit.columns.reason') }}</th>
                <th scope="col">{{ $t('audit.columns.lostCost') }}</th>
                <th scope="col">{{ $t('audit.columns.correlation') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="record in records"
                :key="record.id"
                :ref="(element) => setRowRef(record.id, element)"
                tabindex="-1"
                :data-testid="`audit-record-${record.id}`"
                :data-action="record.action"
              >
                <th scope="row">{{ actorText(record.actor, t, (key) => i18n.te(key)) }}</th>
                <td>
                  <time :datetime="record.when">{{ format.dateTime(record.when) }}</time>
                </td>
                <td>
                  {{ actionText(record.action, t, (key) => i18n.te(key)) }}
                  <span v-if="record.decision" class="d-block text-body-2 monospace">{{
                    record.decision
                  }}</span>
                </td>
                <td>{{ originText(record.origin) }}</td>
                <td>
                  {{ targetTypeText(record.target.type) }}
                  <router-link
                    v-if="targetLocation(record, projectId)"
                    :to="targetLocation(record, projectId) ?? ''"
                    class="monospace"
                  >
                    {{ record.target.id }}
                  </router-link>
                  <span v-else class="monospace">{{ record.target.id }}</span>
                </td>
                <td v-if="scope === 'organization'" class="monospace">
                  {{ record.project ?? '—' }}
                </td>
                <td class="audit-reason">{{ record.reason ?? '—' }}</td>
                <td>{{ record.lost_cost ? format.money(record.lost_cost) : '—' }}</td>
                <td>
                  <template v-if="record.correlationid">
                    <code class="monospace correlation">{{ record.correlationid }}</code>
                    <v-btn
                      :icon="mdiContentCopy"
                      size="x-small"
                      variant="text"
                      :aria-label="$t('audit.copyCorrelation')"
                      @click="copyCorrelation(record.correlationid)"
                    />
                  </template>
                  <template v-else>—</template>
                </td>
              </tr>
            </tbody>
          </v-table>
        </div>
        <div class="mt-4">
          <v-btn
            v-if="nextCursor"
            variant="outlined"
            :loading="loadingMore"
            data-testid="audit-load-more"
            @click="loadMore"
          >
            {{ $t('common.loadMore') }}
          </v-btn>
          <p v-else class="text-body-2 text-medium-emphasis" data-testid="audit-end">
            {{ $t('audit.end') }}
          </p>
          <ProblemAlert v-if="moreFailure" :error="moreFailure" class="mt-2" />
        </div>
      </template>
    </template>
  </div>
</template>

<style scoped>
.audit-filters {
  display: grid;
  gap: 0.75rem;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  align-items: start;
}

.audit-actions {
  grid-column: 1 / -1;
}

.audit-reason {
  white-space: pre-wrap;
  min-width: 12rem;
}

.correlation {
  font-size: 0.75rem;
}
</style>
