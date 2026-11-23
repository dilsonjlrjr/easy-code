<script setup lang="ts">
import { computed } from 'vue'
import type { Project } from '../api/projectsApi'

/**
 * ESP-04 3 / ESP-03 3 (US-50 CA-1): the current situation of a project in short sentences: each
 * current stage with its state, the agents running and what waits (gates, questions).
 */
const props = defineProps<{ project: Project }>()

const summary = computed(() => props.project.summary)
</script>

<template>
  <p v-if="project.state === 'created'" class="text-body-2">
    {{ $t('projects.list.notStarted') }}
  </p>
  <ul v-else-if="summary" class="plain-list text-body-2" data-testid="project-situation">
    <li v-for="item in summary.current_stages ?? []" :key="item.stage">
      {{
        $t('dashboard.currentStage', {
          stage: $t(`stages.${item.stage}`),
          state: $t(`states.stage.${item.state}`),
        })
      }}
    </li>
    <li v-for="wave in summary.active_waves ?? []" :key="wave.wave">
      {{
        $t('dashboard.agentRunning', {
          agent: $t(`agents.${wave.agent}`),
          state: $t(`states.wave.${wave.state}`),
        })
      }}
    </li>
    <li v-if="summary.gates_awaiting > 0">
      {{ $t('dashboard.gatesAwaiting', { n: summary.gates_awaiting }) }}
    </li>
    <li v-if="summary.open_questions > 0">
      {{ $t('projects.list.openQuestions', { count: summary.open_questions }) }}
    </li>
  </ul>
</template>
