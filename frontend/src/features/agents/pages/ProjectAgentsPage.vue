<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { isApiError } from '@/shared/api/problemDetails'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import { useSessionStore } from '@/shared/session/sessionStore'
import AppBreadcrumbs from '@/shared/ui/AppBreadcrumbs.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { projectsApi, type Project } from '@/features/projects/api/projectsApi'
import type { ModelRef } from '@/shared/api/contract'
import {
  agentsApi,
  type AgentTools,
  type AgentWithEtag,
  type NewAgentConfiguration,
  type PinnedSkill,
  type ProjectAgent,
  type Skill,
} from '../api/agentsApi'
import AgentConfigurationEditor from '../components/AgentConfigurationEditor.vue'
import AgentHistory from '../components/AgentHistory.vue'
import SkillDetailDialog from '../components/SkillDetailDialog.vue'
import SkillPickerDialog from '../components/SkillPickerDialog.vue'
import {
  acceptingVersion,
  hasSkillUpdate,
  withSkill,
  withoutSkill,
} from '../composables/skillAssociation'

/**
 * ESP-22 "Agentes e skills" for one project: the seven agents with their configuration, and the
 * skill association by the project lead (US-43). Members and approvers read only (CA-22-9).
 * Changing a skill saves a new configuration version at once (ER-22-1). The lead also edits the
 * instructions adjustment and the tools in an edit mode with a summary before saving (US-25).
 */
const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const router = useRouter()
const format = useFormat()
const session = useSessionStore()
const { message, announce } = useAnnouncer()
const projectId = String(route.params.projectId)

const project = ref<Project>()
const agents = ref<ProjectAgent[]>([])
const catalog = ref<Skill[]>([])
const selected = ref<AgentWithEtag>()
const saving = ref(false)
const saveFailure = ref<unknown>()
const pickerOpen = ref(false)
const detailOpen = ref(false)
const detailSkill = ref<string>()
const acceptOpen = ref(false)
const acceptTarget = ref<PinnedSkill>()
const detailHeading = ref<HTMLElement>()
/** US-25: the edit mode, whether it has unsaved changes, and a counter that reloads the history. */
const editing = ref(false)
const editorDirty = ref(false)
const historyRevision = ref(0)

const loader = useLoader(async () => {
  const [loadedProject, loadedAgents] = await Promise.all([
    projectsApi.getProject(projectId),
    agentsApi.listProjectAgents(projectId),
  ])
  project.value = loadedProject
  agents.value = loadedAgents
  // The library only names the skills; without it the ids still show.
  catalog.value = await agentsApi.listSkills().catch(() => [])
})

const agentId = computed(() => {
  const requested = typeof route.params.agent === 'string' ? route.params.agent : ''
  return agents.value.some((item) => item.agent === requested)
    ? requested
    : (agents.value[0]?.agent ?? '')
})

const agentLoader = useLoader(async () => {
  selected.value = undefined
  selected.value = await agentsApi.getProjectAgent(projectId, agentId.value)
})

const isLead = computed(() => project.value?.my_roles.includes('lead') ?? false)
const agent = computed(() => selected.value?.agent)
const configuration = computed(() => agent.value?.configuration)
const notFound = computed(() => isApiError(loader.error.value) && loader.error.value.status === 404)
const conflict = computed(() => isApiError(saveFailure.value) && saveFailure.value.status === 412)

const breadcrumbs = computed(() => [
  { title: t('pages.projects.title'), to: { name: ROUTE_NAMES.projects } },
  { title: project.value?.name ?? '', to: { name: ROUTE_NAMES.project, params: { projectId } } },
  { title: t('pages.agents.title') },
])

void loader.run()
watch(agentId, (value) => {
  if (value) void agentLoader.run()
})

async function choose(value: string): Promise<void> {
  await router.replace({ name: ROUTE_NAMES.projectAgents, params: { projectId, agent: value } })
  // ESP-22 section 8: the focus goes to the detail title only when the user chose an agent.
  await nextTick()
  detailHeading.value?.focus()
}

function agentName(item: Pick<ProjectAgent, 'agent' | 'name'>): string {
  const key = `agents.${item.agent}`
  return i18n.te(key) ? t(key) : item.name
}

