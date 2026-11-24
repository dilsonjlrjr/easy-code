<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
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
const uid = useId()
/** Tone of each change, reinforcing its text and symbol (never color alone). */
const OPERATION_TONES = { added: 'ok', removed: 'crit', modified: 'warn' } as const

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
  <!-- Layout: canvas "Artefato" (card and the v1/v2 diff marks of the article). -->
  <section aria-labelledby="comparison-heading" class="card cmp" data-testid="artifact-comparison">
    <h2 id="comparison-heading">{{ $t('comparison.title') }}</h2>
    <div role="status" class="sr-only">{{ message }}</div>
    <!-- ESP-21 6: with a single version the comparison is unavailable, with the reason. -->
    <p v-if="versions.length < 2" class="t2 cmp-text" data-testid="comparison-unavailable">
      {{ $t('comparison.singleVersion') }}
    </p>
    <template v-else>
      <form class="cmp-form" @submit.prevent="compare">
        <label :for="`${uid}-from`" class="cmp-field">
          <span :id="`${uid}-from-label`" class="cmp-label">{{ $t('comparison.from') }}</span>
          <select
            :id="`${uid}-from`"
            v-model.number="from"
            class="fld"
            :aria-labelledby="`${uid}-from-label`"
          >
            <option v-for="option in options" :key="option.value" :value="option.value">
              {{ option.title }}
            </option>
          </select>
        </label>
        <label :for="`${uid}-to`" class="cmp-field">
          <span :id="`${uid}-to-label`" class="cmp-label">{{ $t('comparison.to') }}</span>
          <select
            :id="`${uid}-to`"
            v-model.number="to"
            class="fld"
            :aria-labelledby="`${uid}-to-label`"
          >
            <option v-for="option in options" :key="option.value" :value="option.value">
              {{ option.title }}
            </option>
          </select>
        </label>
        <span class="cmp-buttons">
          <button type="submit" class="btn btn-p" :disabled="!canCompare">
            {{ $t('comparison.compare') }}
          </button>
          <button v-if="pair" type="button" class="btn" @click="hide">
            {{ $t('comparison.hide') }}
          </button>
        </span>
      </form>
      <p v-if="from !== undefined && from === to" class="t2 cmp-text">
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
        <p class="cmp-summary" data-testid="comparison-summary">
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

        <div v-if="drivingFindings.length" class="cmp-text" data-testid="comparison-findings">
          <span class="t2">{{ $t('comparison.drivingFindings') }}</span>
          <template v-for="(finding, index) in drivingFindings" :key="finding">
            {{ ' '
            }}<router-link
              :to="{ name: ROUTE_NAMES.finding, params: { projectId, findingId: finding } }"
              class="mono"
              >{{ finding }}</router-link
            ><span v-if="index < drivingFindings.length - 1">,</span>
          </template>
        </div>

        <h3>{{ $t('comparison.header') }}</h3>
        <p v-if="comparison.header.length === 0" class="t2 cmp-text">
          {{ $t('comparison.noHeaderChanges') }}
        </p>
        <div v-else class="table-scroll">
          <table class="tbl" data-testid="comparison-header">
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
                <th scope="row" class="mono">{{ pointerText(change.path) }}</th>
                <td>
                  <span class="chip" :class="OPERATION_TONES[change.operation]">
                    <span aria-hidden="true" class="diff-symbol">{{
                      $t(`comparison.symbols.${change.operation}`)
                    }}</span>
                    {{ $t(`comparison.operations.${change.operation}`) }}
                  </span>
                </td>
                <td>
                  <pre class="mono diff-value">{{ diffValue(change.before) }}</pre>
                </td>
                <td>
                  <pre class="mono diff-value">{{ diffValue(change.after) }}</pre>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <h3>{{ $t('comparison.body') }}</h3>
        <p class="t2 cmp-note">{{ $t('artifacts.sanitizedNotice') }}</p>
        <p v-if="comparison.body.every((line) => line.operation === 'equal')" class="t2 cmp-text">
          {{ $t('comparison.noBodyChanges') }}
        </p>
        <!-- Lines are plain text (never HTML): agent content stays inert (RNF-05). -->
        <ol v-else class="diff-lines mono" data-testid="comparison-body">
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
.cmp {
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.cmp h3 {
  margin: 6px 0 0;
  font-size: 14px;
  line-height: 20px;
  font-weight: 700;
}

.cmp-text,
.cmp-summary {
  margin: 0;
  font-size: 13px;
}

.cmp-summary {
  font-size: 14px;
  font-weight: 600;
}

.cmp-note {
  margin: -8px 0 0;
  font-size: 12px;
}

.cmp-form {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: flex-end;
}

.cmp-field {
  flex: 1 1 160px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.cmp-label {
  font-size: 13px;
  font-weight: 600;
}

.cmp-buttons {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

/* Canvas "Artefato": removed lines struck through in red, added lines in green. */
.diff-lines {
  list-style: none;
  margin: 0;
  padding: 8px 0;
  border: 1px solid rgba(140, 150, 255, 0.16);
  border-radius: 10px;
  background: rgba(10, 13, 40, 0.8);
  overflow-x: auto;
  line-height: 20px;
}

.diff-lines li {
  display: flex;
  gap: 8px;
  padding: 2px 10px;
  white-space: pre-wrap;
}

.diff-symbol {
  display: inline-block;
  min-width: 1ch;
  font-weight: 700;
}

.diff-lines .diff-added {
  background: rgba(34, 197, 94, 0.16);
  color: #bbf7d0;
}

.diff-lines .diff-removed {
  background: rgba(239, 68, 68, 0.16);
  color: #fca5a5;
}

.diff-lines .diff-removed .diff-text {
  text-decoration: line-through;
}

.diff-lines .diff-modified {
  background: rgba(234, 179, 8, 0.16);
  color: #fcd34d;
}

.diff-value {
  white-space: pre-wrap;
  margin: 0;
}
</style>
