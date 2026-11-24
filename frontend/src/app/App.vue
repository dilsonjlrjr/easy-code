<script setup lang="ts">
import { computed, watchEffect } from 'vue'
import { useRoute } from 'vue-router'
import { useTheme } from 'vuetify'
import { useDocumentTitleSync } from '@/shared/composables/useDocumentTitle'
import { useInterfaceLanguageSync } from '@/shared/composables/useInterfaceLanguage'
import AppShell from './layout/AppShell.vue'

useInterfaceLanguageSync()
useDocumentTitleSync()

// The interface canvas defines one look, dark with the blue and violet glow ("Fundamentos visuais").
const theme = useTheme()
watchEffect(() => {
  theme.change('dark')
  document.documentElement.style.colorScheme = 'dark'
})

const route = useRoute()
const usesShell = computed(() => route.meta.layout !== 'blank')
</script>

<template>
  <v-app class="ec">
    <AppShell v-if="usesShell">
      <router-view />
    </AppShell>
    <main v-else id="main-content">
      <router-view />
    </main>
  </v-app>
</template>
