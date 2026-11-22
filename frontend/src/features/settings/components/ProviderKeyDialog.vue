<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { hasProblemSlug, isApiError } from '@/shared/api/problemDetails'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import type { Provider } from '../api/providersApi'
import { useProvidersStore } from '../stores/providersStore'

const props = defineProps<{ provider: Provider | undefined }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [provider: Provider] }>()

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
  <v-dialog v-model="open" max-width="520" :persistent="testing">
    <v-card>
      <v-card-title>
        <h2 class="text-h6">
          {{
            isReplacement
              ? $t('settings.providers.replaceKeyTitle', { provider: providerName })
              : $t('settings.providers.addKeyTitle', { provider: providerName })
          }}
        </h2>
      </v-card-title>
      <v-form @submit.prevent="save">
        <v-card-text>
          <p class="text-body-2 mb-4">{{ $t('settings.providers.keyHelp') }}</p>
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
          <div role="status" class="text-body-2">
            <template v-if="testing">
              <v-progress-linear indeterminate class="mb-2" />
              {{ $t('settings.providers.testingKey') }}
            </template>
          </div>
          <v-alert v-if="invalidKeyMessage !== undefined" type="error" variant="tonal" role="alert">
            {{ $t('settings.providers.keyInvalid', { message: invalidKeyMessage }) }}
            <p class="mt-1">{{ $t('settings.providers.previousKeyKept') }}</p>
          </v-alert>
          <v-alert v-else-if="providerDidNotAnswer" type="error" variant="tonal" role="alert">
            {{ $t('settings.providers.keyNoAnswer') }}
          </v-alert>
          <ProblemAlert v-else-if="otherFailure" :error="failure" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="testing" @click="open = false">
            {{ $t('common.cancel') }}
          </v-btn>
          <v-btn
            type="submit"
            color="primary"
            variant="flat"
            :loading="testing"
            :disabled="key.length < MIN_KEY_LENGTH"
          >
            {{ $t('settings.providers.saveKey') }}
          </v-btn>
        </v-card-actions>
      </v-form>
    </v-card>
  </v-dialog>
</template>
