<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import type { Model, PricesPerMillion, Provider } from '../api/providersApi'
import { useProvidersStore } from '../stores/providersStore'
import ModelDialog from './ModelDialog.vue'
import ProviderKeyDialog from './ProviderKeyDialog.vue'

const { t } = useI18n()
const format = useFormat()
const store = useProvidersStore()
const { message, announce } = useAnnouncer()
const loader = useLoader(async () => {
  await Promise.all([store.loadProviders(), store.loadModels()])
})

const keyDialogOpen = ref(false)
const keyDialogProvider = ref<Provider>()
const removeDialogOpen = ref(false)
const removeTarget = ref<Provider>()
const removing = ref(false)
const removeFailure = ref<unknown>()
const modelDialogOpen = ref(false)
const modelDialogModel = ref<Model>()

const PRICE_TYPES: Array<keyof PricesPerMillion> = ['input', 'output', 'read_cache', 'write_cache']

onMounted(() => {
  void loader.run()
})

function openKeyDialog(provider: Provider): void {
  keyDialogProvider.value = provider
  keyDialogOpen.value = true
}

function onKeySaved(provider: Provider): void {
  announce(t('settings.providers.keySavedAnnouncement', { provider: provider.name }))
}

function askRemove(provider: Provider): void {
  removeTarget.value = provider
  removeFailure.value = undefined
  removeDialogOpen.value = true
}

async function confirmRemove(): Promise<void> {
  if (!removeTarget.value) return
  removing.value = true
  try {
    await store.removeKey(removeTarget.value.id)
    announce(t('settings.providers.keyRemovedAnnouncement', { provider: removeTarget.value.name }))
    removeDialogOpen.value = false
  } catch (error) {
    removeFailure.value = error
  } finally {
    removing.value = false
  }
}

function openModelDialog(model?: Model): void {
  modelDialogModel.value = model
  modelDialogOpen.value = true
}

function yesNo(value: boolean): string {
  return value ? t('common.yes') : t('common.no')
}

/** Canvas chip tone for the key state; the state is always written as text too. */
function keyTone(provider: Provider): string {
  if (provider.key.status === 'active') return 'ok'
  if (provider.key.status === 'testing') return 'info'
  return 'warn'
}

function providerName(id: string): string {
  return store.providers.find((provider) => provider.id === id)?.name ?? id
}

function lastTestText(provider: Provider): string {
  const test = provider.key.last_test
  if (!test) return ''
  const when = format.dateTime(test.tested_at)
  if (test.result === 'valid') return t('settings.providers.lastTestValid', { when })
  if (test.result === 'invalid') {
    return t('settings.providers.lastTestInvalid', { when, message: test.message ?? '' })
  }
  return t('settings.providers.lastTestNoAnswer', { when })
}
</script>

