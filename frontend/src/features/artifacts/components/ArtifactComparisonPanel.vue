<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import {
  artifactsApi,
  type ArtifactComparison,
  type ArtifactVersionSummary,
} from '../api/artifactsApi'
import {
  comparedPair,
  defaultPair,
  diffCounts,
  diffValue,
  pointerText,
} from '../composables/artifactDiff'

/**
 * US-27 (ESP-21 3, 4, 8): compare any two versions of the artifact. Header fields and body lines
 * added, removed and modified are marked by text and symbol, not only by color, and the count of
 * changes is announced. The findings that drove the versions come with a link (CA-2).
 */
const props = defineProps<{
  projectId: string
  artifactId: string
  versions: ArtifactVersionSummary[]
}>()

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { message, announce } = useAnnouncer()

const numbers = computed(() => props.versions.map((item) => item.version))
const pair = computed(() => comparedPair(route.query, numbers.value))
const initial = pair.value ?? defaultPair(numbers.value)
const from = ref<number | undefined>(initial?.from)
const to = ref<number | undefined>(initial?.to)
const comparison = ref<ArtifactComparison>()

const options = computed(() =>
  props.versions.map((item) => ({
    value: item.version,
    title: t('artifacts.versionNumber', { n: item.version }),
  })),
)
const canCompare = computed(
  () => from.value !== undefined && to.value !== undefined && from.value !== to.value,
)
const counts = computed(() => (comparison.value ? diffCounts(comparison.value) : undefined))
/** CA-2: the findings of the compared versions (the api's, plus those in the history). */
const drivingFindings = computed(() => {
  const ids = new Set(comparison.value?.driving_findings ?? [])
  const shown = pair.value
  if (shown) {
    const low = Math.min(shown.from, shown.to)
    const high = Math.max(shown.from, shown.to)
    for (const item of props.versions) {
      if (item.finding && item.version > low && item.version <= high) ids.add(item.finding)
    }
  }
  return [...ids]
})

const loader = useLoader(async () => {
  const shown = pair.value
  if (!shown) {
    comparison.value = undefined
    return
  }
  comparison.value = await artifactsApi.compareVersions(
    props.projectId,
    props.artifactId,
    shown.from,
    shown.to,
  )
  const total = diffCounts(comparison.value)
  announce(
    t('comparison.announce', {
      from: shown.from,
      to: shown.to,
      added: total.added,
      removed: total.removed,
      modified: total.modified,
    }),
  )
})

function compare(): void {
  if (!canCompare.value) return
  void router.replace({
    query: { ...route.query, from: String(from.value), to: String(to.value) },
  })
}

function hide(): void {
  const query = { ...route.query }
  delete query.from
  delete query.to
  void router.replace({ query })
}

watch(
  pair,
  (value) => {
    if (value) {
      from.value = value.from
      to.value = value.to
    }
    void loader.run()
  },
  { immediate: true },
)
</script>

