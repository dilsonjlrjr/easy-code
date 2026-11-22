<script setup lang="ts">
import { computed } from 'vue'
import { hasProblemSlug } from '@/shared/api/problemDetails'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'

const props = defineProps<{ error: unknown }>()

/** US-05 CA-2 / ESP-05 ER-05-10: the refusal lists what is missing to start. */
const missing = computed(() =>
  hasProblemSlug(props.error, 'incomplete-configuration')
    ? (props.error.problem?.missing ?? [])
    : undefined,
)
const missingCapacity = computed(() =>
  hasProblemSlug(props.error, 'model-without-capability')
    ? props.error.problem?.missing_capacity
    : undefined,
)
</script>

<template>
  <v-alert v-if="missing" type="warning" variant="tonal" role="alert" data-testid="start-refusal">
    <p class="font-weight-medium">{{ $t('projects.start.incomplete') }}</p>
    <ul class="mt-2 ml-4">
      <li v-for="item in missing" :key="item">{{ $t(`projects.start.missing.${item}`) }}</li>
    </ul>
  </v-alert>
  <v-alert
    v-else-if="missingCapacity"
    type="warning"
    variant="tonal"
    role="alert"
    data-testid="start-refusal"
  >
    {{
      $t('projects.configuration.missingCapacity', {
        capacity: $t(`projects.capacities.${missingCapacity}`),
      })
    }}
  </v-alert>
  <ProblemAlert v-else :error="error" />
</template>
