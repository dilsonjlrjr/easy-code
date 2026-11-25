<script setup lang="ts">
import { computed, ref } from 'vue'
import { relativePoint, type ViewerWidth } from '../composables/prototypeModel'

/** A pin over the frame: its number and the point relative to the frame (US-57). */
export interface FramePin {
  key: string
  number: number
  x: number
  y: number
  /** Pins of a decided round are read only (CA-3) and drawn apart from the drafts. */
  recorded: boolean
}

/**
 * ESP-11 "Área do protótipo": the page of the screen inside `iframe sandbox="allow-scripts"`, from the
 * content origin only (ER-11-1, ADR-19). The platform never reads the frame: annotation pins are drawn on
 * the area that wraps it (US-57 RN-1), and in annotation mode a transparent button over the frame takes
 * the click, so the prototype gets no event and no position.
 */
const props = defineProps<{
  url: string
  title: string
  path: string
  label: string
  width: ViewerWidth
  annotating: boolean
  pins: FramePin[]
}>()

const emit = defineEmits<{ mark: [point: { x: number; y: number }] }>()

const area = ref<HTMLElement>()
const frameWidth = computed(() => (props.width === 'wide' ? '100%' : '420px'))

function mark(event: MouseEvent): void {
  const box = area.value?.getBoundingClientRect()
  if (!box) return
  emit('mark', relativePoint(event.clientX, event.clientY, box))
}
</script>

<template>
  <!-- Values copied from the canvas Prototipo artboard (.frame, .fbar, .fdot, .pin). -->
  <div class="frame" :style="{ width: frameWidth }" data-testid="prototype-frame">
    <div class="fbar">
      <span class="fdot" aria-hidden="true"></span>
      <span class="fdot" aria-hidden="true"></span>
      <span class="fdot" aria-hidden="true"></span>
      <span class="mono fbar-path">{{ path }}</span>
      <span class="fbar-label">{{ label }}</span>
    </div>
    <div ref="area" class="pv-area">
      <iframe
        :src="url"
        :title="title"
        sandbox="allow-scripts"
        referrerpolicy="no-referrer"
        class="pv-frame"
        data-testid="prototype-iframe"
      ></iframe>
      <button
        v-if="annotating"
        type="button"
        class="pv-overlay"
        :aria-label="$t('prototype.annotations.markPoint')"
        data-testid="prototype-overlay"
        @click="mark"
      ></button>
      <span
        v-for="pin in pins"
        :key="pin.key"
        class="pin"
        :class="{ 'pin-recorded': pin.recorded }"
        :style="{ left: `${pin.x * 100}%`, top: `${pin.y * 100}%` }"
        aria-hidden="true"
        >{{ pin.number }}</span
      >
    </div>
  </div>
</template>

<style scoped>
.frame {
  border-radius: 12px;
  overflow: hidden;
  border: 1px solid rgba(140, 150, 255, 0.3);
  background: #0a0d28;
  margin: 0 auto;
  max-width: 100%;
  transition: width 0.2s;
}

.fbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  background: #141a3f;
  font-size: 12px;
  color: #a9b0d6;
  flex-wrap: wrap;
}

.fdot {
  width: 10px;
  height: 10px;
  border-radius: 5px;
  background: #3b3f6b;
}

.fbar-path {
  margin-left: 8px;
  font-size: 12px;
  overflow-wrap: anywhere;
}

.fbar-label {
  margin-left: auto;
}

.pv-area {
  position: relative;
  background: #f3f4f6;
  min-height: 520px;
}

.pv-frame {
  display: block;
  width: 100%;
  height: 640px;
  border: 0;
  background: #ffffff;
}

.pv-overlay {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  border: 2px dashed rgba(168, 85, 247, 0.7);
  background: rgba(168, 85, 247, 0.06);
  cursor: crosshair;
  padding: 0;
}

.pin {
  position: absolute;
  width: 26px;
  height: 26px;
  margin: -13px 0 0 -13px;
  border-radius: 13px;
  background: #a855f7;
  color: #ffffff;
  font-size: 12px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 0 0 3px rgba(168, 85, 247, 0.35);
  pointer-events: none;
}

.pin-recorded {
  background: #6366f1;
  box-shadow: 0 0 0 3px rgba(99, 102, 241, 0.35);
}
</style>
