<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { createEventStream, type CloudEvent } from '@/shared/events/eventStream'
import { describeEvent } from '@/shared/events/eventDescriptions'
import { useFormat } from '@/shared/format/useFormat'
import { noticesUrl } from '@/features/pending-items/api/pendingItemsApi'
import type { Project } from '@/features/projects/api/projectsApi'

/**
 * ESP-03 "Atividade recente" (ER-03-11, PL-03): the first load replays the last 24 h of
 * `watchNotices` with `since=0`, then the list follows live (ER-03-5). Newest first, the event type
 * in text, the project and the age. Only domain events reach this stream (sse.md 3).
 */
const props = defineProps<{ projects: Project[] }>()

const SHOWN = 10
const { t } = useI18n()
const format = useFormat()
const events = ref<CloudEvent[]>([])

const items = computed(() =>
  [...events.value]
    .sort((left, right) => Date.parse(right.time) - Date.parse(left.time))
    .slice(0, SHOWN),
)

function projectName(event: CloudEvent): string | undefined {
  return props.projects.find((project) => project.id === event.projectid)?.name
}

function receive(event: CloudEvent): void {
  if (events.value.some((item) => item.id === event.id)) return
  // Keep a bounded window; the list shows only the newest ones.
  events.value = [...events.value, event].slice(-50)
}

const stream = createEventStream({
  url: noticesUrl(),
  since: 0,
  order: 'position',
  onEvent: receive,
})

onMounted(() => {
  stream.start()
})
onBeforeUnmount(() => {
  stream.stop()
})

const translate = (key: string, values?: Record<string, unknown>) => t(key, values ?? {})

/** Canvas "Atividade recente": the dot takes the color of the state family the event leads to. */
function dotColor(event: CloudEvent): string {
  const type = event.type.toLowerCase()
  if (/reject|fail|escalat|finding/.test(type)) return '#EF4444'
  if (/question|awaiting_user/.test(type)) return '#A855F7'
  if (/approved|completed|published|closed|accepted/.test(type)) return '#22C55E'
  if (/gate|paus|budget/.test(type)) return '#F59E0B'
  return '#818CF8'
}
</script>

<template>
  <section
    class="card activity"
    aria-labelledby="home-activity-heading"
    data-testid="home-activity"
  >
    <h2 id="home-activity-heading" class="activity-title">{{ $t('home.activity') }}</h2>
    <p v-if="items.length === 0" class="t2 activity-empty">{{ $t('home.noActivity') }}</p>
    <ol v-else class="activity-list">
      <li v-for="event in items" :key="event.id" class="activity-item">
        <span class="activity-rail" aria-hidden="true">
          <span class="activity-dot" :style="{ background: dotColor(event) }" />
          <span class="activity-line" />
        </span>
        <span class="activity-text">
          <span>{{ describeEvent(translate, event) }}</span>
          <span class="t2 activity-meta">
            <template v-if="event.projectid">
              <router-link
                :to="{ name: ROUTE_NAMES.project, params: { projectId: event.projectid } }"
              >
                {{ projectName(event) ?? $t('home.aProject') }}
              </router-link>
              ·
            </template>
            {{ format.age(event.time) }}
          </span>
        </span>
      </li>
    </ol>
  </section>
</template>

<style scoped>
.activity {
  padding: 18px 22px;
}

.activity-title {
  margin-bottom: 8px;
}

.activity-empty {
  margin: 0;
}

.activity-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.activity-item {
  display: flex;
  gap: 14px;
}

.activity-rail {
  display: flex;
  flex-direction: column;
  align-items: center;
  width: 12px;
  flex: none;
}

.activity-dot {
  width: 12px;
  height: 12px;
  border-radius: 6px;
  margin-top: 4px;
}

.activity-line {
  flex: 1;
  width: 2px;
  background: rgba(140, 150, 255, 0.22);
  margin: 4px 0;
}

.activity-text {
  display: flex;
  flex-direction: column;
  padding-bottom: 14px;
}

.activity-meta {
  font-size: 12px;
}
</style>
