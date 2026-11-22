<script setup lang="ts">
import type { RouteName } from '@/app/router/routeNames'
import type { NavigationItem } from './navigationItems'

defineProps<{
  items: NavigationItem[]
  isCurrent: (routeName: RouteName) => boolean
}>()
</script>

<template>
  <v-bottom-navigation tag="nav" grow color="primary" :aria-label="$t('navigation.mainLabel')">
    <v-btn
      v-for="item in items"
      :key="item.routeName"
      class="bottom-navigation-item"
      :to="{ name: item.routeName }"
      :aria-current="isCurrent(item.routeName) ? 'page' : undefined"
    >
      <v-icon :icon="item.icon" />
      <span class="bottom-navigation-label">{{ $t(item.labelKey) }}</span>
    </v-btn>
  </v-bottom-navigation>
</template>

<style scoped>
/* Five destinations must fit 320 px without horizontal scroll (ESP-00 CA-00-19). */
.bottom-navigation-item {
  min-width: 0 !important;
  flex: 1 1 0 !important;
}

.bottom-navigation-label {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.7rem;
}
</style>
