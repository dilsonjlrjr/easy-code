<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
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
  <section aria-labelledby="providers-heading">
    <h2 id="providers-heading" class="text-h6 mb-2">{{ $t('settings.providers.title') }}</h2>
    <p class="text-body-2 text-medium-emphasis mb-4">{{ $t('settings.providers.privacy') }}</p>
    <div role="status" class="sr-only">{{ message }}</div>

    <LoadingState v-if="loader.loading.value && !loader.loaded.value" type="card@2" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else>
      <ul class="provider-list">
        <li v-for="provider in store.providers" :key="provider.id">
          <v-card variant="outlined" class="pa-4" :data-testid="`provider-${provider.id}`">
            <h3 class="text-subtitle-1 font-weight-medium">{{ provider.name }}</h3>
            <p class="mt-1">
              <span class="font-weight-medium">{{ $t('settings.providers.keyStatusLabel') }}</span>
              {{ $t(`settings.providers.keyStatus.${provider.key.status}`) }}
              <span
                v-if="provider.key.status === 'active' && provider.key.last_4"
                class="monospace"
              >
                · {{ $t('settings.providers.keyEnding', { last4: provider.key.last_4 }) }}
              </span>
            </p>
            <p v-if="provider.key.saved_at" class="text-body-2">
              {{
                $t('settings.providers.savedBy', {
                  when: format.dateTime(provider.key.saved_at),
                  who: provider.key.saved_by?.name ?? '',
                })
              }}
            </p>
            <p v-if="provider.key.last_test" class="text-body-2">{{ lastTestText(provider) }}</p>
            <div class="d-flex flex-wrap ga-2 mt-3">
              <v-btn
                color="primary"
                variant="tonal"
                :disabled="provider.key.status === 'testing'"
                @click="openKeyDialog(provider)"
              >
                {{
                  provider.key.status === 'active'
                    ? $t('settings.providers.replaceKey')
                    : $t('settings.providers.addKey')
                }}
              </v-btn>
              <v-btn
                v-if="provider.key.status === 'active'"
                variant="text"
                color="error"
                @click="askRemove(provider)"
              >
                {{ $t('settings.providers.removeKey') }}
              </v-btn>
            </div>
          </v-card>
        </li>
      </ul>

      <section aria-labelledby="models-heading" class="mt-8">
        <div class="d-flex flex-wrap align-center ga-2 mb-2">
          <h2 id="models-heading" class="text-h6">{{ $t('settings.models.title') }}</h2>
          <v-spacer />
          <v-btn color="primary" variant="tonal" @click="openModelDialog()">
            {{ $t('settings.models.add') }}
          </v-btn>
        </div>
        <p class="text-body-2 text-medium-emphasis mb-3">
          {{ $t('settings.models.capabilityRule') }}
        </p>
        <p v-if="store.models.length === 0" class="text-body-2">
          {{ $t('settings.models.empty') }}
        </p>
        <div v-else class="table-scroll">
          <v-table density="comfortable">
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
                  {{ model.display_name }}
                  <div class="text-body-2 text-medium-emphasis">
                    {{ providerName(model.provider) }} ·
                    <span class="monospace">{{ model.model }}</span>
                  </div>
                </td>
                <td>
                  {{
                    model.active ? $t('settings.models.isActive') : $t('settings.models.isInactive')
                  }}
                </td>
                <td>{{ yesNo(model.capabilities.tool_use) }}</td>
                <td>{{ format.integer(model.capabilities.context_tokens) }}</td>
                <td>{{ yesNo(model.capabilities.prompt_cache) }}</td>
                <td>{{ yesNo(model.capabilities.image_input) }}</td>
                <td>
                  <template v-if="model.current_price">
                    <div v-for="priceType in PRICE_TYPES" :key="priceType" class="text-body-2">
                      {{ $t(`settings.prices.types.${priceType}`) }}:
                      {{
                        format.amount(
                          model.current_price.per_million_tokens[priceType],
                          model.current_price.currency,
                        )
                      }}
                    </div>
                  </template>
                  <span v-else class="font-weight-medium">{{
                    $t('settings.prices.noCurrentPrice')
                  }}</span>
                </td>
                <td>
                  <v-btn
                    variant="text"
                    size="small"
                    :aria-label="$t('settings.models.editNamed', { model: model.display_name })"
                    @click="openModelDialog(model)"
                  >
                    {{ $t('common.edit') }}
                  </v-btn>
                </td>
              </tr>
            </tbody>
          </v-table>
        </div>
      </section>
    </template>

    <ProviderKeyDialog v-model="keyDialogOpen" :provider="keyDialogProvider" @saved="onKeySaved" />
    <ModelDialog
      v-model="modelDialogOpen"
      :providers="store.providers"
      :model="modelDialogModel"
      @saved="announce($t('settings.models.savedAnnouncement'))"
    />

    <v-dialog v-model="removeDialogOpen" max-width="480">
      <v-card>
        <v-card-title>
          <h2 class="text-h6">
            {{ $t('settings.providers.removeKeyTitle', { provider: removeTarget?.name ?? '' }) }}
          </h2>
        </v-card-title>
        <v-card-text>
          <p>{{ $t('settings.providers.removeKeyWarning') }}</p>
          <ProblemAlert v-if="removeFailure" :error="removeFailure" class="mt-2" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="removeDialogOpen = false">{{ $t('common.cancel') }}</v-btn>
          <v-btn color="error" variant="flat" :loading="removing" @click="confirmRemove">
            {{ $t('settings.providers.removeKey') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </section>
</template>

<style scoped>
.provider-list {
  display: grid;
  gap: 1rem;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  list-style: none;
  padding: 0;
}
</style>
