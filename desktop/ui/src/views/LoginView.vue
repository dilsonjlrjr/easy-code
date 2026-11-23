<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { bridge, toUIError } from '@/bridge'
import ErrorMessage from '@/components/ErrorMessage.vue'
import { minutesLeft } from '@/format'
import { useLabel } from '@/i18n'
import { store } from '@/store'
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
  <section class="login" aria-labelledby="view-title">
    <h1 id="view-title" tabindex="-1">{{ t('login.title') }}</h1>
    <p>{{ t('app.readOnly') }}</p>

    <p v-if="reasonText" class="notice" role="alert">{{ reasonText }}</p>
    <ErrorMessage :error="error" />

    <template v-if="waiting && status.device">
      <h2>{{ t('login.waiting') }}</h2>
      <p>
        {{
          t('login.instructions', {
            address: status.device.verification_uri,
            minutes,
          })
        }}
      </p>
      <p class="code-line">
        <span id="code-label">{{ t('login.codeLabel') }}</span>
        <output class="user-code" aria-labelledby="code-label">{{
          status.device.user_code
        }}</output>
      </p>
      <div class="actions">
        <button type="button" class="primary" @click="openBrowser">
          {{ t('login.openBrowser') }}
        </button>
        <button type="button" :disabled="busy" @click="cancel">{{ t('login.cancel') }}</button>
      </div>
    </template>

    <template v-else>
      <p>{{ t('login.notAuthenticated') }}</p>
      <p v-if="status.server_from_env">{{ t('login.serverFromEnv', { server: status.server }) }}</p>

      <form v-else-if="editingServer" class="server-form" @submit.prevent="saveServer">
        <label for="server">{{ t('login.serverLabel') }}</label>
        <input
          id="server"
          v-model="serverInput"
          type="url"
          inputmode="url"
          autocomplete="url"
          required
          aria-describedby="server-hint"
        />
        <p id="server-hint" class="hint">{{ t('login.serverHint') }}</p>
        <button type="submit" :disabled="busy">{{ t('login.saveServer') }}</button>
      </form>

      <p v-else-if="status.server">
        {{ t('login.serverCurrent', { server: status.server }) }}
        <button type="button" class="link-button" @click="editingServer = true">
          {{ t('login.changeServer') }}
        </button>
      </p>

      <template v-if="status.server && !editingServer">
        <p class="hint">{{ t('login.signInHint') }}</p>
        <button type="button" class="primary" :disabled="busy" @click="signIn">
          {{ status.reason === 'code_expired' ? t('login.tryAgain') : t('login.signIn') }}
        </button>
      </template>
    </template>
  </section>
</template>
