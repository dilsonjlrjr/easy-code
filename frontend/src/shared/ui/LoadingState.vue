<script setup lang="ts">
defineProps<{ type?: string }>()
</script>

<template>
  <!-- ESP-00 section 6: skeleton with the accessible label "Carregando". -->
  <div role="status" :aria-label="$t('common.loading')" aria-busy="true">
    <v-skeleton-loader :type="type ?? 'article, table-row@3'" />
  </div>
</template>
