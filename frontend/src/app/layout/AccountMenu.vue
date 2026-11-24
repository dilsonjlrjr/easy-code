<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { LOGIN_REASONS, ROUTE_NAMES } from '@/app/router/routeNames'
import { useInterfaceLanguage } from '@/shared/composables/useInterfaceLanguage'
import { useSessionStore } from '@/shared/session/sessionStore'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'

defineProps<{ pendingCount: number }>()

const session = useSessionStore()
const router = useRouter()
const { availableLocales, currentLocale, changeLocale } = useInterfaceLanguage()

const userName = computed(() => session.user?.name ?? '')
const organizationName = computed(() => session.user?.organization?.name ?? '')
const initials = computed(() =>
  userName.value
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? '')
    .join(''),
)

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
  <v-menu v-if="session.user" location="bottom end" offset="8" :close-on-content-click="false">
    <template #activator="{ props: activatorProps, isActive }">
      <button
        v-bind="activatorProps"
        type="button"
        class="ibtn account-trigger"
        :class="{ open: isActive }"
        :aria-label="$t('account.menuLabel', { name: userName })"
        data-testid="account-menu"
      >
        <span class="avatar">{{ initials }}</span>
        <CanvasIcon name="chevronDown" :size="14" :stroke-width="2" class="chevron" />
      </button>
    </template>
    <div
      class="umenu account-menu"
      role="menu"
      :aria-label="$t('account.menuLabel', { name: userName })"
    >
      <div class="account-head">
        <span class="avatar avatar-large">{{ initials }}</span>
        <span class="account-identity">
          <strong>{{ userName }}</strong>
          <span v-if="organizationName" class="t2 account-detail">
            {{ $t('account.organizationSso', { organization: organizationName }) }}
          </span>
        </span>
      </div>
      <router-link role="menuitem" class="umi" :to="{ name: ROUTE_NAMES.pendingItems }">
        <CanvasIcon name="inbox" :size="18" />
        {{ $t('account.myPendingItems') }}
        <span v-if="pendingCount > 0" class="sbadge">{{ pendingCount }}</span>
      </router-link>
      <router-link role="menuitem" class="umi" :to="{ name: ROUTE_NAMES.projects }">
        <CanvasIcon name="folder" :size="18" />
        {{ $t('account.myProjects') }}
      </router-link>
      <router-link
        role="menuitem"
        class="umi"
        :to="{ name: ROUTE_NAMES.localClientSessions }"
        data-testid="account-local-client"
      >
        <CanvasIcon name="monitor" :size="18" />
        {{ $t('account.localClient') }}
      </router-link>
      <div class="umi language" role="group" :aria-label="$t('language.menuLabel')">
        <CanvasIcon name="globe" :size="18" />
        {{ $t('language.menuLabel') }}
        <span class="language-options">
          <button
            v-for="locale in availableLocales"
            :key="locale"
            type="button"
            role="menuitemradio"
            class="language-option"
            :class="{ on: locale === currentLocale }"
            :aria-checked="locale === currentLocale"
            :lang="locale"
            data-testid="language-option"
            @click="changeLocale(locale)"
          >
            {{ $t(`language.names.${locale}`) }}
          </button>
        </span>
      </div>
      <div class="account-foot">
        <button type="button" role="menuitem" class="umi sign-out" @click="signOut">
          <CanvasIcon name="signOut" :size="18" />
          {{ $t('account.signOut') }}
        </button>
      </div>
    </div>
  </v-menu>
</template>

<style scoped>
.account-trigger {
  width: auto;
  padding: 0 6px;
  border-radius: 24px;
  gap: 2px;
}

.account-trigger.open {
  background: rgba(255, 255, 255, 0.08);
}

.chevron {
  margin-left: 2px;
}

.avatar {
  width: 34px;
  height: 34px;
  border-radius: 17px;
  background: linear-gradient(135deg, #7c3aed, #a855f7);
  color: #ffffff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  font-size: 13px;
  flex: none;
}

.avatar-large {
  width: 40px;
  height: 40px;
  border-radius: 20px;
}

/* The canvas positions the menu itself; v-menu already places it under the trigger. */
.account-menu {
  position: static;
  color: #f1f3ff;
}

.account-head {
  display: flex;
  gap: 12px;
  align-items: center;
  padding: 14px 16px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
}

.account-identity {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.account-detail {
  font-size: 12px;
}

.language-options {
  margin-left: auto;
  display: flex;
  gap: 4px;
}

.language-option {
  border: 0;
  background: transparent;
  color: #a9b0d6;
  font: inherit;
  font-size: 12px;
  padding: 2px 6px;
  border-radius: 6px;
  cursor: pointer;
}

.language-option.on {
  color: #f1f3ff;
  background: rgba(99, 102, 241, 0.16);
}

.account-foot {
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  margin-top: 4px;
  padding-top: 4px;
}

.sign-out {
  width: 100%;
  border: 0;
  background: transparent;
  font: inherit;
  cursor: pointer;
  color: #fca5a5;
}

.sign-out:hover {
  color: #fca5a5;
}
</style>
