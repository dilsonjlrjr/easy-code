<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ProjectRole, UserRef } from '@/shared/api/contract'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import { useSessionStore } from '@/shared/session/sessionStore'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { useCurrentProjectStore } from '@/features/projects/stores/currentProjectStore'
import { approverGraceUntil, membersApi, sortedRoles, type Member } from '../api/membersApi'

/** ESP-26: people of the project and their roles (US-02). */
const ASSIGNABLE_ROLES: ProjectRole[] = ['approver', 'member']
const MIN_SEARCH = 2

const { t } = useI18n()
const format = useFormat()
const store = useCurrentProjectStore()
const session = useSessionStore()
const { message, announce } = useAnnouncer()

const members = ref<Member[]>([])
const failure = ref<unknown>()
const busy = ref(false)

const projectId = computed(() => store.project?.id ?? '')
const isLead = computed(() => store.project?.my_roles.includes('lead') ?? false)
/** ESP-26 section 4: the lead assigns roles; the administrator only transfers the lead. */
const canTransfer = computed(() => session.isAdministrator)
const memberIds = computed(() => new Set(members.value.map((member) => member.user.id)))

const loader = useLoader(async () => {
  members.value = (await membersApi.listMembers(projectId.value)).items
})

onMounted(() => {
  void loader.run()
})

function rolesText(member: Member): string {
  return sortedRoles(member.roles)
    .map((role) => t(`members.roles.${role}`))
    .join(', ')
}

function isCurrentLead(member: Member): boolean {
  return member.roles.includes('lead')
}

async function run(action: () => Promise<unknown>, done: string): Promise<boolean> {
  busy.value = true
  failure.value = undefined
  try {
    await action()
    announce(done)
    return true
  } catch (error) {
    failure.value = error
    return false
  } finally {
    busy.value = false
    // ESP-26 section 6: after a refusal the list is read again.
    await loader.run()
  }
}

// ------------------------------------------------------------------ change roles

const rolesDialog = ref(false)
const rolesTarget = ref<UserRef>()
const rolesForm = ref<ProjectRole[]>([])
let returnFocus: HTMLElement | undefined

function rememberFocus(): void {
  returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : undefined
}

function restoreFocus(): void {
  void nextTick(() => returnFocus?.focus())
}

function openRoles(member: Member): void {
  rememberFocus()
  rolesTarget.value = member.user
  rolesForm.value = member.roles.filter((role) => role !== 'lead')
  rolesDialog.value = true
}

async function saveRoles(): Promise<void> {
  const target = rolesTarget.value
  if (!target || rolesForm.value.length === 0) return
  const current = members.value.find((member) => member.user.id === target.id)
  // The lead keeps `lead`: only the administrator moves it (US-02 RN-3).
  const roles = current?.roles.includes('lead')
    ? ['lead' as const, ...rolesForm.value]
    : rolesForm.value
  const saved = await run(
    () => membersApi.setMemberRoles(projectId.value, target.id, roles),
    t('members.rolesSaved', { name: target.name }),
  )
  if (saved) rolesDialog.value = false
}

// ------------------------------------------------------------------ add person

const addDialog = ref(false)
const search = ref('')
const results = ref<UserRef[]>([])
const searching = ref(false)
const searched = ref(false)
const chosen = ref<UserRef>()
const addRoles = ref<ProjectRole[]>(['member'])
let searchTimer: ReturnType<typeof setTimeout> | undefined

function openAdd(): void {
  rememberFocus()
  search.value = ''
  results.value = []
  searched.value = false
  chosen.value = undefined
  addRoles.value = ['member']
  addDialog.value = true
}

watch(search, (text) => {
  if (searchTimer !== undefined) clearTimeout(searchTimer)
  if (text.trim().length < MIN_SEARCH) {
    results.value = []
    searched.value = false
    return
  }
  searchTimer = setTimeout(() => {
    void runSearch(text.trim())
  }, 250)
})

async function runSearch(text: string): Promise<void> {
  searching.value = true
  try {
    results.value = (await membersApi.searchUsers(text)).items
    searched.value = true
  } catch (error) {
    failure.value = error
  } finally {
    searching.value = false
  }
}

async function addPerson(): Promise<void> {
  const person = chosen.value
  if (!person || addRoles.value.length === 0) return
  const saved = await run(
    () => membersApi.setMemberRoles(projectId.value, person.id, addRoles.value),
    t('members.added', { name: person.name }),
  )
  if (saved) addDialog.value = false
}

// ------------------------------------------------------------------ remove

const removeDialog = ref(false)
const removeTarget = ref<UserRef>()

