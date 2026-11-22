<script setup lang="ts">
import SkipLink from '@/shared/ui/SkipLink.vue'
import AppTopBar from './AppTopBar.vue'
import BottomNavigationBar from './BottomNavigationBar.vue'
import MainNavigationDrawer from './MainNavigationDrawer.vue'
import { useFocusOnNavigation } from './useFocusOnNavigation'
import { useNavigationLayout } from './useNavigationLayout'

const MAIN_CONTENT_ID = 'main-content'

const { mode, items, railExpanded, isCurrent, toggleRail } = useNavigationLayout()
useFocusOnNavigation(MAIN_CONTENT_ID)
</script>

<template>
  <SkipLink :target-id="MAIN_CONTENT_ID" />
  <AppTopBar
    :show-menu-toggle="mode === 'rail'"
    :menu-expanded="railExpanded"
    @toggle-menu="toggleRail"
  />
  <MainNavigationDrawer
    v-if="mode !== 'bottom-bar'"
    :items="items"
    :rail="mode === 'rail' && !railExpanded"
    :is-current="isCurrent"
  />
  <BottomNavigationBar v-else :items="items" :is-current="isCurrent" />
  <v-main :id="MAIN_CONTENT_ID" tabindex="-1" class="main-content">
    <slot />
  </v-main>
</template>

<style scoped>
.main-content:focus {
  outline: none;
}
</style>
