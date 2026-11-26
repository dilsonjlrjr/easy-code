<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { bridge, toUIError } from '@/bridge'
import ErrorMessage from '@/components/ErrorMessage.vue'
import OpenWebButton from '@/components/OpenWebButton.vue'
import { useLabel, useNoticeLabel } from '@/i18n'
import { navigate, openWeb, store } from '@/store'
import { chip } from '@/tone'
import type { PendingItem, PendingItemsPage, Project, ProjectsPage, Status, UIError } from '@/types'

// ESP-28 Início (canvas Cliente · Início): projects where I have a role with state and current stage (US-46 CA-1), pending items
// and recent notices; every item that asks for action opens the web.
defineProps<{ status?: Status | null }>()
const { t } = useI18n()
const label = useLabel()
const noticeLabel = useNoticeLabel()
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
    store.pendingTotal = pendingPage.total
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

// The four counters of the canvas, from the pending items (what waits for the person).
const KPI_TYPES = ['gate', 'question', 'finding_escalated', 'budget'] as const
const kpis = computed(() =>
  KPI_TYPES.map((type) => ({
    type,
    count: (pending.value ?? []).filter((item) => item.type === type).length,
  })),
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
  const notice = noticeLabel(type)
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
  <section class="view" aria-labelledby="view-title">
    <div class="head">
      <div>
        <h1 id="view-title" tabindex="-1">
          {{
            status?.user?.name
              ? t('dashboard.greeting', { name: status.user.name })
              : t('dashboard.title')
          }}
        </h1>
        <p class="t2">{{ t('dashboard.intro') }}</p>
      </div>
    </div>
    <ErrorMessage :error="error" />

    <ul class="kpis">
      <li v-for="kpi in kpis" :key="kpi.type" class="card kpi">
        <span class="ki" :class="chip('pending', kpi.type).replace('chip ', '')" aria-hidden="true">
          <svg
            v-if="kpi.type === 'gate'"
            width="22"
            height="22"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.7"
          >
            <path d="M4 20V8l8-4 8 4v12" />
            <path d="M9 20v-6h6v6" />
          </svg>
          <svg
            v-else-if="kpi.type === 'question'"
            width="22"
            height="22"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.7"
          >
            <circle cx="12" cy="12" r="9" />
            <path d="M9.5 9.5a2.5 2.5 0 1 1 3.5 2.3c-.6.3-1 .9-1 1.6v.6M12 17h.01" />
          </svg>
          <svg
            v-else-if="kpi.type === 'finding_escalated'"
            width="22"
            height="22"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.7"
          >
            <path d="M12 3 2 20h20L12 3Z" />
            <path d="M12 10v4M12 17h.01" />
          </svg>
          <svg
            v-else
            width="22"
            height="22"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.7"
          >
            <circle cx="12" cy="12" r="9" />
            <path
              d="M12 7v10M15 9.5c0-1.4-1.3-2.5-3-2.5s-3 1-3 2.3c0 3 6 1.7 6 4.7 0 1.4-1.3 2.5-3 2.5s-3-1.1-3-2.5"
            />
          </svg>
        </span>
        <span>
          <span class="t2 small">{{ t(`dashboard.kpis.${kpi.type}`) }}</span><br />
          <span class="kpi-value">{{ pending === null ? '—' : kpi.count }}</span>
        </span>
      </li>
    </ul>

    <div class="cols">
      <section class="card col-main" aria-labelledby="projects-title">
        <h2 id="projects-title">{{ t('dashboard.projects') }}</h2>
        <p v-if="projects === null && !error" class="empty">{{ t('common.loading') }}</p>
        <p v-else-if="projects?.length === 0" class="empty">{{ t('dashboard.noProjects') }}</p>
        <ul v-else class="rows">
          <li v-for="project in projects" :key="project.id" class="project">
            <span class="grow">
              <span class="title-line">
                <h3>
                  <button
                    type="button"
                    class="link-button"
                    @click="navigate({ name: 'project', projectId: project.id })"
                  >
                    {{ project.name }}
                  </button>
                </h3>
                <span :class="chip('project', project.state)">{{ stateLabel(project) }}</span>
              </span>
              <span class="t2 small">{{ currentStages(project) }}</span>
              <span v-if="project.summary" class="t2 small">
                {{
                  t('dashboard.waiting', {
                    gates: project.summary.gates_awaiting,
                    questions: project.summary.open_questions,
                  })
                }}
              </span>
            </span>
            <OpenWebButton
              small
              :item="project.name"
              :open="() => openWeb({ kind: 'project', project_id: project.id })"
            />
          </li>
        </ul>
      </section>

      <div class="col-side">
        <section class="card" aria-labelledby="pending-title">
          <div class="card-head">
            <h2 id="pending-title">{{ t('dashboard.pending') }}</h2>
            <span v-if="pending?.length" class="chip info">{{ pending.length }}</span>
          </div>
          <p v-if="pending?.length === 0" class="empty">{{ t('dashboard.noPending') }}</p>
          <ul v-else-if="pending" class="rows">
            <li
              v-for="item in pending"
              :key="`${item.project.id}/${item.target.type}/${item.target.id}`"
              class="top"
            >
              <span class="grow">
                <span :class="chip('pending', item.type)">{{
                  t(`dashboard.pendingTypes.${item.type}`)
                }}</span>
                <span v-if="item.title" style="font-weight: 600">{{ item.title }}</span>
                <span class="t2 small">
                  {{ item.project.name
                  }}<template v-if="item.stage">
                    · {{ t('dashboard.pendingStage', { stage: stageLabel(item.stage) }) }}</template
                  >
                </span>
              </span>
              <OpenWebButton
                small
                :item="item.title ?? item.target.id"
                :open="() => openPending(item)"
              />
            </li>
          </ul>
        </section>

        <section class="card" aria-labelledby="recent-title">
          <h2 id="recent-title">{{ t('dashboard.recent') }}</h2>
          <p v-if="store.recent.length === 0" class="empty">{{ t('dashboard.noRecent') }}</p>
          <ul v-else class="rows">
            <li v-for="notice in store.recent" :key="notice.key" class="small">
              {{ noticeLine(notice.type, notice.projectId) }}
            </li>
          </ul>
        </section>
      </div>
    </div>
  </section>
</template>
