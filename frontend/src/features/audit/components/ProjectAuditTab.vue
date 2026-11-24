<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AuditTrail from './AuditTrail.vue'

/** ESP-27: the project trail, for approvers and the lead (US-35). */
const route = useRoute()
const projectId = computed(() => String(route.params.projectId))
</script>

<template>
  <!-- Layout: canvas primitives of Projeto.dc.html (section card with h2); no own artboard. -->
  <section class="card tab-card" aria-labelledby="audit-heading">
    <h2 id="audit-heading">{{ $t('audit.title') }}</h2>
    <p class="t2 tab-hint">{{ $t('audit.description') }}</p>
    <AuditTrail scope="project" :project-id="projectId" />
  </section>
</template>

<style scoped>
.tab-card {
  padding: 18px 22px;
}

.tab-hint {
  margin: 4px 0 16px;
  font-size: 13px;
}
</style>
