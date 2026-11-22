<script setup lang="ts">
// Placeholder only: sign-in through Keycloak (SSO, ESP-02) arrives with the authentication feature.
</script>

<template>
  <v-container class="fill-height justify-center">
    <v-card max-width="440" width="100%" class="pa-6">
      <h1 class="text-h5 mb-2">{{ $t('pages.login.title') }}</h1>
      <p class="text-body-1 mb-6">{{ $t('pages.login.description') }}</p>
      <v-btn color="primary" block disabled aria-describedby="sign-in-unavailable">
        {{ $t('pages.login.signIn') }}
      </v-btn>
      <p id="sign-in-unavailable" class="text-body-2 text-medium-emphasis mt-4">
        {{ $t('pages.login.unavailable') }}
      </p>
    </v-card>
  </v-container>
</template>
