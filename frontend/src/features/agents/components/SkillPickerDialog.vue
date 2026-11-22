<script setup lang="ts">
import { computed, ref, watch } from 'vue'
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
  <v-dialog
    v-model="open"
    max-width="720"
    scrollable
    :persistent="saving"
    aria-labelledby="skill-picker-title"
  >
    <v-card data-testid="skill-picker">
      <v-card-title>
        <h2 id="skill-picker-title" class="text-h6">
          {{ $t('agentsPage.skills.pickerTitle', { agent: props.agentName }) }}
        </h2>
      </v-card-title>
      <v-card-text>
        <v-radio-group v-model="origin" inline :label="$t('agentsPage.skills.originFilter')">
          <v-radio value="all" :label="$t('agentsPage.skills.originAll')" />
          <v-radio value="native" :label="$t('agentsPage.skills.origin.native')" />
          <v-radio value="organization" :label="$t('agentsPage.skills.origin.organization')" />
        </v-radio-group>
        <LoadingState v-if="loading" type="list-item-three-line@3" />
        <ProblemAlert v-else-if="loadFailure" :error="loadFailure" retry @retry="load" />
        <p v-else-if="skills.length === 0" class="text-body-2">
          {{ $t('agentsPage.skills.noneToPick') }}
        </p>
        <v-radio-group v-else v-model="chosen" :label="$t('agentsPage.skills.choose')">
          <div
            v-for="skill in skills"
            :key="skill.id"
            class="skill-option"
            :data-testid="`skill-option-${skill.id}`"
          >
            <v-radio
              :value="skill.id"
              :label="skill.name"
              :disabled="!canAssociate(skill, props.pinned)"
            />
            <div class="ml-10 text-body-2">
              <!-- ER-22-6: the description is untrusted data, shown as escaped text. -->
              <p>{{ skill.description }}</p>
              <p class="text-medium-emphasis">
                {{ $t(`agentsPage.skills.origin.${skill.origin}`) }} ·
                {{ $t('agentsPage.skills.versionNumber', { n: skill.current_version }) }} ·
                {{
                  $t('agentsPage.skills.tokens', {
                    n: format.integer(skill.estimated_tokens ?? 0),
                  })
                }}
              </p>
              <p v-if="skill.stages?.length" class="text-medium-emphasis">
                {{ $t('agentsPage.skills.stages', { stages: names(skill.stages, 'stages') }) }}
              </p>
              <p v-if="skill.agents?.length" class="text-medium-emphasis">
                {{ $t('agentsPage.skills.agents', { agents: names(skill.agents, 'agents') }) }}
              </p>
              <p v-if="unavailableReason(skill)" class="font-weight-medium">
                {{ unavailableReason(skill) }}
              </p>
            </div>
          </div>
        </v-radio-group>
        <ProblemAlert v-if="failure" :error="failure" class="mt-2" />
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" :disabled="saving" @click="open = false">
          {{ $t('common.cancel') }}
        </v-btn>
        <v-btn
          color="primary"
          variant="flat"
          :disabled="!chosenSkill"
          :loading="saving"
          @click="confirm"
        >
          {{ $t('agentsPage.skills.associate') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.skill-option {
  border-bottom: 1px solid rgb(var(--v-theme-on-surface), 0.12);
  padding: 0.5rem 0;
}
</style>
