<script setup lang="ts">
import { mdiCheckCircle, mdiCloseCircle } from '@mdi/js'
import { useInstallerStore } from '../installerStore'
import { useFieldErrors } from '../useFieldErrors'
import StepFooter from '../components/StepFooter.vue'

const store = useInstallerStore()
const errors = useFieldErrors()
const items = ['database', 'keycloak', 'people', 'skills'] as const
</script>

<template>
  <section>
    <p class="text-body-1 mb-4">{{ $t('installer.welcome.intro') }}</p>
    <h3 class="text-subtitle-1 font-weight-medium mb-2">
      {{ $t('installer.welcome.whatHappens') }}
    </h3>
    <ol class="mb-6 pl-5">
      <li v-for="item in items" :key="item" class="text-body-2 mb-1">
        {{ $t(`installer.welcome.items.${item}`) }}
      </li>
    </ol>
    <form @submit.prevent="store.submitToken()">
      <v-text-field
        v-model="store.token"
        :label="$t('installer.welcome.token')"
        :hint="$t('installer.welcome.tokenHint')"
        persistent-hint
        :error-messages="errors('token')"
        autocomplete="off"
        spellcheck="false"
        class="token-field"
        data-testid="token-field"
        @update:model-value="store.tokenAccepted = false"
      />
    </form>
    <div v-if="store.environmentOk !== null" class="mt-4">
      <h3 class="text-subtitle-1 font-weight-medium mb-2">
        {{ $t('installer.welcome.environment') }}
      </h3>
      <p class="d-flex align-center ga-2 text-body-2">
        <v-icon
          :icon="store.environmentOk ? mdiCheckCircle : mdiCloseCircle"
          :color="store.environmentOk ? 'success' : 'error'"
          size="small"
          aria-hidden="true"
        />
        {{ $t('installer.welcome.configVolume') }}:
        {{
          store.environmentOk
            ? $t('installer.welcome.checkOk')
            : $t('installer.welcome.checkFailed')
        }}
      </p>
      <p class="text-body-2 text-medium-emphasis">
        {{ $t('installer.welcome.version', { version: store.version }) }}
      </p>
    </div>
    <StepFooter :test="store.submitToken" />
  </section>
</template>

<style scoped>
/* Only the typed token is monospace; the label and the hint keep the theme font. */
.token-field :deep(input) {
  font-family: 'Roboto Mono', ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
</style>
