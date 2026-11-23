<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AuditTrail from './AuditTrail.vue'

/** ESP-27: the project trail, for approvers and the lead (US-35). */
const route = useRoute()
const projectId = computed(() => String(route.params.projectId))
</script>

<template>
  <section aria-labelledby="audit-heading">
    <h2 id="audit-heading" class="text-h6 mb-1">{{ $t('audit.title') }}</h2>
    <p class="text-body-2 text-medium-emphasis mb-4">{{ $t('audit.description') }}</p>
    <AuditTrail scope="project" :project-id="projectId" />
  </section>
</template>