<template>
  <!-- Layout: canvas "Configurações" (Configuracoes.dc.html), tab "Provedores e modelos". -->
  <section class="card panel" aria-labelledby="providers-heading">
    <h2 id="providers-heading">{{ $t('settings.providers.title') }}</h2>
    <p class="t2 panel-help">{{ $t('settings.providers.description') }}</p>
    <div role="status" class="sr-only">{{ message }}</div>

    <LoadingState v-if="loader.loading.value && !loader.loaded.value" type="card@2" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <ul v-else class="provider-list">
      <li
        v-for="provider in store.providers"
        :key="provider.id"
        class="li provider-row"
        :data-testid="`provider-${provider.id}`"
      >
        <div class="provider-main">
          <h3 class="provider-name">{{ provider.name }}</h3>
          <span v-if="provider.key.saved_at" class="t2 provider-sub">
            {{
              $t('settings.providers.savedBy', {
                when: format.dateTime(provider.key.saved_at),
                who: provider.key.saved_by?.name ?? '',
              })
            }}
          </span>
          <span v-if="provider.key.last_test" class="t2 provider-sub">
            {{ lastTestText(provider) }}
          </span>
        </div>
        <span v-if="provider.key.status === 'active' && provider.key.last_4" class="mono t2">
          {{ $t('settings.providers.keyEnding', { last4: provider.key.last_4 }) }}
        </span>
        <span class="chip" :class="keyTone(provider)">
          <span class="sr-only">{{ $t('settings.providers.keyStatusLabel') }}</span>
          {{ $t(`settings.providers.keyStatus.${provider.key.status}`) }}
        </span>
        <span class="provider-actions">
          <button
            type="button"
            class="btn"
            :disabled="provider.key.status === 'testing'"
            @click="openKeyDialog(provider)"
          >
            {{
              provider.key.status === 'active'
                ? $t('settings.providers.replaceKey')
                : $t('settings.providers.addKey')
            }}
          </button>
          <button
            v-if="provider.key.status === 'active'"
            type="button"
            class="btn btn-d"
            @click="askRemove(provider)"
          >
            {{ $t('settings.providers.removeKey') }}
          </button>
        </span>
      </li>
    </ul>
    <div class="note privacy">
      <CanvasIcon name="alert" />
      <span>{{ $t('settings.providers.privacy') }}</span>
    </div>
  </section>

  <section
    v-if="loader.loaded.value && !loader.error.value"
    class="card panel"
    aria-labelledby="models-heading"
  >
    <div class="panel-head">
      <h2 id="models-heading">{{ $t('settings.models.title') }}</h2>
      <button type="button" class="btn" @click="openModelDialog()">
        <CanvasIcon name="plus" :size="16" :stroke-width="2" />
        {{ $t('settings.models.add') }}
      </button>
    </div>
    <p class="t2 panel-help">{{ $t('settings.models.capabilityRule') }}</p>
    <p v-if="store.models.length === 0" class="empty">{{ $t('settings.models.empty') }}</p>
    <div v-else class="table-scroll">
      <table class="tbl">
        <thead>
          <tr>
            <th scope="col">{{ $t('settings.models.model') }}</th>
            <th scope="col">{{ $t('settings.models.active') }}</th>
            <th scope="col">{{ $t('settings.models.toolUse') }}</th>
            <th scope="col">{{ $t('settings.models.contextTokens') }}</th>
            <th scope="col">{{ $t('settings.models.promptCache') }}</th>
            <th scope="col">{{ $t('settings.models.imageInput') }}</th>
            <th scope="col">{{ $t('settings.models.currentPrice') }}</th>
            <th scope="col">
              <span class="sr-only">{{ $t('common.actions') }}</span>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="model in store.models" :key="`${model.provider}/${model.model}`">
            <td>
              <strong>{{ model.display_name }}</strong>
              <div class="t2">
                {{ providerName(model.provider) }} ·
                <span class="mono">{{ model.model }}</span>
              </div>
            </td>
            <td>
              <span class="chip" :class="model.active ? 'ok' : 'mute'">
                {{
                  model.active ? $t('settings.models.isActive') : $t('settings.models.isInactive')
                }}
              </span>
            </td>
            <td>{{ yesNo(model.capabilities.tool_use) }}</td>
            <td>{{ format.integer(model.capabilities.context_tokens) }}</td>
            <td>{{ yesNo(model.capabilities.prompt_cache) }}</td>
            <td>{{ yesNo(model.capabilities.image_input) }}</td>
            <td>
              <template v-if="model.current_price">
                <div v-for="priceType in PRICE_TYPES" :key="priceType">
                  <span class="t2">{{ $t(`settings.prices.types.${priceType}`) }}:</span>
                  {{
                    format.amount(
                      model.current_price.per_million_tokens[priceType],
                      model.current_price.currency,
                    )
                  }}
                </div>
              </template>
              <span v-else class="chip warn">{{ $t('settings.prices.noCurrentPrice') }}</span>
            </td>
            <td>
              <button
                type="button"
                class="btn"
                :aria-label="$t('settings.models.editNamed', { model: model.display_name })"
                @click="openModelDialog(model)"
              >
                {{ $t('common.edit') }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>

  <ProviderKeyDialog v-model="keyDialogOpen" :provider="keyDialogProvider" @saved="onKeySaved" />
  <ModelDialog
    v-model="modelDialogOpen"
    :providers="store.providers"
    :model="modelDialogModel"
    @saved="announce($t('settings.models.savedAnnouncement'))"
  />

  <v-dialog v-model="removeDialogOpen" max-width="480" aria-labelledby="remove-key-title">
    <v-card>
      <div class="dialog-body">
        <h2 id="remove-key-title">
          {{ $t('settings.providers.removeKeyTitle', { provider: removeTarget?.name ?? '' }) }}
        </h2>
        <p>{{ $t('settings.providers.removeKeyWarning') }}</p>
        <ProblemAlert v-if="removeFailure" :error="removeFailure" />
      </div>
      <div class="dialog-actions">
        <button type="button" class="btn" @click="removeDialogOpen = false">
          {{ $t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-d"
          :disabled="removing"
          :aria-busy="removing ? 'true' : undefined"
          @click="confirmRemove"
        >
          {{ $t('settings.providers.removeKey') }}
        </button>
      </div>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.panel {
  padding: 18px 22px;
}

.panel-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.panel-help {
  margin: 4px 0 8px;
  font-size: 13px;
}

.empty {
  margin: 0;
}

.provider-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.provider-row {
  flex-wrap: wrap;
}

.provider-main {
  flex: 1;
  min-width: 200px;
  display: flex;
  flex-direction: column;
}

.provider-name {
  margin: 0;
  font-size: 14px;
  line-height: 20px;
  font-weight: 700;
}

.provider-sub {
  font-size: 13px;
}

.provider-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.privacy {
  margin-top: 14px;
}

.privacy svg {
  flex: none;
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

.dialog-body {
  padding: 22px 24px 8px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.dialog-body p {
  margin: 0;
}

.dialog-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 24px 22px;
}
</style>
