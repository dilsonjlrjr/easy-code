<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { bridge, toUIError } from '@/bridge'
import ErrorMessage from '@/components/ErrorMessage.vue'
import OpenWebButton from '@/components/OpenWebButton.vue'
import { useLabel } from '@/i18n'
import { navigate, openWeb, store } from '@/store'
import type { PendingItem, PendingItemsPage, Project, ProjectsPage, UIError } from '@/types'

// ESP-28 Painel: projects where I have a role with state and current stage (US-46 CA-1), pending items
// and recent notices; every item that asks for action opens the web.
const { t } = useI18n()
const label = useLabel()
const projects = ref<Project[] | null>(null)
const pending = ref<PendingItem[] | null>(null)
const error = ref<UIError | null>(null)
let reloadTimer: ReturnType<typeof setTimeout> | undefined

const names = computed(() => new Map((projects.value ?? []).map((p) => [p.id, p.name])))

async function load(): Promise<void> {
  try {
    const [projectsPage, pendingPage] = await Promise.all([
      bridge().call<ProjectsPage>('Projects'),
      bridge().call<PendingItemsPage>('PendingItems'),
    ])
    projects.value = projectsPage.items
    pending.value = pendingPage.items
    error.value = null
  } catch (e) {
    error.value = toUIError(e)
  }
}

onMounted(load)
onBeforeUnmount(() => {
  clearTimeout(reloadTimer)
})
// A notice, a gap or a reconnection: read again from the server (debounced).
watch(
  () => store.dashboardVersion,
  () => {
    clearTimeout(reloadTimer)
    reloadTimer = setTimeout(() => void load(), 300)
  },
)

function stateLabel(project: Project): string {
  return label(`states.project.${project.state}`, project.state)
}

function stageLabel(stage: string): string {
  return label(`stages.${stage}`, stage)
}

function currentStages(project: Project): string {
  const stages = project.summary?.current_stages ?? []
  if (stages.length === 0) return t('dashboard.noCurrentStage')
  return t('dashboard.currentStages', {
    stages: stages
      .map((s) => `${stageLabel(s.stage)} (${label(`states.stage.${s.state}`, s.state)})`)
      .join(', '),
  })
}

function noticeLine(type: string, projectId?: string): string {
  const notice = label(`notices.${type}`, t('notices.other', { type }))
  const project = (projectId && names.value.get(projectId)) ?? t('common.none')
  return t('notices.line', { notice, project })
}

function openPending(item: PendingItem): Promise<unknown> {
  return bridge().call(
    'OpenPendingItem',
    item.type,
    item.project.id,
    item.target.type,
    item.target.id,
  )
}
</script>

<template>
  <section aria-labelledby="view-title">
    <h1 id="view-title" tabindex="-1">{{ t('dashboard.title') }}</h1>
    <p class="hint">{{ t('app.readOnly') }}</p>
    <ErrorMessage :error="error" />

    <section aria-labelledby="projects-title">
      <h2 id="projects-title">{{ t('dashboard.projects') }}</h2>
      <p v-if="projects === null && !error">{{ t('common.loading') }}</p>
      <p v-else-if="projects?.length === 0">{{ t('dashboard.noProjects') }}</p>
      <ul v-else class="cards">
        <li v-for="project in projects" :key="project.id" class="card">
          <h3>
            <button
              type="button"
              class="link-button"
              @click="navigate({ name: 'project', projectId: project.id })"
            >
              {{ project.name }}
            </button>
          </h3>
          <p>{{ t('common.state', { state: stateLabel(project) }) }}</p>
          <p>{{ currentStages(project) }}</p>
          <p v-if="project.summary">
            {{
              t('dashboard.waiting', {
                gates: project.summary.gates_awaiting,
                questions: project.summary.open_questions,
              })
            }}
          </p>
          <OpenWebButton
            :item="project.name"
            :open="() => openWeb({ kind: 'project', project_id: project.id })"
          />
        </li>
      </ul>
    </section>

    <section aria-labelledby="pending-title">
      <h2 id="pending-title">{{ t('dashboard.pending') }}</h2>
      <p v-if="pending?.length === 0">{{ t('dashboard.noPending') }}</p>
      <ul v-else-if="pending" class="rows">
        <li
          v-for="item in pending"
          :key="`${item.project.id}/${item.target.type}/${item.target.id}`"
        >
          <span>
            {{
              t('dashboard.pendingItem', {
                type: t(`dashboard.pendingTypes.${item.type}`),
                project: item.project.name,
              })
            }}
          </span>
          <span v-if="item.stage">{{
            t('dashboard.pendingStage', { stage: stageLabel(item.stage) })
          }}</span>
          <span v-if="item.title">{{ item.title }}</span>
          <OpenWebButton :item="item.title ?? item.target.id" :open="() => openPending(item)" />
        </li>
      </ul>
    </section>

    <section aria-labelledby="recent-title">
      <h2 id="recent-title">{{ t('dashboard.recent') }}</h2>
      <p v-if="store.recent.length === 0">{{ t('dashboard.noRecent') }}</p>
      <ul v-else class="rows">
        <li v-for="notice in store.recent" :key="notice.key">
          {{ noticeLine(notice.type, notice.projectId) }}
        </li>
      </ul>
    </section>
  </section>
</template>
