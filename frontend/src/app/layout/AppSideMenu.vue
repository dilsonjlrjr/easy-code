<script setup lang="ts">
import { ROUTE_NAMES, type RouteName } from '@/app/router/routeNames'
import AppLogo from '@/shared/ui/AppLogo.vue'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import type { NavigationItem } from './navigationItems'

defineProps<{
  items: NavigationItem[]
  isCurrent: (routeName: RouteName) => boolean
  pendingCount: number
}>()
</script>

<template>
  <aside class="side side-menu">
    <router-link :to="{ name: ROUTE_NAMES.home }" class="brand">
      <AppLogo />
      <span class="brand-text">
        <span class="brand-name">{{ $t('app.brand') }}</span>
        <span class="brand-tagline">{{ $t('app.tagline') }}</span>
      </span>
    </router-link>
    <nav class="snav" :aria-label="$t('navigation.mainLabel')">
      <router-link
        v-for="item in items"
        :key="item.routeName"
        :to="{ name: item.routeName }"
        :class="{ on: isCurrent(item.routeName) }"
        :aria-current="isCurrent(item.routeName) ? 'page' : undefined"
      >
        <CanvasIcon :name="item.icon" />
        {{ $t(item.labelKey) }}
        <span v-if="item.showsPendingCount && pendingCount > 0" class="sbadge">
          {{ pendingCount }}
        </span>
      </router-link>
    </nav>
  </aside>
</template>

<style scoped>
.side-menu {
  flex: 1 1 232px;
  max-width: 100%;
  padding: 16px 12px;
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  gap: 22px;
}

.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  text-decoration: none;
  color: #ffffff;
  padding: 0 6px;
}

.brand-text {
  display: flex;
  flex-direction: column;
}

.brand-name {
  font-size: 20px;
  line-height: 24px;
  font-weight: 700;
  letter-spacing: -0.02em;
}

.brand-tagline {
  font-size: 11px;
  line-height: 14px;
  color: #c7cceb;
}
</style>
