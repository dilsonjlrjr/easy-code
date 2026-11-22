<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useDocumentTitleSync } from '@/shared/composables/useDocumentTitle'
import { useInterfaceLanguageSync } from '@/shared/composables/useInterfaceLanguage'
import { useThemeSync } from '@/shared/composables/useThemePreference'
import AppShell from './layout/AppShell.vue'

useInterfaceLanguageSync()
useThemeSync()
useDocumentTitleSync()

const route = useRoute()
const usesShell = computed(() => route.meta.layout !== 'blank')
</script>

<template>
  <v-app>
    <AppShell v-if="usesShell">
      <router-view />
    </AppShell>
    <v-main v-else id="main-content">
      <router-view />
    </v-main>
  </v-app>
</template>
