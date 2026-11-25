<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useLoader } from '@/shared/composables/useLoader'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { artifactsApi, type Artifact } from '@/features/artifacts/api/artifactsApi'
import {
  referencedArtifact,
  structuredArtifact,
  type DicHeader,
} from '@/features/artifacts/composables/structuredArtifact'
import type { StageViewProps } from '../stageViews'
import { entities, relationships, type EntityColumn } from './dataEntities'

/**
 * ESP-09 section 3 "Modelo" (canvas EtapaDados "Modelo de dados"): the entities of the DIC header,
 * one card per table with its columns, keys and personal data marked in text, the US or ADR that
 * requires it (PL-32) and the relationships written as cardinality (ESP-09 section 8). The full
 * dictionary, indexes and persistence stay on the DIC and PERSIST artifact pages.
 */
const props = defineProps<StageViewProps>()
const { t } = useI18n()

const dic = ref<Artifact>()
const persist = ref<Artifact>()

const dicSummary = computed(() => props.stage.artifacts.find((item) => item.type === 'DIC'))
const persistSummary = computed(() => props.stage.artifacts.find((item) => item.type === 'PERSIST'))

const loader = useLoader(async () => {
  const [dicRead, persistRead] = await Promise.all([
    dicSummary.value ? artifactsApi.getArtifact(props.projectId, dicSummary.value.id) : undefined,
    persistSummary.value
      ? artifactsApi.getArtifact(props.projectId, persistSummary.value.id).catch(() => undefined)
      : undefined,
  ])
  dic.value = dicRead
  persist.value = persistRead
})

watch(
  () => [props.projectId, props.revision, dicSummary.value?.version],
  () => void loader.run(),
  { immediate: true },
)

const header = computed<DicHeader | undefined>(() => {
  const read = dic.value && structuredArtifact('DIC', dic.value.header)
  return read?.type === 'DIC' ? read.header : undefined
})
const database = computed(() => {
  const read = persist.value && structuredArtifact('PERSIST', persist.value.header)
  const banco = read?.type === 'PERSIST' ? read.header.banco : undefined
  return banco ? `${banco.sgbd} ${banco.versao}`.trim() : undefined
})
const tables = computed(() => (header.value ? entities(header.value) : []))
const links = computed(() => (header.value ? relationships(header.value) : []))

function columnNotes(column: EntityColumn): string {
  const notes = [column.type]
  if (column.primaryKey) notes.push(t('stageData.primaryKey'))
  if (column.references) notes.push(t('stageData.foreignKey', { table: column.references }))
  if (column.unique) notes.push(t('stageData.unique'))
  if (column.nullable && !column.primaryKey) notes.push(t('stageData.nullable'))
  if (column.personal) notes.push(t('stageData.personal'))
  return notes.join(' · ')
}

function originRoute(origin: string) {
  const artifactId = referencedArtifact(origin)
  return artifactId
    ? { name: ROUTE_NAMES.artifact, params: { projectId: props.projectId, artifactId } }
    : undefined
}
</script>

