<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { bridge } from './bridge'
import { normalizeLocale } from './i18n'
import { announce, listen, navigate, store, toggleMenu } from './store'
import AppLogo from './components/AppLogo.vue'
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

const DOTS: Record<string, string> = {
  live: 'dot dot-live',
  connecting: 'dot dot-wait',
  reconnecting: 'dot dot-wait',
  stopped: 'dot dot-off',
}

// The state of each live stream: the notices while signed in, the wave while its view is open (RNF-19:
// in text, the dot only repeats it).
const connections = computed(() => {
  const items: { key: string; long: string; short: string; dot: string }[] = []
  const add = (key: 'notices' | 'wave', state?: string) => {
    if (!state) return
    items.push({
      key,
      long: t(`connection.${key}`, { state: t(`connection.${state}`) }),
      short: t(`connection.short.${state}`),
      dot: DOTS[state] ?? 'dot',
    })
  }
  add('notices', store.connection.notices)
  if (store.view.name === 'wave') add('wave', store.connection.wave)
  return items
})

const onAccount = computed(() => store.view.name === 'account')
</script>

<template>
  <a class="skip-link" href="#main">{{ t('app.skipToContent') }}</a>
  <div class="shell">
    <aside
      v-if="signedIn"
      class="rail"
      :class="{ open: store.menuOpen }"
      :aria-label="t('nav.menu')"
    >
      <button
        type="button"
        class="ibtn"
        :aria-expanded="store.menuOpen ? 'true' : 'false'"
        aria-controls="main-nav"
        :aria-label="store.menuOpen ? t('nav.collapse') : t('nav.expand')"
        @click="toggleMenu"
      >
        <svg
          width="22"
          height="22"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.8"
          aria-hidden="true"
        >
          <path d="M4 7h16M4 12h16M4 17h16" />
        </svg>
      </button>
      <p class="rlogo">
        <AppLogo :size="48" />
        <span v-if="store.menuOpen" class="rlogo-name">
          <strong>{{ t('app.title') }}</strong>
          <span class="t2 small">{{ t('app.subtitle') }}</span>
        </span>
        <span v-else class="visually-hidden">{{ t('app.title') }}</span>
      </p>
      <nav id="main-nav" class="rnav" :aria-label="t('nav.label')">
        <ul>
          <li>
            <button
              type="button"
              :aria-current="!onAccount ? 'page' : undefined"
              @click="navigate({ name: 'dashboard' })"
            >
              <span class="ric">
                <svg
                  width="22"
                  height="22"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.7"
                  aria-hidden="true"
                >
                  <path d="M4 11 12 4l8 7v9h-5v-6H9v6H4v-9Z" />
                </svg>
                <span v-if="store.pendingTotal" class="rbadge" aria-hidden="true">{{
                  store.pendingTotal
                }}</span>
              </span>
              <span
                >{{ t('nav.dashboard')
                }}<span v-if="store.pendingTotal" class="visually-hidden">{{
                  t('nav.pendingCount', { count: store.pendingTotal })
                }}</span></span
              >
            </button>
          </li>
          <li>
            <button
              type="button"
              :aria-current="onAccount ? 'page' : undefined"
              @click="navigate({ name: 'account' })"
            >
              <span class="ric">
                <svg
                  width="22"
                  height="22"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.7"
                  aria-hidden="true"
                >
                  <circle cx="12" cy="8" r="4" />
                  <path d="M4 20c1.5-4 4.5-6 8-6s6.5 2 8 6" />
                </svg>
              </span>
              <span>{{ t('nav.account') }}</span>
            </button>
          </li>
        </ul>
      </nav>
      <div class="rfoot">
        <p v-for="item in connections" :key="item.key" class="rst connection">
          <span :class="item.dot" aria-hidden="true"></span>
          <span v-if="store.menuOpen">{{ item.long }}</span>
          <span v-else
            >{{ item.short }}<span class="visually-hidden"> · {{ item.long }}</span></span
          >
        </p>
        <p class="t2">
          <span v-if="store.menuOpen">{{ t('connection.readOnly') }} · </span
          >{{ t('connection.version', { version: status?.app_version ?? '' }) }}
        </p>
      </div>
    </aside>
    <main id="main" class="content" :class="{ centered: !signedIn }" tabindex="-1">
      <p v-if="status?.version_mismatch" class="notice">
        {{ t('version.mismatch', { app: status.app_version, server: status.server_version }) }}
      </p>
      <p v-if="!status || status.phase === 'starting'">{{ t('app.starting') }}</p>
      <LoginView v-else-if="!signedIn" :key="status.phase" :status="status" />
      <DashboardView v-else-if="store.view.name === 'dashboard'" :status="status" />
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
  </div>
  <div class="visually-hidden" role="status" aria-live="polite">{{ store.announcement }}</div>
</template>
