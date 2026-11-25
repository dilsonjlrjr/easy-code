<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { hasProblemSlug, isApiError } from '@/shared/api/problemDetails'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import type { Provider } from '../api/providersApi'
import { useProvidersStore } from '../stores/providersStore'

const props = defineProps<{ provider: Provider | undefined }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [provider: Provider]; guide: [provider: Provider] }>()

const store = useProvidersStore()
const key = ref('')
const testing = ref(false)
const failure = ref<unknown>()
const MIN_KEY_LENGTH = 8

const providerName = computed(() => props.provider?.name ?? '')
const isReplacement = computed(() => props.provider?.key.status === 'active')
const invalidKeyMessage = computed(() =>
  hasProblemSlug(failure.value, 'invalid-key')
    ? (failure.value.problem?.provider_message ?? failure.value.problem?.detail ?? '')
    : undefined,
)
const providerDidNotAnswer = computed(
  () => isApiError(failure.value) && failure.value.status === 503,
)
const otherFailure = computed(
  () => failure.value !== undefined && !invalidKeyMessage.value && !providerDidNotAnswer.value,
)

watch(open, (isOpen) => {
  // The key is never kept: the field is emptied whenever the dialog opens or closes (ESP-23 section 8).
  key.value = ''
  failure.value = undefined
  if (!isOpen) testing.value = false
})

/** The guide opens in its own dialog; this one closes so only one modal is on screen. */
function openGuide(): void {
  if (!props.provider) return
  open.value = false
  emit('guide', props.provider)
}

async function save(): Promise<void> {
  if (!props.provider || key.value.length < MIN_KEY_LENGTH) return
  testing.value = true
  failure.value = undefined
  try {
    const saved = await store.saveKey(props.provider.id, key.value)
    key.value = ''
    open.value = false
    emit('saved', saved)
  } catch (error) {
    failure.value = error
    key.value = ''
  } finally {
    testing.value = false
  }
}
</script>

<template>
  <!-- Look: canvas "Configurações" (Configuracoes.dc.html) primitives inside the dialog. -->
  <v-dialog v-model="open" max-width="520" :persistent="testing" aria-labelledby="key-dialog-title">
    <v-card>
      <form @submit.prevent="save">
        <div class="dialog-body">
          <h2 id="key-dialog-title">
            {{
              isReplacement
                ? $t('settings.providers.replaceKeyTitle', { provider: providerName })
                : $t('settings.providers.addKeyTitle', { provider: providerName })
            }}
          </h2>
          <p class="t2 help">{{ $t('settings.providers.keyHelp') }}</p>
          <button
            v-if="provider"
            type="button"
            class="guide-link"
            :disabled="testing"
            @click="openGuide"
          >
            {{ $t('settings.keyGuide.whereIsKey', { provider: providerName }) }}
          </button>
          <v-text-field
            v-model="key"
            type="password"
            autocomplete="off"
            spellcheck="false"
            :label="$t('settings.providers.keyLabel')"
            :disabled="testing"
            :rules="[
              (value: string) =>
                value.length >= MIN_KEY_LENGTH || $t('settings.providers.keyTooShort'),
            ]"
          />
          <div role="status" class="t2 testing">
            <template v-if="testing">
              <v-progress-linear indeterminate color="#7C3AED" class="progress" />
              {{ $t('settings.providers.testingKey') }}
            </template>
          </div>
          <v-alert v-if="invalidKeyMessage !== undefined" type="error" variant="tonal" role="alert">
            {{ $t('settings.providers.keyInvalid', { message: invalidKeyMessage }) }}
            <p class="kept">{{ $t('settings.providers.previousKeyKept') }}</p>
          </v-alert>
          <v-alert v-else-if="providerDidNotAnswer" type="error" variant="tonal" role="alert">
            {{ $t('settings.providers.keyNoAnswer') }}
          </v-alert>
          <ProblemAlert v-else-if="otherFailure" :error="failure" />
        </div>
        <div class="dialog-actions">
          <button type="button" class="btn" :disabled="testing" @click="open = false">
            {{ $t('common.cancel') }}
          </button>
          <button
            type="submit"
            class="btn btn-p"
            :disabled="testing || key.length < MIN_KEY_LENGTH"
            :aria-busy="testing ? 'true' : undefined"
          >
            {{ $t('settings.providers.saveKey') }}
          </button>
        </div>
      </form>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.dialog-body {
  padding: 22px 24px 8px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.help {
  margin: 0;
  font-size: 13px;
}

.testing {
  font-size: 13px;
}

.guide-link {
  align-self: flex-start;
  min-height: 32px;
  padding: 0;
  border: 0;
  background: transparent;
  color: #c4b5fd;
  font: inherit;
  font-size: 13px;
  font-weight: 600;
  text-decoration: underline;
  text-underline-offset: 3px;
  cursor: pointer;
}

.guide-link:focus-visible {
  outline: 2px solid #c4b5fd;
  outline-offset: 2px;
}

.progress {
  margin-bottom: 8px;
}

.kept {
  margin: 4px 0 0;
}

.dialog-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 24px 22px;
}
</style>
