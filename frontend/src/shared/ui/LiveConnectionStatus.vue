<script setup lang="ts">
import type { StreamStatus } from '@/shared/events/eventStream'

defineProps<{ status: StreamStatus | undefined }>()
</script>

<template>
  <!-- ESP-00 section 6: connection indicator in text, announced without moving the focus. -->
  <span role="status" class="live-connection-status text-body-2" :data-status="status">
    <template v-if="status">{{ $t(`live.status.${status}`) }}</template>
  </span>
</template>
