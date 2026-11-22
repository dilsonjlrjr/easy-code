<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { ProviderId } from '@/shared/api/contract'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import type { Model, Provider } from '../api/providersApi'
import { useProvidersStore } from '../stores/providersStore'

const props = defineProps<{ providers: Provider[]; model: Model | undefined }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [model: Model] }>()

const store = useProvidersStore()
const saving = ref(false)
const failure = ref<unknown>()
const MODEL_NAME_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/

const form = reactive<{
  provider: ProviderId
  model: string
  displayName: string
  active: boolean
  toolUse: boolean
  contextTokens: number
  promptCache: boolean
  imageInput: boolean
}>({
  provider: 'anthropic',
  model: '',
  displayName: '',
  active: true,
  toolUse: true,
  contextTokens: 200000,
  promptCache: false,
  imageInput: false,
})

const isEdit = computed(() => props.model !== undefined)
const providerOptions = computed(() =>
  props.providers.map((provider) => ({ title: provider.name, value: provider.id })),
)
const valid = computed(
  () =>
    MODEL_NAME_PATTERN.test(form.model) &&
    form.displayName.trim() !== '' &&
    Number.isInteger(form.contextTokens) &&
    form.contextTokens >= 1,
)

watch(open, (isOpen) => {
  if (!isOpen) return
  failure.value = undefined
  const current = props.model
  form.provider = current?.provider ?? props.providers[0]?.id ?? 'anthropic'
  form.model = current?.model ?? ''
  form.displayName = current?.display_name ?? ''
  form.active = current?.active ?? true
  form.toolUse = current?.capabilities.tool_use ?? true
  form.contextTokens = current?.capabilities.context_tokens ?? 200000
  form.promptCache = current?.capabilities.prompt_cache ?? false
  form.imageInput = current?.capabilities.image_input ?? false
})

async function save(): Promise<void> {
  if (!valid.value) return
  saving.value = true
  failure.value = undefined
  try {
    const saved = await store.saveModel(form.provider, form.model, {
      display_name: form.displayName.trim(),
      active: form.active,
      capabilities: {
        tool_use: form.toolUse,
        context_tokens: form.contextTokens,
        prompt_cache: form.promptCache,
        image_input: form.imageInput,
      },
    })
    open.value = false
    emit('saved', saved)
  } catch (error) {
    failure.value = error
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <v-dialog v-model="open" max-width="560">
    <v-card>
      <v-card-title>
        <h2 class="text-h6">
          {{ isEdit ? $t('settings.models.editTitle') : $t('settings.models.addTitle') }}
        </h2>
      </v-card-title>
      <v-form @submit.prevent="save">
        <v-card-text>
          <v-select
            v-model="form.provider"
            :items="providerOptions"
            :label="$t('settings.models.provider')"
            :disabled="isEdit"
          />
          <v-text-field
            v-model="form.model"
            :label="$t('settings.models.modelId')"
            :hint="$t('settings.models.modelIdHint')"
            persistent-hint
            :disabled="isEdit"
            autocomplete="off"
            spellcheck="false"
            class="mb-2"
          />
          <v-text-field
            v-model="form.displayName"
            :label="$t('settings.models.displayName')"
            maxlength="120"
          />
          <v-text-field
            v-model.number="form.contextTokens"
            type="number"
            min="1"
            :label="$t('settings.models.contextTokens')"
          />
          <fieldset class="capabilities">
            <legend class="text-subtitle-2">{{ $t('settings.models.capabilities') }}</legend>
            <v-checkbox
              v-model="form.toolUse"
              :label="$t('settings.models.toolUse')"
              hide-details
            />
            <v-checkbox
              v-model="form.promptCache"
              :label="$t('settings.models.promptCache')"
              hide-details
            />
            <v-checkbox
              v-model="form.imageInput"
              :label="$t('settings.models.imageInput')"
              hide-details
            />
          </fieldset>
          <v-checkbox
            v-model="form.active"
            :label="$t('settings.models.activeLabel')"
            hide-details
          />
          <ProblemAlert v-if="failure" :error="failure" class="mt-2" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="open = false">{{ $t('common.cancel') }}</v-btn>
          <v-btn type="submit" color="primary" variant="flat" :loading="saving" :disabled="!valid">
            {{ $t('common.save') }}
          </v-btn>
        </v-card-actions>
      </v-form>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.capabilities {
  border: 0;
  margin: 0 0 0.5rem;
  padding: 0;
}
</style>
