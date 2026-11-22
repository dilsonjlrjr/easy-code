<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { isApiError } from '@/shared/api/problemDetails'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
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
 * Changing a skill saves a new configuration version at once (ER-22-1); the instructions adjustment
 * and the tools are kept as they are (their edition is US-25, not in this milestone).
 */
const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const router = useRouter()
const format = useFormat()
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
  <v-container max-width="1200">
    <LoadingState v-if="loader.loading.value && !project" />
    <template v-else-if="loader.error.value && !project">
      <h1 class="text-h4 mb-4">{{ $t('pages.agents.title') }}</h1>
      <v-alert v-if="notFound" type="warning" variant="tonal" role="alert">
        <p>{{ $t('problems.not-found') }}</p>
        <router-link :to="{ name: ROUTE_NAMES.projects }">{{
          $t('common.goProjects')
        }}</router-link>
      </v-alert>
      <ProblemAlert v-else :error="loader.error.value" retry @retry="loader.run" />
    </template>

    <template v-else-if="project">
      <AppBreadcrumbs :items="breadcrumbs" />
      <h1 class="text-h4 mb-1">{{ $t('pages.agents.title') }}</h1>
      <p class="text-body-1 mb-4">{{ project.name }}</p>
      <div role="status" class="sr-only">{{ message }}</div>
      <p v-if="!isLead" class="text-body-2 mb-4" data-testid="read-only-notice">
        {{ $t('agentsPage.readOnly') }}
      </p>

      <div class="agents-layout">
        <!-- ESP-22 section 8: the agent list is a group of buttons with the pressed state exposed. -->
        <nav :aria-label="$t('agentsPage.listLabel')">
          <ul class="plain-list">
            <li v-for="item in agents" :key="item.agent" class="mb-2">
              <v-btn
                block
                variant="outlined"
                class="agent-button"
                :aria-pressed="item.agent === agentId ? 'true' : 'false'"
                :class="{ 'agent-button--selected': item.agent === agentId }"
                :data-testid="`agent-${item.agent}`"
                @click="choose(item.agent)"
              >
                <span class="text-left">
                  <span class="d-block font-weight-medium">{{ agentName(item) }}</span>
                  <span class="d-block text-body-2">
                    {{ item.stages.map(stageName).join(', ') }}
                  </span>
                  <span v-if="hasSkillUpdate(item)" class="d-block text-body-2 font-weight-bold">
                    {{ $t('agentsPage.skillUpdateMarker') }}
                  </span>
                </span>
              </v-btn>
            </li>
          </ul>
        </nav>

        <section aria-labelledby="agent-heading">
          <LoadingState v-if="agentLoader.loading.value && !agent" />
          <ProblemAlert
            v-else-if="agentLoader.error.value && !agent"
            :error="agentLoader.error.value"
            retry
            @retry="agentLoader.run"
          />
          <template v-else-if="agent && configuration">
            <h2 id="agent-heading" ref="detailHeading" tabindex="-1" class="text-h5 mb-2">
              {{ agentName(agent) }}
            </h2>
            <!-- CA-22-13: configuration version and instructions version apart. -->
            <dl class="summary-list mb-6" data-testid="agent-header">
              <dt>{{ $t('agentsPage.configurationLabel') }}</dt>
              <dd>{{ $t('agentsPage.configurationVersion', { n: configuration.version }) }}</dd>
              <dt>{{ $t('agentsPage.instructionsLabel') }}</dt>
              <dd>
                {{
                  $t('agentsPage.instructionsVersion', { v: configuration.instructions_version })
                }}
              </dd>
              <template v-if="agent.role">
                <dt>{{ $t('agentsPage.role') }}</dt>
                <dd>{{ agent.role }}</dd>
              </template>
              <dt>{{ $t('agentsPage.stages') }}</dt>
              <dd>{{ agent.stages.map(stageName).join(', ') }}</dd>
              <template v-for="(pair, stage) in agent.models_by_stage ?? {}" :key="stage">
                <dt>{{ $t('agentsPage.modelIn', { stage: stageName(String(stage)) }) }}</dt>
                <dd v-if="pair">
                  {{ $t('waves.roles.reasoning') }}: {{ modelText(pair.reasoning) }} ·
                  {{ $t('waves.roles.execution') }}: {{ modelText(pair.execution) }}
                </dd>
              </template>
              <template v-if="configuration.tools.write_file?.length">
                <dt>{{ $t('agentsPage.writesTo') }}</dt>
                <dd class="monospace">{{ configuration.tools.write_file.join(', ') }}</dd>
              </template>
              <template v-if="agent.delivery?.length">
                <dt>{{ $t('agentsPage.delivers') }}</dt>
                <dd>{{ agent.delivery.join(', ') }}</dd>
              </template>
              <dt>{{ $t('agentsPage.lastChange') }}</dt>
              <dd>
                {{ format.dateTime(configuration.created_at) }}
                <template v-if="configuration.created_by">
                  · {{ configuration.created_by.name }}</template
                >
              </dd>
            </dl>

            <section aria-labelledby="skills-heading" class="mb-6">
              <div class="d-flex flex-wrap align-center ga-2 mb-2">
                <h3 id="skills-heading" class="text-h6">{{ $t('agentsPage.skills.title') }}</h3>
                <v-spacer />
                <v-btn
                  v-if="isLead"
                  color="primary"
                  variant="tonal"
                  :disabled="saving"
                  @click="pickerOpen = true"
                >
                  {{ $t('agentsPage.skills.associate') }}
                </v-btn>
              </div>
              <p class="text-body-2 text-medium-emphasis mb-2">
                {{ $t('agentsPage.skills.loadingRule') }}
              </p>
              <ProblemAlert v-if="saveFailure" :error="saveFailure" class="mb-2" />
              <v-btn
                v-if="conflict"
                variant="outlined"
                size="small"
                class="mb-2"
                data-testid="reload-agent"
                @click="reload"
              >
                {{ $t('agentsPage.reload') }}
              </v-btn>
              <p v-if="configuration.skills.length === 0" class="text-body-2">
                {{ $t('agentsPage.skills.none') }}
              </p>
              <div v-else class="table-scroll">
                <v-table density="comfortable" data-testid="agent-skills">
                  <thead>
                    <tr>
                      <th scope="col">{{ $t('agentsPage.skills.name') }}</th>
                      <th scope="col">{{ $t('agentsPage.skills.originLabel') }}</th>
                      <th scope="col">{{ $t('agentsPage.skills.pinned') }}</th>
                      <th scope="col">{{ $t('agentsPage.skills.estimatedTokens') }}</th>
                      <th v-if="isLead" scope="col">
                        <span class="sr-only">{{ $t('common.actions') }}</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr
                      v-for="item in configuration.skills"
                      :key="item.skill"
                      :data-testid="`pinned-${item.skill}`"
                    >
                      <td>
                        <v-btn
                          variant="text"
                          size="small"
                          class="px-1"
                          @click="openDetail(item.skill)"
                        >
                          {{ catalogSkill(item.skill)?.name ?? item.skill }}
                        </v-btn>
                      </td>
                      <td>
                        <template v-if="catalogSkill(item.skill)">
                          {{ $t(`agentsPage.skills.origin.${catalogSkill(item.skill)?.origin}`) }}
                        </template>
                      </td>
                      <td>
                        {{ $t('agentsPage.skills.versionNumber', { n: item.version }) }}
                        <span v-if="item.available_version" class="d-block font-weight-bold">
                          {{
                            $t('agentsPage.skills.versionAvailable', { n: item.available_version })
                          }}
                        </span>
                      </td>
                      <td>{{ format.integer(catalogSkill(item.skill)?.estimated_tokens ?? 0) }}</td>
                      <td v-if="isLead">
                        <div class="d-flex flex-wrap ga-1">
                          <v-btn
                            v-if="item.available_version"
                            variant="tonal"
                            size="small"
                            :disabled="saving"
                            @click="askAccept(item)"
                          >
                            {{ $t('agentsPage.skills.accept') }}
                          </v-btn>
                          <v-btn
                            variant="text"
                            size="small"
                            color="error"
                            :disabled="saving"
                            :aria-label="
                              $t('agentsPage.skills.removeNamed', {
                                skill: catalogSkill(item.skill)?.name ?? item.skill,
                              })
                            "
                            @click="remove(item)"
                          >
                            {{ $t('common.remove') }}
                          </v-btn>
                        </div>
                      </td>
                    </tr>
                  </tbody>
                </v-table>
              </div>
            </section>

            <section aria-labelledby="tools-heading" class="mb-6">
              <h3 id="tools-heading" class="text-h6 mb-2">{{ $t('agentsPage.tools.title') }}</h3>
              <p v-if="toolLines(configuration.tools).length === 0" class="text-body-2">
                {{ $t('agentsPage.tools.none') }}
              </p>
              <dl v-else class="summary-list" data-testid="agent-tools">
                <template v-for="line in toolLines(configuration.tools)" :key="line.key">
                  <dt>{{ $t(`agentsPage.tools.${line.key}`) }}</dt>
                  <dd>
                    <span class="monospace">{{ line.value }}</span> ·
                    {{ $t('agentsPage.tools.allowed') }}
                  </dd>
                </template>
              </dl>
              <template v-if="agent.limits?.length">
                <h4 class="text-subtitle-1 font-weight-medium mt-4 mb-1">
                  {{ $t('agentsPage.limits') }}
                </h4>
                <ul class="ml-4">
                  <li v-for="(limit, index) in agent.limits" :key="index" class="text-body-2">
                    {{ limit }}
                  </li>
                </ul>
              </template>
              <h4 class="text-subtitle-1 font-weight-medium mt-4 mb-1">
                {{ $t('agentsPage.outputCriteria') }}
              </h4>
              <p v-if="configuration.output_criteria.length === 0" class="text-body-2">
                {{ $t('agentsPage.noOutputCriteria') }}
              </p>
              <ul v-else class="ml-4">
                <li
                  v-for="check in configuration.output_criteria"
                  :key="check"
                  class="text-body-2 monospace"
                >
                  {{ check }}
                </li>
              </ul>
            </section>
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
          <v-card-title>
            <h2 id="accept-skill-title" class="text-h6">
              {{
                $t('agentsPage.skills.acceptTitle', {
                  skill: catalogSkill(acceptTarget?.skill ?? '')?.name ?? acceptTarget?.skill,
                })
              }}
            </h2>
          </v-card-title>
          <v-card-text>
            <p>
              {{
                $t('agentsPage.skills.acceptCompare', {
                  current: acceptTarget?.version,
                  next: acceptTarget?.available_version,
                })
              }}
            </p>
            <p class="text-body-2 mt-2">{{ $t('agentsPage.skills.acceptRule') }}</p>
          </v-card-text>
          <v-card-actions>
            <v-spacer />
            <v-btn variant="text" :disabled="saving" @click="acceptOpen = false">
              {{ $t('common.cancel') }}
            </v-btn>
            <v-btn color="primary" variant="flat" :loading="saving" @click="confirmAccept">
              {{ $t('agentsPage.skills.accept') }}
            </v-btn>
          </v-card-actions>
        </v-card>
      </v-dialog>
    </template>
  </v-container>
</template>

<style scoped>
.agents-layout {
  display: grid;
  gap: 2rem;
  grid-template-columns: minmax(0, 1fr);
}

@media (min-width: 960px) {
  .agents-layout {
    grid-template-columns: minmax(220px, 1fr) minmax(0, 3fr);
  }
}

.agent-button {
  height: auto !important;
  justify-content: flex-start;
  padding: 0.5rem 0.75rem;
  text-transform: none;
  white-space: normal;
}

.agent-button--selected {
  border-width: 2px;
  font-weight: 600;
}
</style>
