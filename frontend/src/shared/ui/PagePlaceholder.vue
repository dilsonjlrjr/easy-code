<script setup lang="ts">
defineProps<{
  title: string
  description: string
}>()
</script>

<template>
  <!-- Layout: canvas page head (h1 + `.t2` lead) with an informative `.note`. -->
  <div class="page-placeholder">
    <div>
      <h1>{{ title }}</h1>
      <p class="t2 page-placeholder-description">{{ description }}</p>
    </div>
    <p class="note page-placeholder-note" role="alert">
      {{ $t('placeholder.underConstruction') }}
    </p>
  </div>
</template>

<style scoped>
.page-placeholder {
  display: flex;
  flex-direction: column;
  gap: 20px;
}
.page-placeholder-description {
  margin: 4px 0 0;
}
.page-placeholder-note {
  margin: 0;
}
</style>