/** Canvas "Agentes e skills": each agent button carries a decorative initials badge. */
const AGENT_BADGES: Record<string, { initials: string; color: string }> = {
  'requirements-agent': { initials: 'RQ', color: '#2563EB' },
  'architect-agent': { initials: 'AR', color: '#7C3AED' },
  'dba-agent': { initials: 'DB', color: '#0B766B' },
  'designer-agent': { initials: 'DS', color: '#95581A' },
  'developer-agent': { initials: 'DV', color: '#2563EB' },
  'qa-agent': { initials: 'QA', color: '#0B766B' },
  'security-agent': { initials: 'SG', color: '#B91C1C' },
}

function agentBadge(id: string): { initials: string; color: string } {
  return AGENT_BADGES[id] ?? { initials: id.slice(0, 2).toUpperCase(), color: '#4F46E5' }
}

function catalogSkill(id: string): Skill | undefined {
  return catalog.value.find((item) => item.id === id)
}

function stageName(id: string): string {
  const key = `stages.${id}`
  return i18n.te(key) ? t(key) : id
}

function modelText(model: ModelRef): string {
  return t('agentsPage.modelRef', { model: model.model, provider: providerName(model.provider) })
}

function providerName(id: string): string {
  const key = `providers.${id}`
  return i18n.te(key) ? t(key) : id
}

function categoryName(id: string): string {
  const key = `evidence.categories.${id}`
  return i18n.te(key) ? t(key) : id
}

/** ESP-22 3.4 (PL-64): one line per tool, by the contract structure, with paths or categories. */
function toolLines(tools: AgentTools | undefined): Array<{ key: string; value: string }> {
  if (!tools) return []
  const lines: Array<{ key: string; value: string }> = []
  const paths = (key: keyof AgentTools, values: string[] | undefined) => {
    if (values?.length) lines.push({ key, value: values.join(', ') })
  }
  paths('read_file', tools.read_file)
  paths('write_file', tools.write_file)
  if (tools.execute_command?.allowed) {
    lines.push({
      key: 'execute_command',
      value: (tools.execute_command.categories ?? []).map(categoryName).join(', '),
    })
  }
  paths('git', tools.git)
  if (tools.browser) lines.push({ key: 'browser', value: t('agentsPage.tools.allowed') })
  paths('scanners', tools.scanners)
  return lines
}

/** ESP-22 6 "Edição em curso": leaving with unsaved changes asks first. */
function confirmLeave(): boolean {
  if (!editing.value || !editorDirty.value) return true
  return window.confirm(t('agentsPage.edit.leaveConfirm'))
}

onBeforeRouteLeave(() => confirmLeave())
onBeforeRouteUpdate((to, from) => {
  if (to.params.agent === from.params.agent) return true
  if (!confirmLeave()) return false
  editing.value = false
  editorDirty.value = false
  return true
})

async function saveEdition(next: NewAgentConfiguration): Promise<void> {
  await save(next)
  editing.value = false
  editorDirty.value = false
}

async function save(next: NewAgentConfiguration): Promise<void> {
  if (!selected.value) return
  saving.value = true
  saveFailure.value = undefined
  try {
    const saved = await agentsApi.modifyAgentConfiguration(
      projectId,
      agentId.value,
      next,
      selected.value.etag,
    )
    announce(t('agentsPage.saved', { n: saved.configuration.version }))
    const [reloaded, list] = await Promise.all([
      agentsApi.getProjectAgent(projectId, agentId.value),
      agentsApi.listProjectAgents(projectId),
    ])
    selected.value = reloaded
    agents.value = list
    historyRevision.value += 1
  } catch (error) {
    saveFailure.value = error
    throw error
  } finally {
    saving.value = false
  }
}

function associate(skill: Skill): Promise<void> {
  if (!configuration.value) return Promise.resolve()
  return save(withSkill(configuration.value, skill.id, skill.current_version))
}

async function remove(item: PinnedSkill): Promise<void> {
  if (!configuration.value) return
  await save(withoutSkill(configuration.value, item.skill)).catch(() => undefined)
}

