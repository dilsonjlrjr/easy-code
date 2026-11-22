<script setup lang="ts">
import { ref, watch } from 'vue'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import type { Skill } from '../api/agentsApi'

/**
 * ESP-22 "Ver detalhe da Skill": metadata and description from getSkill. The text is untrusted
 * data (ER-22-6, CA-22-11): it is shown as escaped text and never widens the agent tools.
 */
const props = defineProps<{
  skillId?: string
  read: (skillId: string) => Promise<Skill>
}>()

const open = defineModel<boolean>({ required: true })
const format = useFormat()
const skill = ref<Skill>()
const loading = ref(false)
const failure = ref<unknown>()

async function load(): Promise<void> {
  if (!props.skillId) return
  loading.value = true
  failure.value = undefined
  skill.value = undefined
  try {
    skill.value = await props.read(props.skillId)
  } catch (error) {
    failure.value = error
  } finally {
    loading.value = false
  }
}

watch(open, (value) => {
  if (value) void load()
})
</script>

<template>
  <v-dialog v-model="open" max-width="640" scrollable aria-labelledby="skill-detail-title">
    <v-card data-testid="skill-detail">
      <v-card-title>
        <h2 id="skill-detail-title" class="text-h6">
          {{ skill?.name ?? props.skillId }}
        </h2>
      </v-card-title>
      <v-card-text>
        <LoadingState v-if="loading" type="paragraph" />
        <ProblemAlert v-else-if="failure" :error="failure" retry @retry="load" />
        <template v-else-if="skill">
          <p class="text-body-2 text-medium-emphasis mb-2">
            <span class="monospace">{{ skill.id }}</span> ·
            {{ $t(`agentsPage.skills.origin.${skill.origin}`) }} ·
            {{ $t('agentsPage.skills.versionNumber', { n: skill.current_version }) }} ·
            {{ $t(`agentsPage.skills.status.${skill.status}`) }}
          </p>
          <p class="skill-description" data-testid="skill-description">{{ skill.description }}</p>
          <h3 class="text-subtitle-1 font-weight-medium mt-4 mb-1">
            {{ $t('agentsPage.skills.versions') }}
          </h3>
          <ul class="plain-list">
            <li v-for="item in skill.versions ?? []" :key="item.version" class="text-body-2">
              {{ $t('agentsPage.skills.versionNumber', { n: item.version }) }} ·
              {{ format.dateTime(item.published_at) }} ·
              {{ $t(`agentsPage.skills.status.${item.status}`) }}
            </li>
          </ul>
        </template>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="open = false">{{ $t('common.close') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.skill-description {
  white-space: pre-wrap;
}
</style>
