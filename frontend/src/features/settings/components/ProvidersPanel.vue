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
import { CLAUDE_CODE, CLAUDE_CODE_INSTALL, providerUsable, usesSubscription } from '../claudeCode'
import ModelDialog from './ModelDialog.vue'
import ProviderKeyDialog from './ProviderKeyDialog.vue'
import ProviderKeyGuideDialog from './ProviderKeyGuideDialog.vue'

const { t } = useI18n()
const format = useFormat()
const store = useProvidersStore()
const { message, announce } = useAnnouncer()
const loader = useLoader(async () => {
  await Promise.all([store.loadProviders(), store.loadModels()])
})

const keyDialogOpen = ref(false)
const keyDialogProvider = ref<Provider>()
const guideOpen = ref(false)
const guideProvider = ref<Provider>()
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

function openGuide(provider: Provider): void {
  guideProvider.value = provider
  guideOpen.value = true
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

/** US-58 CA-4: the price still estimates the cost, which the subscription covers. */
function coveredBySubscription(model: Model): boolean {
  return usesSubscription(store.providers.find((provider) => provider.id === model.provider))
}

function providerName(id: string): string {
  return store.providers.find((provider) => provider.id === id)?.name ?? id
}

/** US-58 CA-1: version and moment of the last Claude Code detection, never a credential. */
function detectionText(provider: Provider): string {
  const detection = provider.detection
  if (!detection) return ''
  const parts: string[] = []
  if (detection.detected && detection.version) {
    parts.push(t('settings.providers.claudeCode.version', { version: detection.version }))
  }
  parts.push(
    detection.checked_at
      ? t('settings.providers.claudeCode.checkedAt', {
          when: format.dateTime(detection.checked_at),
        })
      : t('settings.providers.claudeCode.notChecked'),
  )
  return parts.join(' · ')
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
          <!-- US-58 CA-1, CA-2: Claude Code detection on the server and the kind of credential. -->
          <span
            v-if="provider.detection"
            class="t2 provider-sub detection"
            :data-testid="`detection-${provider.id}`"
          >
            <span class="chip" :class="provider.detection.detected ? 'ok' : 'warn'">
              <span class="sr-only">{{ $t('settings.providers.claudeCode.detectionLabel') }}</span>
              {{
                provider.detection.detected
                  ? $t('settings.providers.claudeCode.detected')
                  : $t('settings.providers.claudeCode.notDetected')
              }}
            </span>
            <span>{{ detectionText(provider) }}</span>
          </span>
          <span
            v-if="provider.detection && provider.key.credential_kind"
            class="t2 provider-sub"
            :data-testid="`credential-kind-${provider.id}`"
          >
            {{ $t('settings.providers.claudeCode.credentialKindLabel') }}
            {{ $t(`settings.providers.claudeCode.credentialKind.${provider.key.credential_kind}`) }}
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
            class="btn btn-ghost"
            :aria-label="$t('settings.keyGuide.howToNamed', { provider: provider.name })"
            :data-testid="`key-guide-${provider.id}`"
            @click="openGuide(provider)"
          >
            <CanvasIcon name="question" :size="16" />
            {{
              provider.id === CLAUDE_CODE
                ? $t('settings.providers.claudeCode.howToToken')
                : $t('settings.keyGuide.howTo')
            }}
          </button>
          <button
            type="button"
            class="btn"
            :disabled="provider.key.status === 'testing'"
            @click="openKeyDialog(provider)"
          >
            {{
              provider.id === CLAUDE_CODE
                ? provider.key.status === 'active'
                  ? $t('settings.providers.claudeCode.replaceToken')
                  : $t('settings.providers.claudeCode.addToken')
                : provider.key.status === 'active'
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
        <div v-if="provider.detection" class="provider-notes">
          <div
            v-if="!provider.detection.detected"
            class="note note-warn"
            :data-testid="`install-${provider.id}`"
          >
            <CanvasIcon name="alert" />
            <div>
              <h4 class="install-title">{{ $t('settings.providers.claudeCode.installTitle') }}</h4>
              <p>{{ $t('settings.providers.claudeCode.installStep') }}</p>
              <code
                class="mono install-command"
                :aria-label="$t('settings.providers.claudeCode.installCommandLabel')"
                >{{ CLAUDE_CODE_INSTALL }}</code
              >
            </div>
          </div>
          <p v-if="!providerUsable(provider)" class="t2 provider-sub">
            {{ $t('settings.providers.claudeCode.notUsable') }}
          </p>
          <div
            v-if="usesSubscription(provider)"
            class="note note-warn"
            :data-testid="`subscription-${provider.id}`"
          >
            <CanvasIcon name="alert" />
            <div>
              <p>{{ $t('settings.providers.claudeCode.individualUse') }}</p>
              <p>{{ $t('settings.providers.claudeCode.costNote') }}</p>
            </div>
          </div>
        </div>
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
              <div v-if="coveredBySubscription(model)" class="t2">
                {{ $t('settings.providers.claudeCode.costCovered') }}
              </div>
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

  <ProviderKeyDialog
    v-model="keyDialogOpen"
    :provider="keyDialogProvider"
    @saved="onKeySaved"
    @guide="openGuide"
  />
  <ProviderKeyGuideDialog v-model="guideOpen" :provider="guideProvider" @add-key="openKeyDialog" />
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

.detection {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-top: 4px;
}

.provider-notes {
  flex-basis: 100%;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.provider-notes p {
  margin: 0;
}

.install-title {
  margin: 0 0 4px;
  font-size: 13px;
  font-weight: 700;
}

.install-command {
  display: inline-block;
  margin-top: 6px;
  word-break: break-all;
}

.provider-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.btn-ghost {
  background: transparent;
  border-color: transparent;
  color: #c4b5fd;
}

.btn-ghost:hover {
  background: rgba(139, 92, 246, 0.12);
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
