<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import { searchApi, type SearchResults } from '../api/searchApi'
import {
  isSearchShortcut,
  MAX_TERM_LENGTH,
  missingCharacters,
  moveActive,
  resultCount,
  SEARCH_DEBOUNCE_MS,
  searchSections,
  SEVERITY_TONES,
} from '../searchResults'

/**
 * Header search of US-56 (canvas Main, top row): projects, artifacts and findings of the projects where
 * the person has a role, grouped by type. The results are real links; the arrows walk them, Enter opens
 * one, Esc closes; the count is announced in a polite live region, so the focus stays where it is (CA-5).
 */
const FIELD_ID = 'header-search'
const PANEL_ID = `${FIELD_ID}-panel`
const HINT_ID = `${FIELD_ID}-hint`

const { t } = useI18n()
const router = useRouter()
const { message: announcement, announce } = useAnnouncer()

const root = ref<HTMLElement | null>(null)
const field = ref<HTMLInputElement | null>(null)
const term = ref('')
const open = ref(false)
const status = ref<'idle' | 'loading' | 'done' | 'error'>('idle')
const results = ref<SearchResults | null>(null)
const searchedTerm = ref('')

let timer: ReturnType<typeof setTimeout> | undefined
let controller: AbortController | undefined

const missing = computed(() => missingCharacters(term.value))
const hint = computed(() => {
  if (!term.value.trim() || missing.value === 0) return ''
  return missing.value === 1
    ? t('search.missingOne')
    : t('search.missingMany', { n: missing.value })
})
const sections = computed(() => (results.value ? searchSections(results.value, FIELD_ID) : []))
const optionIds = computed(() =>
  sections.value.flatMap((section) => section.options.map((option) => option.id)),
)
const panelShown = computed(() => open.value && term.value.trim() !== '')

function cancelPending(): void {
  clearTimeout(timer)
  controller?.abort()
  controller = undefined
}

async function run(): Promise<void> {
  cancelPending()
  const asked = term.value.trim()
  const current = new AbortController()
  controller = current
  status.value = 'loading'
  try {
    const found = await searchApi.search(asked, current.signal)
    if (current.signal.aborted) return
    results.value = found
    searchedTerm.value = asked
    status.value = 'done'
    const n = resultCount(found)
    announce(
      n === 0
        ? t('search.empty', { term: asked })
        : n === 1
          ? t('search.countOne')
          : t('search.countMany', { n }),
    )
  } catch {
    if (current.signal.aborted) return
    status.value = 'error'
    announce(t('search.error'))
  }
}

watch(term, () => {
  cancelPending()
  results.value = null
  if (missing.value > 0) {
    // CA-3: nothing is sent; the hint says how many characters are missing.
    status.value = 'idle'
    return
  }
  status.value = 'loading'
  timer = setTimeout(() => void run(), SEARCH_DEBOUNCE_MS)
})

function close(): void {
  open.value = false
}

function reset(): void {
  cancelPending()
  term.value = ''
  results.value = null
  status.value = 'idle'
  close()
}

function focusOption(index: number): void {
  const id = optionIds.value[index]
  if (id) document.getElementById(id)?.focus()
}

function onFieldKeydown(event: KeyboardEvent): void {
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    if (!optionIds.value.length) return
    event.preventDefault()
    open.value = true
    const step = event.key === 'ArrowDown' ? 1 : -1
    void nextTick(() => {
      focusOption(moveActive(-1, optionIds.value.length, step))
    })
  } else if (event.key === 'Escape') {
    // First Esc closes the results and keeps the term; the second one clears it.
    event.preventDefault()
    if (panelShown.value) close()
    else term.value = ''
  }
}