function askAccept(item: PinnedSkill): void {
  acceptTarget.value = item
  acceptOpen.value = true
}

async function confirmAccept(): Promise<void> {
  if (!configuration.value || !acceptTarget.value) return
  try {
    await save(acceptingVersion(configuration.value, acceptTarget.value.skill))
    acceptOpen.value = false
  } catch {
    acceptOpen.value = false
  }
}

function openDetail(skill: string): void {
  detailSkill.value = skill
  detailOpen.value = true
}

async function reload(): Promise<void> {
  saveFailure.value = undefined
  await agentLoader.run()
}
</script>

<template>
  <!-- Layout: canvas "Agentes e skills" (Agentes.dc.html). -->
  <LoadingState v-if="loader.loading.value && !project" />
  <template v-else-if="loader.error.value && !project">
    <h1>{{ $t('pages.agents.title') }}</h1>
    <v-alert v-if="notFound" type="warning" variant="tonal" role="alert">
      <p>{{ $t('problems.not-found') }}</p>
      <router-link :to="{ name: ROUTE_NAMES.projects }">{{ $t('common.goProjects') }}</router-link>
    </v-alert>
    <ProblemAlert v-else :error="loader.error.value" retry @retry="loader.run" />
  </template>

  <template v-else-if="project">
    <AppBreadcrumbs :items="breadcrumbs" />
    <div class="page-head">
      <div>
        <h1>{{ $t('pages.agents.title') }}</h1>
        <p class="t2 page-description">{{ project.name }} · {{ $t('agentsPage.description') }}</p>
      </div>
      <router-link
        v-if="session.isAdministrator"
        class="btn"
        :to="{ name: ROUTE_NAMES.settings, params: { tab: 'skills' } }"
      >
        {{ $t('settings.tabs.skills') }}
      </router-link>
    </div>
    <div role="status" class="sr-only">{{ message }}</div>
    <p v-if="!isLead" class="note" data-testid="read-only-notice">
      {{ $t('agentsPage.readOnly') }}
    </p>

    <div class="agents-layout">
      <!-- ESP-22 section 8: the agent list is a group of buttons with the pressed state exposed. -->
      <nav class="card agent-list" :aria-label="$t('agentsPage.listLabel')">
        <ul class="plain-list">
          <li v-for="item in agents" :key="item.agent">
            <button
              type="button"
              class="agb"
              :class="{ on: item.agent === agentId }"
              :aria-pressed="item.agent === agentId ? 'true' : 'false'"
              :data-testid="`agent-${item.agent}`"
              @click="choose(item.agent)"
            >
              <span
                class="av agb-badge"
                :style="{ background: agentBadge(item.agent).color }"
                aria-hidden="true"
                >{{ agentBadge(item.agent).initials }}</span
              >
              <span class="agb-text">
                <span class="agb-name">{{ agentName(item) }}</span>
                <span class="t2 agb-stages">{{ item.stages.map(stageName).join(', ') }}</span>
                <span v-if="hasSkillUpdate(item)" class="agb-update">
                  {{ $t('agentsPage.skillUpdateMarker') }}
                </span>
              </span>
            </button>
          </li>
        </ul>
      </nav>

      <section class="agent-detail" aria-labelledby="agent-heading">
        <LoadingState v-if="agentLoader.loading.value && !agent" />
        <ProblemAlert
          v-else-if="agentLoader.error.value && !agent"
          :error="agentLoader.error.value"
          retry
          @retry="agentLoader.run"
        />
        <template v-else-if="agent && configuration">
          <!-- CA-22-13: configuration version and instructions version apart. -->
          <div class="card detail-card" data-testid="agent-header">
            <div class="detail-head">
              <h2 id="agent-heading" ref="detailHeading" tabindex="-1" class="detail-title">
                {{ agentName(agent) }}
              </h2>
              <span class="detail-chips">
                <span class="chip mute">
                  {{ $t('agentsPage.configurationVersion', { n: configuration.version }) }}
                </span>
                <span class="chip mute">
                  {{
                    $t('agentsPage.instructionsVersion', { v: configuration.instructions_version })
                  }}
                </span>
              </span>
            </div>
            <p v-if="agent.role" class="detail-role">
              <span class="sr-only">{{ $t('agentsPage.role') }}: </span>{{ agent.role }}
            </p>
            <div class="kv-grid">
              <div class="kv">
                <span class="t2">{{ $t('agentsPage.stages') }}</span>
                <span class="kv-value">{{ agent.stages.map(stageName).join(', ') }}</span>
              </div>
              <template v-for="(pair, stage) in agent.models_by_stage ?? {}" :key="stage">
                <div v-if="pair" class="kv">
                  <span class="t2">
                    {{ $t('agentsPage.modelIn', { stage: stageName(String(stage)) }) }}
                  </span>
                  <span class="kv-value">
                    {{ $t('waves.roles.reasoning') }}: {{ modelText(pair.reasoning) }} ·
                    {{ $t('waves.roles.execution') }}: {{ modelText(pair.execution) }}
                  </span>
                </div>
              </template>
              <div v-if="configuration.tools.write_file?.length" class="kv">
                <span class="t2">{{ $t('agentsPage.writesTo') }}</span>
                <span class="mono kv-value">{{ configuration.tools.write_file.join(', ') }}</span>
              </div>
              <div v-if="agent.delivery?.length" class="kv">
                <span class="t2">{{ $t('agentsPage.delivers') }}</span>
                <span class="kv-value">{{ agent.delivery.join(', ') }}</span>
              </div>
              <div class="kv">
                <span class="t2">{{ $t('agentsPage.lastChange') }}</span>
                <span class="kv-value">
                  {{ format.dateTime(configuration.created_at) }}
                  <template v-if="configuration.created_by">
                    · {{ configuration.created_by.name }}</template
                  >
                </span>
              </div>
            </div>
            <div v-if="configuration.adjustment_instructions">
              <span class="lb">{{ $t('agentsPage.edit.instructionsTitle') }}</span>
              <p class="adjustment-text" data-testid="agent-adjustment-text">
                {{ configuration.adjustment_instructions }}
              </p>
            </div>
            <div v-if="isLead && !editing">
              <button
                type="button"
                class="btn"
                :disabled="saving"
                data-testid="edit-agent"
                @click="editing = true"
              >
                {{ $t('agentsPage.edit.action') }}
              </button>
            </div>
          </div>

          <section v-if="editing" class="card panel" aria-labelledby="edit-heading">
            <h2 id="edit-heading" class="panel-title">{{ $t('agentsPage.edit.title') }}</h2>
            <AgentConfigurationEditor
              :key="configuration.version"
              :agent="agent"
              :submit="saveEdition"
              @cancel="editing = false"
              @dirty="editorDirty = $event"
            />
          </section>

          <div class="card-pair">
            <section class="card panel skills-card" aria-labelledby="skills-heading">
              <h2 id="skills-heading" class="panel-title">{{ $t('agentsPage.skills.title') }}</h2>
              <p class="t2 panel-help">{{ $t('agentsPage.skills.loadingRule') }}</p>
              <ProblemAlert v-if="saveFailure" :error="saveFailure" class="panel-alert" />
              <button
                v-if="conflict"
                type="button"
                class="btn panel-reload"
                data-testid="reload-agent"
                @click="reload"
              >
                {{ $t('agentsPage.reload') }}
              </button>
              <p v-if="configuration.skills.length === 0" class="kv">
                {{ $t('agentsPage.skills.none') }}
              </p>
              <ul v-else class="plain-list" data-testid="agent-skills">
                <li
                  v-for="item in configuration.skills"
                  :key="item.skill"
                  class="kv skill-row"
                  :data-testid="`pinned-${item.skill}`"
                >
                  <span class="skill-main">
                    <button type="button" class="skill-name" @click="openDetail(item.skill)">
                      {{ catalogSkill(item.skill)?.name ?? item.skill }}
                    </button>
                    <span class="t2 skill-meta">
                      {{ $t('agentsPage.skills.pinned') }}:
                      {{ $t('agentsPage.skills.versionNumber', { n: item.version }) }} ·
                      {{
                        $t('agentsPage.skills.tokens', {
                          n: format.integer(catalogSkill(item.skill)?.estimated_tokens ?? 0),
                        })
                      }}
                    </span>
                    <span v-if="item.available_version" class="skill-update">
                      {{ $t('agentsPage.skills.versionAvailable', { n: item.available_version }) }}
                    </span>
                  </span>
                  <span class="skill-side">
                    <span
                      v-if="catalogSkill(item.skill)"
                      class="chip"
                      :class="catalogSkill(item.skill)?.origin === 'organization' ? 'vio' : 'mute'"
                    >
                      {{ $t(`agentsPage.skills.origin.${catalogSkill(item.skill)?.origin}`) }}
                    </span>
                    <template v-if="isLead">
                      <button
                        v-if="item.available_version"
                        type="button"
                        class="btn"
                        :disabled="saving"
                        @click="askAccept(item)"
                      >
                        {{ $t('agentsPage.skills.accept') }}
                      </button>
                      <button
                        type="button"
                        class="btn btn-d"
                        :disabled="saving"
                        :aria-label="
                          $t('agentsPage.skills.removeNamed', {
                            skill: catalogSkill(item.skill)?.name ?? item.skill,
                          })
                        "
                        @click="remove(item)"
                      >
                        {{ $t('common.remove') }}
                      </button>
                    </template>
                  </span>
                </li>
              </ul>
              <button
                v-if="isLead"
                type="button"
                class="btn panel-action"
                :disabled="saving"
                @click="pickerOpen = true"
              >
                {{ $t('agentsPage.skills.associate') }}
              </button>
            </section>

            <section class="card panel tools-card" aria-labelledby="tools-heading">
              <h2 id="tools-heading" class="panel-title">{{ $t('agentsPage.tools.title') }}</h2>
              <p class="t2 panel-help">{{ $t('agentsPage.tools.help') }}</p>
              <p v-if="toolLines(configuration.tools).length === 0" class="kv">
                {{ $t('agentsPage.tools.none') }}
              </p>
              <dl v-else class="tool-list" data-testid="agent-tools">
                <div v-for="line in toolLines(configuration.tools)" :key="line.key" class="kv">
                  <dt>
                    {{ $t(`agentsPage.tools.${line.key}`) }}
                    <span class="mono t2 tool-value">{{ line.value }}</span>
                  </dt>
                  <dd>
                    <span class="chip ok">{{ $t('agentsPage.tools.allowed') }}</span>
                  </dd>
                </div>
              </dl>
              <template v-if="agent.limits?.length">
                <h3 class="lb sub-label">{{ $t('agentsPage.limits') }}</h3>
                <ul class="bullet-list">
                  <li v-for="(limit, index) in agent.limits" :key="index">{{ limit }}</li>
                </ul>
              </template>
              <h3 class="lb sub-label">{{ $t('agentsPage.outputCriteria') }}</h3>
              <p v-if="configuration.output_criteria.length === 0" class="sub-text">
                {{ $t('agentsPage.noOutputCriteria') }}
              </p>
              <ul v-else class="bullet-list">
                <li v-for="check in configuration.output_criteria" :key="check" class="mono">
                  {{ check }}
                </li>
              </ul>
            </section>
          </div>

          <AgentHistory :project-id="projectId" :agent="agent.agent" :revision="historyRevision" />
        </template>
      </section>
    </div>

    <SkillPickerDialog
      v-if="agent && configuration"
      v-model="pickerOpen"
      :agent="agent.agent"
      :agent-name="agentName(agent)"
      :pinned="configuration.skills"
      :list="agentsApi.listSkills"
      :submit="associate"
    />
    <SkillDetailDialog v-model="detailOpen" :skill-id="detailSkill" :read="agentsApi.getSkill" />
    <v-dialog v-model="acceptOpen" max-width="480" aria-labelledby="accept-skill-title">
      <v-card data-testid="accept-skill-dialog">
        <div class="dialog-body">
          <h2 id="accept-skill-title">
            {{
              $t('agentsPage.skills.acceptTitle', {
                skill: catalogSkill(acceptTarget?.skill ?? '')?.name ?? acceptTarget?.skill,
              })
            }}
          </h2>
          <p>
            {{
              $t('agentsPage.skills.acceptCompare', {
                current: acceptTarget?.version,
                next: acceptTarget?.available_version,
              })
            }}
          </p>
          <p class="t2">{{ $t('agentsPage.skills.acceptRule') }}</p>
        </div>
        <div class="dialog-actions">
          <button type="button" class="btn" :disabled="saving" @click="acceptOpen = false">
            {{ $t('common.cancel') }}
          </button>
          <button
            type="button"
            class="btn btn-p"
            :disabled="saving"
            :aria-busy="saving ? 'true' : undefined"
            @click="confirmAccept"
          >
            {{ $t('agentsPage.skills.accept') }}
          </button>
        </div>
      </v-card>
    </v-dialog>
  </template>
