<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import type { PinnedSkill, Skill } from '../api/agentsApi'
import { canAssociate } from '../composables/skillAssociation'

/**
 * ESP-22 "Associar skill" (PC-37): one picker with an origin filter (catalog or organization); each
 * option shows description, stages and agents, version and estimated tokens (US-43 CA-1), and only
 * `unblocked` versions can be chosen (CA-22-5, ER-22-3).
 */
const props = defineProps<{
  agent: string
  agentName: string
  pinned: PinnedSkill[]
  list: (filter: { agent: string; origin?: Skill['origin'] }) => Promise<Skill[]>
  submit: (skill: Skill) => Promise<void>
}>()

const open = defineModel<boolean>({ required: true })
const { t } = useI18n()
const format = useFormat()
const origin = ref<'all' | Skill['origin']>('all')
const skills = ref<Skill[]>([])
const chosen = ref<string>()
const loading = ref(false)
const loadFailure = ref<unknown>()
const saving = ref(false)
const failure = ref<unknown>()
const fieldId = useId()

const originChoices = computed<Array<{ value: 'all' | Skill['origin']; label: string }>>(() => [
  { value: 'all', label: t('agentsPage.skills.originAll') },
  { value: 'native', label: t('agentsPage.skills.origin.native') },
  { value: 'organization', label: t('agentsPage.skills.origin.organization') },
])
const chosenSkill = computed(() => skills.value.find((skill) => skill.id === chosen.value))

async function load(): Promise<void> {
  loading.value = true
  loadFailure.value = undefined
  try {
    skills.value = await props.list({
      agent: props.agent,
      origin: origin.value === 'all' ? undefined : origin.value,
    })
  } catch (error) {
    loadFailure.value = error
  } finally {
    loading.value = false
  }
}

watch(open, (value) => {
  if (!value) return
  origin.value = 'all'
  chosen.value = undefined
  failure.value = undefined
  void load()
})
watch(origin, () => {
  if (open.value) {
    chosen.value = undefined
    void load()
  }
})

function names(keys: string[] | undefined, prefix: string): string {
  return (keys ?? []).map((key) => t(`${prefix}.${key}`)).join(', ')
}

function unavailableReason(skill: Skill): string | undefined {
  if (props.pinned.some((item) => item.skill === skill.id))
    return t('agentsPage.skills.alreadyPinned')
  if (skill.status !== 'unblocked') return t(`agentsPage.skills.status.${skill.status}`)
  return undefined
}

