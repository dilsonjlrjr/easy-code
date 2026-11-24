<script setup lang="ts">
import { useId } from 'vue'

/** The EasyCode mark from the interface canvas: two chevrons in a blue-violet gradient around binary. */
withDefaults(defineProps<{ width?: number; height?: number }>(), { width: 64, height: 39 })

const gradientId = `easycode-logo-${useId()}`
// The mark's binary digits are drawing, not text to translate.
const DIGITS_TOP = '0101'
const DIGITS_BOTTOM = '0010'
</script>

<template>
  <svg :width="width" :height="height" viewBox="0 0 92 56" aria-hidden="true">
    <defs>
      <linearGradient :id="gradientId" x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stop-color="#3B82F6" />
        <stop offset="1" stop-color="#8B5CF6" />
      </linearGradient>
    </defs>
    <path
      d="M22 4 6 28l16 24"
      fill="none"
      :stroke="`url(#${gradientId})`"
      stroke-width="7"
      stroke-linecap="round"
      stroke-linejoin="round"
    />
    <path
      d="M70 4l16 24-16 24"
      fill="none"
      :stroke="`url(#${gradientId})`"
      stroke-width="7"
      stroke-linecap="round"
      stroke-linejoin="round"
    />
    <text
      x="46"
      y="25"
      text-anchor="middle"
      fill="#FFFFFF"
      font-family="Inter, sans-serif"
      font-size="15"
      font-weight="700"
    >
      {{ DIGITS_TOP }}
    </text>
    <text
      x="46"
      y="43"
      text-anchor="middle"
      fill="#FFFFFF"
      font-family="Inter, sans-serif"
      font-size="15"
      font-weight="700"
    >
      {{ DIGITS_BOTTOM }}
    </text>
  </svg>
</template>
