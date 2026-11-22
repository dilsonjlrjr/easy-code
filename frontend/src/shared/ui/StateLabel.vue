<script setup lang="ts">
import { computed } from 'vue'
import { FAMILY_COLORS, FAMILY_ICONS, stateFamily, type StateMachine } from './stateFamilies'

const props = defineProps<{
  machine: StateMachine
  state: string
  /** Optional qualifier after the label, such as the pause reason. */
  detail?: string
}>()

const family = computed(() => stateFamily(props.machine, props.state))
</script>

<template>
  <!-- ESP-00 ER-00-6: the state is text; color and icon only reinforce it. -->
  <v-chip
    :color="FAMILY_COLORS[family]"
    variant="tonal"
    size="small"
    label
    class="state-label"
    :data-state="state"
    :data-family="family"
  >
    <v-icon :icon="FAMILY_ICONS[family]" start aria-hidden="true" />
    <span>{{ $t(`states.${machine}.${state}`) }}</span>
    <span v-if="detail">&nbsp;({{ detail }})</span>
  </v-chip>
</template>
