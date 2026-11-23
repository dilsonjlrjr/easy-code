<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { bridge } from './bridge'
import { normalizeLocale } from './i18n'
import { announce, listen, navigate, store } from './store'
import type { Status } from './types'
import AccountView from './views/AccountView.vue'
import DashboardView from './views/DashboardView.vue'
import GateView from './views/GateView.vue'
import LoginView from './views/LoginView.vue'
import ProjectView from './views/ProjectView.vue'
import WaveView from './views/WaveView.vue'

const { t, locale } = useI18n()
let stop: (() => void) | undefined

onMounted(async () => {
  stop = listen(() => {
    announce(t('connection.reloaded'))
  })
  store.status = await bridge().call<Status>('Status')
})
onBeforeUnmount(() => stop?.())

const status = computed(() => store.status)
const signedIn = computed(() => status.value?.phase === 'signed_in')

// The interface language follows the preference kept by the Go side (pt-BR by default, RNF-21).
watch(
  () => status.value?.locale,
  (value) => {
    locale.value = normalizeLocale(value)
    document.documentElement.lang = locale.value
  },
  { immediate: true },
)

// RNF-19: on every view change the focus goes to the heading of the new view.
watch(
  () => [store.view, status.value?.phase],
  async () => {
    await nextTick()
    document.querySelector<HTMLElement>('#main h1')?.focus()
  },
  { deep: true },
)

const connectionText = computed(() => {
  const parts: string[] = []
  if (store.connection.notices)
    parts.push(t('connection.notices', { state: t(`connection.${store.connection.notices}`) }))
  if (store.view.name === 'wave' && store.connection.wave)
    parts.push(t('connection.wave', { state: t(`connection.${store.connection.wave}`) }))
  return parts.join(' · ')
})
</script>

<template>
  <a class="skip-link" href="#main">{{ t('app.skipToContent') }}</a>
  <header class="top-bar">
    <p class="brand">
      <span>{{ t('app.title') }}</span>
      <span class="subtitle">{{ t('app.subtitle') }}</span>
    </p>
    <nav v-if="signedIn" :aria-label="t('nav.label')">
      <ul>
        <li>
          <button
            type="button"
            :aria-current="store.view.name !== 'account' ? 'page' : undefined"
            @click="navigate({ name: 'dashboard' })"
          >
            {{ t('nav.dashboard') }}
          </button>
        </li>
        <li>
          <button
            type="button"
            :aria-current="store.view.name === 'account' ? 'page' : undefined"
            @click="navigate({ name: 'account' })"
          >
            {{ t('nav.account') }}
          </button>
        </li>
      </ul>
    </nav>
    <p v-if="signedIn && connectionText" class="connection" :aria-label="t('connection.label')">
      {{ connectionText }}
    </p>
  </header>
  <p v-if="status?.version_mismatch" class="notice banner">
    {{ t('version.mismatch', { app: status.app_version, server: status.server_version }) }}
  </p>
  <main id="main" tabindex="-1">
    <p v-if="!status || status.phase === 'starting'">{{ t('app.starting') }}</p>
    <LoginView v-else-if="!signedIn" :key="status.phase" :status="status" />
    <DashboardView v-else-if="store.view.name === 'dashboard'" />
    <ProjectView
      v-else-if="store.view.name === 'project'"
      :key="store.view.projectId"
      :project-id="store.view.projectId"
    />
    <WaveView
      v-else-if="store.view.name === 'wave'"
      :key="`${store.view.projectId}/${store.view.waveId}`"
      :project-id="store.view.projectId"
      :wave-id="store.view.waveId"
    />
    <GateView
      v-else-if="store.view.name === 'gate'"
      :key="`${store.view.gateId}/${store.view.round}`"
      :project-id="store.view.projectId"
      :gate-id="store.view.gateId"
      :round="store.view.round"
    />
    <AccountView v-else-if="store.view.name === 'account'" :status="status" />
  </main>
  <div class="visually-hidden" role="status" aria-live="polite">{{ store.announcement }}</div>
</template>
