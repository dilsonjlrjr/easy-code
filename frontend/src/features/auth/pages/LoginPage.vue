<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { LOGIN_REASONS } from '@/app/router/routeNames'
import { safeReturnPath } from '@/app/router/safeReturnPath'
import { redirectToSso } from '@/shared/session/sessionApi'

/**
 * ESP-02: the page shows only after signing out, an expired session, denied access or a failed SSO
 * return (ER-02-10). Its single action leaves for the corporate SSO; the platform keeps no password.
 */
const props = defineProps<{ denied?: boolean }>()

const route = useRoute()
const redirecting = ref(false)
const signInButton = ref<{ $el: HTMLElement }>()

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
  signInButton.value?.$el.focus()
})
</script>

<template>
  <v-container class="fill-height justify-center">
    <v-card max-width="440" width="100%" class="pa-6">
      <p class="text-overline mb-0">{{ $t('app.name') }}</p>
      <p class="text-body-2 text-medium-emphasis mb-4">{{ $t('pages.login.subtitle') }}</p>
      <h1 class="text-h5 mb-2">
        {{ denied ? $t('pages.login.deniedTitle') : $t('pages.login.title') }}
      </h1>

      <!-- US-01 CA-2 / ER-02-4: no session and no project data, only the help to get access. -->
      <v-alert v-if="denied" type="error" variant="tonal" role="alert" class="mb-4">
        {{ $t('pages.login.denied') }}
      </v-alert>
      <v-alert
        v-else-if="sessionExpired"
        type="info"
        variant="tonal"
        role="status"
        class="mb-4"
        data-testid="session-expired"
      >
        {{ $t('pages.login.sessionExpired') }}
      </v-alert>
      <v-alert v-else-if="loginFailed" type="warning" variant="tonal" role="alert" class="mb-4">
        {{ $t('pages.login.loginFailed') }}
      </v-alert>
      <v-alert
        v-else-if="reason === LOGIN_REASONS.signedOut"
        type="success"
        variant="tonal"
        role="status"
        class="mb-4"
      >
        {{ $t('pages.login.signedOut') }}
      </v-alert>

      <p class="text-body-1 mb-6">{{ $t('pages.login.description') }}</p>
      <v-btn
        ref="signInButton"
        color="primary"
        block
        :loading="redirecting"
        :disabled="redirecting"
        @click="signIn"
      >
        {{ loginFailed ? $t('pages.login.tryAgain') : $t('pages.login.signIn') }}
      </v-btn>
      <p class="text-body-2 text-medium-emphasis mt-4">{{ $t('pages.login.help') }}</p>
    </v-card>
  </v-container>
</template>
