<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import AppBreadcrumbs from '@/shared/ui/AppBreadcrumbs.vue'
import ConfirmActionDialog from '@/shared/ui/ConfirmActionDialog.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { localClientApi, type LocalClientSession } from '../api/localClientApi'

/**
 * ESP-23 3.4 "Minha conta › Cliente local" (US-45 CA-5, RN-3; PD-08): every signed-in person sees
 * and revokes only their own desktop app sessions. A revoked session leaves the list and the app's
 * next call fails with "session revoked" within 60 s (CA-2, RNF-06). No token is ever shown.
 */
const { t } = useI18n()
const format = useFormat()
const { message, announce } = useAnnouncer()
const sessions = ref<LocalClientSession[]>([])
const target = ref<LocalClientSession>()
const dialogOpen = ref(false)

const loader = useLoader(async () => {
  sessions.value = await localClientApi.listSessions()
})
void loader.run()

const breadcrumbs = computed(() => [
  { title: t('localClient.account') },
  { title: t('pages.localClient.title') },
])

function ask(session: LocalClientSession): void {
  target.value = session
  dialogOpen.value = true
}

async function revoke(): Promise<void> {
  const chosen = target.value
  if (!chosen) return
  await localClientApi.revokeSession(chosen.id)
  sessions.value = sessions.value.filter((item) => item.id !== chosen.id)
  announce(t('localClient.revoked'))
}
</script>

<template>
  <v-container max-width="1000">
    <AppBreadcrumbs :items="breadcrumbs" />
    <h1 class="text-h4 mb-2">{{ $t('pages.localClient.title') }}</h1>
    <p class="text-body-1 mb-4">{{ $t('localClient.about') }}</p>
    <div role="status" class="sr-only">{{ message }}</div>

    <LoadingState v-if="loader.loading.value && !loader.loaded.value" type="table-row@2" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <section v-else-if="sessions.length === 0" data-testid="no-local-sessions">
      <p class="text-body-1 mb-2">{{ $t('localClient.none') }}</p>
      <p class="text-body-2">{{ $t('localClient.howToSignIn') }}</p>
    </section>
    <div v-else class="table-scroll">
      <v-table density="comfortable" data-testid="local-sessions">
        <caption class="sr-only">
          {{
            $t('pages.localClient.title')
          }}
        </caption>
        <thead>
          <tr>
            <th scope="col">{{ $t('localClient.startedAt') }}</th>
            <th scope="col">{{ $t('localClient.lastUse') }}</th>
            <th scope="col">{{ $t('localClient.ipAddress') }}</th>
            <th scope="col">
              <span class="sr-only">{{ $t('common.actions') }}</span>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="session in sessions" :key="session.id" :data-testid="`session-${session.id}`">
            <td>{{ format.dateTime(session.started_at) }}</td>
            <td>
              {{
                session.last_use_at
                  ? format.dateTime(session.last_use_at)
                  : $t('localClient.neverUsed')
              }}
            </td>
            <td class="monospace">{{ session.ip_address ?? $t('localClient.unknownIp') }}</td>
            <td>
              <v-btn
                size="small"
                variant="outlined"
                color="error"
                :aria-label="
                  $t('localClient.revokeNamed', { when: format.dateTime(session.started_at) })
                "
                @click="ask(session)"
              >
                {{ $t('localClient.revoke') }}
              </v-btn>
            </td>
          </tr>
        </tbody>
      </v-table>
    </div>
    <p class="text-body-2 mt-4">
      <router-link :to="{ name: ROUTE_NAMES.deviceConfirmation }">
        {{ $t('localClient.confirmCodeLink') }}
      </router-link>
    </p>

    <ConfirmActionDialog
      v-model="dialogOpen"
      :title="$t('localClient.revokeTitle')"
      :lines="[
        $t('localClient.revokeEffect', {
          when: target ? format.dateTime(target.started_at) : '',
          ip: target?.ip_address ?? $t('localClient.unknownIp'),
        }),
        $t('localClient.revokeDelay'),
      ]"
      :keep-label="$t('localClient.keep')"
      :confirm-label="$t('localClient.revoke')"
      test-id="revoke-session-dialog"
      :submit="revoke"
    />
  </v-container>
</template>
