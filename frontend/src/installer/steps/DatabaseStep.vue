<script setup lang="ts">
import { computed } from 'vue'
import { useInstallerStore } from '../installerStore'
import { useFieldErrors } from '../useFieldErrors'
import PasswordField from '../components/PasswordField.vue'
import StepFooter from '../components/StepFooter.vue'

const store = useInstallerStore()
const errors = useFieldErrors()
const sslModes = ['disable', 'require', 'verify-full'] as const
const port = computed({
  get: () => String(store.database.port),
  set: (value: string) => {
    store.database.port = Number.parseInt(value, 10) || 0
  },
})
</script>

<template>
  <section>
    <p class="text-body-1 mb-2">{{ $t('installer.database.intro') }}</p>
    <p v-if="store.bundled" class="text-body-2 text-medium-emphasis mb-4">
      {{ $t('installer.database.bundled') }}
    </p>
    <v-row dense>
      <v-col cols="12" sm="8">
        <v-text-field
          v-model="store.database.host"
          :label="$t('installer.database.host')"
          :error-messages="errors('database.host')"
        />
      </v-col>
      <v-col cols="12" sm="4">
        <v-text-field
          v-model="port"
          :label="$t('installer.database.port')"
          inputmode="numeric"
          :error-messages="errors('database.port')"
        />
      </v-col>
      <v-col cols="12" sm="6">
        <v-text-field
          v-model="store.database.admin_user"
          :label="$t('installer.database.adminUser')"
          autocomplete="off"
          :error-messages="errors('database.admin_user')"
        />
      </v-col>
      <v-col cols="12" sm="6">
        <PasswordField
          v-model="store.database.admin_password"
          :label="$t('installer.database.adminPassword')"
          :error-messages="errors('database.admin_password')"
        />
      </v-col>
      <v-col cols="12" sm="6">
        <v-text-field
          v-model="store.database.name"
          :label="$t('installer.database.name')"
          :error-messages="errors('database.name')"
        />
      </v-col>
      <v-col cols="12" sm="6">
        <v-select
          v-model="store.database.ssl_mode"
          :label="$t('installer.database.sslMode')"
          :items="
            sslModes.map((value) => ({ value, title: $t(`installer.database.sslModes.${value}`) }))
          "
          :error-messages="errors('database.ssl_mode')"
        />
      </v-col>
    </v-row>
    <v-alert
      v-if="store.databaseProbe && store.passed('database')"
      type="info"
      variant="tonal"
      class="mt-2"
    >
      <p>{{ $t('installer.database.found', { version: store.databaseProbe.server_version }) }}</p>
      <p>
        {{
          store.databaseProbe.exists
            ? $t('installer.database.willUse', { name: store.database.name })
            : $t('installer.database.willCreate', { name: store.database.name })
        }}
      </p>
    </v-alert>
    <StepFooter :test="store.testDatabase" />
  </section>
</template>
