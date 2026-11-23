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
  <section aria-labelledby="view-title">
    <h1 id="view-title" tabindex="-1">{{ t('account.title') }}</h1>
    <ErrorMessage :error="error" />
    <dl class="facts">
      <dt>{{ t('account.user') }}</dt>
      <dd>{{ status.user?.name ?? t('common.none') }}</dd>
      <template v-if="status.user?.organization?.name">
        <dt>{{ t('account.organization') }}</dt>
        <dd>{{ status.user.organization.name }}</dd>
      </template>
      <dt>{{ t('account.server') }}</dt>
      <dd>{{ status.server }}</dd>
      <dt>{{ t('account.appVersion') }}</dt>
      <dd>{{ status.app_version }}</dd>
      <dt>{{ t('account.serverVersion') }}</dt>
      <dd>{{ status.server_version ?? t('common.none') }}</dd>
    </dl>

    <div class="field">
      <label for="locale">{{ t('account.language') }}</label>
      <select id="locale" :value="status.locale" @change="onLocale">
        <option v-for="code in SUPPORTED_LOCALES" :key="code" :value="code">
          {{ t(`account.languages.${code}`) }}
        </option>
      </select>
    </div>
    <label class="check" for="sound">
      <input id="sound" type="checkbox" :checked="soundOn" @change="onSound" />
      {{ t('account.sound') }}
    </label>

    <div class="actions">
      <OpenWebButton
        :label="t('account.sessions')"
        :open="() => openWeb({ kind: 'account_local_client' })"
      />
      <button type="button" @click="update('SignOut')">{{ t('account.signOut') }}</button>
    </div>
  </section>
</template>
