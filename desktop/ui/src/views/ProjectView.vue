<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { bridge, toUIError } from '@/bridge'
import ErrorMessage from '@/components/ErrorMessage.vue'
import OpenWebButton from '@/components/OpenWebButton.vue'
import { useLabel } from '@/i18n'
import { navigate, openWeb } from '@/store'
import type { Project, StageSummary, UIError, WaveSummary, Workflow } from '@/types'

// ESP-28 Projeto: stages with state and the waves of the project (read-only).
const props = defineProps<{ projectId: string }>()
const { t } = useI18n()
const label = useLabel()
const project = ref<Project | null>(null)
const stages = ref<StageSummary[]>([])
const waves = ref<WaveSummary[]>([])
const error = ref<UIError | null>(null)

onMounted(async () => {
  try {
    const view = await bridge().call<{
      project: Project
      workflow: Workflow
      waves: { items: WaveSummary[] }
    }>('Project', props.projectId)
    project.value = view.project
    stages.value = view.workflow.stages
    waves.value = view.waves.items
  } catch (e) {
    error.value = toUIError(e)
  }
})

function stageName(stage: StageSummary): string {
  return stage.short_name ?? label(`stages.${stage.id}`, stage.name)
}

function stageState(stage: StageSummary): string {
  return stage.state ? label(`states.stage.${stage.state}`, stage.state) : t('project.notStarted')
}

function openGate(stage: StageSummary): void {
  if (stage.gate && stage.current_round)
    navigate({
      name: 'gate',
      projectId: props.projectId,
      gateId: stage.gate,
      round: stage.current_round,
    })
}

function waveLine(wave: WaveSummary): string {
  return t('project.waveLine', {
    id: wave.id,
    agent: label(`agents.${wave.agent}`, wave.agent),
    state: label(`states.wave.${wave.state}`, wave.state),
  })
}
</script>

<template>
  <section aria-labelledby="view-title">
    <button type="button" class="link-button" @click="navigate({ name: 'dashboard' })">
      {{ t('common.back') }}
    </button>
    <h1 id="view-title" tabindex="-1">{{ project?.name ?? t('common.loading') }}</h1>
    <ErrorMessage :error="error" />
    <template v-if="project">
      <p>
        {{
          t('common.state', {
            state: label(`states.project.${project.state}`, project.state),
          })
        }}
      </p>
      <OpenWebButton
        :item="project.name"
        :open="() => openWeb({ kind: 'project', project_id: projectId })"
      />

      <h2>{{ t('project.stages') }}</h2>
      <ul class="rows">
        <li v-for="stage in stages" :key="stage.id">
          <span>{{
            t('project.stageLine', { stage: stageName(stage), state: stageState(stage) })
          }}</span>
          <span v-if="stage.gate && stage.current_round">
            {{
              t('project.gateLine', {
                gate: stage.gate,
                round: stage.current_round,
                state: stage.gate_state
                  ? label(`states.gate.${stage.gate_state}`, stage.gate_state)
                  : '',
              })
            }}
            <button type="button" class="link-button" @click="openGate(stage)">
              {{ t('project.viewGate', { stage: stageName(stage) }) }}
            </button>
          </span>
        </li>
      </ul>

      <h2>{{ t('project.waves') }}</h2>
      <p v-if="waves.length === 0">{{ t('project.noWaves') }}</p>
      <ul v-else class="rows">
        <li v-for="wave in waves" :key="wave.id">
          <span>{{ waveLine(wave) }}</span>
          <button
            type="button"
            class="link-button"
            @click="navigate({ name: 'wave', projectId, waveId: wave.id })"
          >
            {{ t('project.openWave', { id: wave.id }) }}
          </button>
        </li>
      </ul>
    </template>
  </section>
</template>
