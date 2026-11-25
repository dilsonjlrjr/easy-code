<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useLoader } from '@/shared/composables/useLoader'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import { useCurrentProjectStore } from '@/features/projects/stores/currentProjectStore'
import { artifactsApi, type Artifact } from '@/features/artifacts/api/artifactsApi'
import ArtifactEditDialog from '@/features/artifacts/components/ArtifactEditDialog.vue'
import { isImposedAdr } from '@/features/artifacts/composables/artifactEditing'
import { useArtifactEditNotice } from '@/features/artifacts/composables/useArtifactEditNotice'
import type { StageViewProps } from '../stageViews'
import {
  adrOptions,
  apiOperations,
  arqComponents,
  arqRisks,
  c4Model,
  chosenOption,
  openApiAttachment,
} from './architectureView'

/**
 * ESP-08 (canvas EtapaArquitetura): Decisões (the ADR list with the chosen option of each, and the
 * detail with the options side by side) and Desenho (the C4 as text, components, risks and the operations
 * of the OpenAPI attachment, PL-24). Tasks stay in the TaskPlan of StageDetail; the exit criteria, waves
 * and artifacts in StageDetail too. Every text here is generated content shown as text (RNF-05).
 */
const props = defineProps<StageViewProps>()
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const store = useCurrentProjectStore()

const TABS = ['decisions', 'design'] as const
type Tab = (typeof TABS)[number]

const tab = computed<Tab>({
  get: () => {
    const value = route.query.arqTab
    return typeof value === 'string' && (TABS as readonly string[]).includes(value)
      ? (value as Tab)
      : 'decisions'
  },
  set: (value) => void router.replace({ query: { ...route.query, arqTab: value } }),
})

function onTabKey(event: KeyboardEvent): void {
  const index = TABS.indexOf(tab.value)
  const next =
    event.key === 'ArrowRight' || event.key === 'ArrowLeft'
      ? (index + 1) % TABS.length
      : event.key === 'Home'
        ? 0
        : event.key === 'End'
          ? TABS.length - 1
          : undefined
  const target = next === undefined ? undefined : TABS[next]
  if (target === undefined) return
  event.preventDefault()
  tab.value = target
  void nextTick(() => document.getElementById(`arq-tab-${target}`)?.focus())
}

// ------------------------------------------------------------------ data

const adrs = ref<Artifact[]>([])
const arq = ref<Artifact>()
const openapi = ref<string>()
const selected = ref<string>()

const loader = useLoader(async () => {
  const summaries = props.stage.artifacts.filter((artifact) =>
    ['ADR', 'ARQ'].includes(artifact.type),
  )
  const read = await Promise.all(
    summaries.map((summary) => artifactsApi.getArtifact(props.projectId, summary.id)),
  )
  adrs.value = read
    .filter((artifact) => artifact.type === 'ADR')
    .sort((a, b) => a.id.localeCompare(b.id))
  arq.value = read.find((artifact) => artifact.type === 'ARQ')
  if (!adrs.value.some((adr) => adr.id === selected.value)) selected.value = adrs.value[0]?.id
  openapi.value = undefined
  const path = openApiAttachment(arq.value?.attachments)
  if (arq.value && path) {
    openapi.value = await artifactsApi.getAttachment(
      props.projectId,
      arq.value.id,
      arq.value.version,
      path,
    )
  }
})

watch(
  () => [
    props.projectId,
    props.revision,
    props.stage.artifacts.map((a) => `${a.id}:${String(a.version)}`).join(),
  ],
  () => void loader.run(),
  { immediate: true },
)

