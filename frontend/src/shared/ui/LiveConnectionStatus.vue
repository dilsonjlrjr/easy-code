<script setup lang="ts">
import type { StreamStatus } from '@/shared/events/eventStream'

defineProps<{ status: StreamStatus | undefined }>()
</script>

<template>
  <!-- ESP-00 section 6: connection indicator in text, announced without moving the focus. -->
  <!-- Look: canvas `.chip mute` with a `.dot`; the text says the state, the dot only reinforces it. -->
  <span
    role="status"
    class="live-connection-status"
    :class="status ? 'chip mute' : undefined"
    :data-status="status"
  >
    <template v-if="status">
      <span class="dot live-dot" :class="`live-dot-${status}`" aria-hidden="true" />
      {{ $t(`live.status.${status}`) }}
    </template>
  </span>
</template>

<style scoped>
.live-dot {
  width: 8px;
  height: 8px;
  border-radius: 4px;
  background: #a9b0d6;
}

.live-dot-live {
  background: #86efac;
}

.live-dot-connecting,
.live-dot-reconnecting,
.live-dot-reloading {
  background: #fcd34d;
}
</style>
