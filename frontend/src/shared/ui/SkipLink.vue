<script setup lang="ts">
defineProps<{ targetId: string }>()
</script>

<template>
  <a class="skip-link" :href="`#${targetId}`">{{ $t('app.skipToContent') }}</a>
</template>

<style scoped>
.skip-link {
  position: absolute;
  inset-inline-start: 8px;
  top: -100px;
  z-index: 10000;
  padding: 12px 16px;
  background: rgb(var(--v-theme-surface));
  color: rgb(var(--v-theme-on-surface));
  border-radius: 4px;
}

.skip-link:focus {
  top: 8px;
}
</style>
