<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { artifactsApi, type Artifact } from '@/features/artifacts/api/artifactsApi'
import { displayPath, pagePath, screenMap } from '@/features/prototype/composables/prototypeModel'
import { isApiError } from '@/shared/api/problemDetails'
import { useLoader } from '@/shared/composables/useLoader'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import MarkdownContent from '@/shared/ui/MarkdownContent.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import type { StageViewProps } from '../stageViews'
import { flowAttachment } from './designView'

/**
 * ESP-10 (canvas EtapaDesign): the screens and the navigation flow of the TELAS header, with the entry to
 * the prototype viewer (ESP-11). Everything of TELAS is text written by the Designer: shown as text only
 * (RNF-05). The view never changes TELAS nor sends the stage to the gate (CA-10-12).
 */
const props = defineProps<StageViewProps>()

const telas = ref<Artifact>()
const absent = ref(false)
const tab = ref<'screens' | 'flow'>('screens')
const telasId = 'TELAS'

const map = computed(() => screenMap(telas.value?.header))
const firstWithPage = computed(() =>
  map.value.screens.find((screen) => pagePath(map.value.prototype, screen.file)),
)

const loader = useLoader(async () => {
  absent.value = false
  try {
    telas.value = await artifactsApi.getArtifact(props.projectId, 'TELAS')
  } catch (error) {
    if (isApiError(error) && error.status === 404) {
      telas.value = undefined
      absent.value = true
      return
    }
    throw error
  }
})

watch([() => props.projectId, () => props.revision], () => void loader.run(), { immediate: true })

/**
 * ESP-10 "Fluxo": the content of the flow file of this TELAS version (PL-24, getArtifactAttachment),
 * rendered only through the sanitizer (RNF-05). Its failure stays in the tab; the screens still show.
 */
const flowPath = computed(() => flowAttachment(telas.value?.attachments, map.value.flow))
const flowText = ref<string>()
const flowLoader = useLoader(async () => {
  flowText.value = undefined
  const current = telas.value
  const path = flowPath.value
  if (!current || !path) return
  flowText.value = await artifactsApi.getAttachment(props.projectId, telasId, current.version, path)
})

watch(
  () => `${String(telas.value?.version)}:${flowPath.value ?? ''}`,
  () => void flowLoader.run(),
)

function prototypeRoute(screen?: string) {
  return {
    name: ROUTE_NAMES.prototype,
    params: { projectId: props.projectId },
    query: screen ? { screen } : {},
  }
}

function selectTab(next: 'screens' | 'flow', event?: KeyboardEvent): void {
  tab.value = next
  if (event) {
    const target = document.getElementById(`design-tab-${next}`)
    target?.focus()
  }
}

function onTabKey(event: KeyboardEvent): void {
  if (event.key === 'ArrowRight' || event.key === 'ArrowLeft') {
    event.preventDefault()
    selectTab(tab.value === 'screens' ? 'flow' : 'screens', event)
  }
}
</script>

