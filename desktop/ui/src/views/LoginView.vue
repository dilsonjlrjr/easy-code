<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { bridge, toUIError } from '@/bridge'
import ErrorMessage from '@/components/ErrorMessage.vue'
import { minutesLeft } from '@/format'
import { useLabel } from '@/i18n'
import { store } from '@/store'
import AppLogo from '@/components/AppLogo.vue'
import type { Status, UIError } from '@/types'

// US-45 / ESP-28: sign-in by device flow. The app shows the code and the address and opens the browser;
// no password is typed here.
const props = defineProps<{ status: Status }>()
const { t } = useI18n()
const label = useLabel()

const serverInput = ref(props.status.server ?? '')
const editingServer = ref(!props.status.server && !props.status.server_from_env)
const busy = ref(false)
const error = ref<UIError | null>(null)
const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | undefined

onMounted(() => {
  timer = setInterval(() => {
    now.value = Date.now()
  }, 15_000)
})
onBeforeUnmount(() => {
  clearInterval(timer)
})

const waiting = computed(() => props.status.phase === 'awaiting_approval' && props.status.device)
const reasonText = computed(() => {
  return props.status.reason ? label(`login.reasons.${props.status.reason}`, '') : ''
})
const minutes = computed(() =>
  props.status.device ? minutesLeft(props.status.device.expires_at, now.value) : 0,
)

async function run(action: () => Promise<Status>): Promise<void> {
  busy.value = true
  error.value = null
  try {
    store.status = await action()
  } catch (e) {
    error.value = toUIError(e)
  } finally {
    busy.value = false
  }
}

async function saveServer(): Promise<void> {
  await run(() => bridge().call<Status>('SetServer', serverInput.value))
  if (!error.value) editingServer.value = false
}

async function signIn(): Promise<void> {
  await run(() => bridge().call<Status>('SignIn'))
}

async function cancel(): Promise<void> {
  await run(() => bridge().call<Status>('CancelSignIn'))
}

async function openBrowser(): Promise<void> {
  error.value = null
  try {
    await bridge().call('OpenVerificationPage')
  } catch (e) {
    error.value = toUIError(e)
  }
}
</script>

<template>
  <section class="card pad login" aria-labelledby="view-title">
    <p class="brand">
      <AppLogo :size="64" />
      <span class="rlogo-name">
        <strong style="font-size: 1.375rem; line-height: 1.75rem">{{ t('app.title') }}</strong>
        <span class="t2 small">{{ t('app.subtitle') }}</span>
      </span>
    </p>
    <div>
      <h1 id="view-title" tabindex="-1" style="font-size: 1.5rem; line-height: 2rem">
        {{ t('login.title') }}
      </h1>
      <p class="t2">{{ waiting ? t('login.signInHint') : t('app.readOnly') }}</p>
    </div>
    <p v-if="reasonText" class="notice" role="alert">{{ reasonText }}</p>
    <ErrorMessage :error="error" />

    <template v-if="waiting && status.device">
      <div class="server-box">
        <span class="grow">
          <span class="lb">{{ t('account.server') }}</span>
          <span class="mono">{{ status.server }}</span>
        </span>
      </div>
      <section aria-labelledby="waiting-title" class="login">
        <div class="title-line">
          <span class="chip vio">{{ t('login.awaitingChip') }}</span>
          <h2 id="waiting-title" style="font-size: 1rem">{{ t('login.waiting') }}</h2>
        </div>
        <p class="visually-hidden">
          {{ t('login.instructions', { address: status.device.verification_uri, minutes }) }}
        </p>
        <p class="step" aria-hidden="true">
          <span class="num">{{ 1 }}</span>
          <span>{{ t('login.stepOpen', { address: status.device.verification_uri }) }}</span>
        </p>
        <p class="step" aria-hidden="true">
          <span class="num">{{ 2 }}</span><span>{{ t('login.stepCode') }}</span>
        </p>
        <p id="code-label" class="lb">{{ t('login.codeLabel') }}</p>
        <output class="user-code" aria-labelledby="code-label">{{
          status.device.user_code
        }}</output>
        <p class="t2 small">{{ t('login.expires', { minutes }) }}</p>
      </section>
      <div class="actions">
        <button type="button" class="btn primary" style="flex: 1 1 12rem" @click="openBrowser">
          {{ t('login.openBrowser') }}
        </button>
        <button type="button" class="btn" :disabled="busy" @click="cancel">
          {{ t('login.cancel') }}
        </button>
      </div>
    </template>

    <template v-else>
      <p>{{ t('login.notAuthenticated') }}</p>
      <p v-if="status.server_from_env" class="server-box">
        {{ t('login.serverFromEnv', { server: status.server }) }}
      </p>

      <form
        v-else-if="editingServer"
        class="field"
        style="max-width: none"
        @submit.prevent="saveServer"
      >
        <label for="server">{{ t('login.serverLabel') }}</label>
        <input
          id="server"
          v-model="serverInput"
          class="fld"
          type="url"
          inputmode="url"
          autocomplete="url"
          required
          aria-describedby="server-hint"
        />
        <p id="server-hint" class="hint small">{{ t('login.serverHint') }}</p>
        <button type="submit" class="btn" :disabled="busy" style="align-self: flex-start">
          {{ t('login.saveServer') }}
        </button>
      </form>

      <div v-else-if="status.server" class="server-box">
        <span class="grow">
          <span class="lb">{{ t('account.server') }}</span>
          <span class="mono">{{ t('login.serverCurrent', { server: status.server }) }}</span>
        </span>
        <button type="button" class="link-button" @click="editingServer = true">
          {{ t('login.changeServer') }}
        </button>
      </div>

      <template v-if="status.server && !editingServer">
        <p class="hint">{{ t('login.signInHint') }}</p>
        <button type="button" class="btn primary" :disabled="busy" @click="signIn">
          {{ status.reason === 'code_expired' ? t('login.tryAgain') : t('login.signIn') }}
        </button>
      </template>
    </template>

    <p class="note">{{ t('app.readOnly') }} {{ t('login.tokenNote') }}</p>
  </section>
</template>
