<script setup lang="ts">
import { computed } from 'vue'
import { stateFamily, stateTone, type StateMachine } from './stateFamilies'

const props = defineProps<{
  machine: StateMachine
  state: string
  /** Optional qualifier after the label, such as the pause reason. */
  detail?: string
}>()

const family = computed(() => stateFamily(props.machine, props.state))
const tone = computed(() => stateTone(props.machine, props.state))
</script>

<template>
  <!-- ESP-00 ER-00-6: the state is text; the color only reinforces it (canvas "Estados"). -->
  <span
    class="chip state-label"
    :class="tone"
    :data-machine="machine"
    :data-state="state"
    :data-family="family"
  >
    {{ $t(`states.${machine}.${state}`) }}<template v-if="detail">&nbsp;({{ detail }})</template>
  </span>
</template>