<template>
  <section class="design-view" aria-labelledby="design-view-heading" data-testid="design-view">
    <div class="view-head">
      <h3 id="design-view-heading">{{ $t('designStage.heading') }}</h3>
      <router-link
        v-if="telas && firstWithPage"
        class="btn"
        :to="prototypeRoute()"
        data-testid="design-open-prototype"
      >
        <CanvasIcon name="monitor" :size="16" />
        {{ $t('designStage.openPrototype') }}
      </router-link>
    </div>

    <LoadingState v-if="loader.loading.value && !telas && !absent" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <p v-else-if="absent" class="card t2 empty" data-testid="design-no-telas">
      {{ $t('designStage.noTelas') }}
    </p>
    <template v-else-if="telas">
      <dl class="card design-strip">
        <div class="strip-item">
          <dt class="t2">{{ $t('designStage.artifact') }}</dt>
          <dd>
            <router-link
              class="mono"
              :to="{ name: ROUTE_NAMES.artifact, params: { projectId, artifactId: 'TELAS' } }"
            >
              {{ telasId }}
            </router-link>
            · {{ $t('artifacts.versionNumber', { n: telas.version }) }}
            <StateLabel machine="artifact" :state="telas.state" />
          </dd>
        </div>
        <div v-if="map.accessibility" class="strip-item" data-testid="design-accessibility">
          <dt class="t2">{{ $t('designStage.accessibility') }}</dt>
          <dd>
            {{
              map.accessibility.verified
                ? $t('designStage.accessibilityVerified', { level: 'WCAG 2.2 AA' })
                : $t('designStage.accessibilityNotVerified')
            }}
          </dd>
        </div>
        <div v-if="map.contractGaps.length > 0" class="strip-item" data-testid="design-gaps">
          <dt class="t2">{{ $t('designStage.contractGaps') }}</dt>
          <dd>
            <template v-for="(gap, index) in map.contractGaps" :key="gap">
              <router-link
                class="mono"
                :to="{ name: ROUTE_NAMES.finding, params: { projectId, findingId: gap } }"
                >{{ gap }}</router-link><span v-if="index < map.contractGaps.length - 1">, </span>
            </template>
          </dd>
        </div>
      </dl>
      <p v-if="map.accessibility?.notes" class="t2 notes">{{ map.accessibility.notes }}</p>

      <div class="tabs" role="tablist" :aria-label="$t('designStage.sections')">
        <button
          id="design-tab-screens"
          role="tab"
          type="button"
          class="tab"
          :class="{ on: tab === 'screens' }"
          :aria-selected="tab === 'screens'"
          :tabindex="tab === 'screens' ? 0 : -1"
          aria-controls="design-panel-screens"
          @click="selectTab('screens')"
          @keydown="onTabKey"
        >
          {{ $t('designStage.screens') }}
          <span class="chip mute">{{ map.screens.length }}</span>
        </button>
        <button
          id="design-tab-flow"
          role="tab"
          type="button"
          class="tab"
          :class="{ on: tab === 'flow' }"
          :aria-selected="tab === 'flow'"
          :tabindex="tab === 'flow' ? 0 : -1"
          aria-controls="design-panel-flow"
          @click="selectTab('flow')"
          @keydown="onTabKey"
        >
          {{ $t('designStage.flow') }}
        </button>
      </div>

      <div
        v-show="tab === 'screens'"
        id="design-panel-screens"
        role="tabpanel"
        aria-labelledby="design-tab-screens"
        class="card panel"
      >
        <p v-if="map.screens.length === 0" class="t2 empty-text">
          {{ $t('designStage.noScreens') }}
        </p>
        <div v-else class="table-scroll">
          <table class="tbl" data-testid="design-screens">
            <caption class="sr-only">
              {{
                $t('designStage.screensCaption')
              }}
            </caption>
            <thead>
              <tr>
                <th scope="col">{{ $t('designStage.columns.screen') }}</th>
                <th scope="col">{{ $t('designStage.columns.serves') }}</th>
                <th scope="col">{{ $t('designStage.columns.states') }}</th>
                <th scope="col">{{ $t('designStage.columns.file') }}</th>
                <th scope="col">
                  <span class="sr-only">{{ $t('designStage.columns.actions') }}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="screen in map.screens"
                :key="screen.id"
                :data-testid="`design-screen-${screen.id}`"
              >
                <td>
                  <span class="mono t2 cell-id">{{ screen.id }}</span>
                  <strong class="cell-name">{{ screen.name }}</strong>
                </td>
                <td>
                  <span class="links">
                    <router-link
                      v-for="us in screen.us"
                      :key="us"
                      class="mono"
                      :to="{ name: ROUTE_NAMES.artifact, params: { projectId, artifactId: us } }"
                      >{{ us }}</router-link
                    >
                  </span>
                  <span v-if="screen.criteria.length > 0" class="t2 cell-sub">
                    {{ screen.criteria.join(', ') }}
                  </span>
                </td>
                <td>
                  <span class="states">
                    <span v-for="state in screen.states" :key="state" class="chip mute">
                      {{ state.replace(/_/g, ' ') }}
                    </span>
                  </span>
                </td>
                <td class="mono cell-file">
                  {{
                    pagePath(map.prototype, screen.file)
                      ? displayPath(map.prototype, screen.file)
                      : $t('designStage.noFile')
                  }}
                </td>
                <td>
                  <router-link
                    v-if="pagePath(map.prototype, screen.file)"
                    class="ib"
                    :to="prototypeRoute(screen.id)"
                    :aria-label="$t('designStage.openScreen', { id: screen.id, name: screen.name })"
                    :data-testid="`design-open-${screen.id}`"
                  >
                    {{ $t('designStage.openInPrototype') }}
                  </router-link>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div
        v-show="tab === 'flow'"
        id="design-panel-flow"
        role="tabpanel"
        aria-labelledby="design-tab-flow"
        class="card panel flow-panel"
      >
        <h4 class="flow-title">{{ $t('designStage.flowTitle') }}</h4>
        <LoadingState v-if="flowLoader.loading.value" type="paragraph" />
        <ProblemAlert
          v-else-if="flowLoader.error.value"
          :error="flowLoader.error.value"
          retry
          @retry="flowLoader.run"
        />
        <MarkdownContent
          v-else-if="flowText !== undefined"
          :source="flowText"
          class="flow-content"
          data-testid="design-flow-content"
        />
        <p v-else-if="map.flow" class="t2 fine" data-testid="design-flow-missing">
          {{ $t('designStage.flowMissing', { flow: map.flow }) }}
        </p>
        <!-- ESP-10 section 8: the textual alternative of the flow, the screens in the order of TELAS. -->
        <ol class="flow" data-testid="design-flow">
          <li v-for="screen in map.screens" :key="screen.id" class="box">
            <span class="mono">{{ screen.id }}</span> {{ screen.name }}
          </li>
        </ol>
        <p class="t2 fine">
          {{ $t('designStage.flowFiles', { flow: map.flow, style: map.style }) }}
        </p>
      </div>
    </template>
  </section>
