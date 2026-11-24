<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmActionDialog from '@/shared/ui/ConfirmActionDialog.vue'
import type { GateApprovalModeValue, ProjectConfiguration, StageSummary } from '../api/projectsApi'
import { gateModeRows, modeChangeTakesEffect, type GateModeRow } from '../composables/gateApproval'

/**
 * ESP-29 3.2 / US-07: the approval mode of each gate. Only the lead changes it, and only for the gates
 * that allow both modes (gate.validation, gate.implementation); the others show the mode in text,
 * the Release "always human" (CA-3, CA-8). Each change is confirmed with its effect (RN-3).
 */
const props = defineProps<{
  configuration: ProjectConfiguration
  stages: StageSummary[]
  canEdit: boolean
  /** Saves the new mode; throws the refusal, which the dialog shows. */
  change: (gate: string, mode: GateApprovalModeValue) => Promise<void>
}>()

const i18n = useI18n()
const { t } = i18n
const rows = computed(() => gateModeRows(props.configuration.gate_approval, props.stages))
const pending = ref<{ row: GateModeRow; mode: GateApprovalModeValue }>()
const dialogOpen = ref(false)
/**
 * The radio shows the saved mode only: a choice kept back in the dialog redraws the group, since
 * Vuetify keeps the clicked radio checked otherwise.
 */
const redraw = ref(0)
watch(dialogOpen, (open) => {
  if (!open) redraw.value += 1
})

function gateName(row: GateModeRow): string {
  if (!row.stage) return row.gate
  const key = `stages.${row.stage}`
  const stage = i18n.te(key)
    ? t(key)
    : (props.stages.find((item) => item.id === row.stage)?.name ?? row.stage)
  return t('projects.gateModes.gateOf', { stage })
}

function modeText(row: GateModeRow): string {
  if (row.alwaysHuman) return t('projects.gateModes.alwaysHuman')
  return t(`projects.gateModes.modes.${row.mode}`)
}

function ask(row: GateModeRow, mode: unknown): void {
  if ((mode !== 'human' && mode !== 'automatic') || mode === row.mode) return
  pending.value = { row, mode }
  dialogOpen.value = true
}

const dialogLines = computed(() => {
  if (!pending.value) return []
  const effect = modeChangeTakesEffect(pending.value.mode)
  return [
    t(`projects.gateModes.effect.${pending.value.mode}`),
    t(effect === 'now' ? 'projects.gateModes.appliesNow' : 'projects.gateModes.appliesNextRound'),
  ]
})

async function submit(): Promise<void> {
  if (!pending.value) return
  await props.change(pending.value.row.gate, pending.value.mode)
}
</script>

<template>
  <!-- Layout: canvas primitives of Projeto.dc.html (no own artboard): .card, h2, .kv rows. -->
  <section aria-labelledby="gates-heading" class="card gates" data-testid="gate-modes">
    <h2 id="gates-heading">{{ $t('projects.gateModes.title') }}</h2>
    <p class="t2 help">{{ $t('projects.gateModes.help') }}</p>
    <p v-if="rows.length === 0" class="t2 help">{{ $t('projects.gateModes.none') }}</p>
    <ul v-else class="gate-modes">
      <li
        v-for="row in rows"
        :key="row.gate"
        class="kv gate-row"
        :data-testid="`gate-mode-${row.gate}`"
      >
        <v-radio-group
          v-if="canEdit && row.configurable"
          :key="`${row.gate}-${row.mode}-${redraw}`"
          :model-value="row.mode"
          :label="gateName(row)"
          density="compact"
          inline
          hide-details
          @update:model-value="ask(row, $event)"
        >
          <v-radio value="human" :label="$t('projects.gateModes.modes.human')" />
          <v-radio value="automatic" :label="$t('projects.gateModes.modes.automatic')" />
        </v-radio-group>
        <template v-else>
          <!-- The space after the colon is in the text, so it reads "Gate …: Human". -->
          <span class="t2">{{ `${gateName(row)}: ` }}</span>
          <span class="value">{{ modeText(row) }}</span>
        </template>
      </li>
    </ul>
    <p class="t2 help foot">{{ $t('projects.gateModes.automaticExplanation') }}</p>
    <ConfirmActionDialog
      v-model="dialogOpen"
      :title="
        pending
          ? $t('projects.gateModes.dialogTitle', {
              gate: gateName(pending.row),
              mode: $t(`projects.gateModes.modes.${pending.mode}`),
            })
          : ''
      "
      :lines="dialogLines"
      :keep-label="$t('projects.gateModes.keep')"
      :confirm-label="$t('projects.gateModes.confirm')"
      confirm-color="primary"
      test-id="gate-mode-dialog"
      :submit="submit"
    />
  </section>
</template>

<style scoped>
.gates {
  padding: 18px 22px;
}

.help {
  margin: 4px 0 10px;
  font-size: 13px;
}

.foot {
  margin: 10px 0 0;
}

.gate-modes {
  list-style: none;
  margin: 0;
  padding: 0;
}

.gate-row {
  align-items: center;
  flex-wrap: wrap;
}

.value {
  font-weight: 600;
  text-align: right;
}
</style>