</template>

<style scoped>
.page-head {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: flex-end;
  gap: 16px;
}

.page-description {
  margin: 4px 0 0;
}

.note {
  margin: 0;
}

.agents-layout {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  align-items: flex-start;
}

.agent-list {
  flex: 1 1 260px;
  min-width: 0;
  padding: 12px;
}

.plain-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.agb {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  min-height: 56px;
  padding: 10px 12px;
  border: 1px solid transparent;
  border-radius: 10px;
  background: transparent;
  color: #f1f3ff;
  font: inherit;
  text-align: left;
  cursor: pointer;
  box-sizing: border-box;
}

.agb:hover {
  background: rgba(255, 255, 255, 0.05);
}

.agb.on {
  background: rgba(99, 102, 241, 0.16);
  border-color: rgba(129, 140, 248, 0.45);
}

.agb-badge {
  width: 34px;
  height: 34px;
  border-radius: 10px;
}

.agb-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.agb-name {
  font-weight: 600;
}

.agb-stages {
  font-size: 12px;
}

.agb-update {
  font-size: 12px;
  font-weight: 700;
  color: #fcd34d;
}

.agent-detail {
  flex: 3 1 560px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.detail-card {
  padding: 22px 24px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.detail-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.detail-title {
  font-size: 20px;
}

.detail-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

.detail-role {
  margin: 0;
}

.kv-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 0 24px;
}

.kv-value {
  text-align: right;
}

.adjustment-text {
  margin: 4px 0 0;
  white-space: pre-wrap;
}

.card-pair {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  align-items: flex-start;
}

.panel {
  padding: 18px 22px;
}

.skills-card {
  flex: 1 1 320px;
  min-width: 0;
}

.tools-card {
  flex: 1 1 280px;
  min-width: 0;
}

.panel-title {
  margin-bottom: 4px;
}

.panel-help {
  margin: 0 0 6px;
  font-size: 13px;
}

.panel-alert,
.panel-reload {
  margin-bottom: 8px;
}

.panel-action {
  margin-top: 12px;
}

p.kv {
  margin: 0;
}

.skill-row {
  align-items: center;
}

.skill-main {
  min-width: 0;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
}

.skill-name {
  min-height: 32px;
  padding: 0;
  border: 0;
  background: transparent;
  color: #f1f3ff;
  font: inherit;
  text-align: left;
  cursor: pointer;
  text-decoration: underline;
  text-decoration-color: rgba(167, 139, 250, 0.55);
  text-underline-offset: 3px;
}

.skill-name:hover {
  color: #ffffff;
}

.skill-meta {
  font-size: 12px;
}

.skill-update {
  font-size: 12px;
  font-weight: 700;
  color: #fcd34d;
}

.skill-side {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
}

.tool-list {
  margin: 0;
}

.tool-list dt {
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.tool-list dd {
  margin: 0;
}

.tool-value {
  word-break: break-all;
}

.sub-label {
  display: block;
  margin: 14px 0 0;
}

.sub-text {
  margin: 4px 0 0;
  font-size: 13px;
}

.bullet-list {
  margin: 4px 0 0;
  padding-left: 18px;
  font-size: 13px;
}

.dialog-body {
  padding: 22px 24px 8px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.dialog-body p {
  margin: 0;
}

.dialog-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 24px 22px;
}
</style>
