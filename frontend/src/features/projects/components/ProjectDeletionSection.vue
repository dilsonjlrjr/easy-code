<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import ConfirmActionDialog from '@/shared/ui/ConfirmActionDialog.vue'
import type { Project } from '../api/projectsApi'
import { currentProjectApi } from '../stores/currentProjectStore'

/**
 * ESP-29 3.6 / US-04: deleting an ended project. Only the lead, only a completed or canceled project
 * (CA-2: the others are told to cancel first); the typed project name confirms (CA-1) and the
 * dialog says what happens to the data before anything is sent (RN-1, RNF-20).
 */
const props = defineProps<{ project: Project }>()

const { t } = useI18n()
const router = useRouter()
const open = ref(false)
const typedName = ref('')

const isLead = computed(() => props.project.my_roles.includes('lead'))
const ended = computed(() => ['completed', 'canceled'].includes(props.project.state))
const nameMatches = computed(() => typedName.value.trim() === props.project.name)
const lines = computed(() => [
  t('projects.deletion.final'),
  t('projects.deletion.effect'),
  t('projects.deletion.retention'),
])

function openDialog(): void {
  typedName.value = ''
  open.value = true
}

async function submit(idempotencyKey: string): Promise<void> {
  await currentProjectApi().deleteProject(props.project.id, typedName.value.trim(), idempotencyKey)
  // ESP-29 6: back to the project list with "project deleted".
  await router.push({ name: ROUTE_NAMES.projects, query: { notice: 'deleted' } })
}
</script>

<template>
  <section
    v-if="isLead"
    aria-labelledby="deletion-heading"
    class="mt-8"
    data-testid="project-deletion"
  >
    <h3 id="deletion-heading" class="text-subtitle-1 font-weight-medium mb-1">
      {{ $t('projects.deletion.title') }}
    </h3>
    <template v-if="ended">
      <p class="text-body-2 mb-2">{{ $t('projects.deletion.help') }}</p>
      <v-btn color="error" variant="outlined" data-testid="delete-project" @click="openDialog">
        {{ $t('projects.deletion.action') }}
      </v-btn>
    </template>
    <!-- CA-2 / CA-29-15: the action does not show; the screen says to cancel first. -->
    <p v-else class="text-body-2" data-testid="deletion-cancel-first">
      {{ $t('projects.deletion.cancelFirst') }}
    </p>
    <ConfirmActionDialog
      v-model="open"
      :title="$t('projects.deletion.dialogTitle', { name: project.name })"
      :lines="lines"
      :keep-label="$t('projects.deletion.keep')"
      :confirm-label="$t('projects.deletion.confirm')"
      :confirm-disabled="!nameMatches"
      test-id="project-delete-dialog"
      :submit="submit"
    >
      <v-text-field
        v-model="typedName"
        :label="$t('projects.deletion.typeName', { name: project.name })"
        autocomplete="off"
        class="mt-2"
      />
    </ConfirmActionDialog>
  </section>
</template>
