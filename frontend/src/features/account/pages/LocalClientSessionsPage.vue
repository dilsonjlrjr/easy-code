<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import AppBreadcrumbs from '@/shared/ui/AppBreadcrumbs.vue'
import ConfirmActionDialog from '@/shared/ui/ConfirmActionDialog.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { useSessionStore } from '@/shared/session/sessionStore'
import { localClientApi, ssoDevicePage, type LocalClientSession } from '../api/localClientApi'

/**
 * ESP-23 3.4 "Minha conta › Cliente local" (US-45 CA-5, RN-3; PD-08): every signed-in person sees
 * and revokes only their own desktop app sessions. A revoked session leaves the list and the app's
 * next call fails with "session revoked" within 60 s (CA-2, RNF-06). No token is ever shown. A new
 * session stays flagged until the person says it was them (S-19, RK-16); the code itself is
 * confirmed in the SSO device page, linked here.
 */
const { t } = useI18n()
const format = useFormat()
const { message, announce } = useAnnouncer()
const sessions = ref<LocalClientSession[]>([])
const target = ref<LocalClientSession>()
const dialogOpen = ref(false)
const sessionStore = useSessionStore()
const devicePage = ref<string>()
const actionFailure = ref<unknown>()
const acknowledging = ref<string>()

const loader = useLoader(async () => {
  sessions.value = await localClientApi.listSessions()
})
void loader.run()
// The SSO device page comes from the platform's parameters; without them the link is left out.
localClientApi
  .getConfiguration()
  .then((configuration) => {
    devicePage.value = ssoDevicePage(configuration)
  })
  .catch(() => undefined)

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
  await sessionStore.refresh().catch(() => undefined)
}

async function acknowledge(item: LocalClientSession): Promise<void> {
  acknowledging.value = item.id
  actionFailure.value = undefined
  try {
    await localClientApi.acknowledgeSession(item.id)
    sessions.value = sessions.value.map((entry) =>
      entry.id === item.id ? { ...entry, acknowledged: true } : entry,
    )
    announce(t('localClient.acknowledged'))
    await sessionStore.refresh().catch(() => undefined)
  } catch (error) {
    actionFailure.value = error
  } finally {
    acknowledging.value = undefined
  }
}
</script>

<template>
  <!-- Look: canvas "Configurações" (Configuracoes.dc.html), card "Cliente local". -->
  <AppBreadcrumbs :items="breadcrumbs" />
  <div>
    <h1>{{ $t('pages.localClient.title') }}</h1>
    <p class="page-description">{{ $t('localClient.about') }}</p>
  </div>
  <div role="status" class="sr-only">{{ message }}</div>
  <ProblemAlert v-if="actionFailure" :error="actionFailure" />

  <LoadingState v-if="loader.loading.value && !loader.loaded.value" type="table-row@2" />
  <ProblemAlert
    v-else-if="loader.error.value"
    :error="loader.error.value"
    retry
    @retry="loader.run"
  />
  <section v-else-if="sessions.length === 0" class="card panel" data-testid="no-local-sessions">
    <p class="empty-title">{{ $t('localClient.none') }}</p>
    <p class="t2 empty-help">{{ $t('localClient.howToSignIn') }}</p>
  </section>
  <section v-else class="card panel">
    <div class="table-scroll">
      <table class="tbl" data-testid="local-sessions">
        <caption class="sr-only">
          {{
            $t('pages.localClient.title')
          }}
        </caption>
        <thead>
          <tr>
            <th scope="col">{{ $t('localClient.device') }}</th>
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
            <td>
              <span class="mono device">{{ session.device }}</span>
              <span class="flags">
                <span v-if="session.current" class="chip info">
                  {{ $t('localClient.current') }}
                </span>
                <!-- S-19: a new session is flagged in text until the person confirms it. -->
                <span v-if="!session.acknowledged" class="chip warn" data-testid="session-new">
                  {{ $t('localClient.newSession') }}
                </span>
              </span>
            </td>
            <td>{{ format.dateTime(session.started_at) }}</td>
            <td>{{ format.dateTime(session.last_use_at) }}</td>
            <td class="mono">{{ session.ip_address ?? $t('localClient.unknownIp') }}</td>
            <td>
              <div class="row-actions">
                <button
                  v-if="!session.acknowledged"
                  type="button"
                  class="btn"
                  :disabled="acknowledging === session.id"
                  :aria-busy="acknowledging === session.id ? 'true' : undefined"
                  :aria-label="
                    $t('localClient.acknowledgeNamed', {
                      when: format.dateTime(session.started_at),
                    })
                  "
                  @click="acknowledge(session)"
                >
                  {{ $t('localClient.acknowledge') }}
                </button>
                <button
                  type="button"
                  class="btn btn-d"
                  :aria-label="
                    $t('localClient.revokeNamed', { when: format.dateTime(session.started_at) })
                  "
                  @click="ask(session)"
                >
                  {{ $t('localClient.revoke') }}
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
  <p v-if="devicePage" class="t2 code-help">
    {{ $t('localClient.codeHelp') }}
    <a :href="devicePage" data-testid="sso-device-page">{{ $t('localClient.confirmCodeLink') }}</a>
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
</template>

<style scoped>
.page-description {
  margin: 6px 0 0;
}

.panel {
  padding: 18px 22px;
}

.empty-title {
  margin: 0;
}

.empty-help {
  margin: 6px 0 0;
  font-size: 13px;
}

.tbl {
  min-width: 620px;
}

.tbl th {
  padding: 8px 12px 8px 0;
}

.tbl td {
  padding: 12px 12px 12px 0;
  vertical-align: middle;
}

.device {
  display: block;
}

.flags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 4px;
}

.flags:empty {
  display: none;
}

.row-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: flex-end;
}

.code-help {
  margin: 0;
  font-size: 13px;
}
</style>
