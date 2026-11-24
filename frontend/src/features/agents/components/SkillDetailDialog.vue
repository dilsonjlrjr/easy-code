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

/** Canvas chip tone for a skill version state; the state is always written as text too. */
function statusTone(status: Skill['status']): string {
  if (status === 'unblocked') return 'ok'
  if (status === 'blocked') return 'warn'
  if (status === 'deactivated') return 'mute'
  return 'info'
}
</script>

<template>
  <!-- Look: canvas "Agentes e skills" (Agentes.dc.html) primitives inside the dialog. -->
  <v-dialog v-model="open" max-width="640" scrollable aria-labelledby="skill-detail-title">
    <v-card data-testid="skill-detail">
      <div class="dialog-head">
        <h2 id="skill-detail-title">{{ skill?.name ?? props.skillId }}</h2>
      </div>
      <v-card-text class="dialog-body">
        <LoadingState v-if="loading" type="paragraph" />
        <ProblemAlert v-else-if="failure" :error="failure" retry @retry="load" />
        <template v-else-if="skill">
          <p class="meta">
            <span class="mono">{{ skill.id }}</span>
            <span class="chip" :class="skill.origin === 'organization' ? 'vio' : 'mute'">
              {{ $t(`agentsPage.skills.origin.${skill.origin}`) }}
            </span>
            <span class="chip mute">
              {{ $t('agentsPage.skills.versionNumber', { n: skill.current_version }) }}
            </span>
            <span class="chip" :class="statusTone(skill.status)">
              {{ $t(`agentsPage.skills.status.${skill.status}`) }}
            </span>
          </p>
          <p class="skill-description" data-testid="skill-description">{{ skill.description }}</p>
          <h3 class="lb versions-title">{{ $t('agentsPage.skills.versions') }}</h3>
          <ul class="version-list">
            <li v-for="item in skill.versions ?? []" :key="item.version" class="kv">
              <span>
                {{ $t('agentsPage.skills.versionNumber', { n: item.version }) }} ·
                <span class="t2">{{ format.dateTime(item.published_at) }}</span>
              </span>
              <span class="chip" :class="statusTone(item.status)">
                {{ $t(`agentsPage.skills.status.${item.status}`) }}
              </span>
            </li>
          </ul>
        </template>
      </v-card-text>
      <div class="dialog-actions">
        <button type="button" class="btn" @click="open = false">{{ $t('common.close') }}</button>
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

.meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin: 0 0 12px;
}

.skill-description {
  margin: 0;
  white-space: pre-wrap;
}

.versions-title {
  display: block;
  margin: 16px 0 6px;
}

.version-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.dialog-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 24px 22px;
}
</style>
