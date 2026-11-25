<script setup lang="ts">
import { ROUTE_NAMES } from '@/app/router/routeNames'
import HeaderSearch from '@/features/search/components/HeaderSearch.vue'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import AccountMenu from './AccountMenu.vue'

defineProps<{ pendingCount: number }>()
</script>

<template>
  <!-- Canvas top row: the search (US-56), the bell to Pendências (violet dot when something waits) and the account. -->
  <header class="top-bar">
    <HeaderSearch />
    <div class="top-actions">
      <router-link
        class="ibtn"
        :to="{ name: ROUTE_NAMES.pendingItems }"
        :aria-label="$t('navigation.pendingCount', { n: pendingCount })"
        data-testid="top-bar-pending"
      >
        <CanvasIcon name="bell" />
        <span v-if="pendingCount > 0" class="bell-dot" />
      </router-link>
      <AccountMenu :pending-count="pendingCount" />
    </div>
  </header>
</template>

<style scoped>
.top-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  min-height: 44px;
}

.top-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 4px;
}

.bell-dot {
  position: absolute;
  top: 10px;
  right: 11px;
  width: 8px;
  height: 8px;
  border-radius: 4px;
  background: #a855f7;
}
</style>
