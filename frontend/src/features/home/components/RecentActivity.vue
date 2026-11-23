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
</script>

<template>
  <section aria-labelledby="home-activity-heading" class="mb-8" data-testid="home-activity">
    <h2 id="home-activity-heading" class="text-h6 mb-2">{{ $t('home.activity') }}</h2>
    <p v-if="items.length === 0" class="text-body-2">{{ $t('home.noActivity') }}</p>
    <ol v-else class="plain-list">
      <li v-for="event in items" :key="event.id" class="mb-2 text-body-2">
        <span class="font-weight-medium">{{ describeEvent(translate, event) }}</span>
        <template v-if="event.projectid">
          ·
          <router-link :to="{ name: ROUTE_NAMES.project, params: { projectId: event.projectid } }">
            {{ projectName(event) ?? $t('home.aProject') }}
          </router-link>
        </template>
        · {{ format.age(event.time) }}
      </li>
    </ol>
  </section>
</template>
