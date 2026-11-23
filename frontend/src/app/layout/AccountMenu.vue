<script setup lang="ts">
import { mdiAccountCircleOutline } from '@mdi/js'
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { LOGIN_REASONS, ROUTE_NAMES } from '@/app/router/routeNames'
import { useSessionStore } from '@/shared/session/sessionStore'

const session = useSessionStore()
const router = useRouter()
const userName = computed(() => session.user?.name ?? '')
const organizationName = computed(() => session.user?.organization?.name ?? '')

/** US-01 CA-3 / ESP-00 CA-00-16: ends the session; the next page asks to sign in. */
async function signOut(): Promise<void> {
  try {
    await session.signOut()
  } finally {
    await router.push({ name: ROUTE_NAMES.login, query: { reason: LOGIN_REASONS.signedOut } })
  }
}
</script>

<template>
  <v-menu v-if="session.user" location="bottom end">
    <template #activator="{ props: activatorProps }">
      <v-btn
        v-bind="activatorProps"
        :icon="mdiAccountCircleOutline"
        :aria-label="$t('account.menuLabel', { name: userName })"
        data-testid="account-menu"
      />
    </template>
    <v-card min-width="240">
      <v-card-text class="pb-2">
        <p class="font-weight-medium">{{ userName }}</p>
        <p class="text-body-2 text-medium-emphasis">{{ organizationName }}</p>
      </v-card-text>
      <v-divider />
      <v-list
        :aria-label="$t('account.menuLabel', { name: userName })"
        role="menu"
        density="comfortable"
      >
        <v-list-item
          role="menuitem"
          :to="{ name: ROUTE_NAMES.pendingItems }"
          :title="$t('account.myPendingItems')"
        />
        <v-list-item
          role="menuitem"
          :to="{ name: ROUTE_NAMES.projects }"
          :title="$t('account.myProjects')"
        />
        <v-list-item
          role="menuitem"
          :to="{ name: ROUTE_NAMES.localClientSessions }"
          :title="$t('account.localClient')"
          data-testid="account-local-client"
        />
        <v-list-item role="menuitem" :title="$t('account.signOut')" @click="signOut" />
      </v-list>
    </v-card>
  </v-menu>
</template>
