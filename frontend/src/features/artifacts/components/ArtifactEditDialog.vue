<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Command } from '@/shared/api/contract'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import { ApiError } from '@/shared/api/problemDetails'
import { waitForCommand } from '@/shared/commands/waitForCommand'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { projectsApi } from '@/features/projects/api/projectsApi'
import { ARTIFACT_EDIT_LIMIT, artifactsApi, type ArtifactEdit } from '../api/artifactsApi'
import {
  HUMAN_EDIT_TYPES,
  newArtifactTemplate,
  type HumanEditType,
} from '../composables/artifactEditing'

/**
 * US-53 (PD-03): a person creates, changes or removes an artifact item. What is sent is the whole
 * Markdown file plus the reason; the stage agent validates and formats it in an `edit_review` wave
 * before the version counts (RN-1). A refusal keeps the text typed so it can be fixed (CA-2).
 */
const props = defineProps<{
  projectId: string
  operation: ArtifactEdit['operation']
  /** The artifact changed or removed (modify, delete). */
  artifactId?: string
  baseVersion?: number
  /** The current Markdown file, the starting point of a change. */
  initialContent?: string
  /** The scope change finding that opened an approved artifact (CA-7). */
  finding?: string
  /** create: the type offered first. */
  initialType?: HumanEditType
}>()
const emit = defineEmits<{ submitted: [command: Command] }>()
const open = defineModel<boolean>({ required: true })

const { t } = useI18n()
const id = useId()
const type = ref<HumanEditType>(props.initialType ?? 'US')
const content = ref('')
const reason = ref('')
const sending = ref(false)
const failure = ref<unknown>()
let key = newIdempotencyKey()

watch(
  open,
  (value) => {
    if (!value) return
    key = newIdempotencyKey()
    failure.value = undefined
    reason.value = ''
    type.value = props.initialType ?? 'US'
    content.value =
      props.operation === 'create' ? newArtifactTemplate(type.value) : (props.initialContent ?? '')
  },
  { immediate: true },
)

function chooseType(next: HumanEditType): void {
  // A new type restarts the guide text only while the person has not written anything else.
  if (content.value === newArtifactTemplate(type.value)) content.value = newArtifactTemplate(next)
  type.value = next
}

const contentBytes = computed(() => new TextEncoder().encode(content.value).length)
const reasonValid = computed(() => {
  const length = reason.value.trim().length
  return length >= 3 && length <= 2000
})
const contentValid = computed(
  () =>
    props.operation === 'delete' ||
    (content.value.trim().length > 0 && contentBytes.value <= ARTIFACT_EDIT_LIMIT),
)
const title = computed(() => {
  if (props.operation === 'create') return t('artifactEdit.createTitle')
  if (props.operation === 'delete')
    return t('artifactEdit.deleteTitle', { artifact: props.artifactId ?? '' })
  return t('artifactEdit.modifyTitle', { artifact: props.artifactId ?? '' })
})
const typeItems = computed(() =>
  HUMAN_EDIT_TYPES.map((value) => ({
    value,
    title: value === 'ADR' ? t('artifactEdit.imposedAdr') : value,
  })),
)

function edit(): ArtifactEdit {
  const base = {
    reason: reason.value.trim(),
    ...(props.finding ? { finding: props.finding } : {}),
  }
  if (props.operation === 'create') {
    return { operation: 'create', type: type.value, content: content.value, ...base }
  }
  return {
    operation: props.operation,
    artifact: props.artifactId,
    base_version: props.baseVersion,
    ...(props.operation === 'modify' ? { content: content.value } : {}),
    ...base,
  }
}

async function submit(): Promise<void> {
  if (!reasonValid.value || !contentValid.value) return
  sending.value = true
  failure.value = undefined
  try {
    const accepted = await artifactsApi.editArtifact(props.projectId, edit(), key)
    const finished = await waitForCommand(accepted, (commandId) =>
      projectsApi.getCommand(props.projectId, commandId),
    )
    if (finished.state === 'refused') {
      throw new ApiError(finished.refusal?.status ?? 409, finished.refusal)
    }
    open.value = false
    emit('submitted', finished)
  } catch (error) {
    failure.value = error
  } finally {
    sending.value = false
  }
}
</script>

<template>
  <v-dialog
    v-model="open"
    max-width="900"
    :persistent="sending"
    :aria-labelledby="`${id}-title`"
    :aria-describedby="`${id}-description`"
  >
    <v-card data-testid="artifact-edit-dialog">
      <v-card-title>
        <h2 :id="`${id}-title`" class="text-h6 text-wrap">{{ title }}</h2>
      </v-card-title>
      <v-form @submit.prevent="submit">
        <v-card-text>
          <div :id="`${id}-description`">
            <p class="mb-2">{{ $t('artifactEdit.agentReview') }}</p>
            <p v-if="operation === 'delete'" class="mb-2">{{ $t('artifactEdit.deleteEffect') }}</p>
            <p v-if="finding" class="mb-2">
              {{ $t('artifactEdit.scopeChange', { finding }) }}
            </p>
          </div>
          <v-select
            v-if="operation === 'create'"
            :model-value="type"
            :items="typeItems"
            :label="$t('artifactEdit.type')"
            class="mb-2"
            @update:model-value="chooseType"
          />
          <p v-if="operation === 'create' && type === 'ADR'" class="text-body-2 mb-2">
            {{ $t('artifactEdit.imposedAdrHelp') }}
          </p>
          <v-textarea
            v-if="operation !== 'delete'"
            v-model="content"
            :label="$t('artifactEdit.content')"
            :hint="$t('artifactEdit.contentHint')"
            persistent-hint
            :error-messages="
              contentBytes > ARTIFACT_EDIT_LIMIT ? [$t('artifactEdit.contentTooLarge')] : []
            "
            rows="14"
            class="monospace mb-2"
            spellcheck="false"
            data-testid="artifact-edit-content"
          />
          <v-textarea
            v-model="reason"
            :label="$t('artifactEdit.reason')"
            :hint="$t('artifactEdit.reasonHint')"
            persistent-hint
            counter="2000"
            rows="2"
            auto-grow
          />
          <ProblemAlert v-if="failure" :error="failure" class="mt-3" />
          <p v-if="failure" class="text-body-2 mt-2">{{ $t('artifactEdit.keptText') }}</p>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="sending" @click="open = false">
            {{ $t('common.cancel') }}
          </v-btn>
          <v-btn
            type="submit"
            :color="operation === 'delete' ? 'error' : 'primary'"
            variant="flat"
            :loading="sending"
            :disabled="!reasonValid || !contentValid"
          >
            {{ $t(`artifactEdit.submit.${operation}`) }}
          </v-btn>
        </v-card-actions>
      </v-form>
    </v-card>
  </v-dialog>
</template>
