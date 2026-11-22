<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { safeReturnPath } from '@/app/router/safeReturnPath'
import { loadRuntimeConfig } from '@/shared/config/runtimeConfig'
import { useSessionStore } from '@/shared/session/sessionStore'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'

// SSO sign-in through Keycloak (ESP-02) arrives with MARCO-2. Until then the only way in is the fixed
// development identity, offered only when the runtime configuration enables it (profile marco1).
const devSignInEnabled = loadRuntimeConfig().devSignIn
const session = useSessionStore()
const route = useRoute()
const router = useRouter()
const signingIn = ref(false)
const failure = ref<unknown>()

async function signInWithDevIdentity(): Promise<void> {
  signingIn.value = true
  failure.value = undefined
  try {
    await session.signInWithDevIdentity()
    await router.replace(safeReturnPath(route.query.return))
  } catch (error) {
    failure.value = error
  } finally {
    signingIn.value = false
  }
}
</script>

<template>
  <v-container class="fill-height justify-center">
    <v-card max-width="440" width="100%" class="pa-6">
      <h1 class="text-h5 mb-2">{{ $t('pages.login.title') }}</h1>
      <p class="text-body-1 mb-6">{{ $t('pages.login.description') }}</p>
      <template v-if="devSignInEnabled">
        <v-btn
          color="primary"
          block
          :loading="signingIn"
          aria-describedby="dev-sign-in-explanation"
          @click="signInWithDevIdentity"
        >
          {{ $t('pages.login.devSignIn') }}
        </v-btn>
        <p id="dev-sign-in-explanation" class="text-body-2 text-medium-emphasis mt-4">
          {{ $t('pages.login.devSignInExplanation') }}
        </p>
        <ProblemAlert v-if="failure" :error="failure" class="mt-4" />
      </template>
      <template v-else>
        <v-btn color="primary" block disabled aria-describedby="sign-in-unavailable">
          {{ $t('pages.login.signIn') }}
        </v-btn>
        <p id="sign-in-unavailable" class="text-body-2 text-medium-emphasis mt-4">
          {{ $t('pages.login.unavailable') }}
        </p>
      </template>
    </v-card>
  </v-container>
</template>
