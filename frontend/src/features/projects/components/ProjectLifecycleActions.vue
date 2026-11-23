<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import type { AvailableAction, Command } from '@/shared/api/contract'
import { ApiError, hasProblemSlug } from '@/shared/api/problemDetails'
import { waitForCommand } from '@/shared/commands/waitForCommand'
import ConfirmActionDialog from '@/shared/ui/ConfirmActionDialog.vue'
import type { Project, Workflow } from '../api/projectsApi'
import { currentProjectApi } from '../stores/currentProjectStore'

/**
 * ESP-06 section 4: the project lifecycle (US-51 pause, resume and cancel; US-52 conclude). The
 * buttons are exactly the project's `available_actions` for this user (ER-06-6): the api leaves
 * them out for a member or approver (CA-6) and for a concluded or canceled project (CA-5). Each
 * action is a command followed until applied or refused (ER-06-7); cancel asks for the typed
 * project name (RN-1).
 */
const props = defineProps<{
  project: Project
  workflow?: Workflow
}>()
const emit = defineEmits<{ changed: [] }>()

type LifecycleAction = 'pause' | 'resume' | 'cancel' | 'record_publication'

const { t } = useI18n()
/** The last action opened; it stays while the dialog closes so the focus goes back (ESP-18 8). */
const dialog = ref<LifecycleAction>('pause')
const dialogOpen = ref(false)
const notice = ref<string>()
const typedName = ref('')

const offered = computed(() =>
  (['pause', 'resume', 'cancel', 'record_publication'] as const).filter((action) =>
    (props.project.available_actions ?? []).includes(action as AvailableAction),
  ),
)
const nameMatches = computed(() => typedName.value.trim() === props.project.name)
const releaseApproved = computed(
  () => props.workflow?.stages.find((stage) => stage.id === 'release')?.state === 'approved',
)
const activeWaves = computed(() => props.project.summary?.active_waves ?? [])

const LABELS: Record<LifecycleAction, string> = {
  pause: 'pause',
  resume: 'resume',
  cancel: 'cancel',
  record_publication: 'complete',
}

function openDialog(action: LifecycleAction): void {
  typedName.value = ''
  dialog.value = action
  dialogOpen.value = true
}

async function run(send: () => Promise<Command>, key: string): Promise<void> {
  const api = currentProjectApi()
  notice.value = t(`projects.lifecycle.${key}.pending`)
  try {
    const accepted = await send()
    const finished = await waitForCommand(accepted, (id) => api.getCommand(props.project.id, id))
    if (finished.state === 'refused') {
      throw new ApiError(finished.refusal?.status ?? 409, finished.refusal)
    }
    notice.value = t(`projects.lifecycle.${key}.applied`)
  } catch (error) {
    notice.value = undefined
    // ER-06-6: a refusal shows the reason and the real state.
    emit('changed')
    throw error
  }
  emit('changed')
}

function submit(action: LifecycleAction): (idempotencyKey: string) => Promise<void> {
  const api = currentProjectApi()
  const id = props.project.id
  return (key) => {
    if (action === 'pause') return run(() => api.pauseProject(id, key), 'pause')
    if (action === 'resume') return run(() => api.resumeProject(id, key), 'resume')
    if (action === 'cancel')
      return run(() => api.cancelProject(id, typedName.value.trim(), key), 'cancel')
    return run(() => api.completeProject(id, key), 'complete')
  }
}

/**
 * US-52 CA-2, CA-3: why the conclusion was refused. The api says it in `reason`
 * (`release_not_approved`, `active_wave` with `waves`); without it the page reads the workflow.
 */
function completionReason(failure: unknown): string | undefined {
  if (!hasProblemSlug(failure, 'state-conflict')) return undefined
  const problem = failure.problem
  const waves = Array.isArray(problem?.waves)
    ? problem.waves.filter((wave): wave is string => typeof wave === 'string')
    : activeWaves.value.map((wave) => wave.wave)
  const reason =
    problem?.reason ??
    (!releaseApproved.value ? 'release_not_approved' : waves.length ? 'active_wave' : undefined)
  if (reason === 'release_not_approved') return t('projects.lifecycle.complete.releaseNotApproved')
  if (reason === 'active_wave')
    return t('projects.lifecycle.complete.activeWaves', { waves: waves.join(', ') })
  return undefined
}

const dialogTexts = computed(() => {
  const action = dialog.value
  const key = LABELS[action]
  const name = props.project.name
  const lines: Record<LifecycleAction, string[]> = {
    pause: [t('projects.lifecycle.pause.effect'), t('projects.lifecycle.pause.keeps')],
    resume: [
      t('projects.lifecycle.resume.effect'),
      ...(props.project.pause_reason === 'budget' ? [t('projects.lifecycle.resume.budget')] : []),
    ],
    cancel: [
      t('projects.lifecycle.cancel.final'),
      t('projects.lifecycle.cancel.effect'),
      t('projects.lifecycle.cancel.keeps'),
    ],
    record_publication: [
      t('projects.lifecycle.complete.effect'),
      t('projects.lifecycle.complete.conditions'),
    ],
  }
  return {
    title: t(`projects.lifecycle.${key}.title`, { name }),
    lines: lines[action],
    confirm: t(`projects.lifecycle.${key}.confirm`),
    color: action === 'cancel' ? 'error' : 'primary',
  }
})
</script>

<template>
  <div v-if="offered.length" class="d-flex flex-wrap ga-2 mb-3" data-testid="project-actions">
    <v-btn
      v-for="action in offered"
      :key="action"
      :color="action === 'cancel' ? 'error' : 'primary'"
      :variant="action === 'resume' || action === 'record_publication' ? 'flat' : 'outlined'"
      @click="openDialog(action)"
    >
      {{ $t(`projects.lifecycle.${LABELS[action]}.action`) }}
    </v-btn>
  </div>
  <div v-if="notice" role="status" class="text-body-2 mb-2" data-testid="project-action-notice">
    {{ notice }}
  </div>
  <ConfirmActionDialog
    v-model="dialogOpen"
    :title="dialogTexts.title"
    :lines="dialogTexts.lines"
    :keep-label="$t('projects.lifecycle.keep')"
    :confirm-label="dialogTexts.confirm"
    :confirm-color="dialogTexts.color"
    :confirm-disabled="dialog === 'cancel' && !nameMatches"
    :test-id="`project-${LABELS[dialog]}-dialog`"
    :submit="submit(dialog)"
  >
    <template #default="{ failure }">
      <v-text-field
        v-if="dialog === 'cancel'"
        v-model="typedName"
        :label="$t('projects.lifecycle.cancel.typeName', { name: project.name })"
        autocomplete="off"
        class="mt-2"
      />
      <p v-if="dialog === 'resume' && project.pause_reason === 'budget'" class="mb-2">
        <router-link
          :to="{ name: ROUTE_NAMES.projectConfiguration, params: { projectId: project.id } }"
          @click="dialogOpen = false"
        >
          {{ $t('projects.lifecycle.resume.raiseLimit') }}
        </router-link>
      </p>
      <p
        v-if="dialog === 'record_publication' && completionReason(failure)"
        class="mt-2 font-weight-medium"
        data-testid="completion-reason"
      >
        {{ completionReason(failure) }}
      </p>
    </template>
  </ConfirmActionDialog>
</template>
