<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { LOGIN_REASONS } from '@/app/router/routeNames'
import { safeReturnPath } from '@/app/router/safeReturnPath'
import { redirectToSso } from '@/shared/session/sessionApi'
import AppLogo from '@/shared/ui/AppLogo.vue'

/**
 * ESP-02: the page shows only after signing out, an expired session, denied access or a failed SSO
 * return (ER-02-10). Its single action leaves for the corporate SSO; the platform keeps no password.
 * Layout: canvas "Desktop · Login (SSO)".
 */
const props = defineProps<{ denied?: boolean }>()

const route = useRoute()
const redirecting = ref(false)
const signInButton = ref<HTMLButtonElement>()

const reason = computed(() => (props.denied ? undefined : route.query.reason))
const sessionExpired = computed(() => reason.value === LOGIN_REASONS.sessionExpired)
const loginFailed = computed(() => reason.value === LOGIN_REASONS.loginFailed)

function signIn(): void {
  redirecting.value = true
  redirectToSso(safeReturnPath(route.query.return))
}

onMounted(async () => {
  // ESP-02 section 8: the initial focus is on the sign-in button.
  await nextTick()
  signInButton.value?.focus()
})
</script>

<template>
  <div class="login">
    <section class="card login-card">
      <div class="login-brand">
        <AppLogo :width="76" :height="46" />
        <span class="login-brand-text">
          <span class="login-brand-name">{{ $t('app.brand') }}</span>
          <span class="t2 login-brand-tagline">{{ $t('app.tagline') }}</span>
        </span>
      </div>
      <div>
        <h1 class="login-title">
          {{ denied ? $t('pages.login.deniedTitle') : $t('pages.login.title') }}
        </h1>
        <p class="t2 login-description">{{ $t('pages.login.description') }}</p>
      </div>

      <!-- US-01 CA-2 / ER-02-4: no session and no project data, only the help to get access. -->
      <p v-if="denied" class="note note-crit" role="alert">{{ $t('pages.login.denied') }}</p>
      <p v-else-if="sessionExpired" class="note" role="status" data-testid="session-expired">
        {{ $t('pages.login.sessionExpired') }}
      </p>
      <p v-else-if="loginFailed" class="note note-warn" role="alert">
        {{ $t('pages.login.loginFailed') }}
      </p>
      <p v-else-if="reason === LOGIN_REASONS.signedOut" class="note note-ok" role="status">
        {{ $t('pages.login.signedOut') }}
      </p>

      <button
        ref="signInButton"
        type="button"
        class="btn btn-p login-action"
        :disabled="redirecting"
        :aria-busy="redirecting"
        @click="signIn"
      >
        {{ loginFailed ? $t('pages.login.tryAgain') : $t('pages.login.signIn') }}
      </button>
      <p class="t2 login-help">{{ $t('pages.login.help') }}</p>
    </section>
  </div>
</template>

<style scoped>
.login {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  box-sizing: border-box;
}

.login-card {
  width: 100%;
  max-width: 440px;
  padding: 36px 32px;
  display: flex;
  flex-direction: column;
  gap: 22px;
  align-items: stretch;
  box-sizing: border-box;
}

.login-brand {
  display: flex;
  align-items: center;
  gap: 12px;
}

.login-brand-text {
  display: flex;
  flex-direction: column;
}

.login-brand-name {
  font-size: 24px;
  line-height: 28px;
  font-weight: 700;
}

.login-brand-tagline {
  font-size: 13px;
}

.login-title {
  font-size: 24px;
  line-height: 32px;
}

.login-description {
  margin: 6px 0 0;
}

.login-action {
  min-height: 48px;
}

.login-help {
  margin: 0;
  font-size: 13px;
}

.note {
  margin: 0;
}

.note-crit {
  border-color: rgba(239, 68, 68, 0.45);
  background: rgba(239, 68, 68, 0.12);
}

.note-warn {
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.1);
}

.note-ok {
  border-color: rgba(34, 197, 94, 0.45);
  background: rgba(34, 197, 94, 0.1);
}
</style>