const adr = computed(() => adrs.value.find((item) => item.id === selected.value))
const options = computed(() => (adr.value ? adrOptions(adr.value.header) : []))
const chosen = computed(() => (adr.value ? chosenOption(adr.value.header) : -1))
const c4Source = computed(() => {
  const header = typeof arq.value?.header.c4 === 'string' ? arq.value.header.c4 : ''
  if (header) return header
  return /```mermaid\s*\n([\s\S]*?)```/.exec(arq.value?.body ?? '')?.[1] ?? ''
})
const c4 = computed(() => c4Model(c4Source.value))
const components = computed(() => (arq.value ? arqComponents(arq.value.header) : []))
const risks = computed(() => (arq.value ? arqRisks(arq.value.header) : []))
const operations = computed(() => (openapi.value ? apiOperations(openapi.value) : []))
const openApiPath = computed(() => openApiAttachment(arq.value?.attachments))

function chosenText(item: Artifact): string {
  const index = chosenOption(item.header)
  const name = adrOptions(item.header)[index]?.nome
  if (isImposedAdr(item.header)) return t('stageArchitecture.imposed')
  return name ? t('stageArchitecture.chosenIs', { option: name }) : t('stageArchitecture.noChoice')
}

function list(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === 'string')
    : []
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

/** ESP-08 3: impact in text (baixo, médio, alto, crítico), never only by colour. */
function impactTone(impact: string): string {
  return { critico: 'crit', alto: 'crit', medio: 'warn', baixo: 'mute' }[impact] ?? 'mute'
}

// ------------------------------------------------------------------ imposed ADR (US-53 CA-5)

const createOpen = ref(false)
const canImpose = computed(() => {
  const project = store.project
  return (
    !!project &&
    project.state === 'in_progress' &&
    project.my_roles.some((role) => ['member', 'approver', 'lead'].includes(role)) &&
    ['running', 'returned', 'reopened'].includes(props.stage.state ?? '')
  )
})
const { notice, message, track, dismiss } = useArtifactEditNotice(
  () => props.projectId,
  async () => {
    await loader.run()
  },
)
</script>

