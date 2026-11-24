<script setup lang="ts">
import { mdiCheckCircle, mdiDownload, mdiOpenInNew } from '@mdi/js'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useInstallerStore } from '../installerStore'
import { useReason } from '../useReason'

const store = useInstallerStore()
const { t } = useI18n()
const { reasonText } = useReason()
const finished = ref(false)
const address = computed(() => store.status?.public_origin ?? store.organization.public_origin)
const admin = computed(() => store.status?.administrator)
const skillResults = computed(() => store.status?.skills ?? [])

async function finish(): Promise<void> {
  await store.finish()
  finished.value = true
}

/** ESP-31 "Baixar resumo": addresses, realm, clients and the administrator; never a password. */
function download(): void {
  const plan = store.plan
  const lines = [
    `# ${t('installer.summary.heading')}`,
    '',
    `${t('installer.done.address')}: ${address.value}`,
    `${t('installer.organization.name')}: ${plan.organization.name}`,
    `${t('installer.database.host')}: ${plan.database.host}:${String(plan.database.port)}/${plan.database.name}`,
    `${t('installer.keycloak.publicUrl')}: ${plan.sso.public_url}`,
    `${t('installer.keycloak.realm')}: ${plan.sso.realm} (${t(`installer.keycloak.modes.${plan.sso.mode}`)})`,
    'Clients: easycode-api, easycode-events, easycode-desktop',
    `${t('installer.done.administrator')}: ${admin.value?.name ?? ''} (${admin.value?.username ?? ''})`,
    ...skillResults.value.map(
      (s) => `- ${s.file_name}: ${s.status}${s.reason ? ` (${s.reason})` : ''}`,
    ),
    '',
    t('installer.summary.noSecrets'),
  ]
  const url = URL.createObjectURL(new Blob([lines.join('\n') + '\n'], { type: 'text/plain' }))
  const link = document.createElement('a')
  link.href = url
  link.download = 'easycode-instalacao.txt'
  link.click()
  URL.revokeObjectURL(url)
}
</script>

<template>
  <section>
    <v-alert type="success" variant="tonal" :icon="mdiCheckCircle" class="mb-4">
      <h3 class="text-h6">{{ $t('installer.done.title') }}</h3>
    </v-alert>
    <dl class="summary-list mb-4">
      <dt>{{ $t('installer.done.address') }}</dt>
      <dd>
        <a :href="address" target="_blank" rel="noopener">{{ address }}</a>
      </dd>
      <dt>{{ $t('installer.done.administrator') }}</dt>
      <dd>{{ admin?.name }} ({{ admin?.username }})</dd>
    </dl>
    <template v-if="skillResults.length > 0">
      <h3 class="text-subtitle-1 font-weight-medium mb-2">
        {{ $t('installer.done.skillResults') }}
      </h3>
      <ul class="pl-5 mb-4">
        <li v-for="skill in skillResults" :key="skill.file_name" class="text-body-2">
          {{ skill.file_name }}:
          {{
            skill.status === 'received'
              ? $t('installer.progress.statuses.done')
              : reasonText(skill.reason ?? 'failed')
          }}
        </li>
      </ul>
    </template>
    <h3 class="text-subtitle-1 font-weight-medium mb-2">{{ $t('installer.done.nextSteps') }}</h3>
    <ol class="pl-5 mb-6">
      <li class="text-body-2 mb-1">{{ $t('installer.done.nextKey') }}</li>
      <li class="text-body-2">{{ $t('installer.done.nextPassword') }}</li>
    </ol>
    <div role="status" aria-live="polite" class="mb-4">
      <p class="text-body-2">
        {{ finished ? $t('installer.done.waiting') : $t('installer.done.finishHint') }}
      </p>
    </div>
    <div class="d-flex flex-wrap ga-3">
      <v-btn variant="outlined" :prepend-icon="mdiDownload" @click="download">{{
        $t('installer.actions.download')
      }}</v-btn>
      <v-spacer />
      <v-btn v-if="!finished" color="primary" variant="flat" data-testid="finish" @click="finish">
        {{ $t('installer.actions.finish') }}
      </v-btn>
      <v-btn v-else color="primary" variant="flat" :href="address" :append-icon="mdiOpenInNew">
        {{ $t('installer.actions.enter') }}
      </v-btn>
    </div>
  </section>
</template>
