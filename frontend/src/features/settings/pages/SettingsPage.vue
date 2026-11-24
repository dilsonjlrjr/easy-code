<script setup lang="ts">
import { computed, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES, SETTINGS_TABS } from '@/app/router/routeNames'
import { useSessionStore } from '@/shared/session/sessionStore'
import AppBreadcrumbs from '@/shared/ui/AppBreadcrumbs.vue'
import AuditTrail from '@/features/audit/components/AuditTrail.vue'
import PricesPanel from '../components/PricesPanel.vue'
import ProvidersPanel from '../components/ProvidersPanel.vue'
import SkillLibraryPanel from '../components/SkillLibraryPanel.vue'

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

/** ESP-00 section 8: arrow keys, Home and End move between the tabs (WAI-ARIA tabs pattern). */
async function onTabKey(event: KeyboardEvent, current: SettingsTab): Promise<void> {
  const index = SETTINGS_TABS.indexOf(current)
  const last = SETTINGS_TABS.length - 1
  const moves: Record<string, number> = {
    ArrowRight: index === last ? 0 : index + 1,
    ArrowLeft: index === 0 ? last : index - 1,
    Home: 0,
    End: last,
  }
  const target = moves[event.key]
  if (target === undefined) return
  event.preventDefault()
  const next = SETTINGS_TABS[target]
  if (!next) return
  tab.value = next
  await nextTick()
  document.getElementById(`settings-tab-${next}`)?.focus()
}

const breadcrumbs = computed(() => [
  { title: t('pages.settings.title'), to: { name: ROUTE_NAMES.settings } },
  { title: t(`settings.tabs.${tab.value}`) },
])
</script>

<template>
  <!-- Layout: canvas "Configurações" (Configuracoes.dc.html). -->
  <template v-if="session.isAdministrator">
    <AppBreadcrumbs :items="breadcrumbs" />
    <div>
      <h1>{{ $t('pages.settings.title') }}</h1>
      <p class="t2 page-description">{{ $t('pages.settings.description') }}</p>
    </div>
    <div class="tabs" role="tablist" :aria-label="$t('settings.tabsLabel')">
      <button
        v-for="item in SETTINGS_TABS"
        :id="`settings-tab-${item}`"
        :key="item"
        type="button"
        role="tab"
        class="tab"
        :class="{ on: tab === item }"
        :aria-selected="tab === item ? 'true' : 'false'"
        aria-controls="settings-panel"
        :tabindex="tab === item ? 0 : -1"
        @click="tab = item"
        @keydown="onTabKey($event, item)"
      >
        {{ $t(`settings.tabs.${item}`) }}
      </button>
    </div>
    <div id="settings-panel" role="tabpanel" class="panel" :aria-labelledby="`settings-tab-${tab}`">
      <ProvidersPanel v-if="tab === 'providers'" />
      <PricesPanel v-else-if="tab === 'prices'" />
      <SkillLibraryPanel v-else-if="tab === 'skills'" />
      <!-- ESP-27: the organization trail (keys, prices, models, skills, sessions). -->
      <section
        v-else-if="tab === 'audit'"
        class="card audit-card"
        aria-labelledby="organization-audit-heading"
      >
        <h2 id="organization-audit-heading">{{ $t('audit.organizationTitle') }}</h2>
        <p class="t2 card-help">{{ $t('audit.organizationDescription') }}</p>
        <AuditTrail scope="organization" />
      </section>
    </div>
  </template>
  <!-- ER-23-15: the direct address shows "no permission" with a link to Home. -->
  <template v-else>
    <h1>{{ $t('pages.settings.title') }}</h1>
    <div class="note forbidden" role="alert">
      <span>
        {{ $t('problems.forbidden') }}
        <router-link :to="{ name: ROUTE_NAMES.home }">{{ $t('common.goHome') }}</router-link>
      </span>
    </div>
  </template>
</template>

<style scoped>
.page-description {
  margin: 4px 0 0;
}

.panel {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.audit-card {
  padding: 18px 22px;
}

.card-help {
  margin: 4px 0 8px;
  font-size: 13px;
}

.forbidden {
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.1);
}
</style>
