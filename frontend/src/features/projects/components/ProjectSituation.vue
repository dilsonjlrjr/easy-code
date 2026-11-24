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
  <!-- Look: canvas "Projetos", the "Agora" line of a project card (13px, joined by " · "). -->
  <p v-if="project.state === 'created'" class="situation">
    {{ $t('projects.list.notStarted') }}
  </p>
  <ul v-else-if="summary" class="situation situation-list" data-testid="project-situation">
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

<style scoped>
.situation {
  margin: 0;
  font-size: 13px;
  line-height: 20px;
}

.situation-list {
  list-style: none;
  padding: 0;
}

.situation-list li {
  display: inline;
}

.situation-list li + li::before {
  content: ' · ';
}
</style>
