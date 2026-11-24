<script setup lang="ts">
import { mdiContentCopy } from '@mdi/js'
import { computed, nextTick, ref, useId, watch } from 'vue'
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
/** Ids of the native date fields, unique per trail on the page. */
const uid = `audit-${useId()}`

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
const periodError = computed(() => !draftPeriodValid.value || invalidPeriod.value)

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
  <!-- Layout: canvas primitives of Projeto.dc.html (filters with .fld, table .tbl); no own artboard. -->
  <div class="audit-trail" data-testid="audit-trail">
    <div role="status" class="sr-only">{{ message }}</div>
    <!-- ER-27-8: a member without approver or lead, or a non-administrator, gets "no permission". -->
    <p v-if="forbidden" class="note note-warn" role="alert" data-testid="audit-forbidden">
      {{ $t('audit.forbidden') }}
    </p>
    <p v-else-if="notFound" class="note note-warn" role="alert">
      {{ $t('problems.not-found') }}
    </p>
    <template v-else>
      <form class="audit-filters" data-testid="audit-filters" @submit.prevent="apply">
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
        <div class="field">
          <label :for="`${uid}-from`" class="field-control">
            <span class="field-label">{{ $t('audit.filters.from') }}</span>
            <input
              :id="`${uid}-from`"
              v-model="draft.from"
              type="date"
              class="fld"
              :disabled="loader.loading.value"
            />
          </label>
        </div>
        <div class="field">
          <label :for="`${uid}-until`" class="field-control">
            <span class="field-label">{{ $t('audit.filters.until') }}</span>
            <input
              :id="`${uid}-until`"
              v-model="draft.until"
              type="date"
              class="fld"
              :disabled="loader.loading.value"
              :aria-invalid="periodError ? 'true' : undefined"
              :aria-describedby="periodError ? `${uid}-until-error` : undefined"
              data-testid="audit-until"
            />
          </label>
          <p v-if="periodError" :id="`${uid}-until-error`" class="field-error">
            {{ $t('audit.filters.invalidPeriod') }}
          </p>
        </div>
        <div class="filter-actions">
          <button type="submit" class="btn btn-p" :disabled="!draftPeriodValid">
            {{ $t('audit.filters.apply') }}
          </button>
          <button v-if="filtered" type="button" class="btn" @click="clearFilters">
            {{ $t('audit.filters.clear') }}
          </button>
          <button type="button" class="btn" data-testid="audit-order" @click="toggleOrder">
            {{ applied.order === 'desc' ? $t('audit.order.toAsc') : $t('audit.order.toDesc') }}
          </button>
        </div>
      </form>
      <p class="t2 audit-hint">
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
        <p v-if="filtered" class="t2 audit-empty" data-testid="audit-empty">
          {{ $t('audit.emptyFiltered') }}
          <button type="button" class="ib" @click="clearFilters">
            {{ $t('audit.filters.clear') }}
          </button>
        </p>
        <p v-else class="t2 audit-empty" data-testid="audit-empty">{{ $t('audit.empty') }}</p>
      </template>
      <template v-else>
        <div class="table-scroll">
          <table class="tbl audit-table" data-testid="audit-records">
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
                <th scope="row" class="row-head">
                  {{ actorText(record.actor, t, (key) => i18n.te(key)) }}
                </th>
                <td>
                  <time :datetime="record.when">{{ format.dateTime(record.when) }}</time>
                </td>
                <td>
                  {{ actionText(record.action, t, (key) => i18n.te(key)) }}
                  <span v-if="record.decision" class="mono decision">{{ record.decision }}</span>
                </td>
                <td>{{ originText(record.origin) }}</td>
                <td>
                  {{ targetTypeText(record.target.type) }}
                  <router-link
                    v-if="targetLocation(record, projectId)"
                    :to="targetLocation(record, projectId) ?? ''"
                    class="mono"
                  >
                    {{ record.target.id }}
                  </router-link>
                  <span v-else class="mono">{{ record.target.id }}</span>
                </td>
                <td v-if="scope === 'organization'" class="mono">
                  {{ record.project ?? '—' }}
                </td>
                <td class="audit-reason">{{ record.reason ?? '—' }}</td>
                <td>{{ record.lost_cost ? format.money(record.lost_cost) : '—' }}</td>
                <td>
                  <span v-if="record.correlationid" class="correlation-cell">
                    <code class="mono correlation">{{ record.correlationid }}</code>
                    <button
                      type="button"
                      class="ibtn copy-button"
                      :aria-label="$t('audit.copyCorrelation')"
                      @click="copyCorrelation(record.correlationid)"
                    >
                      <svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true">
                        <path :d="mdiContentCopy" fill="currentColor" />
                      </svg>
                    </button>
                  </span>
                  <template v-else>—</template>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="audit-more">
          <button
            v-if="nextCursor"
            type="button"
            class="btn"
            :disabled="loadingMore"
            :aria-busy="loadingMore ? 'true' : undefined"
            data-testid="audit-load-more"
            @click="loadMore"
          >
            {{ $t('common.loadMore') }}
          </button>
          <p v-else class="t2 audit-end" data-testid="audit-end">
            {{ $t('audit.end') }}
          </p>
          <ProblemAlert v-if="moreFailure" :error="moreFailure" />
        </div>
      </template>
    </template>
  </div>
</template>

<style scoped>
.audit-trail {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.note {
  margin: 0;
}

.note-warn {
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.1);
}

.audit-filters {
  display: grid;
  gap: 12px;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  align-items: end;
}

.audit-actions {
  grid-column: 1 / -1;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  align-self: start;
}

.field-control {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.field-label {
  font-size: 13px;
  font-weight: 500;
  color: #c7cceb;
}

.field-error {
  margin: 0;
  font-size: 12px;
  color: #fca5a5;
}

.fld[aria-invalid='true'] {
  border-color: rgba(239, 68, 68, 0.7);
}

.filter-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.audit-hint {
  margin: 0;
  font-size: 13px;
}

.audit-empty {
  margin: 0;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.audit-table {
  min-width: 900px;
}

.row-head {
  font-weight: 600;
  color: #f1f3ff;
  border-bottom: 1px solid rgba(140, 150, 255, 0.08);
  padding: 10px 10px 10px 0;
  vertical-align: top;
}

.decision {
  display: block;
}

.audit-reason {
  white-space: pre-wrap;
  min-width: 12rem;
}

.correlation-cell {
  display: inline-flex;
  align-items: center;
  gap: 2px;
}

.correlation {
  font-size: 12px;
}

.copy-button {
  margin: -10px 0;
}

.audit-more {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
}

.audit-end {
  margin: 0;
  font-size: 13px;
}
</style>
