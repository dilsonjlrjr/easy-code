<script setup lang="ts">
import { computed } from 'vue'
import { useInstallerStore } from '../installerStore'
import { useFieldErrors } from '../useFieldErrors'
import { useReason } from '../useReason'
import PasswordField from '../components/PasswordField.vue'
import StepFooter from '../components/StepFooter.vue'

const store = useInstallerStore()
const errors = useFieldErrors()
const { actionText } = useReason()
const modes = ['new_realm', 'existing_realm'] as const
const actions = computed(() => store.ssoProbe?.actions ?? [])
const holders = computed(() => store.ssoProbe?.administrators ?? [])
</script>

<template>
  <section>
    <p class="text-body-1 mb-2">{{ $t('installer.keycloak.intro') }}</p>
    <p v-if="store.bundled" class="text-body-2 text-medium-emphasis mb-4">
      {{ $t('installer.keycloak.bundled') }}
    </p>
    <v-radio-group
      v-model="store.sso.mode"
      :label="$t('installer.keycloak.mode')"
      :error-messages="errors('sso.mode')"
      class="mb-2"
    >
      <v-radio
        v-for="mode in modes"
        :key="mode"
        :value="mode"
        :label="$t(`installer.keycloak.modes.${mode}`)"
      />
    </v-radio-group>
    <v-row dense>
      <v-col cols="12">
        <v-text-field
          v-model="store.sso.public_url"
          :label="$t('installer.keycloak.publicUrl')"
          :hint="$t('installer.keycloak.publicUrlHint')"
          persistent-hint
          inputmode="url"
          :error-messages="errors('sso.public_url')"
        />
      </v-col>
      <v-col cols="12">
        <v-text-field
          v-model="store.sso.internal_url"
          :label="$t('installer.keycloak.internalUrl')"
          :hint="$t('installer.keycloak.internalUrlHint')"
          persistent-hint
          inputmode="url"
          :error-messages="errors('sso.internal_url')"
        />
      </v-col>
      <v-col cols="12" sm="4">
        <v-text-field
          v-model="store.sso.realm"
          :label="$t('installer.keycloak.realm')"
          :error-messages="errors('sso.realm')"
        />
      </v-col>
      <v-col cols="12" sm="4">
        <v-text-field
          v-model="store.sso.admin_user"
          :label="
            store.sso.mode === 'new_realm'
              ? $t('installer.keycloak.adminUserNew')
              : $t('installer.keycloak.adminUserExisting')
          "
          autocomplete="off"
          :error-messages="errors('sso.admin_user')"
        />
      </v-col>
      <v-col cols="12" sm="4">
        <PasswordField
          v-model="store.sso.admin_password"
          :label="$t('installer.keycloak.adminPassword')"
          :error-messages="errors('sso.admin_password')"
        />
      </v-col>
    </v-row>
    <p class="text-body-2 text-medium-emphasis">{{ $t('installer.keycloak.credentialNote') }}</p>
    <v-alert
      v-if="actions.length > 0"
      type="info"
      variant="tonal"
      class="mt-4"
      :title="$t('installer.keycloak.willCreate')"
    >
      <ul class="pl-5">
        <li v-for="action in actions" :key="action">{{ actionText(action) }}</li>
      </ul>
      <p class="mt-2 text-body-2">{{ $t('installer.keycloak.manageUsersNote') }}</p>
    </v-alert>
    <v-alert v-if="holders.length > 0" type="warning" variant="tonal" class="mt-4">
      <p>{{ $t('installer.keycloak.currentAdministrators') }}</p>
      <ul class="pl-5">
        <li v-for="holder in holders" :key="holder.id">
          {{ holder.name }} ({{ holder.username }})
        </li>
      </ul>
    </v-alert>
    <StepFooter :test="store.testKeycloak" />
  </section>
</template>
