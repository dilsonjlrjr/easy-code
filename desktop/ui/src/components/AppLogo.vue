<script setup lang="ts">
import { useId } from 'vue'

// The EasyCode mark of the canvas: blue-violet chevrons around the binary digits, in the text color of
// the theme. Decorative: the name is always next to it in text.
withDefaults(defineProps<{ size?: number }>(), { size: 48 })
const gradient = `logo-${useId()}`
// The binary digits of the mark (drawing, not text to translate).
const DIGITS = ['0101', '0010'] as const
</script>

<template>
  <svg :width="size" :height="Math.round((size * 56) / 92)" viewBox="0 0 92 56" aria-hidden="true">
    <defs>
      <linearGradient :id="gradient" x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stop-color="#3B82F6" />
        <stop offset="1" stop-color="#8B5CF6" />
      </linearGradient>
    </defs>
    <path
      d="M22 4 6 28l16 24"
      fill="none"
      :stroke="`url(#${gradient})`"
      stroke-width="7"
      stroke-linecap="round"
      stroke-linejoin="round"
    />
    <path
      d="M70 4l16 24-16 24"
      fill="none"
      :stroke="`url(#${gradient})`"
      stroke-width="7"
      stroke-linecap="round"
      stroke-linejoin="round"
    />
    <text x="46" y="25" text-anchor="middle" fill="currentColor" font-size="15" font-weight="700">
      {{ DIGITS[0] }}
    </text>
    <text x="46" y="43" text-anchor="middle" fill="currentColor" font-size="15" font-weight="700">
      {{ DIGITS[1] }}
    </text>
  </svg>
</template>
