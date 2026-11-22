<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES, SETTINGS_TABS } from '@/app/router/routeNames'
import { useSessionStore } from '@/shared/session/sessionStore'
import AppBreadcrumbs from '@/shared/ui/AppBreadcrumbs.vue'
import PricesPanel from '../components/PricesPanel.vue'
import ProvidersPanel from '../components/ProvidersPanel.vue'

type SettingsTab = (typeof SETTINGS_TABS)[number]

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()

/** The active tab lives in the URL (ESP-23 section 2). */
const tab = computed<SettingsTab>({
  get: () => {
    const value = route.params.tab
    return SETTINGS_TABS.find((item) => item === value) ?? 'providers'
  },
  set: (next) => {
    void router.replace({ name: ROUTE_NAMES.settings, params: { tab: next } })
  },
})

const breadcrumbs = computed(() => [
  { title: t('pages.settings.title'), to: { name: ROUTE_NAMES.settings } },
  { title: t(`settings.tabs.${tab.value}`) },
])
</script>

<template>
  <v-container max-width="1200">
    <template v-if="session.isAdministrator">
      <AppBreadcrumbs :items="breadcrumbs" />
      <h1 class="text-h4 mb-2">{{ $t('pages.settings.title') }}</h1>
      <p class="text-body-1 text-medium-emphasis mb-4">{{ $t('pages.settings.description') }}</p>
      <v-tabs v-model="tab" color="primary" class="mb-6">
        <v-tab v-for="item in SETTINGS_TABS" :key="item" :value="item">
          {{ $t(`settings.tabs.${item}`) }}
        </v-tab>
      </v-tabs>
      <v-tabs-window v-model="tab">
        <v-tabs-window-item value="providers">
          <ProvidersPanel v-if="tab === 'providers'" />
        </v-tabs-window-item>
        <v-tabs-window-item value="prices">
          <PricesPanel v-if="tab === 'prices'" />
        </v-tabs-window-item>
      </v-tabs-window>
    </template>
    <!-- ER-23-15: the direct address shows "no permission" with a link to Home. -->
    <template v-else>
      <h1 class="text-h4 mb-4">{{ $t('pages.settings.title') }}</h1>
      <v-alert type="warning" variant="tonal" role="alert">
        <p>{{ $t('problems.forbidden') }}</p>
        <router-link :to="{ name: ROUTE_NAMES.home }">{{ $t('common.goHome') }}</router-link>
      </v-alert>
    </template>
  </v-container>
</template>