<template>
  <!-- Look: canvas EtapaDados "Modelo de dados" (`.card`, `.ent` table cards, `.col` rows). -->
  <section
    class="card data-model"
    aria-labelledby="data-entities-heading"
    data-testid="data-entities"
  >
    <div class="dm-head">
      <h3 id="data-entities-heading">
        {{ $t('stageData.title') }}
        <span v-if="tables.length" class="chip mute">{{ tables.length }}</span>
      </h3>
      <span v-if="header?.modelo_dbml || database" class="t2 dm-source">
        <span v-if="header?.modelo_dbml" class="mono">{{ header.modelo_dbml }}</span>
        <template v-if="header?.modelo_dbml && database"> · </template>
        <span v-if="database">{{ database }}</span>
      </span>
    </div>

    <LoadingState v-if="loader.loading.value && !loader.loaded.value" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run()"
    />
    <p v-else-if="!dicSummary" class="t2 dm-text" data-testid="data-entities-empty">
      {{ $t('stageData.empty') }}
    </p>
    <template v-else>
      <p v-if="dicSummary.state === 'draft'" class="note dm-text" role="note">
        {{ $t('stageData.draft') }}
      </p>
      <p v-if="!header" class="t2 dm-text">{{ $t('stageData.unreadable') }}</p>
      <ul v-else class="ent-list" :aria-label="$t('stageData.title')">
        <li
          v-for="table in tables"
          :key="table.name"
          class="ent"
          :data-testid="`entity-${table.name}`"
        >
          <h4 class="ent-head">
            <span class="mono">{{ table.name }}</span>
            <span v-if="table.origins.length" class="ent-origins">
              <span class="sr-only">{{ $t('stageData.requiredBy') }}</span>
              <template v-for="origin in table.origins" :key="origin">
                <router-link
                  v-if="originRoute(origin)"
                  :to="originRoute(origin)!"
                  class="chip mute mono ent-origin"
                >
                  {{ origin }}
                </router-link>
                <span v-else class="chip mute mono">{{ origin }}</span>
              </template>
            </span>
          </h4>
          <p class="t2 ent-description">{{ table.description }}</p>
          <ul class="ent-columns" :aria-label="$t('stageData.columnsOf', { table: table.name })">
            <li v-for="column in table.columns" :key="column.name" class="col">
              <span class="mono">{{ column.name }}</span>
              <span class="t2 col-notes">{{ columnNotes(column) }}</span>
            </li>
          </ul>
        </li>
      </ul>

      <div v-if="header" class="dm-links">
        <h4 class="lb">{{ $t('stageData.relationships') }}</h4>
        <p v-if="links.length === 0" class="t2 dm-text">{{ $t('stageData.noRelationships') }}</p>
        <ul v-else class="dm-link-list" data-testid="data-relationships">
          <li v-for="link in links" :key="`${link.child}-${link.constraint}`" class="t2">
            {{
              $t(link.cardinality === 'one_to_one' ? 'stageData.oneToOne' : 'stageData.oneToMany', {
                parent: link.parent,
                child: link.child,
                columns: link.columns.join(', '),
              })
            }}
          </li>
        </ul>
      </div>

      <div class="dm-actions">
        <router-link
          :to="{
            name: ROUTE_NAMES.artifact,
            params: { projectId: props.projectId, artifactId: dicSummary.id },
          }"
          class="btn"
        >
          {{ $t('stageData.openDic') }}
        </router-link>
      </div>
    </template>
  </section>
</template>

<style scoped>
.data-model {
  padding: 20px 22px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.dm-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.dm-head h3 {
  margin: 0;
  font-size: 18px;
  line-height: 24px;
  font-weight: 700;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.dm-source {
  font-size: 13px;
}

.dm-text {
  margin: 0;
  font-size: 13px;
}

.ent-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}

.ent {
  flex: 1 1 220px;
  min-width: 0;
  padding: 0;
  border-radius: 12px;
  border: 1px solid rgba(140, 150, 255, 0.3);
  background: rgba(20, 26, 66, 0.95);
  overflow: hidden;
}

.ent-head {
  margin: 0;
  padding: 10px 14px;
  font-size: 14px;
  background: rgba(99, 102, 241, 0.18);
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.ent-origins {
  display: inline-flex;
  gap: 6px;
  flex-wrap: wrap;
}

.ent-origin {
  position: relative;
  text-decoration: none;
}

/* The chip stays canvas-sized; the hit area grows to 44px (WCAG 2.5.8, brief rule 3). */
.ent-origin::after {
  content: '';
  position: absolute;
  inset: -10px -4px;
}

.ent-description {
  margin: 0;
  padding: 6px 14px;
  font-size: 12px;
  border-top: 1px solid rgba(255, 255, 255, 0.06);
}

.ent-columns {
  list-style: none;
  margin: 0;
  padding: 0;
}

.col {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  padding: 6px 14px;
  font-size: 13px;
  border-top: 1px solid rgba(255, 255, 255, 0.06);
}

.col-notes {
  text-align: right;
  overflow-wrap: anywhere;
}

.dm-links {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding-top: 12px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.dm-links h4 {
  margin: 0;
}

.dm-link-list {
  margin: 0;
  padding-left: 18px;
  font-size: 13px;
}

.dm-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
</style>
