<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import { ApiError } from '@/shared/api/problemDetails'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import ConfirmActionDialog from '@/shared/ui/ConfirmActionDialog.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { agentsApi, type Skill, type SkillVersion } from '@/features/agents/api/agentsApi'
import SkillDetailDialog from '@/features/agents/components/SkillDetailDialog.vue'
import {
  canUnblock,
  followImport,
  isHttpsUrl,
  skillLibraryApi,
  type SkillImport,
} from '../api/skillLibraryApi'

/**
 * ESP-23 3.3 "Biblioteca de skills" (US-44): the organization skills with their state in text, the
 * native catalog, and the administrator's imports by zip or git address, followed until the version
 * settles (CA-1). A refusal says why or shows the limit (CA-2, CA-3); a version blocked for suspicious
 * content is released only by an administrator with a justification (CA-4). Skill text is data: it
 * is shown escaped, never rendered (RN-1).
 */
const i18n = useI18n()
const { t } = i18n
const format = useFormat()

const organization = ref<Skill[]>([])
const native = ref<Skill[]>([])
const source = ref<'upload' | 'git_repository'>('upload')
const packageFile = ref<File | File[] | null>(null)
const git = ref({ url: '', reference: '', subfolder: '' })
const sending = ref(false)
const sendFailure = ref<unknown>()
const imports = ref<SkillImport[]>([])
const detailOpen = ref(false)
const detailSkill = ref<string>()
const action = ref<{ kind: 'unblock' | 'deactivate'; skill: Skill; version: number }>()
const actionOpen = ref(false)
const justification = ref('')

const loader = useLoader(async () => {
  const [ownSkills, catalog] = await Promise.all([
    agentsApi.listSkills({ origin: 'organization' }),
    agentsApi.listSkills({ origin: 'native' }),
  ])
  organization.value = ownSkills
  native.value = catalog
})
void loader.run()

const chosenFile = computed(() =>
  Array.isArray(packageFile.value) ? packageFile.value[0] : (packageFile.value ?? undefined),
)
const gitValid = computed(() => isHttpsUrl(git.value.url))
const canSend = computed(() =>
  source.value === 'upload' ? chosenFile.value !== undefined : gitValid.value,
)
const justificationValid = computed(() => {
  const length = justification.value.trim().length
  return length >= 10 && length <= 2000
})

function currentVersion(skill: Skill): SkillVersion | undefined {
  return skill.versions?.find((item) => item.version === skill.current_version)
}

function reasonsText(reasons: string[] | undefined): string {
  return (reasons ?? []).map((reason) => t(`skillLibrary.reasons.${reason}`)).join(', ')
}

function stageNames(skill: Skill): string {
  return (skill.stages ?? [])
    .map((id) => (i18n.te(`stages.${id}`) ? t(`stages.${id}`) : id))
    .join(', ')
}

function agentNames(skill: Skill): string {
  return (skill.agents ?? [])
    .map((id) => (i18n.te(`agents.${id}`) ? t(`agents.${id}`) : id))
    .join(', ')
}

function importText(item: SkillImport): string {
  if (item.state === 'received') return t('skillLibrary.imports.received')
  if (item.state === 'refused') return t('skillLibrary.imports.refused')
  const status = item.version_status ?? 'imported'
  return t('skillLibrary.imports.versionCreated', {
    skill: item.skill ?? '',
    version: item.version ?? '',
    status: t(`agentsPage.skills.status.${status}`),
  })
}

/** CA-2: a refusal after the upload (git download, package checks) carries its Problem Details. */
function refusalError(item: SkillImport): ApiError {
  return new ApiError(item.refusal?.status ?? 400, item.refusal)
}

async function send(): Promise<void> {
  if (!canSend.value) return
  sending.value = true
  sendFailure.value = undefined
  const key = newIdempotencyKey()
  try {
    const accepted =
      source.value === 'upload' && chosenFile.value
        ? await skillLibraryApi.importPackage(chosenFile.value, key)
        : await skillLibraryApi.importGit(
            {
              git_url: git.value.url.trim(),
              reference: git.value.reference.trim(),
              subfolder: git.value.subfolder.trim(),
            },
            key,
          )
    packageFile.value = null
    git.value = { url: '', reference: '', subfolder: '' }
    imports.value = [accepted, ...imports.value]
    const settled = await followImport(accepted, skillLibraryApi.getImport)
    imports.value = imports.value.map((item) => (item.id === settled.id ? settled : item))
    await loader.run()
  } catch (error) {
    sendFailure.value = error
  } finally {
    sending.value = false
  }
}

function openDetail(skill: Skill): void {
  detailSkill.value = skill.id
  detailOpen.value = true
}