</template>

<style scoped>
/* Values copied from the canvas EtapaDesign artboard. */
.design-view {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.view-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.view-head h3 {
  margin: 0;
  font-size: 18px;
  line-height: 24px;
  font-weight: 700;
}

.empty {
  margin: 0;
  padding: 18px 22px;
  font-size: 13px;
}

.design-strip {
  margin: 0;
  padding: 14px 18px;
  display: flex;
  flex-wrap: wrap;
  gap: 10px 24px;
  align-items: center;
}

.strip-item {
  display: inline-flex;
  gap: 6px;
  align-items: baseline;
  font-size: 13px;
  flex-wrap: wrap;
}

.strip-item dd {
  margin: 0;
  font-weight: 600;
  display: inline-flex;
  gap: 6px;
  align-items: center;
  flex-wrap: wrap;
}

.notes,
.fine {
  margin: 0;
  font-size: 13px;
}

.panel {
  padding: 8px 0;
  overflow: hidden;
}

.empty-text {
  margin: 0;
  padding: 10px 18px;
  font-size: 13px;
}

.cell-id {
  display: block;
  font-size: 12px;
}

.cell-name {
  font-size: 15px;
}

.cell-sub {
  display: block;
  font-size: 12px;
}

.cell-file {
  font-size: 12px;
  overflow-wrap: anywhere;
}

.links,
.states {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.ib {
  min-height: 44px;
  text-decoration: none;
  white-space: nowrap;
}

.flow-panel {
  padding: 20px 22px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.flow-title {
  margin: 0;
  font-size: 18px;
  line-height: 24px;
}

.flow {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

.box {
  padding: 8px 12px;
  border-radius: 10px;
  border: 1px solid rgba(140, 150, 255, 0.24);
  background: rgba(255, 255, 255, 0.04);
  font-size: 13px;
}
</style>
