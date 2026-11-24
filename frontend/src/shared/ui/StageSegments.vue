<script setup lang="ts">
import { computed } from 'vue'
import type { StageState } from '@/shared/api/contract'
import type { ProjectSummary } from '@/features/projects/api/projectsApi'

/**
 * Canvas "Indicador e progresso": one bar per stage of the workflow, with the color of its state.
 * Approved stages come first (they are approved in workflow order), then the current ones.
 */
const props = defineProps<{ summary?: ProjectSummary; total?: number }>()

const STATE_COLORS: Partial<Record<StageState, string>> = {
  approved: '#22C55E',
  running: '#818CF8',
  awaiting_gate: '#F59E0B',
  returned: '#EF4444',
  reopened: '#EF4444',
}
const EMPTY = 'rgba(140,150,255,.22)'

const segments = computed(() => {
  const total = props.summary?.stages_total ?? props.total ?? 9
  const approved = props.summary?.approved_stages ?? 0
  const current = (props.summary?.current_stages ?? []).map(
    (item) => STATE_COLORS[item.state] ?? EMPTY,
  )
  const colors = [...Array<string>(approved).fill(STATE_COLORS.approved ?? EMPTY), ...current]
  while (colors.length < total) colors.push(EMPTY)
  return colors.slice(0, total)
})
</script>

<template>
  <span class="stage-segments" aria-hidden="true">
    <span
      v-for="(color, index) in segments"
      :key="index"
      class="sg"
      :style="{ background: color }"
    />
  </span>
</template>

<style scoped>
.stage-segments {
  display: flex;
  gap: 3px;
}
</style>
