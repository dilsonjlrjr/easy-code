<script setup lang="ts">
import { computed, reactive, ref, useId, watch } from 'vue'
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
const fieldId = useId()
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
  <!-- Look: canvas "Configurações" (Configuracoes.dc.html) primitives (.fld, .btn) inside the dialog. -->
  <v-dialog v-model="open" max-width="560" aria-labelledby="model-dialog-title">
    <v-card>
      <form @submit.prevent="save">
        <div class="dialog-body">
          <h2 id="model-dialog-title">
            {{ isEdit ? $t('settings.models.editTitle') : $t('settings.models.addTitle') }}
          </h2>
          <div class="field">
            <label :for="`${fieldId}-provider`" class="control">
              <span class="field-label">{{ $t('settings.models.provider') }}</span>
              <select
                :id="`${fieldId}-provider`"
                v-model="form.provider"
                class="fld"
                :disabled="isEdit"
              >
                <option v-for="option in providerOptions" :key="option.value" :value="option.value">
                  {{ option.title }}
                </option>
              </select>
            </label>
          </div>
          <div class="field">
            <label :for="`${fieldId}-model`" class="control">
              <span class="field-label">{{ $t('settings.models.modelId') }}</span>
              <input
                :id="`${fieldId}-model`"
                v-model="form.model"
                class="fld"
                :disabled="isEdit"
                autocomplete="off"
                spellcheck="false"
                :aria-describedby="`${fieldId}-model-hint`"
              />
            </label>
            <span :id="`${fieldId}-model-hint`" class="t2 hint">
              {{ $t('settings.models.modelIdHint') }}
            </span>
          </div>
          <div class="field">
            <label :for="`${fieldId}-name`" class="control">
              <span class="field-label">{{ $t('settings.models.displayName') }}</span>
              <input
                :id="`${fieldId}-name`"
                v-model="form.displayName"
                class="fld"
                maxlength="120"
              />
            </label>
          </div>
          <div class="field">
            <label :for="`${fieldId}-context`" class="control">
              <span class="field-label">{{ $t('settings.models.contextTokens') }}</span>
              <input
                :id="`${fieldId}-context`"
                v-model.number="form.contextTokens"
                class="fld"
                type="number"
                min="1"
              />
            </label>
          </div>
          <fieldset class="capabilities">
            <legend class="lb">{{ $t('settings.models.capabilities') }}</legend>
            <label :for="`${fieldId}-toolUse`" class="check">
              <input :id="`${fieldId}-toolUse`" v-model="form.toolUse" type="checkbox" />
              {{ $t('settings.models.toolUse') }}
            </label>
            <label :for="`${fieldId}-promptCache`" class="check">
              <input :id="`${fieldId}-promptCache`" v-model="form.promptCache" type="checkbox" />
              {{ $t('settings.models.promptCache') }}
            </label>
            <label :for="`${fieldId}-imageInput`" class="check">
              <input :id="`${fieldId}-imageInput`" v-model="form.imageInput" type="checkbox" />
              {{ $t('settings.models.imageInput') }}
            </label>
          </fieldset>
          <label :for="`${fieldId}-active`" class="check">
            <input :id="`${fieldId}-active`" v-model="form.active" type="checkbox" />
            {{ $t('settings.models.activeLabel') }}
          </label>
          <ProblemAlert v-if="failure" :error="failure" />
        </div>
        <div class="dialog-actions">
          <button type="button" class="btn" @click="open = false">{{ $t('common.cancel') }}</button>
          <button
            type="submit"
            class="btn btn-p"
            :disabled="saving || !valid"
            :aria-busy="saving ? 'true' : undefined"
          >
            {{ $t('common.save') }}
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
  gap: 14px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.control {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.field-label {
  font-weight: 600;
}

.hint {
  font-size: 12px;
}

.fld:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.capabilities {
  border: 0;
  margin: 0;
  padding: 0;
}

.capabilities legend {
  padding: 0;
  margin-bottom: 2px;
}

.check {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 44px;
  cursor: pointer;
}

.check input {
  width: 18px;
  height: 18px;
  margin: 0;
  flex: none;
  accent-color: #7c3aed;
  cursor: pointer;
}

.dialog-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 24px 22px;
}
</style>