function ask(kind: 'unblock' | 'deactivate', skill: Skill): void {
  justification.value = ''
  action.value = { kind, skill, version: skill.current_version }
  actionOpen.value = true
}

async function submitAction(key: string): Promise<void> {
  const chosen = action.value
  if (!chosen || !justificationValid.value) return
  const text = justification.value.trim()
  if (chosen.kind === 'unblock') {
    await skillLibraryApi.unblock(chosen.skill.id, chosen.version, text, key)
  } else {
    await skillLibraryApi.deactivate(chosen.skill.id, chosen.version, text, key)
  }
  await loader.run()
}
</script>

<template>
  <section aria-labelledby="skill-library-heading">
    <h2 id="skill-library-heading" class="text-h6 mb-1">{{ $t('skillLibrary.title') }}</h2>
    <p class="text-body-2 text-medium-emphasis mb-4">{{ $t('skillLibrary.help') }}</p>

    <section aria-labelledby="skill-import-heading" class="mb-8" data-testid="skill-import">
      <h3 id="skill-import-heading" class="text-subtitle-1 font-weight-medium mb-2">
        {{ $t('skillLibrary.import.title') }}
      </h3>
      <v-radio-group
        v-model="source"
        :label="$t('skillLibrary.import.source')"
        inline
        density="compact"
        hide-details
        class="mb-2"
      >
        <v-radio value="upload" :label="$t('skillLibrary.import.upload')" />
        <v-radio value="git_repository" :label="$t('skillLibrary.import.git')" />
      </v-radio-group>
      <v-form @submit.prevent="send">
        <v-file-input
          v-if="source === 'upload'"
          v-model="packageFile"
          accept=".zip,application/zip"
          :label="$t('skillLibrary.import.package')"
          :hint="$t('skillLibrary.import.packageHint')"
          persistent-hint
          prepend-icon=""
          data-testid="skill-package"
        />
        <template v-else>
          <v-text-field
            v-model="git.url"
            :label="$t('skillLibrary.import.gitUrl')"
            :hint="$t('skillLibrary.import.gitHint')"
            persistent-hint
            :error-messages="git.url && !gitValid ? [$t('skillLibrary.import.httpsOnly')] : []"
            autocomplete="off"
            class="mb-2"
          />
          <v-text-field
            v-model="git.reference"
            :label="$t('skillLibrary.import.reference')"
            autocomplete="off"
            class="mb-2"
          />
          <v-text-field
            v-model="git.subfolder"
            :label="$t('skillLibrary.import.subfolder')"
            autocomplete="off"
          />
        </template>
        <ProblemAlert v-if="sendFailure" :error="sendFailure" class="my-2" />
        <v-btn
          type="submit"
          color="primary"
          variant="flat"
          class="mt-3"
          :loading="sending"
          :disabled="!canSend"
        >
          {{ $t('skillLibrary.import.send') }}
        </v-btn>
      </v-form>
      <div aria-live="polite" class="mt-3" data-testid="skill-imports">
        <div v-for="item in imports" :key="item.id" class="mb-2">
          <p class="text-body-2">
            {{ importText(item) }}
            <template v-if="item.block_reasons?.length">
              · {{ $t('skillLibrary.blockReasons', { reasons: reasonsText(item.block_reasons) }) }}
            </template>
          </p>
          <ProblemAlert
            v-if="item.state === 'refused' && item.refusal"
            :error="refusalError(item)"
          />
        </div>
      </div>
    </section>

    <LoadingState v-if="loader.loading.value && !loader.loaded.value" type="list-item@3" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else>
      <section aria-labelledby="organization-skills-heading" class="mb-8">
        <h3 id="organization-skills-heading" class="text-subtitle-1 font-weight-medium mb-2">
          {{ $t('skillLibrary.organization', { n: organization.length }) }}
        </h3>
        <p v-if="organization.length === 0" class="text-body-2">
          {{ $t('skillLibrary.noOrganization') }}
        </p>
        <div v-else class="table-scroll">
          <v-table density="comfortable" data-testid="organization-skills">
            <thead>
              <tr>
                <th scope="col">{{ $t('agentsPage.skills.name') }}</th>
                <th scope="col">{{ $t('skillLibrary.columns.usedBy') }}</th>
                <th scope="col">{{ $t('skillLibrary.columns.version') }}</th>
                <th scope="col">{{ $t('agentsPage.skills.estimatedTokens') }}</th>
                <th scope="col">{{ $t('skillLibrary.columns.status') }}</th>
                <th scope="col">
                  <span class="sr-only">{{ $t('common.actions') }}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="skill in organization" :key="skill.id" :data-testid="`skill-${skill.id}`">
                <td>
                  <v-btn variant="text" size="small" class="px-1" @click="openDetail(skill)">
                    {{ skill.name }}
                  </v-btn>
                  <span class="d-block text-body-2 skill-description">{{ skill.description }}</span>
                </td>
                <td class="text-body-2">
                  <span class="d-block">{{ stageNames(skill) }}</span>
                  <span class="d-block">{{ agentNames(skill) }}</span>
                </td>
                <td>
                  {{ $t('agentsPage.skills.versionNumber', { n: skill.current_version }) }}
                  <span v-if="currentVersion(skill)?.submission_origin" class="d-block text-body-2">
                    {{ $t(`skillLibrary.origins.${currentVersion(skill)?.submission_origin}`) }}
                  </span>
                </td>
                <td>{{ format.integer(skill.estimated_tokens ?? 0) }}</td>
                <td>
                  <span class="font-weight-medium">
                    {{ $t(`agentsPage.skills.status.${skill.status}`) }}
                  </span>
                  <span
                    v-if="currentVersion(skill)?.block_reasons?.length"
                    class="d-block text-body-2"
                  >
                    {{
                      $t('skillLibrary.blockReasons', {
                        reasons: reasonsText(currentVersion(skill)?.block_reasons),
                      })
                    }}
                  </span>
                  <span v-if="currentVersion(skill)?.unblocked_by" class="d-block text-body-2">
                    {{
                      $t('skillLibrary.unblockedBy', {
                        name: currentVersion(skill)?.unblocked_by?.name,
                      })
                    }}
                  </span>
                  <span v-if="currentVersion(skill)?.deactivated_by" class="d-block text-body-2">
                    {{
                      $t('skillLibrary.deactivatedBy', {
                        name: currentVersion(skill)?.deactivated_by?.name,
                        when: format.dateTime(currentVersion(skill)?.deactivated_at),
                      })
                    }}
                  </span>
                </td>
                <td>
                  <div class="d-flex flex-wrap ga-1">
                    <v-btn
                      v-if="
                        skill.status === 'blocked' &&
                        canUnblock(currentVersion(skill)?.block_reasons)
                      "
                      size="small"
                      variant="tonal"
                      :aria-label="$t('skillLibrary.unblockNamed', { skill: skill.name })"
                      @click="ask('unblock', skill)"
                    >
                      {{ $t('skillLibrary.unblock') }}
                    </v-btn>
                    <v-btn
                      v-if="skill.status === 'unblocked' || skill.status === 'blocked'"
                      size="small"
                      variant="text"
                      color="error"
                      :aria-label="$t('skillLibrary.deactivateNamed', { skill: skill.name })"
                      @click="ask('deactivate', skill)"
                    >
                      {{ $t('skillLibrary.deactivate') }}
                    </v-btn>
                  </div>
                </td>
              </tr>
            </tbody>
          </v-table>
        </div>
      </section>

      <section aria-labelledby="native-skills-heading">
        <h3 id="native-skills-heading" class="text-subtitle-1 font-weight-medium mb-2">
          {{ $t('skillLibrary.native', { n: native.length }) }}
        </h3>
        <ul class="plain-list" data-testid="native-skills">
          <li v-for="skill in native" :key="skill.id" class="mb-2">
            <span class="font-weight-medium">{{ skill.name }}</span>
            · {{ $t('agentsPage.skills.versionNumber', { n: skill.current_version }) }} ·
            {{ format.integer(skill.estimated_tokens ?? 0) }}
            {{ $t('skillLibrary.tokens') }}
            <span class="d-block text-body-2 skill-description">{{ skill.description }}</span>
          </li>
        </ul>
      </section>
    </template>

    <SkillDetailDialog v-model="detailOpen" :skill-id="detailSkill" :read="agentsApi.getSkill" />
    <ConfirmActionDialog
      v-model="actionOpen"
      :title="
        action
          ? $t(`skillLibrary.${action.kind}Title`, {
              skill: action.skill.name,
              version: action.version,
            })
          : ''
      "
      :lines="action ? [$t(`skillLibrary.${action.kind}Effect`)] : []"
      :keep-label="$t('common.cancel')"
      :confirm-label="action ? $t(`skillLibrary.${action.kind}`) : ''"
      :confirm-color="action?.kind === 'deactivate' ? 'error' : 'primary'"
      :confirm-disabled="!justificationValid"
      test-id="skill-action-dialog"
      :submit="submitAction"
    >
      <v-textarea
        v-model="justification"
        :label="$t('skillLibrary.justification')"
        :hint="$t('skillLibrary.justificationHint')"
        persistent-hint
        counter="2000"
        rows="3"
        class="mt-2"
      />
    </ConfirmActionDialog>
  </section>
</template>

<style scoped>
.skill-description {
  white-space: pre-wrap;
}
</style>
