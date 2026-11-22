<script setup lang="ts">
defineProps<{
  title: string
  description: string
}>()
</script>

<template>
  <v-container class="page-placeholder" max-width="1200">
    <h1 class="text-h4 mb-2">{{ title }}</h1>
    <p class="text-body-1 text-medium-emphasis mb-6">{{ description }}</p>
    <v-alert type="info" variant="tonal" :text="$t('placeholder.underConstruction')" />
  </v-container>
</template>
