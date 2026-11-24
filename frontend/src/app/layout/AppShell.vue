<script setup lang="ts">
import SkipLink from '@/shared/ui/SkipLink.vue'
import AppSideMenu from './AppSideMenu.vue'
import AppTopBar from './AppTopBar.vue'
import { useFocusOnNavigation } from './useFocusOnNavigation'
import { useNavigationLayout } from './useNavigationLayout'
import { usePendingCount } from './usePendingCount'

const MAIN_CONTENT_ID = 'main-content'

const { items, isCurrent } = useNavigationLayout()
const { count: pendingCount } = usePendingCount()
useFocusOnNavigation(MAIN_CONTENT_ID)
</script>

<template>
  <SkipLink :target-id="MAIN_CONTENT_ID" />
  <!-- Canvas "Desktop · Início": side menu of 232 px and a fluid column; they wrap on narrow screens. -->
  <div class="shell">
    <AppSideMenu :items="items" :is-current="isCurrent" :pending-count="pendingCount" />
    <div class="shell-column">
      <AppTopBar :pending-count="pendingCount" />
      <main :id="MAIN_CONTENT_ID" tabindex="-1" class="shell-main">
        <slot />
      </main>
    </div>
  </div>
</template>

<style scoped>
.shell {
  min-height: 100vh;
  display: flex;
  flex-wrap: wrap;
  align-items: stretch;
}

.shell-column {
  flex: 999 1 560px;
  min-width: 0;
  padding: 16px 28px 40px;
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.shell-main {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.shell-main:focus {
  outline: none;
}
</style>