function onOptionKeydown(event: KeyboardEvent, index: number): void {
  const size = optionIds.value.length
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    const step = event.key === 'ArrowDown' ? 1 : -1
    // Up from the first result goes back to the field.
    if (step === -1 && index === 0) field.value?.focus()
    else focusOption(moveActive(index, size, step))
  } else if (event.key === 'Home' || event.key === 'End') {
    event.preventDefault()
    focusOption(event.key === 'Home' ? 0 : size - 1)
  } else if (event.key === 'Escape') {
    event.preventDefault()
    // Back to the field first: its focus would open the results again.
    field.value?.focus()
    close()
  }
}

function onFocusOut(event: FocusEvent): void {
  const next = event.relatedTarget as Node | null
  if (!next || !root.value?.contains(next)) close()
}

function onShortcut(event: KeyboardEvent): void {
  if (!isSearchShortcut(event)) return
  event.preventDefault()
  field.value?.focus()
}

const stopClosingOnNavigation = router.afterEach(() => {
  close()
})
onMounted(() => {
  window.addEventListener('keydown', onShortcut)
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onShortcut)
  stopClosingOnNavigation()
  cancelPending()
})
</script>

<template>
  <div ref="root" class="hs" role="search" @focusout="onFocusOut">
    <CanvasIcon name="search" class="hs-icon" :stroke-width="1.8" />
    <label :for="FIELD_ID" class="hs-label">
      <span class="sr-only">{{ $t('search.label') }}</span>
      <input
        :id="FIELD_ID"
        ref="field"
        v-model="term"
        type="search"
        class="hs-field"
        autocomplete="off"
        :maxlength="MAX_TERM_LENGTH"
        :placeholder="$t('search.placeholder')"
        :aria-describedby="`${HINT_ID} ${FIELD_ID}-shortcut`"
        :aria-controls="panelShown ? PANEL_ID : undefined"
        data-testid="header-search"
        @focus="open = true"
        @input="open = true"
        @keydown="onFieldKeydown"
      />
    </label>
    <kbd :id="`${FIELD_ID}-shortcut`" class="hs-key" :aria-label="$t('search.shortcut')">/</kbd>
    <p class="sr-only" role="status" aria-live="polite">{{ announcement }}</p>

    <div v-show="panelShown" :id="PANEL_ID" class="hs-panel" data-testid="header-search-panel">
      <p v-show="hint" :id="HINT_ID" class="hs-note t2">{{ hint }}</p>
      <template v-if="!hint">
        <p v-if="status === 'loading'" class="hs-note t2">{{ $t('search.loading') }}</p>
        <div v-else-if="status === 'error'" class="hs-note">
          <span>{{ $t('search.error') }}</span>
          <button type="button" class="btn" @click="run">{{ $t('search.retry') }}</button>
        </div>
        <div v-else-if="status === 'done' && !sections.length" class="hs-note">
          <span>{{ $t('search.empty', { term: searchedTerm }) }}</span>
          <router-link class="hs-link" :to="{ name: ROUTE_NAMES.projects }" @click="reset">
            {{ $t('search.emptyHint') }}
          </router-link>
        </div>
        <section
          v-for="section in sections"
          :key="section.kind"
          class="hs-group"
          :aria-labelledby="`${FIELD_ID}-${section.kind}-label`"
          :data-testid="`header-search-${section.kind}`"
        >
          <p :id="`${FIELD_ID}-${section.kind}-label`" class="lb hs-group-label">
            {{
              $t('search.group', { group: $t(`search.groups.${section.kind}`), n: section.total })
            }}
          </p>
          <ul class="hs-list">
            <li v-for="option in section.options" :key="option.id">
              <router-link
                :id="option.id"
                class="hs-opt"
                :class="{ 'hs-all': option.seeAll }"
                :to="option.to"
                @click="reset"
                @keydown="onOptionKeydown($event, optionIds.indexOf(option.id))"
              >
                <template v-if="option.seeAll">{{ $t(`search.seeAll.${section.kind}`) }}</template>
                <template v-else-if="option.project">
                  <span class="hs-main">{{ option.project.name }}</span>
                  <StateLabel machine="project" :state="option.project.state" />
                </template>
                <template v-else-if="option.artifact">
                  <span class="hs-main">
                    <span class="hs-title"
                      ><span class="mono">{{ option.artifact.id }}</span>
                      {{ option.artifact.title }}</span
                    >
                    <span class="t2 hs-sub">{{ option.artifact.project.name }}</span>
                  </span>
                  <StateLabel machine="artifact" :state="option.artifact.state" />
                </template>
                <template v-else-if="option.finding">
                  <span class="hs-main">
                    <span class="hs-title"
                      ><span class="mono">{{ option.finding.id }}</span>
                      {{ option.finding.title }}</span
                    >
                    <span class="t2 hs-sub">{{ option.finding.project.name }}</span>
                  </span>
                  <span class="chip" :class="SEVERITY_TONES[option.finding.severity]">
                    {{ $t(`findings.severity.${option.finding.severity}`) }}
                  </span>
                  <StateLabel machine="finding" :state="option.finding.status" />
                </template>
              </router-link>
            </li>
          </ul>
        </section>
      </template>
    </div>
  </div>
