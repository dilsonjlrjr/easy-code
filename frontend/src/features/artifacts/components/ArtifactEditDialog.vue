<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import { ApiError } from '@/shared/api/problemDetails'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { projectsApi } from '@/features/projects/api/projectsApi'
import {
  ARTIFACT_EDIT_LIMIT,
  artifactsApi,
  type ArtifactEdit,
  type ArtifactEditStatus,
} from '../api/artifactsApi'
import {
  followArtifactEdit,
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
const emit = defineEmits<{ submitted: [edit: ArtifactEditStatus] }>()
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
const contentTooLarge = computed(() => contentBytes.value > ARTIFACT_EDIT_LIMIT)
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
    const read = () => artifactsApi.getArtifactEdit(props.projectId, accepted.edit)
    // The dialog waits only for the guard of the command; the agent's review follows on the page.
    const guarded = await followArtifactEdit(await read(), read, (state) => state === 'pending')
    if (guarded.state === 'refused') {
      const command = await projectsApi.getCommand(props.projectId, guarded.command)
      throw new ApiError(command.refusal?.status ?? 409, command.refusal)
    }
    open.value = false
    emit('submitted', guarded)
  } catch (error) {
    failure.value = error
  } finally {
    sending.value = false
  }
}
</script>

<template>
  <!-- Look: canvas dialog surface (#151B45 card) with `.fld` fields and `.btn` actions. -->
  <v-dialog
    v-model="open"
    max-width="900"
    :persistent="sending"
    :aria-labelledby="`${id}-title`"
    :aria-describedby="`${id}-description`"
  >
    <div class="dlg" data-testid="artifact-edit-dialog">
      <h2 :id="`${id}-title`">{{ title }}</h2>
      <form class="dlg-form" novalidate @submit.prevent="submit">
        <div :id="`${id}-description`" class="dlg-text">
          <p>{{ $t('artifactEdit.agentReview') }}</p>
          <p v-if="operation === 'delete'">{{ $t('artifactEdit.deleteEffect') }}</p>
          <p v-if="finding">
            {{ $t('artifactEdit.scopeChange', { finding }) }}
          </p>
        </div>
        <span v-if="operation === 'create'" class="dlg-field">
          <label :for="`${id}-type`" class="dlg-wrap">
            <span :id="`${id}-type-label`" class="dlg-label">{{ $t('artifactEdit.type') }}</span>
            <select
              :id="`${id}-type`"
              :aria-labelledby="`${id}-type-label`"
              class="fld"
              :value="type"
              @change="chooseType(($event.target as HTMLSelectElement).value as HumanEditType)"
            >
              <option v-for="item in typeItems" :key="item.value" :value="item.value">
                {{ item.title }}
              </option>
            </select>
          </label>
        </span>
        <p v-if="operation === 'create' && type === 'ADR'" class="t2 dlg-help">
          {{ $t('artifactEdit.imposedAdrHelp') }}
        </p>
        <span v-if="operation !== 'delete'" class="dlg-field" data-testid="artifact-edit-content">
          <label :for="`${id}-content`" class="dlg-wrap">
            <span :id="`${id}-content-label`" class="dlg-label">{{
              $t('artifactEdit.content')
            }}</span>
            <textarea
              :id="`${id}-content`"
              v-model="content"
              :aria-labelledby="`${id}-content-label`"
              class="fld mono"
              rows="14"
              spellcheck="false"
              :aria-invalid="contentTooLarge ? 'true' : 'false'"
              :aria-describedby="`${id}-content-messages`"
            />
          </label>
          <span :id="`${id}-content-messages`" class="dlg-messages" aria-live="polite">
            <span v-if="contentTooLarge" class="dlg-error">{{
              $t('artifactEdit.contentTooLarge')
            }}</span>
            <span v-else class="t2">{{ $t('artifactEdit.contentHint') }}</span>
          </span>
        </span>
        <span class="dlg-field">
          <label :for="`${id}-reason`" class="dlg-wrap">
            <span :id="`${id}-reason-label`" class="dlg-label">{{
              $t('artifactEdit.reason')
            }}</span>
            <textarea
              :id="`${id}-reason`"
              v-model="reason"
              :aria-labelledby="`${id}-reason-label`"
              class="fld"
              rows="2"
              :aria-describedby="`${id}-reason-messages`"
            />
          </label>
          <span :id="`${id}-reason-messages`" class="dlg-messages">
            <span class="t2">{{ $t('artifactEdit.reasonHint') }}</span>
            <span class="t2 dlg-counter">{{
              $t('common.counter', { n: reason.length, max: 2000 })
            }}</span>
          </span>
        </span>
        <ProblemAlert v-if="failure" :error="failure" data-testid="artifact-edit-failure" />
        <p v-if="failure" class="t2 dlg-help">{{ $t('artifactEdit.keptText') }}</p>
        <div class="dlg-actions">
          <button type="button" class="btn" :disabled="sending" @click="open = false">
            {{ $t('common.cancel') }}
          </button>
          <button
            type="submit"
            class="btn"
            :class="operation === 'delete' ? 'btn-d' : 'btn-p'"
            :disabled="sending || !reasonValid || !contentValid"
            :aria-busy="sending ? 'true' : undefined"
          >
            {{ $t(`artifactEdit.submit.${operation}`) }}
          </button>
        </div>
      </form>
    </div>
  </v-dialog>
</template>

<style scoped>
.dlg {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 22px 24px;
  background: #151b45;
  border: 1px solid rgba(140, 150, 255, 0.24);
  border-radius: 14px;
  box-shadow: 0 16px 48px rgba(0, 0, 0, 0.45);
  color: #f1f3ff;
  font-size: 14px;
  line-height: 20px;
  overflow-y: auto;
}

.dlg-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.dlg-text {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.dlg-text p,
.dlg-help {
  margin: 0;
}

.dlg-help {
  font-size: 13px;
}

.dlg-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.dlg-wrap {
  display: contents;
}

.dlg-label {
  font-weight: 600;
}

.dlg-messages {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  font-size: 12px;
  line-height: 16px;
}

.dlg-error {
  color: #fca5a5;
}

.dlg-counter {
  flex: none;
}

.fld[aria-invalid='true'] {
  border-color: #fca5a5;
}

.dlg-actions {
  display: flex;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 8px;
}
</style>
