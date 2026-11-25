<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import { ApiError } from '@/shared/api/problemDetails'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { projectsApi } from '@/features/projects/api/projectsApi'
import {
  artifactsApi,
  type Artifact,
  type ArtifactEditStatus,
} from '@/features/artifacts/api/artifactsApi'
import { followArtifactEdit } from '@/features/artifacts/composables/artifactEditing'
import {
  emptyFields,
  fieldsComplete,
  fieldsOf,
  itemEdit,
  NFR_CATEGORIES,
  PRIORITIES,
  reviewHints,
  VERIFICATION_TYPES,
  type ActorFields,
  type EditorFields,
  type Kind,
  type NfrFields,
  type RequirementItem,
  type RuleFields,
  type StoryFields,
  type TermFields,
} from './requirementsView'

/**
 * ESP-07 editor (canvas "Editor"): the fields of a story, NFR, rule, actor or term. Saving sends the
 * whole file through `editArtifact` (US-53, ER-07-19): a new version with the person as author, valid
 * only after the Requisitos agent validates and formats it (ER-07-20). A refusal keeps the text (CA-07-26).
 */
const props = defineProps<{
  projectId: string
  kind: Kind
  operation: 'create' | 'modify'
  item?: RequirementItem
  /** Where a new rule (a story) or actor and term (the VISAO) goes. */
  targets: Artifact[]
}>()
const emit = defineEmits<{ submitted: [edit: ArtifactEditStatus]; cancel: [] }>()

const { t } = useI18n()
const id = useId()
const initial = props.item ? fieldsOf(props.item) : emptyFields(props.kind)
const fields = reactive(structuredClone(initial)) as EditorFields
const story = fields as StoryFields
const nfr = fields as NfrFields
const rule = fields as RuleFields
const actor = fields as ActorFields
const term = fields as TermFields
const target = ref(props.item?.artifact.id ?? props.targets[0]?.id ?? '')
const reason = ref('')
const sending = ref(false)
const failure = ref<unknown>()
const heading = ref<HTMLElement>()
const key = newIdempotencyKey()

const hints = computed(() => reviewHints(props.kind, fields))
const reasonValid = computed(() => {
  const length = reason.value.trim().length
  return length >= 3 && length <= 2000
})
const targetArtifact = computed(() =>
  props.targets.find((artifact) => artifact.id === target.value),
)
const needsTarget = computed(
  () => props.operation === 'create' && !['stories', 'nfrs'].includes(props.kind),
)
const ready = computed(
  () =>
    fieldsComplete(props.kind, fields) &&
    reasonValid.value &&
    (!needsTarget.value || targetArtifact.value !== undefined),
)
const title = computed(() =>
  props.operation === 'create'
    ? t(`stageRequirements.editor.new.${props.kind}`)
    : t('stageRequirements.editor.editing', { item: props.item?.label ?? '' }),
)

onMounted(async () => {
  await nextTick()
  heading.value?.focus()
})

function addCriterion(): void {
  story.criterios.push({
    id: `CA-${String(story.criterios.length + 1)}`,
    dado: '',
    quando: '',
    entao: '',
  })
}

function removeCriterion(index: number): void {
  story.criterios.splice(index, 1)
}

async function save(): Promise<void> {
  if (!ready.value) return
  const edit = itemEdit(
    props.operation,
    props.kind,
    fields,
    reason.value.trim(),
    props.item,
    targetArtifact.value,
  )
  if (!edit) return
  sending.value = true
  failure.value = undefined
  try {
    const accepted = await artifactsApi.editArtifact(props.projectId, edit, key)
    const read = () => artifactsApi.getArtifactEdit(props.projectId, accepted.edit)
    const guarded = await followArtifactEdit(await read(), read, (state) => state === 'pending')
    if (guarded.state === 'refused') {
      const command = await projectsApi.getCommand(props.projectId, guarded.command)
      throw new ApiError(command.refusal?.status ?? 409, command.refusal)
    }
    emit('submitted', guarded)
  } catch (error) {
    failure.value = error
  } finally {
    sending.value = false
  }
}
</script>

