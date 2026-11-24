<script setup lang="ts">
import { computed } from 'vue'
import { CANVAS_ICONS, type CanvasIconName } from './canvasIcons'

/**
 * Stroke icons drawn exactly as in the interface canvas (24 × 24 grid, `currentColor`).
 * Decorative by default; pass `label` when the icon is the only content of a control.
 */
const props = withDefaults(
  defineProps<{ name: CanvasIconName; size?: number; strokeWidth?: number; label?: string }>(),
  { size: 20, strokeWidth: 1.7, label: undefined },
)

const shapes = computed(() => CANVAS_ICONS[props.name])
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    :stroke-width="strokeWidth"
    :aria-hidden="label ? undefined : 'true'"
    :role="label ? 'img' : undefined"
    :aria-label="label"
  >
    <component :is="shape.tag" v-for="(shape, index) in shapes" :key="index" v-bind="shape.attrs" />
  </svg>
</template>
