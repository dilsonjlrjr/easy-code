<script setup lang="ts">
import { computed, ref, watch } from 'vue'
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
  <v-form data-testid="agent-editor" @submit.prevent="reviewOpen = true">
    <p v-if="dirty" class="text-body-2 font-weight-medium mb-2" data-testid="unsaved-changes">
      {{ $t('agentsPage.edit.unsaved') }}
    </p>
    <h3 class="text-h6 mb-1">{{ $t('agentsPage.edit.instructionsTitle') }}</h3>
    <p class="text-body-2 text-medium-emphasis mb-2">
      {{ $t('agentsPage.edit.instructionsHelp') }}
    </p>
    <v-textarea
      v-model="form.adjustment"
      :label="$t('agentsPage.edit.adjustment')"
      :counter="ADJUSTMENT_LIMIT"
      :error-messages="tooLong ? [$t('agentsPage.edit.tooLong', { n: ADJUSTMENT_LIMIT })] : []"
      rows="6"
      auto-grow
      class="mb-4"
      data-testid="agent-adjustment"
    />

    <h3 class="text-h6 mb-1">{{ $t('agentsPage.tools.title') }}</h3>
    <p class="text-body-2 text-medium-emphasis mb-2">{{ $t('agentsPage.edit.toolsHelp') }}</p>
    <p v-if="groups.length === 0" class="text-body-2">{{ $t('agentsPage.tools.none') }}</p>
    <fieldset
      v-for="group in groups"
      :key="group.tool"
      class="tool-group mb-3"
      :data-testid="`tool-group-${group.tool}`"
    >
      <legend class="text-subtitle-2">{{ $t(`agentsPage.tools.${group.tool}`) }}</legend>
      <v-checkbox
        v-for="option in group.items"
        :key="option.key"
        v-model="form.tools"
        :value="option.key"
        :disabled="!option.allowed && !form.tools.includes(option.key)"
        density="compact"
        hide-details
      >
        <template #label>
          <span :class="{ monospace: option.tool !== 'browser' && !!option.value }">
            {{ optionLabel(option) }}
          </span>
          <span v-if="!option.allowed" class="ml-2 text-body-2">
            · {{ $t('agentsPage.edit.outsideRole') }}
          </span>
        </template>
      </v-checkbox>
    </fieldset>

    <div class="d-flex flex-wrap ga-2 mt-4">
      <v-btn type="submit" color="primary" variant="flat" :disabled="!dirty || tooLong">
        {{ $t('agentsPage.edit.review') }}
      </v-btn>
      <v-btn variant="text" @click="emit('cancel')">{{ $t('common.cancel') }}</v-btn>
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
  </v-form>
</template>

<style scoped>
.tool-group {
  border: 0;
  margin: 0;
  padding: 0;
}
</style>
