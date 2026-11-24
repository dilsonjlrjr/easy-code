<script setup lang="ts">
import { SUPPORTED_LOCALES } from '@/shared/i18n'
import { useInstallerStore } from '../installerStore'
import { useFieldErrors } from '../useFieldErrors'
import StepFooter from '../components/StepFooter.vue'

const store = useInstallerStore()
const errors = useFieldErrors()
const currencies = ['BRL', 'USD', 'EUR']
</script>

<template>
  <section>
    <p class="text-body-1 mb-4">{{ $t('installer.organization.intro') }}</p>
    <v-row dense>
      <v-col cols="12">
        <v-text-field
          v-model="store.organization.name"
          :label="$t('installer.organization.name')"
          :error-messages="errors('organization.name')"
        />
      </v-col>
      <v-col cols="12">
        <v-text-field
          v-model="store.organization.public_origin"
          :label="$t('installer.organization.publicOrigin')"
          :hint="$t('installer.organization.publicOriginHint')"
          persistent-hint
          inputmode="url"
          :error-messages="errors('organization.public_origin')"
        />
      </v-col>
      <v-col cols="12" sm="6">
        <v-select
          v-model="store.organization.currency"
          :label="$t('installer.organization.currency')"
          :items="currencies"
          :error-messages="errors('organization.currency')"
        />
      </v-col>
      <v-col cols="12" sm="6">
        <v-select
          v-model="store.organization.locale"
          :label="$t('installer.organization.locale')"
          :items="
            SUPPORTED_LOCALES.map((value) => ({ value, title: $t(`language.names.${value}`) }))
          "
          :error-messages="errors('organization.locale')"
        />
      </v-col>
    </v-row>
    <StepFooter :test="store.testOrganization" />
  </section>
</template>
