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
  <!-- Look: canvas `.note`, in the warning tone. -->
  <div v-if="missing" class="note note-warn" role="alert" data-testid="start-refusal">
    <div>
      <p class="refusal-title">{{ $t('projects.start.incomplete') }}</p>
      <ul class="refusal-list">
        <li v-for="item in missing" :key="item">{{ $t(`projects.start.missing.${item}`) }}</li>
      </ul>
    </div>
  </div>
  <p
    v-else-if="missingCapacity"
    class="note note-warn refusal-single"
    role="alert"
    data-testid="start-refusal"
  >
    {{
      $t('projects.configuration.missingCapacity', {
        capacity: $t(`projects.capacities.${missingCapacity}`),
      })
    }}
  </p>
  <ProblemAlert v-else :error="error" />
</template>

<style scoped>
.note-warn {
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.1);
}

.refusal-single {
  margin: 0;
}

.refusal-title {
  margin: 0;
  font-weight: 600;
}

.refusal-list {
  margin: 4px 0 0;
  padding-left: 18px;
}
</style>