</template>

<style scoped>
/* Canvas Main, top row: a field of up to 520 px with the search icon on the left. */
.hs {
  flex: 1 1 280px;
  max-width: 520px;
  position: relative;
}

.hs-label {
  display: block;
}

.hs-icon {
  z-index: 1;
  position: absolute;
  left: 14px;
  top: 12px;
  color: #c7cceb;
  pointer-events: none;
}

.hs-field {
  width: 100%;
  min-height: 44px;
  box-sizing: border-box;
  padding: 0 56px 0 44px;
  border: 1px solid rgba(140, 150, 255, 0.22);
  border-radius: 10px;
  background: rgba(20, 26, 66, 0.72);
  color: #f1f3ff;
  font: inherit;
}

.hs-field::placeholder {
  color: #a9b0d6;
}

.hs-key {
  position: absolute;
  right: 12px;
  top: 11px;
  min-width: 22px;
  height: 22px;
  box-sizing: border-box;
  padding: 0 6px;
  border: 1px solid rgba(140, 150, 255, 0.3);
  border-radius: 6px;
  color: #c7cceb;
  font:
    12px/20px ui-monospace,
    SFMono-Regular,
    Menlo,
    monospace;
  text-align: center;
  pointer-events: none;
}

.hs-panel {
  position: absolute;
  left: 0;
  top: calc(100% + 6px);
  width: max(100%, 320px);
  max-width: calc(100vw - 32px);
  max-height: min(70vh, 560px);
  overflow-y: auto;
  z-index: 30;
  box-sizing: border-box;
  padding: 6px 0;
  background: #151b45;
  border: 1px solid rgba(140, 150, 255, 0.24);
  border-radius: 14px;
  box-shadow: 0 16px 48px rgba(0, 0, 0, 0.45);
}

.hs-note {
  margin: 0;
  padding: 12px 16px;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
  font-size: 14px;
  color: #e3e6fa;
}

.hs-note.t2 {
  color: #a9b0d6;
}

.hs-group + .hs-group {
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  margin-top: 4px;
  padding-top: 4px;
}

.hs-group-label {
  margin: 0;
  padding: 10px 16px 4px;
}

.hs-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.hs-opt,
.hs-link {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 44px;
  box-sizing: border-box;
  padding: 6px 16px;
  color: #e3e6fa;
  text-decoration: none;
  font-size: 14px;
}

.hs-link {
  padding: 0;
  min-height: 44px;
  color: #c4b5fd;
}

.hs-opt:hover,
.hs-opt:focus-visible {
  background: rgba(255, 255, 255, 0.06);
  color: #ffffff;
}

.hs-all {
  color: #c4b5fd;
  font-weight: 600;
}

.hs-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.hs-title,
.hs-sub {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.hs-sub {
  font-size: 12px;
}
</style>
