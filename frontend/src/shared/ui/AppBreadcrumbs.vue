<script setup lang="ts">
import type { RouteLocationRaw } from 'vue-router'

export interface BreadcrumbItem {
  title: string
  to?: RouteLocationRaw
}

defineProps<{ items: BreadcrumbItem[] }>()
</script>

<template>
  <!-- ESP-00 section 8: the path is a `nav` named "Caminho"; every level except the current is a link. -->
  <nav :aria-label="$t('common.breadcrumbs')" class="app-breadcrumbs">
    <ol>
      <li v-for="(item, index) in items" :key="index">
        <router-link v-if="item.to && index < items.length - 1" :to="item.to">
          {{ item.title }}
        </router-link>
        <span v-else aria-current="page">{{ item.title }}</span>
      </li>
    </ol>
  </nav>
</template>

<style scoped>
.app-breadcrumbs ol {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
  list-style: none;
  margin: 0 0 0.75rem;
  padding: 0;
  font-size: 0.875rem;
}

.app-breadcrumbs li:not(:last-child)::after {
  content: '›';
  margin-left: 0.25rem;
  color: rgb(var(--v-theme-on-surface), 0.7);
}

.app-breadcrumbs a {
  color: rgb(var(--v-theme-primary));
}
</style>
