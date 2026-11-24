<script setup lang="ts">
import type { RouteLocationRaw } from 'vue-router'

export interface BreadcrumbItem {
  title: string
  to?: RouteLocationRaw
}

defineProps<{ items: BreadcrumbItem[] }>()
</script>

<template>
  <!-- ESP-00 section 8: the path is a `nav` named "Caminho"; every level except the current is a link.
       Look: canvas breadcrumb (`.bc`). -->
  <nav :aria-label="$t('common.breadcrumbs')" class="bc">
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
