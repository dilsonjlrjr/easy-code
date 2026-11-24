<script setup lang="ts">
defineProps<{ type?: string }>()
</script>

<template>
  <!-- ESP-00 section 6: skeleton with the accessible label "Carregando".
       Look: canvas tones (bones in the card border tint on a transparent ground). -->
  <div role="status" :aria-label="$t('common.loading')" aria-busy="true" class="loading-state">
    <v-skeleton-loader :type="type ?? 'article, table-row@3'" />
  </div>
</template>

<style scoped>
.loading-state :deep(.v-skeleton-loader) {
  background: transparent;
}
.loading-state :deep(.v-skeleton-loader__bone) {
  background: rgba(140, 150, 255, 0.12);
}
.loading-state :deep(.v-skeleton-loader__bone.v-skeleton-loader__text),
.loading-state :deep(.v-skeleton-loader__bone.v-skeleton-loader__heading) {
  border-radius: 6px;
}
.loading-state :deep(.v-skeleton-loader__bone.v-skeleton-loader__avatar) {
  background: rgba(140, 150, 255, 0.16);
}
.loading-state :deep(.v-skeleton-loader__bone::after) {
  background: linear-gradient(90deg, transparent, rgba(167, 139, 250, 0.1), transparent);
}
</style>