function openRemove(member: Member): void {
  rememberFocus()
  removeTarget.value = member.user
  removeDialog.value = true
}

async function confirmRemove(): Promise<void> {
  const target = removeTarget.value
  if (!target) return
  await run(
    () => membersApi.removeMember(projectId.value, target.id),
    t('members.removed', { name: target.name }),
  )
  removeDialog.value = false
}

// ------------------------------------------------------------------ transfer lead

const transferDialog = ref(false)
const transferTarget = ref<Member>()
const currentLead = computed(() => members.value.find(isCurrentLead)?.user)

function openTransfer(member: Member): void {
  rememberFocus()
  transferTarget.value = member
  transferDialog.value = true
}

async function confirmTransfer(): Promise<void> {
  const target = transferTarget.value
  if (!target) return
  const roles: ProjectRole[] = ['lead', ...target.roles.filter((role) => role !== 'lead')]
  await run(
    () => membersApi.setMemberRoles(projectId.value, target.user.id, roles),
    t('members.transferred', { name: target.user.name }),
  )
  transferDialog.value = false
}

watch([rolesDialog, addDialog, removeDialog, transferDialog], (open, wasOpen) => {
  if (wasOpen.some(Boolean) && !open.some(Boolean)) restoreFocus()
})
</script>

<template>
  <section aria-labelledby="members-heading">
    <div class="d-flex flex-wrap align-center ga-2 mb-2">
      <h2 id="members-heading" class="text-h6">{{ $t('members.title') }}</h2>
      <v-spacer />
      <v-btn v-if="isLead" color="primary" variant="flat" @click="openAdd">
        {{ $t('members.add') }}
      </v-btn>
    </div>
    <p class="text-body-2 text-medium-emphasis mb-4">{{ $t('members.help') }}</p>
    <div role="status" class="sr-only">{{ message }}</div>
    <ProblemAlert v-if="failure" :error="failure" class="mb-4" data-testid="members-failure" />

    <LoadingState v-if="loader.loading.value && members.length === 0" />
    <ProblemAlert
      v-else-if="loader.error.value && members.length === 0"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <v-table v-else>
      <caption class="sr-only">
        {{
          $t('members.caption')
        }}
      </caption>
      <thead>
        <tr>
          <th scope="col">{{ $t('members.person') }}</th>
          <th scope="col">{{ $t('members.rolesColumn') }}</th>
          <th scope="col">{{ $t('members.assigned') }}</th>
          <th v-if="isLead || canTransfer" scope="col">{{ $t('common.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="member in members"
          :key="member.user.id"
          :data-testid="`member-${member.user.id}`"
        >
          <th scope="row" class="font-weight-regular">{{ member.user.name }}</th>
          <td>
            {{ rolesText(member) }}
            <!-- CA-26-8: a self-assigned approver does not decide gates for 24 h. -->
            <p v-if="approverGraceUntil(member)" class="text-body-2 text-medium-emphasis">
              {{
                $t('members.grace', {
                  when: format.dateTime(approverGraceUntil(member)?.toISOString()),
                })
              }}
            </p>
          </td>
          <td>
            <template v-if="member.assigned_by && member.assigned_at">
              {{
                $t('members.assignedBy', {
                  who: member.assigned_by.name,
                  when: format.dateTime(member.assigned_at),
                })
              }}
            </template>
          </td>
          <td v-if="isLead || canTransfer">
            <div class="d-flex flex-wrap ga-1">
              <v-btn
                v-if="isLead"
                size="small"
                variant="text"
                :aria-label="$t('members.changeRolesOf', { name: member.user.name })"
                @click="openRoles(member)"
              >
                {{ $t('members.changeRoles') }}
              </v-btn>
              <v-btn
                v-if="isLead"
                size="small"
                variant="text"
                color="error"
                :aria-label="$t('members.removeNamed', { name: member.user.name })"
                @click="openRemove(member)"
              >
                {{ $t('common.remove') }}
              </v-btn>
              <v-btn
                v-if="canTransfer && !isCurrentLead(member) && member.user.id !== session.user?.id"
                size="small"
                variant="text"
                :aria-label="$t('members.transferTo', { name: member.user.name })"
                @click="openTransfer(member)"
              >
                {{ $t('members.transfer') }}
              </v-btn>
            </div>
          </td>
        </tr>
      </tbody>
    </v-table>

    <!-- Change roles: one checkbox per role, never only an icon or color (ESP-26 section 8). -->
    <v-dialog v-model="rolesDialog" max-width="480">
      <v-card>
        <v-card-title>
          <h2 class="text-h6">
            {{ $t('members.changeRolesOf', { name: rolesTarget?.name ?? '' }) }}
          </h2>
        </v-card-title>
        <v-form @submit.prevent="saveRoles">
          <v-card-text>
            <fieldset class="roles-fieldset">
              <legend class="text-body-2 mb-1">{{ $t('members.rolesColumn') }}</legend>
              <v-checkbox
                v-for="role in ASSIGNABLE_ROLES"
                :key="role"
                v-model="rolesForm"
                :value="role"
                :label="$t(`members.roles.${role}`)"
                density="compact"
                hide-details
              />
            </fieldset>
            <p v-if="rolesForm.length === 0" class="text-body-2 text-error mt-2" role="alert">
              {{ $t('members.oneRoleRequired') }}
            </p>
          </v-card-text>
          <v-card-actions>
            <v-spacer />
            <v-btn variant="text" @click="rolesDialog = false">{{ $t('common.cancel') }}</v-btn>
            <v-btn
              type="submit"
              color="primary"
              variant="flat"
              :loading="busy"
              :disabled="rolesForm.length === 0"
            >
              {{ $t('common.save') }}
            </v-btn>
          </v-card-actions>
        </v-form>
      </v-card>
    </v-dialog>

    <v-dialog v-model="addDialog" max-width="560">
      <v-card>
        <v-card-title>
          <h2 class="text-h6">{{ $t('members.add') }}</h2>
        </v-card-title>
        <v-form @submit.prevent="addPerson">
          <v-card-text>
            <v-text-field
              v-model="search"
              :label="$t('members.search')"
              :hint="$t('members.searchHint')"
              persistent-hint
              :loading="searching"
              autocomplete="off"
            />
            <p v-if="searched && results.length === 0" class="text-body-2 mt-2" role="status">
              {{ $t('members.noResults') }}
            </p>
            <fieldset v-else-if="results.length > 0" class="roles-fieldset mt-2">
              <legend class="text-body-2 mb-1">{{ $t('members.person') }}</legend>
              <v-radio-group v-model="chosen" hide-details>
                <v-radio
                  v-for="person in results"
                  :key="person.id"
                  :value="person"
                  :disabled="memberIds.has(person.id)"
                  :label="
                    memberIds.has(person.id)
                      ? $t('members.alreadyMember', { name: person.name })
                      : person.name
                  "
                />
              </v-radio-group>
            </fieldset>
            <fieldset class="roles-fieldset mt-2">
              <legend class="text-body-2 mb-1">{{ $t('members.rolesColumn') }}</legend>
              <v-checkbox
                v-for="role in ASSIGNABLE_ROLES"
                :key="role"
                v-model="addRoles"
                :value="role"
                :label="$t(`members.roles.${role}`)"
                density="compact"
                hide-details
              />
            </fieldset>
          </v-card-text>
          <v-card-actions>
            <v-spacer />
            <v-btn variant="text" @click="addDialog = false">{{ $t('common.cancel') }}</v-btn>
            <v-btn
              type="submit"
              color="primary"
              variant="flat"
              :loading="busy"
              :disabled="!chosen || addRoles.length === 0"
            >
              {{ $t('members.addConfirm') }}
            </v-btn>
          </v-card-actions>
        </v-form>
      </v-card>
    </v-dialog>

    <!-- ESP-26 section 8: confirmation with the initial focus on "cancel". -->
    <v-dialog v-model="removeDialog" max-width="480">
      <v-card>
        <v-card-title>
          <h2 class="text-h6">
            {{ $t('members.removeTitle', { name: removeTarget?.name ?? '' }) }}
          </h2>
        </v-card-title>
        <v-card-text>{{ $t('members.removeWarning') }}</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="removeDialog = false">
            {{ $t('common.cancel') }}
          </v-btn>
          <v-btn color="error" variant="flat" :loading="busy" @click="confirmRemove">
            {{ $t('common.remove') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="transferDialog" max-width="520">
      <v-card>
        <v-card-title>
          <h2 class="text-h6">{{ $t('members.transferTitle') }}</h2>
        </v-card-title>
        <v-card-text>
          {{
            $t('members.transferWarning', {
              current: currentLead?.name ?? '',
              next: transferTarget?.user.name ?? '',
            })
          }}
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="transferDialog = false">
            {{ $t('common.cancel') }}
          </v-btn>
          <v-btn color="primary" variant="flat" :loading="busy" @click="confirmTransfer">
            {{ $t('members.transfer') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </section>
</template>

<style scoped>
.roles-fieldset {
  border: 0;
  margin: 0;
  padding: 0;
}
</style>