<template>
  <!-- Layout: canvas EtapaArquitetura (tabs Decisões and Desenho). -->
  <section class="arq" aria-labelledby="arq-heading" data-testid="architecture-view">
    <h3 id="arq-heading" class="sr-only">{{ $t('stageArchitecture.heading') }}</h3>
    <div class="tabs" role="tablist" :aria-label="$t('stageArchitecture.tabsLabel')">
      <button
        v-for="value in TABS"
        :id="`arq-tab-${value}`"
        :key="value"
        type="button"
        role="tab"
        class="tab"
        :class="{ on: tab === value }"
        :aria-selected="tab === value ? 'true' : 'false'"
        :aria-controls="`arq-panel-${value}`"
        :tabindex="tab === value ? 0 : -1"
        @click="tab = value"
        @keydown="onTabKey"
      >
        {{ $t(`stageArchitecture.tabs.${value}`) }}
        <span class="chip mute">{{
          value === 'decisions' ? adrs.length : operations.length + components.length
        }}</span>
      </button>
    </div>
    <div role="status" class="sr-only">{{ message }}</div>
    <div v-if="notice" class="note" role="alert">
      <span class="note-text">{{ notice }}</span>
      <button type="button" class="ibtn" :aria-label="$t('common.close')" @click="dismiss">
        <CanvasIcon name="close" :size="18" />
      </button>
    </div>
    <LoadingState v-if="loader.loading.value && adrs.length === 0 && !arq" type="paragraph" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />

    <div
      v-if="tab === 'decisions'"
      id="arq-panel-decisions"
      role="tabpanel"
      aria-labelledby="arq-tab-decisions"
      class="arq-cols"
    >
      <section class="card arq-list" :aria-label="$t('stageArchitecture.listLabel')">
        <div class="arq-list-head">
          <span class="lb">{{ $t('stageArchitecture.adrs') }}</span>
          <button
            v-if="canImpose"
            type="button"
            class="btn btn-p"
            data-testid="architecture-impose"
            @click="createOpen = true"
          >
            {{ $t('stageArchitecture.impose') }}
          </button>
        </div>
        <p v-if="adrs.length === 0" class="t2">{{ $t('stageArchitecture.noAdrs') }}</p>
        <ul v-else class="plain-list" data-testid="architecture-adrs">
          <li v-for="item in adrs" :key="item.id">
            <button
              type="button"
              class="adr-item"
              :class="{ on: item.id === selected }"
              :aria-pressed="item.id === selected ? 'true' : 'false'"
              @click="selected = item.id"
            >
              <span class="mono t2">{{ item.id }}</span>
              <span class="adr-main">
                <span>{{ item.title }}</span>
                <span class="adr-meta">
                  <StateLabel machine="artifact" :state="item.state" />
                  <span class="t2">{{ chosenText(item) }}</span>
                </span>
              </span>
            </button>
          </li>
        </ul>
      </section>

      <section
        v-if="adr"
        class="card arq-detail"
        aria-labelledby="arq-adr-heading"
        data-testid="architecture-adr"
      >
        <div class="adr-head">
          <div>
            <span class="mono t2">
              {{ adr.id }} ·
              <router-link
                :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: adr.wave } }"
                >{{ adr.wave }}</router-link
              >
            </span>
            <h4 id="arq-adr-heading">
              <router-link
                :to="{ name: ROUTE_NAMES.artifact, params: { projectId, artifactId: adr.id } }"
                >{{ adr.title }}</router-link
              >
            </h4>
          </div>
          <StateLabel machine="artifact" :state="adr.state" />
        </div>
        <p v-if="isImposedAdr(adr.header)" class="t2" data-testid="architecture-imposed">
          {{
            adr.author.type === 'human'
              ? $t('stageArchitecture.imposedBy', { person: adr.author.edited_by?.name ?? '' })
              : $t('stageArchitecture.imposed')
          }}
        </p>
        <div v-if="text(adr.header.contexto)" class="adr-block">
          <span class="lb">{{ $t('stageArchitecture.context') }}</span>
          <p>{{ text(adr.header.contexto) }}</p>
        </div>
        <div v-if="options.length > 0" class="adr-block">
          <span class="lb">{{ $t('stageArchitecture.options', { n: options.length }) }}</span>
          <ul class="plain-list adr-options">
            <li
              v-for="(option, index) in options"
              :key="index"
              class="adr-option"
              :class="{ chosen: index === chosen }"
            >
              <span class="adr-option-head">
                <strong>{{ option.nome }}</strong>
                <span v-if="index === chosen" class="chip ok">{{
                  $t('stageArchitecture.chosen')
                }}</span>
              </span>
              <span v-for="(pro, p) in option.pros" :key="`p${p}`" class="adr-pro">
                <span aria-hidden="true">+</span>
                <span class="sr-only">{{ $t('stageArchitecture.pro') }}</span>{{ pro }}
              </span>
              <span v-for="(con, c) in option.contras" :key="`c${c}`" class="adr-con">
                <span aria-hidden="true">−</span>
                <span class="sr-only">{{ $t('stageArchitecture.con') }}</span>{{ con }}
              </span>
            </li>
          </ul>
        </div>
        <div v-if="text(adr.header.decisao)" class="adr-block">
          <span class="lb">{{ $t('stageArchitecture.decision') }}</span>
          <p>{{ text(adr.header.decisao) }}</p>
        </div>
        <div v-if="list(adr.header.consequencias).length > 0" class="adr-block">
          <span class="lb">{{ $t('stageArchitecture.consequences') }}</span>
          <ul class="adr-bullets">
            <li v-for="(item, index) in list(adr.header.consequencias)" :key="index">{{ item }}</li>
          </ul>
        </div>
        <div
          v-if="list(adr.header.motivada_por).length + list(adr.header.restricoes).length > 0"
          class="adr-block"
        >
          <span class="lb">{{ $t('stageArchitecture.motivatedBy') }}</span>
          <span class="adr-chips">
            <span
              v-for="item in [...list(adr.header.motivada_por), ...list(adr.header.restricoes)]"
              :key="item"
              class="chip mute mono"
              >{{ item }}</span
            >
          </span>
        </div>
        <div v-if="list(adr.header.skills).length > 0" class="adr-block">
          <span class="lb">{{ $t('stageArchitecture.skills') }}</span>
          <span class="t2">{{ list(adr.header.skills).join(', ') }}</span>
        </div>
        <p v-if="text(adr.header.substitui)" class="t2">
          {{ $t('stageArchitecture.replaces') }}
          <router-link
            :to="{
              name: ROUTE_NAMES.artifact,
              params: { projectId, artifactId: text(adr.header.substitui) },
            }"
            class="mono"
            >{{ text(adr.header.substitui) }}</router-link
          >
        </p>
      </section>
      <ArtifactEditDialog
        v-if="canImpose"
        v-model="createOpen"
        :project-id="projectId"
        operation="create"
        initial-type="ADR"
        @submitted="track"
      />
    </div>

    <div
      v-else
      id="arq-panel-design"
      role="tabpanel"
      aria-labelledby="arq-tab-design"
      class="arq-design"
    >
      <p v-if="!arq" class="t2">{{ $t('stageArchitecture.noArq') }}</p>
      <template v-else>
        <section
          class="card arq-card"
          aria-labelledby="arq-c4-heading"
          data-testid="architecture-c4"
        >
          <h4 id="arq-c4-heading">{{ $t('stageArchitecture.c4') }}</h4>
          <p v-if="!c4Source" class="t2">{{ $t('stageArchitecture.noC4') }}</p>
          <template v-else>
            <ul v-if="c4.elements.length > 0" class="c4-boxes plain-list">
              <li
                v-for="element in c4.elements"
                :key="element.id"
                class="c4-box"
                :class="{ ext: element.external }"
              >
                <strong>{{ element.label }}</strong>
                <span class="t2">{{ element.detail }}</span>
                <span class="chip" :class="element.external ? 'warn' : 'mute'">
                  {{
                    element.external
                      ? $t('stageArchitecture.external')
                      : $t(`stageArchitecture.kind.${element.kind}`, element.kind)
                  }}
                </span>
              </li>
            </ul>
            <ul v-if="c4.relations.length > 0" class="plain-list c4-relations">
              <li v-for="(relation, index) in c4.relations" :key="index" class="li">
                {{ $t('stageArchitecture.relation', { ...relation }) }}
              </li>
            </ul>
            <details class="c4-source">
              <summary>{{ $t('stageArchitecture.c4Source') }}</summary>
              <pre class="mono">{{ c4Source }}</pre>
            </details>
          </template>
        </section>

        <section
          v-if="components.length > 0"
          class="card arq-card"
          aria-labelledby="arq-components-heading"
        >
          <h4 id="arq-components-heading">{{ $t('stageArchitecture.components') }}</h4>
          <div class="tbl-wrap">
            <table class="tbl">
              <thead>
                <tr>
                  <th scope="col">{{ $t('stageArchitecture.component') }}</th>
                  <th scope="col">{{ $t('stageArchitecture.responsibility') }}</th>
                  <th scope="col">{{ $t('stageArchitecture.justifiedBy') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="component in components" :key="component.nome">
                  <td>
                    <strong>{{ component.nome }}</strong>
                    <span v-if="component.tecnologia" class="t2">
                      · {{ component.tecnologia }}</span
                    >
                  </td>
                  <td>{{ component.responsabilidade }}</td>
                  <td class="mono">
                    {{
                      component.atende.length > 0
                        ? component.atende.join(', ')
                        : $t('stageArchitecture.notJustified')
                    }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section
          class="card arq-card"
          aria-labelledby="arq-api-heading"
          data-testid="architecture-api"
        >
          <div class="arq-card-head">
            <h4 id="arq-api-heading">{{ $t('stageArchitecture.api') }}</h4>
            <router-link
              v-if="openApiPath"
              :to="{
                name: ROUTE_NAMES.artifact,
                params: { projectId, artifactId: arq.id },
                hash: '#attachments',
              }"
              class="mono"
            >
              {{ openApiPath }}
            </router-link>
          </div>
          <p v-if="!openApiPath" class="t2">{{ $t('stageArchitecture.noApi') }}</p>
          <ul v-else-if="operations.length > 0" class="plain-list">
            <li
              v-for="operation in operations"
              :key="`${operation.method} ${operation.path}`"
              class="li api-op"
            >
              <span class="mth mono">{{ operation.method }}</span>
              <span class="mono">{{ operation.path }}</span>
              <span class="t2">{{ operation.summary }}</span>
            </li>
          </ul>
          <p v-else class="t2">{{ $t('stageArchitecture.noOperations') }}</p>
        </section>

        <section class="card arq-card" aria-labelledby="arq-risks-heading">
          <h4 id="arq-risks-heading">{{ $t('stageArchitecture.risks') }}</h4>
          <p v-if="risks.length === 0" class="t2">{{ $t('stageArchitecture.noRisks') }}</p>
          <ul v-else class="plain-list">
            <li v-for="risk in risks" :key="risk.id" class="li risk">
              <span class="chip" :class="impactTone(risk.impacto)">{{
                $t(`stageArchitecture.impact.${risk.impacto}`, risk.impacto)
              }}</span>
              <span>{{ risk.id }} · {{ risk.descricao }}</span>
              <span class="t2">{{
                $t('stageArchitecture.mitigation', { text: risk.mitigacao })
              }}</span>
            </li>
          </ul>
        </section>
      </template>
    </div>
  </section>
</template>

<style scoped>
.arq {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.arq-cols {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  align-items: flex-start;
}

.arq-list {
  flex: 2 1 320px;
  min-width: 0;
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.arq-detail {
  flex: 3 1 440px;
  min-width: 0;
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.arq-list-head,
.adr-head,
.arq-card-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.arq-detail h4,
.arq-card h4 {
  margin: 0;
  font-size: 18px;
  line-height: 24px;
  font-weight: 700;
}

.plain-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.adr-item {
  display: grid;
  grid-template-columns: minmax(64px, max-content) 1fr;
  gap: 12px;
  width: 100%;
  min-height: 44px;
  padding: 10px 12px;
  border: 1px solid transparent;
  border-radius: 10px;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.adr-item:hover {
  background: rgba(255, 255, 255, 0.05);
}

.adr-item.on {
  border-color: rgba(140, 150, 255, 0.35);
  background: rgba(99, 102, 241, 0.14);
}

.adr-main {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  overflow-wrap: anywhere;
}

.adr-meta {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  align-items: center;
  font-size: 13px;
}

.adr-block {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.adr-block p {
  margin: 0;
  overflow-wrap: anywhere;
}

.adr-options {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 10px;
}

.adr-option {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 12px;
  border: 1px solid rgba(140, 150, 255, 0.16);
  border-radius: 10px;
  font-size: 13px;
}

.adr-option.chosen {
  border-color: rgba(34, 197, 94, 0.5);
}

.adr-option-head {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
  font-size: 14px;
}

.adr-pro > span:first-child {
  color: #86efac;
}

.adr-con > span:first-child {
  color: #fca5a5;
}

.adr-bullets {
  margin: 0;
  padding-left: 18px;
}

.adr-chips {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.arq-design {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.arq-card {
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.c4-boxes {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 10px;
}

.c4-box {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 12px;
  border: 1px solid rgba(140, 150, 255, 0.28);
  border-radius: 10px;
  font-size: 13px;
}

.c4-box.ext {
  border-style: dashed;
}

.c4-source pre {
  margin: 8px 0 0;
  white-space: pre-wrap;
  font-size: 12px;
}

.c4-source summary {
  min-height: 44px;
  display: flex;
  align-items: center;
  cursor: pointer;
}

.tbl-wrap {
  overflow-x: auto;
}

.api-op,
.risk {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  align-items: center;
  overflow-wrap: anywhere;
}

.mth {
  min-width: 56px;
  font-weight: 700;
  color: #a78bfa;
}
</style>