<template>
  <form class="ed" :aria-labelledby="`${id}-heading`" novalidate @submit.prevent="save">
    <div class="ed-head">
      <h4 :id="`${id}-heading`" ref="heading" tabindex="-1">{{ title }}</h4>
      <span class="chip o-voce">{{ $t('stageRequirements.editor.authorYou') }}</span>
    </div>

    <label v-if="needsTarget" :for="`${id}-target`" class="ed-field">
      <span class="ed-label">{{ $t(`stageRequirements.editor.target.${kind}`) }}</span>
      <select :id="`${id}-target`" v-model="target" class="fld">
        <option v-for="artifact in targets" :key="artifact.id" :value="artifact.id">
          {{ artifact.id }} · {{ artifact.title }}
        </option>
      </select>
    </label>

    <template v-if="kind === 'stories'">
      <label :for="`${id}-actor`" class="ed-field">
        <span class="ed-label">{{ $t('stageRequirements.fields.actor') }}</span>
        <input :id="`${id}-actor`" v-model="story.ator" class="fld" autocomplete="off" />
      </label>
      <label :for="`${id}-want`" class="ed-field">
        <span class="ed-label">{{ $t('stageRequirements.fields.want') }}</span>
        <input :id="`${id}-want`" v-model="story.quero" class="fld" autocomplete="off" />
      </label>
      <label :for="`${id}-so`" class="ed-field">
        <span class="ed-label">{{ $t('stageRequirements.fields.soThat') }}</span>
        <input :id="`${id}-so`" v-model="story.para" class="fld" autocomplete="off" />
      </label>
      <div class="ed-field">
        <span :id="`${id}-priority`" class="ed-label">{{
          $t('stageRequirements.fields.priority')
        }}</span>
        <div role="radiogroup" class="ed-radios" :aria-labelledby="`${id}-priority`">
          <button
            v-for="priority in PRIORITIES"
            :key="priority"
            type="button"
            role="radio"
            class="ib"
            :class="{ on: story.prioridade === priority }"
            :aria-checked="story.prioridade === priority ? 'true' : 'false'"
            @click="story.prioridade = priority"
          >
            {{ $t(`stageRequirements.priority.${priority}`) }}
          </button>
        </div>
      </div>
      <fieldset class="ed-criteria">
        <legend class="ed-label">{{ $t('stageRequirements.fields.criteria') }}</legend>
        <div v-for="(criterion, index) in story.criterios" :key="index" class="ca">
          <label :for="`${id}-ca-${index}-given`" class="ca-row">
            <span class="kw">{{ $t('stageRequirements.fields.given', { id: criterion.id }) }}</span>
            <input :id="`${id}-ca-${index}-given`" v-model="criterion.dado" class="fld" />
          </label>
          <label :for="`${id}-ca-${index}-when`" class="ca-row">
            <span class="kw">{{ $t('stageRequirements.fields.when', { id: criterion.id }) }}</span>
            <input :id="`${id}-ca-${index}-when`" v-model="criterion.quando" class="fld" />
          </label>
          <label :for="`${id}-ca-${index}-then`" class="ca-row">
            <span class="kw">{{ $t('stageRequirements.fields.then', { id: criterion.id }) }}</span>
            <input :id="`${id}-ca-${index}-then`" v-model="criterion.entao" class="fld" />
          </label>
          <button type="button" class="ib d ca-remove" @click="removeCriterion(index)">
            {{ $t('stageRequirements.editor.removeCriterion', { id: criterion.id }) }}
          </button>
        </div>
        <button type="button" class="ib" @click="addCriterion">
          {{ $t('stageRequirements.editor.addCriterion') }}
        </button>
      </fieldset>
    </template>

    <template v-else-if="kind === 'nfrs'">
      <label :for="`${id}-category`" class="ed-field">
        <span class="ed-label">{{ $t('stageRequirements.fields.category') }}</span>
        <select :id="`${id}-category`" v-model="nfr.categoria" class="fld">
          <option v-for="category in NFR_CATEGORIES" :key="category" :value="category">
            {{ $t(`stageRequirements.categories.${category}`) }}
          </option>
        </select>
      </label>
      <label :for="`${id}-requirement`" class="ed-field">
        <span class="ed-label">{{ $t('stageRequirements.fields.requirement') }}</span>
        <input :id="`${id}-requirement`" v-model="nfr.requisito" class="fld" />
      </label>
      <label :for="`${id}-metric`" class="ed-field">
        <span class="ed-label">{{ $t('stageRequirements.fields.metric') }}</span>
        <input :id="`${id}-metric`" v-model="nfr.metrica" class="fld" />
      </label>
      <label :for="`${id}-verification-type`" class="ed-field">
        <span class="ed-label">{{ $t('stageRequirements.fields.verificationType') }}</span>
        <select :id="`${id}-verification-type`" v-model="nfr.verificacaoTipo" class="fld">
          <option v-for="type in VERIFICATION_TYPES" :key="type" :value="type">
            {{ $t(`stageRequirements.verification.${type}`) }}
          </option>
        </select>
      </label>
      <label :for="`${id}-verification`" class="ed-field">
        <span class="ed-label">{{ $t('stageRequirements.fields.verification') }}</span>
        <input :id="`${id}-verification`" v-model="nfr.verificacaoDetalhe" class="fld" />
      </label>
    </template>

    <label v-else-if="kind === 'rules'" :for="`${id}-rule`" class="ed-field">
      <span class="ed-label">{{ $t('stageRequirements.fields.rule') }}</span>
      <textarea :id="`${id}-rule`" v-model="rule.descricao" class="fld" rows="3" />
    </label>

    <template v-else-if="kind === 'actors'">
      <label :for="`${id}-name`" class="ed-field">
        <span class="ed-label">{{ $t('stageRequirements.fields.actorName') }}</span>
        <input :id="`${id}-name`" v-model="actor.nome" class="fld" />
      </label>
      <label :for="`${id}-who`" class="ed-field">
        <span class="ed-label">{{ $t('stageRequirements.fields.actorWho') }}</span>
        <input :id="`${id}-who`" v-model="actor.quem_e" class="fld" />
      </label>
      <label :for="`${id}-need`" class="ed-field">
        <span class="ed-label">{{ $t('stageRequirements.fields.actorNeed') }}</span>
        <input :id="`${id}-need`" v-model="actor.necessidade" class="fld" />
      </label>
    </template>

    <template v-else>
      <label :for="`${id}-term`" class="ed-field">
        <span class="ed-label">{{ $t('stageRequirements.fields.term') }}</span>
        <input :id="`${id}-term`" v-model="term.termo" class="fld" />
      </label>
      <label :for="`${id}-definition`" class="ed-field">
        <span class="ed-label">{{ $t('stageRequirements.fields.definition') }}</span>
        <textarea :id="`${id}-definition`" v-model="term.definicao" class="fld" rows="3" />
      </label>
    </template>

    <label :for="`${id}-reason`" class="ed-field">
      <span class="ed-label">{{ $t('artifactEdit.reason') }}</span>
      <textarea
        :id="`${id}-reason`"
        v-model="reason"
        class="fld"
        rows="2"
        :aria-describedby="`${id}-reason-help`"
      />
    </label>
    <span :id="`${id}-reason-help`" class="t2 ed-help">{{ $t('artifactEdit.reasonHint') }}</span>

    <div class="ed-review" aria-live="polite">
      <span class="lb">{{ $t('stageRequirements.editor.review') }}</span>
      <span v-if="hints.length === 0" class="t2">{{ $t('stageRequirements.hints.none') }}</span>
      <span v-for="hint in hints" :key="hint.key + (hint.values?.id ?? '')" class="ed-hint">
        <span aria-hidden="true">!</span> {{ $t(hint.key, hint.values ?? {}) }}
      </span>
      <span class="t2">{{ $t('stageRequirements.editor.agentValidates') }}</span>
    </div>

    <ProblemAlert v-if="failure" :error="failure" data-testid="requirements-edit-failure" />
    <p v-if="failure" class="t2 ed-help">{{ $t('artifactEdit.keptText') }}</p>
    <div class="ed-actions">
      <button
        type="submit"
        class="btn btn-p"
        :disabled="sending || !ready"
        :aria-busy="sending ? 'true' : undefined"
        data-testid="requirements-save"
      >
        {{ $t('common.save') }}
      </button>
      <button type="button" class="btn" :disabled="sending" @click="emit('cancel')">
        {{ $t('common.cancel') }}
      </button>
    </div>
  </form>
</template>

<style scoped>
.ed {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.ed-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.ed-head h4 {
  margin: 0;
  font-size: 18px;
  line-height: 24px;
  font-weight: 700;
}

.ed-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.ed-label {
  font-size: 13px;
  font-weight: 600;
  color: #c7cceb;
}

.ed-help {
  margin: -8px 0 0;
  font-size: 12px;
}

.ed-radios {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.ed-criteria {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin: 0;
  padding: 0;
  border: 0;
}

.ca {
  display: grid;
  grid-template-columns: 1fr;
  gap: 6px;
  padding: 10px 12px;
  border: 1px solid rgba(140, 150, 255, 0.16);
  border-radius: 10px;
}

.ca-row {
  display: grid;
  grid-template-columns: minmax(110px, max-content) 1fr;
  gap: 8px;
  align-items: center;
}

.kw {
  font-size: 12px;
  font-weight: 700;
  color: #a78bfa;
}

.ca-remove {
  justify-self: end;
}

.ed-review {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 12px 14px;
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.04);
  font-size: 13px;
}

.ed-hint {
  color: #fcd34d;
}

.ed-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
</style>
