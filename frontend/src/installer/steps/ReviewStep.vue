<script setup lang="ts">
import { mdiPencil } from '@mdi/js'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useInstallerStore } from '../installerStore'

const store = useInstallerStore()
const i18n = useI18n()
const confirming = ref(false)
const masked = '••••••••'

const sections = computed(() => {
  const plan = store.plan
  const admin = plan.administrator.existing_id
    ? `${store.administratorChosen?.name ?? ''} (${store.administratorChosen?.username ?? ''})`
    : `${plan.administrator.first_name} ${plan.administrator.last_name} (${plan.administrator.username}, ${plan.administrator.email})`
  const user = plan.user
    ? plan.user.existing_id
      ? `${store.userChosen?.name ?? ''} (${store.userChosen?.username ?? ''})`
      : `${plan.user.first_name} ${plan.user.last_name} (${plan.user.username}, ${plan.user.email})`
    : null
  return [
    {
      step: 'database' as const,
      rows: [
        ['installer.database.host', `${plan.database.host}:${String(plan.database.port)}`],
        ['installer.database.name', plan.database.name],
        ['installer.database.adminUser', plan.database.admin_user],
        ['installer.database.adminPassword', masked],
      ],
    },
    {
      step: 'organization' as const,
      rows: [
        ['installer.organization.name', plan.organization.name],
        ['installer.organization.publicOrigin', plan.organization.public_origin],
        ['installer.organization.currency', plan.organization.currency],
      ],
    },
    {
      step: 'keycloak' as const,
      rows: [
        ['installer.keycloak.publicUrl', plan.sso.public_url],
        ['installer.review.mode', `installer.keycloak.modes.${plan.sso.mode}`],
        ['installer.keycloak.realm', plan.sso.realm],
        ['installer.keycloak.adminPassword', masked],
      ],
    },
    {
      step: 'administrator' as const,
      rows: [['installer.stepNames.administrator', admin]],
    },
    {
      step: 'user' as const,
      rows: [['installer.stepNames.user', user ?? 'installer.review.skipped']],
    },
    {
      step: 'skills' as const,
      rows: [
        [
          'installer.stepNames.skills',
          plan.skills.length > 0
            ? plan.skills.map((s) => s.file_name).join(', ')
            : 'installer.review.skipped',
        ],
      ],
    },
  ]
})

/** Values that are message keys (mode, skipped) are translated; the rest is shown as typed. */
function shown(value: string): string {
  return value.startsWith('installer.') && i18n.te(value) ? i18n.t(value) : value
}

async function install(): Promise<void> {
  confirming.value = false
  await store.install()
}
</script>

<template>
  <section>
    <p class="text-body-1 mb-4">{{ $t('installer.review.intro') }}</p>
    <v-card v-for="section in sections" :key="section.step" variant="outlined" class="mb-3">
      <v-card-title class="d-flex align-center text-subtitle-1">
        <h3 class="text-subtitle-1 font-weight-medium">
          {{ $t(`installer.stepNames.${section.step}`) }}
        </h3>
        <v-spacer />
        <v-btn
          variant="text"
          size="small"
          :prepend-icon="mdiPencil"
          @click="store.goTo(section.step)"
        >
          {{ $t('installer.review.edit') }}
          <span class="sr-only">{{ $t(`installer.stepNames.${section.step}`) }}</span>
        </v-btn>
      </v-card-title>
      <v-card-text>
        <dl class="summary-list">
          <template v-for="[label, value] in section.rows" :key="label">
            <dt>{{ $t(label ?? '') }}</dt>
            <dd>{{ shown(value ?? '') }}</dd>
          </template>
        </dl>
      </v-card-text>
    </v-card>
    <p class="text-body-2 text-medium-emphasis">{{ $t('installer.review.secretsNote') }}</p>
    <p class="text-body-2 text-medium-emphasis">{{ $t('installer.review.providerNote') }}</p>

    <div class="d-flex flex-wrap ga-3 mt-6">
      <v-btn variant="text" :disabled="store.busy" @click="store.back()">{{
        $t('installer.actions.back')
      }}</v-btn>
      <v-spacer />
      <v-btn
        color="primary"
        variant="flat"
        :loading="store.busy"
        data-testid="install"
        @click="confirming = true"
      >
        {{ $t('installer.actions.install') }}
      </v-btn>
    </div>
    <v-alert
      v-if="store.problems.length > 0 || store.generalProblem"
      type="error"
      variant="tonal"
      role="alert"
      class="mt-4"
    >
      {{ $t('installer.reasons.failed') }}
    </v-alert>

    <v-dialog
      v-model="confirming"
      max-width="480"
      role="alertdialog"
      aria-labelledby="confirm-title"
    >
      <v-card>
        <v-card-title id="confirm-title">{{ $t('installer.review.confirmTitle') }}</v-card-title>
        <v-card-text>{{ $t('installer.review.confirmText') }}</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="confirming = false">{{ $t('installer.review.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" data-testid="confirm-install" @click="install">
            {{ $t('installer.actions.install') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </section>
</template>
