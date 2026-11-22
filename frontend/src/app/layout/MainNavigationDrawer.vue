<script setup lang="ts">
import type { RouteName } from '@/app/router/routeNames'
import type { NavigationItem } from './navigationItems'

defineProps<{
  items: NavigationItem[]
  rail: boolean
  isCurrent: (routeName: RouteName) => boolean
}>()
</script>

<template>
  <v-navigation-drawer permanent :rail="rail" :aria-label="$t('navigation.mainLabel')">
    <!-- The links are plain navigation links; no list widget role (axe aria-required-children). -->
    <v-list nav density="comfortable" role="none">
      <v-list-item
        v-for="item in items"
        :key="item.routeName"
        :to="{ name: item.routeName }"
        :prepend-icon="item.icon"
        :title="$t(item.labelKey)"
        :aria-label="rail ? $t(item.labelKey) : undefined"
        :aria-current="isCurrent(item.routeName) ? 'page' : undefined"
        color="primary"
      />
    </v-list>
  </v-navigation-drawer>
</template>
