<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { bridge, toUIError } from '@/bridge'
import ErrorMessage from '@/components/ErrorMessage.vue'
import OpenWebButton from '@/components/OpenWebButton.vue'
import { useLabel } from '@/i18n'
import { navigate, openWeb } from '@/store'
import { chip, segment } from '@/tone'
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

const approved = computed(() => stages.value.filter((s) => s.state === 'approved').length)

function waveLine(wave: WaveSummary): string {
  return t('project.waveLine', {
    id: wave.id,
    agent: label(`agents.${wave.agent}`, wave.agent),
    state: label(`states.wave.${wave.state}`, wave.state),
  })
}
</script>

<template>
  <section class="view" aria-labelledby="view-title">
    <nav class="bc" :aria-label="t('common.breadcrumb')">
      <ol>
        <li>
          <button type="button" @click="navigate({ name: 'dashboard' })">
            {{ t('nav.dashboard') }}
          </button>
        </li>
        <li>
          <span aria-current="page">{{ project?.name ?? t('common.loading') }}</span>
        </li>
      </ol>
    </nav>
    <div class="head">
      <div>
        <div class="title-line">
          <h1 id="view-title" tabindex="-1">{{ project?.name ?? t('common.loading') }}</h1>
          <span v-if="project" :class="chip('project', project.state)">{{
            t('common.state', { state: label(`states.project.${project.state}`, project.state) })
          }}</span>
        </div>
        <p v-if="stages.length" class="t2">
          {{ t('project.progress', { done: approved, total: stages.length }) }}
        </p>
      </div>
      <OpenWebButton
        v-if="project"
        :item="project.name"
        :open="() => openWeb({ kind: 'project', project_id: projectId })"
      />
    </div>
    <ErrorMessage :error="error" />
    <span v-if="stages.length" class="bar" aria-hidden="true">
      <span v-for="stage in stages" :key="stage.id" :class="segment(stage.state)"></span>
    </span>

    <div v-if="project" class="cols">
      <section class="card col-main" aria-labelledby="stages-title">
        <h2 id="stages-title">{{ t('project.stages') }}</h2>
        <ol class="rows">
          <li v-for="(stage, index) in stages" :key="stage.id">
            <span class="stn" aria-hidden="true">{{ index + 1 }}</span>
            <span class="grow">
              <span style="font-weight: 600">{{
                t('project.stageLine', { stage: stageName(stage), state: stageState(stage) })
              }}</span>
              <span v-if="stage.gate && stage.current_round" class="t2 small">
                {{
                  t('project.gateLine', {
                    gate: stage.gate,
                    round: stage.current_round,
                    state: stage.gate_state
                      ? label(`states.gate.${stage.gate_state}`, stage.gate_state)
                      : '',
                  })
                }}
              </span>
            </span>
            <span :class="chip('stage', stage.state)" aria-hidden="true">{{
              stageState(stage)
            }}</span>
            <button
              v-if="stage.gate && stage.current_round"
              type="button"
              class="btn small"
              :aria-label="t('project.viewGate', { stage: stageName(stage) })"
              @click="openGate(stage)"
            >
              {{ t('project.viewGateShort') }}
            </button>
          </li>
        </ol>
      </section>

      <section class="card col-side" style="display: block" aria-labelledby="waves-title">
        <h2 id="waves-title">{{ t('project.waves') }}</h2>
        <p v-if="waves.length === 0" class="empty">{{ t('project.noWaves') }}</p>
        <ul v-else class="rows">
          <li v-for="wave in waves" :key="wave.id">
            <span class="chip mute mono" aria-hidden="true">{{ wave.id }}</span>
            <span class="grow">
              <span class="visually-hidden">{{ waveLine(wave) }}</span>
              <span aria-hidden="true" style="font-weight: 600">{{
                label(`agents.${wave.agent}`, wave.agent)
              }}</span>
              <span :class="chip('wave', wave.state)" aria-hidden="true">{{
                label(`states.wave.${wave.state}`, wave.state)
              }}</span>
            </span>
            <button
              type="button"
              class="btn small"
              :aria-label="t('project.openWave', { id: wave.id })"
              @click="navigate({ name: 'wave', projectId, waveId: wave.id })"
            >
              {{ t('project.followShort') }}
            </button>
          </li>
        </ul>
      </section>
    </div>
  </section>
</template>
