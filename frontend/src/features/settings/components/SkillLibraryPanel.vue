<script setup lang="ts">
import { computed, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import { ApiError, isApiError } from '@/shared/api/problemDetails'
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
const fieldId = useId()
const packageInput = ref<HTMLInputElement>()

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
const gitUrlInvalid = computed(() => git.value.url !== '' && !gitValid.value)
const canSend = computed(() =>
  source.value === 'upload' ? chosenFile.value !== undefined : gitValid.value,
)
/** As built: the git source answers 400 `host_not_allowed` until the egress gateway exists (ADR-16). */
const gitUnavailable = computed(
  () =>
    isApiError(sendFailure.value) &&
    (sendFailure.value.problem?.errors ?? []).some(
      (error) => error.field === 'git_url' && error.message === 'host_not_allowed',
    ),
)
const justificationCounter = computed(() => `${String(justification.value.length)} / 2000`)
const justificationValid = computed(() => {
  const length = justification.value.trim().length
  return length >= 10 && length <= 2000
})

function onPackageChange(event: Event): void {
  packageFile.value = (event.target as HTMLInputElement).files?.[0] ?? null
}

/** Canvas chip tone for a skill version state; the state is always written as text too. */
function statusTone(status: Skill['status']): string {
  if (status === 'unblocked') return 'ok'
  if (status === 'blocked') return 'warn'
  if (status === 'deactivated') return 'mute'
  return 'info'
}

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
    if (packageInput.value) packageInput.value.value = ''
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
  <!-- Layout: canvas "Configurações" (Configuracoes.dc.html), tab "Biblioteca de skills". -->
  <section class="card panel" aria-labelledby="skill-library-heading">
    <h2 id="skill-library-heading">{{ $t('skillLibrary.title') }}</h2>
    <p class="t2 panel-help">{{ $t('skillLibrary.help') }}</p>

    <LoadingState v-if="loader.loading.value && !loader.loaded.value" type="list-item@3" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else>
      <section aria-labelledby="organization-skills-heading" class="group">
        <h3 id="organization-skills-heading" class="lb">
          {{ $t('skillLibrary.organization', { n: organization.length }) }}
        </h3>
        <p v-if="organization.length === 0" class="empty">
          {{ $t('skillLibrary.noOrganization') }}
        </p>
        <div v-else class="table-scroll">
          <table class="tbl" data-testid="organization-skills">
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
                  <button type="button" class="skill-name" @click="openDetail(skill)">
                    {{ skill.name }}
                  </button>
                  <span class="t2 skill-description">{{ skill.description }}</span>
                </td>
                <td>
                  <span class="block">{{ stageNames(skill) }}</span>
                  <span class="block t2">{{ agentNames(skill) }}</span>
                </td>
                <td>
                  {{ $t('agentsPage.skills.versionNumber', { n: skill.current_version }) }}
                  <span v-if="currentVersion(skill)?.submission_origin" class="block t2">
                    {{ $t(`skillLibrary.origins.${currentVersion(skill)?.submission_origin}`) }}
                  </span>
                </td>
                <td>{{ format.integer(skill.estimated_tokens ?? 0) }}</td>
                <td>
                  <span class="chip" :class="statusTone(skill.status)">
                    {{ $t(`agentsPage.skills.status.${skill.status}`) }}
                  </span>
                  <span v-if="currentVersion(skill)?.block_reasons?.length" class="block t2">
                    {{
                      $t('skillLibrary.blockReasons', {
                        reasons: reasonsText(currentVersion(skill)?.block_reasons),
                      })
                    }}
                  </span>
                  <span v-if="currentVersion(skill)?.unblocked_by" class="block t2">
                    {{
                      $t('skillLibrary.unblockedBy', {
                        name: currentVersion(skill)?.unblocked_by?.name,
                      })
                    }}
                  </span>
                  <span v-if="currentVersion(skill)?.deactivated_by" class="block t2">
                    {{
                      $t('skillLibrary.deactivatedBy', {
                        name: currentVersion(skill)?.deactivated_by?.name,
                        when: format.dateTime(currentVersion(skill)?.deactivated_at),
                      })
                    }}
                  </span>
                </td>
                <td>
                  <div class="row-actions">
                    <button
                      v-if="
                        skill.status === 'blocked' &&
                        canUnblock(currentVersion(skill)?.block_reasons)
                      "
                      type="button"
                      class="btn"
                      :aria-label="$t('skillLibrary.unblockNamed', { skill: skill.name })"
                      @click="ask('unblock', skill)"
                    >
                      {{ $t('skillLibrary.unblock') }}
                    </button>
                    <button
                      v-if="skill.status === 'unblocked' || skill.status === 'blocked'"
                      type="button"
                      class="btn btn-d"
                      :aria-label="$t('skillLibrary.deactivateNamed', { skill: skill.name })"
                      @click="ask('deactivate', skill)"
                    >
                      {{ $t('skillLibrary.deactivate') }}
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section aria-labelledby="native-skills-heading" class="group">
        <h3 id="native-skills-heading" class="lb">
          {{ $t('skillLibrary.native', { n: native.length }) }}
        </h3>
        <ul class="native-list" data-testid="native-skills">
          <li v-for="skill in native" :key="skill.id" class="kv">
            <span class="native-main">
              <strong>{{ skill.name }}</strong>
              <span class="t2 skill-description">{{ skill.description }}</span>
            </span>
            <span class="native-side">
              <span class="chip mute">
                {{ $t('agentsPage.skills.versionNumber', { n: skill.current_version }) }}
              </span>
              <span class="t2">
                {{ format.integer(skill.estimated_tokens ?? 0) }} {{ $t('skillLibrary.tokens') }}
              </span>
            </span>
          </li>
        </ul>
      </section>
    </template>
  </section>

  <section class="card panel" aria-labelledby="skill-import-heading" data-testid="skill-import">
    <h2 id="skill-import-heading">{{ $t('skillLibrary.import.title') }}</h2>
    <fieldset class="source">
      <legend class="lb">{{ $t('skillLibrary.import.source') }}</legend>
      <div class="source-options">
        <label :for="`${fieldId}-upload`" class="op" :class="{ on: source === 'upload' }">
          <input
            :id="`${fieldId}-upload`"
            v-model="source"
            type="radio"
            :name="`${fieldId}-source`"
            value="upload"
          />
          {{ $t('skillLibrary.import.upload') }}
        </label>
        <label :for="`${fieldId}-git`" class="op" :class="{ on: source === 'git_repository' }">
          <input
            :id="`${fieldId}-git`"
            v-model="source"
            type="radio"
            :name="`${fieldId}-source`"
            value="git_repository"
          />
          {{ $t('skillLibrary.import.git') }}
        </label>
      </div>
    </fieldset>
    <form class="import-form" @submit.prevent="send">
      <div v-if="source === 'upload'" class="field" data-testid="skill-package">
        <label :for="`${fieldId}-package`" class="control">
          <span class="field-label">{{ $t('skillLibrary.import.package') }}</span>
          <input
            :id="`${fieldId}-package`"
            ref="packageInput"
            type="file"
            class="fld file"
            accept=".zip,application/zip"
            :aria-describedby="`${fieldId}-package-hint`"
            @change="onPackageChange"
          />
        </label>
        <span :id="`${fieldId}-package-hint`" class="t2 hint">
          {{ $t('skillLibrary.import.packageHint') }}
        </span>
      </div>
      <template v-else>
        <div class="field">
          <label :for="`${fieldId}-git-url`" class="control">
            <span class="field-label">{{ $t('skillLibrary.import.gitUrl') }}</span>
            <input
              :id="`${fieldId}-git-url`"
              v-model="git.url"
              class="fld"
              autocomplete="off"
              :aria-invalid="gitUrlInvalid ? 'true' : undefined"
              :aria-describedby="`${fieldId}-git-hint${gitUrlInvalid ? ` ${fieldId}-git-error` : ''}`"
            />
          </label>
          <span :id="`${fieldId}-git-hint`" class="t2 hint">
            {{ $t('skillLibrary.import.gitHint') }}
          </span>
          <span v-if="gitUrlInvalid" :id="`${fieldId}-git-error`" class="field-error">
            {{ $t('skillLibrary.import.httpsOnly') }}
          </span>
        </div>
        <div class="field">
          <label :for="`${fieldId}-reference`" class="control">
            <span class="field-label">{{ $t('skillLibrary.import.reference') }}</span>
            <input
              :id="`${fieldId}-reference`"
              v-model="git.reference"
              class="fld"
              autocomplete="off"
            />
          </label>
        </div>
        <div class="field">
          <label :for="`${fieldId}-subfolder`" class="control">
            <span class="field-label">{{ $t('skillLibrary.import.subfolder') }}</span>
            <input
              :id="`${fieldId}-subfolder`"
              v-model="git.subfolder"
              class="fld"
              autocomplete="off"
            />
          </label>
        </div>
      </template>
      <ProblemAlert v-if="sendFailure" :error="sendFailure" data-testid="skill-send-failure" />
      <p v-if="gitUnavailable" class="note unavailable" data-testid="git-unavailable">
        {{ $t('skillLibrary.import.gitUnavailable') }}
      </p>
      <div>
        <button
          type="submit"
          class="btn btn-p"
          :disabled="sending || !canSend"
          :aria-busy="sending ? 'true' : undefined"
        >
          {{ $t('skillLibrary.import.send') }}
        </button>
      </div>
    </form>
    <div aria-live="polite" class="imports" data-testid="skill-imports">
      <div v-for="item in imports" :key="item.id" class="kv import-row">
        <div class="import-text">
          <p>
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
    </div>
  </section>

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
    <div class="field justification">
      <label :for="`${fieldId}-justification`" class="control">
        <span class="field-label">{{ $t('skillLibrary.justification') }}</span>
        <textarea
          :id="`${fieldId}-justification`"
          v-model="justification"
          class="fld"
          rows="3"
          :aria-describedby="`${fieldId}-justification-hint`"
        />
      </label>
      <span :id="`${fieldId}-justification-hint`" class="t2 hint justification-foot">
        <span>{{ $t('skillLibrary.justificationHint') }}</span>
        <span>{{ justificationCounter }}</span>
      </span>
    </div>
  </ConfirmActionDialog>
</template>

<style scoped>
.panel {
  padding: 18px 22px;
}

.panel-help {
  margin: 4px 0 8px;
  font-size: 13px;
}

.group + .group {
  margin-top: 16px;
}

.group h3 {
  margin: 0 0 6px;
}

.empty {
  margin: 0;
}

.tbl {
  min-width: 620px;
}

.tbl th {
  padding: 8px 12px 8px 0;
}

.tbl td {
  padding: 12px 12px 12px 0;
  vertical-align: middle;
}

.block {
  display: block;
}

.skill-name {
  min-height: 32px;
  padding: 0;
  border: 0;
  background: transparent;
  color: #f1f3ff;
  font: inherit;
  font-weight: 700;
  text-align: left;
  cursor: pointer;
  text-decoration: underline;
  text-decoration-color: rgba(167, 139, 250, 0.55);
  text-underline-offset: 3px;
}

.skill-name:hover {
  color: #ffffff;
}

.skill-description {
  display: block;
  white-space: pre-wrap;
}

.row-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.native-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.native-main {
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.native-side {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  white-space: nowrap;
}

.source {
  border: 0;
  margin: 12px 0 0;
  padding: 0;
}

.source legend {
  padding: 0;
  margin-bottom: 8px;
}

.source-options {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.source-options .op {
  width: auto;
  flex: 1 1 220px;
  align-items: center;
  min-height: 44px;
  box-sizing: border-box;
}

.source-options .op:has(input:focus-visible) {
  outline: 2px solid #c4b5fd;
  outline-offset: 2px;
}

input[type='radio'] {
  width: 18px;
  height: 18px;
  margin: 0;
  flex: none;
  accent-color: #7c3aed;
  cursor: pointer;
}

.import-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
  margin-top: 14px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.control {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.field-label {
  font-weight: 600;
}

.hint {
  font-size: 12px;
}

.field-error {
  font-size: 12px;
  font-weight: 600;
  color: #fca5a5;
}

.file {
  padding: 10px 12px;
}

.unavailable {
  margin: 0;
}

.imports {
  margin-top: 14px;
}

.imports:empty {
  margin-top: 0;
}

.import-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.import-text p {
  margin: 0;
}

.justification {
  margin-top: 8px;
}

.justification-foot {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}
</style>
