<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useLoader } from '@/shared/composables/useLoader'
import { useSessionStore } from '@/shared/session/sessionStore'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import {
  localClientApi,
  normalizeUserCode,
  verificationUrl,
  type LocalClientConfiguration,
} from '../api/localClientApi'

/**
 * US-45 CA-1 (ADR-12, ADR-20; risk S-19 / RK-16): the web side of the desktop app's device flow.
 * Before the person confirms the code in the SSO, the page says which client asks (the read-only
 * desktop app), what it may do, for which account and platform, and warns that only a code shown
 * by their own app, just now, should be approved. The approval itself happens in the SSO: the web
 * never sees a token. Declining approves nothing; the code simply expires (CA-4).
 */
const { t } = useI18n()
const route = useRoute()
const session = useSessionStore()
const configuration = ref<LocalClientConfiguration>()
const typed = ref(typeof route.query.user_code === 'string' ? route.query.user_code : '')
const declined = ref(false)
const touched = ref(false)

const loader = useLoader(async () => {
  configuration.value = await localClientApi.getConfiguration()
})
void loader.run()

const code = computed(() => normalizeUserCode(typed.value))
const codeError = computed(() =>
  touched.value && typed.value && !code.value ? [t('device.invalidCode')] : [],
)
const platform = computed(() => {
  try {
    return configuration.value ? new URL(configuration.value.issuer).host : ''
  } catch {
    return ''
  }
})
const minutes = computed(() => Math.round((configuration.value?.code_validity_s ?? 600) / 60))
const readOnly = computed(() => configuration.value?.scopes.includes('easycode-read') ?? true)

/** The confirmation happens in the SSO, in the same window. */
function approve(): void {
  touched.value = true
  if (!code.value || !configuration.value) return
  window.location.assign(verificationUrl(configuration.value, code.value))
}
</script>

<template>
  <v-container max-width="760">
    <h1 class="text-h4 mb-2">{{ $t('pages.device.title') }}</h1>
    <p class="text-body-1 mb-4">{{ $t('device.intro') }}</p>

    <LoadingState v-if="loader.loading.value && !configuration" />
    <ProblemAlert
      v-else-if="loader.error.value && !configuration"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <v-alert
      v-else-if="declined"
      type="info"
      variant="tonal"
      role="status"
      data-testid="device-declined"
    >
      <p>{{ $t('device.declined') }}</p>
      <router-link :to="{ name: ROUTE_NAMES.localClientSessions }">
        {{ $t('device.seeSessions') }}
      </router-link>
    </v-alert>
    <template v-else-if="configuration">
      <section aria-labelledby="device-request-heading" class="mb-4" data-testid="device-request">
        <h2 id="device-request-heading" class="text-h6 mb-2">{{ $t('device.requestTitle') }}</h2>
        <dl class="summary-list">
          <dt>{{ $t('device.client') }}</dt>
          <dd>
            {{ $t('device.clientName') }}
            (<span class="monospace">{{ configuration.client_id }}</span
            >)
          </dd>
          <dt>{{ $t('device.access') }}</dt>
          <dd>{{ readOnly ? $t('device.readOnly') : configuration.scopes.join(', ') }}</dd>
          <dt>{{ $t('device.origin') }}</dt>
          <dd>{{ $t('device.originText') }}</dd>
          <dt>{{ $t('device.account') }}</dt>
          <dd>
            {{ session.user?.name }}
            <template v-if="session.user?.organization">
              · {{ session.user.organization.name }}</template
            >
          </dd>
          <dt>{{ $t('device.platform') }}</dt>
          <dd class="monospace">{{ platform }}</dd>
        </dl>
      </section>

      <v-alert type="warning" variant="tonal" class="mb-4" data-testid="device-warning">
        <p>{{ $t('device.warning') }}</p>
        <p class="mt-1">{{ $t('device.expires', { n: minutes }) }}</p>
      </v-alert>

      <v-form @submit.prevent="approve">
        <v-text-field
          v-model="typed"
          :label="$t('device.code')"
          :hint="$t('device.codeHint')"
          persistent-hint
          :error-messages="codeError"
          autocomplete="off"
          autocapitalize="characters"
          spellcheck="false"
          class="mb-4 monospace"
          @blur="touched = true"
        />
        <div class="d-flex flex-wrap ga-2">
          <v-btn type="submit" color="primary" variant="flat" :disabled="!code">
            {{ $t('device.approve') }}
          </v-btn>
          <v-btn variant="outlined" @click="declined = true">{{ $t('device.decline') }}</v-btn>
        </div>
      </v-form>
    </template>
  </v-container>
</template>