async function confirm(): Promise<void> {
  if (!chosenSkill.value) return
  saving.value = true
  failure.value = undefined
  try {
    await props.submit(chosenSkill.value)
    open.value = false
  } catch (error) {
    failure.value = error
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <!-- Look: canvas "Agentes e skills" (Agentes.dc.html) primitives; options use the canvas `.op`. -->
  <v-dialog
    v-model="open"
    max-width="720"
    scrollable
    :persistent="saving"
    aria-labelledby="skill-picker-title"
  >
    <v-card data-testid="skill-picker">
      <div class="dialog-head">
        <h2 id="skill-picker-title">
          {{ $t('agentsPage.skills.pickerTitle', { agent: props.agentName }) }}
        </h2>
      </div>
      <v-card-text class="dialog-body">
        <fieldset class="group">
          <legend class="lb">{{ $t('agentsPage.skills.originFilter') }}</legend>
          <div class="origin-row">
            <label
              v-for="choice in originChoices"
              :key="choice.value"
              :for="`${fieldId}-origin-${choice.value}`"
              class="radio"
            >
              <input
                :id="`${fieldId}-origin-${choice.value}`"
                v-model="origin"
                type="radio"
                :name="`${fieldId}-origin`"
                :value="choice.value"
              />
              {{ choice.label }}
            </label>
          </div>
        </fieldset>
        <LoadingState v-if="loading" type="list-item-three-line@3" />
        <ProblemAlert v-else-if="loadFailure" :error="loadFailure" retry @retry="load" />
        <p v-else-if="skills.length === 0" class="empty">
          {{ $t('agentsPage.skills.noneToPick') }}
        </p>
        <fieldset v-else class="group">
          <legend class="lb">{{ $t('agentsPage.skills.choose') }}</legend>
          <div class="options">
            <div
              v-for="skill in skills"
              :key="skill.id"
              class="op"
              :class="{
                on: chosen === skill.id,
                off: !canAssociate(skill, props.pinned),
              }"
              :data-testid="`skill-option-${skill.id}`"
            >
              <div class="op-text">
                <label :for="`${fieldId}-${skill.id}`" class="op-name">
                  <input
                    :id="`${fieldId}-${skill.id}`"
                    v-model="chosen"
                    type="radio"
                    :name="`${fieldId}-skill`"
                    :value="skill.id"
                    :disabled="!canAssociate(skill, props.pinned)"
                    :aria-describedby="`${fieldId}-${skill.id}-about`"
                  />
                  {{ skill.name }}
                </label>
                <div :id="`${fieldId}-${skill.id}-about`" class="op-about">
                  <!-- ER-22-6: the description is untrusted data, shown as escaped text. -->
                  <p>{{ skill.description }}</p>
                  <p class="t2">
                    {{ $t(`agentsPage.skills.origin.${skill.origin}`) }} ·
                    {{ $t('agentsPage.skills.versionNumber', { n: skill.current_version }) }} ·
                    {{
                      $t('agentsPage.skills.tokens', {
                        n: format.integer(skill.estimated_tokens ?? 0),
                      })
                    }}
                  </p>
                  <p v-if="skill.stages?.length" class="t2">
                    {{ $t('agentsPage.skills.stages', { stages: names(skill.stages, 'stages') }) }}
                  </p>
                  <p v-if="skill.agents?.length" class="t2">
                    {{ $t('agentsPage.skills.agents', { agents: names(skill.agents, 'agents') }) }}
                  </p>
                  <p v-if="unavailableReason(skill)" class="reason">
                    {{ unavailableReason(skill) }}
                  </p>
                </div>
              </div>
            </div>
          </div>
        </fieldset>
        <ProblemAlert v-if="failure" :error="failure" class="failure" />
      </v-card-text>
      <div class="dialog-actions">
        <button type="button" class="btn" :disabled="saving" @click="open = false">
          {{ $t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-p"
          :disabled="!chosenSkill || saving"
          :aria-busy="saving ? 'true' : undefined"
          @click="confirm"
        >
          {{ $t('agentsPage.skills.associate') }}
        </button>
      </div>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.dialog-head {
  padding: 22px 24px 8px;
}

.dialog-body.v-card-text {
  padding: 4px 24px 8px;
  font-size: 14px;
  line-height: 20px;
  letter-spacing: 0;
  color: #f1f3ff;
}

.group {
  border: 0;
  margin: 0 0 12px;
  padding: 0;
}

.group legend {
  padding: 0;
  margin-bottom: 6px;
}

.origin-row {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 20px;
}

.radio {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  min-height: 44px;
  cursor: pointer;
}

input[type='radio'] {
  width: 18px;
  height: 18px;
  margin: 1px 0 0;
  flex: none;
  accent-color: #7c3aed;
  cursor: pointer;
}

input[type='radio']:disabled {
  cursor: not-allowed;
}

.options {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.op {
  cursor: default;
}

/* A blocked skill: dashed outline and muted label, keeping every text at 4.5:1 (RNF-18). */
.op.off {
  border-style: dashed;
  color: #a9b0d6;
}

.op-text {
  flex: 1;
  min-width: 0;
}

.op-name {
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 44px;
  font-weight: 600;
  cursor: pointer;
}

.op.off .op-name {
  cursor: not-allowed;
}

.op-about {
  padding-left: 30px;
}

.op-about p {
  margin: 2px 0 0;
  font-size: 13px;
}

.op-about .reason {
  font-weight: 700;
  color: #fcd34d;
}

.empty {
  margin: 0;
}

.failure {
  margin-top: 8px;
}

.dialog-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 24px 22px;
}
</style>