<template>
  <section aria-labelledby="comparison-heading" class="mb-6" data-testid="artifact-comparison">
    <h2 id="comparison-heading" class="text-h6 mb-2">{{ $t('comparison.title') }}</h2>
    <div role="status" class="sr-only">{{ message }}</div>
    <!-- ESP-21 6: with a single version the comparison is unavailable, with the reason. -->
    <p v-if="versions.length < 2" class="text-body-2" data-testid="comparison-unavailable">
      {{ $t('comparison.singleVersion') }}
    </p>
    <template v-else>
      <form class="comparison-form mb-4" @submit.prevent="compare">
        <v-select
          v-model="from"
          :items="options"
          :label="$t('comparison.from')"
          density="comfortable"
          hide-details
        />
        <v-select
          v-model="to"
          :items="options"
          :label="$t('comparison.to')"
          density="comfortable"
          hide-details
        />
        <div class="d-flex ga-2 align-center">
          <v-btn type="submit" color="primary" variant="flat" :disabled="!canCompare">
            {{ $t('comparison.compare') }}
          </v-btn>
          <v-btn v-if="pair" variant="outlined" @click="hide">
            {{ $t('comparison.hide') }}
          </v-btn>
        </div>
      </form>
      <p v-if="from !== undefined && from === to" class="text-body-2 mb-2">
        {{ $t('comparison.sameVersion') }}
      </p>

      <LoadingState v-if="loader.loading.value" type="paragraph" />
      <ProblemAlert
        v-else-if="loader.error.value"
        :error="loader.error.value"
        retry
        @retry="loader.run"
      />
      <template v-else-if="comparison && pair && counts">
        <p class="text-body-1 mb-3" data-testid="comparison-summary">
          {{
            $t('comparison.summary', {
              from: pair.from,
              to: pair.to,
              added: counts.added,
              removed: counts.removed,
              modified: counts.modified,
            })
          }}
        </p>

        <div v-if="drivingFindings.length" class="mb-4" data-testid="comparison-findings">
          <span class="text-body-2">{{ $t('comparison.drivingFindings') }}</span>
          <template v-for="(finding, index) in drivingFindings" :key="finding">
            <router-link
              :to="{ name: ROUTE_NAMES.finding, params: { projectId, findingId: finding } }"
              class="monospace ml-1"
              >{{ finding }}</router-link><span v-if="index < drivingFindings.length - 1">,</span>
          </template>
        </div>

        <h3 class="text-subtitle-1 font-weight-medium mb-2">{{ $t('comparison.header') }}</h3>
        <p v-if="comparison.header.length === 0" class="text-body-2 mb-4">
          {{ $t('comparison.noHeaderChanges') }}
        </p>
        <div v-else class="table-scroll mb-4">
          <v-table density="compact" data-testid="comparison-header">
            <thead>
              <tr>
                <th scope="col">{{ $t('comparison.field') }}</th>
                <th scope="col">{{ $t('comparison.change') }}</th>
                <th scope="col">{{ $t('comparison.before') }}</th>
                <th scope="col">{{ $t('comparison.after') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="change in comparison.header"
                :key="change.path"
                :class="`diff-${change.operation}`"
                :data-operation="change.operation"
              >
                <th scope="row" class="monospace">{{ pointerText(change.path) }}</th>
                <td>
                  <span aria-hidden="true" class="diff-symbol">{{
                    $t(`comparison.symbols.${change.operation}`)
                  }}</span>
                  {{ $t(`comparison.operations.${change.operation}`) }}
                </td>
                <td>
                  <pre class="diff-value">{{ diffValue(change.before) }}</pre>
                </td>
                <td>
                  <pre class="diff-value">{{ diffValue(change.after) }}</pre>
                </td>
              </tr>
            </tbody>
          </v-table>
        </div>

        <h3 class="text-subtitle-1 font-weight-medium mb-2">{{ $t('comparison.body') }}</h3>
        <p class="text-body-2 text-medium-emphasis mb-2">{{ $t('artifacts.sanitizedNotice') }}</p>
        <p v-if="comparison.body.every((line) => line.operation === 'equal')" class="text-body-2">
          {{ $t('comparison.noBodyChanges') }}
        </p>
        <!-- Lines are plain text (never HTML): agent content stays inert (RNF-05). -->
        <ol v-else class="diff-lines" data-testid="comparison-body">
          <li
            v-for="(line, index) in comparison.body"
            :key="index"
            :class="`diff-${line.operation}`"
            :data-operation="line.operation"
          >
            <span class="diff-symbol" aria-hidden="true">{{
              line.operation === 'equal' ? '' : $t(`comparison.symbols.${line.operation}`)
            }}</span>
            <span v-if="line.operation !== 'equal'" class="sr-only"
              >{{ $t(`comparison.operations.${line.operation}`) }}:
            </span>
            <span class="diff-text">{{ line.text }}</span>
          </li>
        </ol>
      </template>
    </template>
  </section>
</template>

<style scoped>
.comparison-form {
  display: grid;
  gap: 0.75rem;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  align-items: center;
}

.diff-lines {
  list-style: none;
  padding: 0;
  font-family: monospace;
  font-size: 0.85rem;
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 4px;
  overflow-x: auto;
}

.diff-lines li {
  display: flex;
  gap: 0.5rem;
  padding: 0 0.5rem;
  white-space: pre-wrap;
}

.diff-symbol {
  display: inline-block;
  min-width: 1ch;
  font-weight: 700;
}

.diff-added {
  background: rgba(var(--v-theme-success), 0.12);
}

.diff-removed {
  background: rgba(var(--v-theme-error), 0.12);
}

.diff-modified {
  background: rgba(var(--v-theme-warning), 0.12);
}

.diff-value {
  white-space: pre-wrap;
  font-family: monospace;
  font-size: 0.85rem;
  margin: 0;
}
</style>
