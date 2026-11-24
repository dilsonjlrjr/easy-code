<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmActionDialog from '@/shared/ui/ConfirmActionDialog.vue'
import type { NewAgentConfiguration, ProjectAgent } from '../api/agentsApi'
import {
  configurationChanges,
  formFromConfiguration,
  nextConfigurationFromForm,
  toolOptions,
  type ToolOption,
} from '../composables/agentConfigurationForm'

/**
 * ESP-22 "Editar configuração" (US-25): the instructions adjustment and the authorized tools of the
 * agent in this project. Only what the role allows can be checked (ER-22-2, no wildcard); saving
 * shows the summary of the changes first and creates a new version (CA-2). The adjustment is
 * untrusted text: it is never rendered as markup (ER-22-6).
 */
const props = defineProps<{
  agent: ProjectAgent
  /** Saves the new version; throws the refusal (409 `tool-not-allowed` with the name, 412). */
  submit: (next: NewAgentConfiguration) => Promise<void>
}>()
const emit = defineEmits<{ cancel: []; dirty: [value: boolean] }>()

const ADJUSTMENT_LIMIT = 20000
const i18n = useI18n()
const { t } = i18n
const form = ref(formFromConfiguration(props.agent.configuration))
const reviewOpen = ref(false)
const fieldId = useId()

const options = computed(() =>
  toolOptions(props.agent.configuration.tools, props.agent.allowed_by_role),
)
const groups = computed(() => {
  const byTool = new Map<string, ToolOption[]>()
  for (const option of options.value)
    byTool.set(option.tool, [...(byTool.get(option.tool) ?? []), option])
  return [...byTool.entries()].map(([tool, items]) => ({ tool, items }))
})
const changes = computed(() =>
  configurationChanges(props.agent.configuration, form.value, options.value),
)
const dirty = computed(() => changes.value.length > 0)
const tooLong = computed(() => form.value.adjustment.length > ADJUSTMENT_LIMIT)

watch(
  dirty,
  (value) => {
    emit('dirty', value)
  },
  { immediate: true },
)

function categoryName(id: string): string {
  const key = `evidence.categories.${id}`
  return i18n.te(key) ? t(key) : id
}

function optionLabel(option: ToolOption): string {
  if (option.tool === 'browser') return t('agentsPage.tools.browser')
  if (!option.value) return t('agentsPage.edit.anyCommand')
  return option.tool === 'execute_command' ? categoryName(option.value) : option.value
}

function changeLine(change: (typeof changes.value)[number]): string {
  if (change.kind === 'instructions') return t('agentsPage.edit.changedInstructions')
  const option = change.option
  const tool = option ? t(`agentsPage.tools.${option.tool}`) : ''
  const value = option ? optionLabel(option) : ''
  return t(`agentsPage.edit.${change.kind === 'tool_added' ? 'toolAdded' : 'toolRemoved'}`, {
    tool,
    value,
  })
}

const reviewLines = computed(() => [
  ...changes.value.map(changeLine),
  t('agentsPage.edit.newVersion', { n: props.agent.configuration.version + 1 }),
])

async function save(): Promise<void> {
  await props.submit(nextConfigurationFromForm(props.agent.configuration, form.value))
}
</script>

<template>
  <!-- Look: canvas "Agentes e skills" (Agentes.dc.html) form primitives (.fld, .lb, .btn). -->
  <form class="editor" data-testid="agent-editor" @submit.prevent="reviewOpen = true">
    <p v-if="dirty" class="chip warn unsaved" data-testid="unsaved-changes">
      {{ $t('agentsPage.edit.unsaved') }}
    </p>
    <div class="block">
      <h3 class="lb">{{ $t('agentsPage.edit.instructionsTitle') }}</h3>
      <p :id="`${fieldId}-help`" class="t2 help">{{ $t('agentsPage.edit.instructionsHelp') }}</p>
      <label :for="`${fieldId}-adjustment`" class="field">
        <span class="field-label">{{ $t('agentsPage.edit.adjustment') }}</span>
        <textarea
          :id="`${fieldId}-adjustment`"
          v-model="form.adjustment"
          class="fld"
          rows="6"
          :aria-invalid="tooLong ? 'true' : undefined"
          :aria-describedby="`${fieldId}-help ${fieldId}-counter${tooLong ? ` ${fieldId}-error` : ''}`"
          data-testid="agent-adjustment"
        />
      </label>
      <div class="field-foot">
        <span v-if="tooLong" :id="`${fieldId}-error`" class="field-error">
          {{ $t('agentsPage.edit.tooLong', { n: ADJUSTMENT_LIMIT }) }}
        </span>
        <span :id="`${fieldId}-counter`" class="t2 counter">
          {{ form.adjustment.length }} / {{ ADJUSTMENT_LIMIT }}
        </span>
      </div>
    </div>

    <div class="block">
      <h3 class="lb">{{ $t('agentsPage.tools.title') }}</h3>
      <p class="t2 help">{{ $t('agentsPage.edit.toolsHelp') }}</p>
      <p v-if="groups.length === 0" class="kv empty">{{ $t('agentsPage.tools.none') }}</p>
      <fieldset
        v-for="group in groups"
        :key="group.tool"
        class="tool-group"
        :data-testid="`tool-group-${group.tool}`"
      >
        <legend class="group-title">{{ $t(`agentsPage.tools.${group.tool}`) }}</legend>
        <label
          v-for="(option, index) in group.items"
          :key="option.key"
          :for="`${fieldId}-${group.tool}-${index}`"
          class="check"
        >
          <input
            :id="`${fieldId}-${group.tool}-${index}`"
            v-model="form.tools"
            type="checkbox"
            :value="option.key"
            :disabled="!option.allowed && !form.tools.includes(option.key)"
          />
          <span :class="{ mono: option.tool !== 'browser' && !!option.value }">
            {{ optionLabel(option) }}
          </span>
          <span v-if="!option.allowed" class="t2"> · {{ $t('agentsPage.edit.outsideRole') }}</span>
        </label>
      </fieldset>
    </div>

    <div class="actions">
      <button type="submit" class="btn btn-p" :disabled="!dirty || tooLong">
        {{ $t('agentsPage.edit.review') }}
      </button>
      <button type="button" class="btn" @click="emit('cancel')">{{ $t('common.cancel') }}</button>
    </div>

    <ConfirmActionDialog
      v-model="reviewOpen"
      :title="$t('agentsPage.edit.reviewTitle')"
      :lines="reviewLines"
      :keep-label="$t('agentsPage.edit.keepEditing')"
      :confirm-label="$t('agentsPage.edit.save')"
      confirm-color="primary"
      test-id="agent-review-dialog"
      :submit="save"
    />
  </form>
</template>

<style scoped>
.editor {
  display: flex;
  flex-direction: column;
  gap: 16px;
  margin-top: 8px;
}

.unsaved {
  align-self: flex-start;
  margin: 0;
}

.block {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.block h3 {
  margin: 0;
}

.help {
  margin: 0;
  font-size: 13px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 4px;
}

.field-label {
  font-weight: 600;
}

.field-foot {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  font-size: 12px;
}

.field-error {
  margin-right: auto;
  color: #fca5a5;
  font-weight: 600;
}

.empty {
  margin: 0;
}

.tool-group {
  border: 0;
  margin: 0;
  padding: 0;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  padding-top: 10px;
}

.group-title {
  padding: 0;
  font-weight: 600;
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

.check input:disabled,
.check input:disabled ~ span {
  cursor: not-allowed;
  opacity: 0.55;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
</style>
