<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { bridge, toUIError } from '@/bridge'
import ErrorMessage from '@/components/ErrorMessage.vue'
import OpenWebButton from '@/components/OpenWebButton.vue'
import { SUPPORTED_LOCALES } from '@/i18n'
import { openWeb, store } from '@/store'
import type { Status, UIError } from '@/types'

// ESP-28 Conta: who is signed in, server and versions; language, notification sound and sign out. The
// sessions of the local client are managed on the web (Minha conta › Cliente local, PD-08).
const props = defineProps<{ status: Status }>()
const { t } = useI18n()
const error = ref<UIError | null>(null)
const soundOn = computed(() => !props.status.mute_sound)

async function update(method: string, ...args: unknown[]): Promise<void> {
  error.value = null
  try {
    store.status = await bridge().call<Status>(method, ...args)
  } catch (e) {
    error.value = toUIError(e)
  }
}

function onSound(event: Event): void {
  void update('SetMuteSound', !(event.target as HTMLInputElement).checked)
}

function onLocale(event: Event): void {
  void update('SetLocale', (event.target as HTMLSelectElement).value)
}
</script>

<template>
  <section class="view" aria-labelledby="view-title" style="max-width: 57.5rem">
    <h1 id="view-title" tabindex="-1">{{ t('account.title') }}</h1>
    <ErrorMessage :error="error" />

    <section class="card pad" aria-labelledby="person-title">
      <h2 id="person-title">{{ status.user?.name ?? t('common.none') }}</h2>
      <p v-if="status.user?.organization?.name" class="t2 small">
        {{ status.user.organization.name }}
      </p>
      <dl class="kv">
        <div>
          <dt>{{ t('account.user') }}</dt>
          <dd>{{ status.user?.name ?? t('common.none') }}</dd>
        </div>
        <div v-if="status.user?.organization?.name">
          <dt>{{ t('account.organization') }}</dt>
          <dd>{{ status.user.organization.name }}</dd>
        </div>
        <div>
          <dt>{{ t('account.server') }}</dt>
          <dd class="mono">{{ status.server }}</dd>
        </div>
        <div>
          <dt>{{ t('account.appVersion') }}</dt>
          <dd>{{ status.app_version }}</dd>
        </div>
        <div>
          <dt>{{ t('account.serverVersion') }}</dt>
          <dd>{{ status.server_version ?? t('common.none') }}</dd>
        </div>
      </dl>
    </section>

    <section class="card pad view" aria-labelledby="prefs-title" style="gap: 0.875rem">
      <h2 id="prefs-title">{{ t('account.preferences') }}</h2>
      <div class="field">
        <label for="locale">{{ t('account.language') }}</label>
        <select id="locale" class="fld" :value="status.locale" @change="onLocale">
          <option v-for="code in SUPPORTED_LOCALES" :key="code" :value="code">
            {{ t(`account.languages.${code}`) }}
          </option>
        </select>
      </div>
      <label class="check" for="sound">
        <input id="sound" type="checkbox" :checked="soundOn" @change="onSound" />
        {{ t('account.sound') }}
      </label>
      <p class="t2 small">{{ t('account.themeNote') }}</p>
    </section>

    <section class="card pad view" aria-labelledby="session-title" style="gap: 0.875rem">
      <h2 id="session-title">{{ t('account.session') }}</h2>
      <div class="actions">
        <OpenWebButton
          :label="t('account.sessions')"
          :open="() => openWeb({ kind: 'account_local_client' })"
        />
        <button type="button" class="btn danger" @click="update('SignOut')">
          {{ t('account.signOut') }}
        </button>
      </div>
    </section>
  </section>
</template>
