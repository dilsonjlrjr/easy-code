import { defineStore } from 'pinia'
import { computed, reactive, ref } from 'vue'
import { InstallerError, installerApi } from './api'
import type {
  Database,
  DatabaseProbe,
  DirectoryUser,
  FieldProblem,
  InstallStatus,
  Organization,
  Person,
  Plan,
  SkillSummary,
  SSO,
  SSOProbe,
} from './types'

/** Steps of the wizard, in order (ESP-31 section 3). */
export const STEP_IDS = [
  'welcome',
  'database',
  'organization',
  'keycloak',
  'administrator',
  'user',
  'skills',
  'review',
  'install',
  'done',
] as const
export type StepId = (typeof STEP_IDS)[number]

/** Steps whose test must pass, with the data it tested, before "Continue". */
const TESTED_STEPS = [
  'welcome',
  'database',
  'organization',
  'keycloak',
  'administrator',
  'user',
] as const
type TestedStep = (typeof TESTED_STEPS)[number]

export type PersonMode = 'new' | 'existing'

export interface SkillDraft {
  file_name: string
  content: string
  summary?: SkillSummary
  problem?: string
}

const emptyPerson = (): Person => ({
  existing_id: '',
  username: '',
  email: '',
  first_name: '',
  last_name: '',
  password: '',
})

/** Reads a file as base64 (the JSON form of a Go []byte). */
export async function fileToBase64(file: File): Promise<string> {
  const buffer = new Uint8Array(await file.arrayBuffer())
  let binary = ''
  const chunk = 0x8000
  for (let i = 0; i < buffer.length; i += chunk) {
    binary += String.fromCharCode(...buffer.subarray(i, i + chunk))
  }
  return btoa(binary)
}

export const useInstallerStore = defineStore('installer', () => {
  const token = ref('')
  const tokenAccepted = ref(false)
  const installedAtStart = ref(false)
  const version = ref('')
  const bundled = ref(false)
  const environmentOk = ref<boolean | null>(null)

  const current = ref<StepId>('welcome')
  const busy = ref(false)
  /** Problems of the last test, by field ("database.host"); "" holds a problem of the whole step. */
  const problems = ref<FieldProblem[]>([])
  const generalProblem = ref<InstallerError | null>(null)

  const database = reactive<Database>({
    host: '',
    port: 5432,
    admin_user: '',
    admin_password: '',
    name: 'easycode',
    ssl_mode: 'require',
  })
  const organization = reactive<Organization>({
    name: '',
    currency: 'BRL',
    public_origin: '',
    locale: 'pt-BR',
  })
  const sso = reactive<SSO>({
    public_url: '',
    internal_url: '',
    mode: 'new_realm',
    realm: 'easycode',
    admin_user: '',
    admin_password: '',
  })
  const administrator = reactive<Person>(emptyPerson())
  const administratorMode = ref<PersonMode>('new')
  const administratorChosen = ref<DirectoryUser | null>(null)
  const user = reactive<Person>(emptyPerson())
  const userMode = ref<PersonMode>('new')
  const userChosen = ref<DirectoryUser | null>(null)
  const userSkipped = ref(false)
  const skills = ref<SkillDraft[]>([])
  const nativeSkills = ref<SkillSummary[]>([])

  const databaseProbe = ref<DatabaseProbe | null>(null)
  const ssoProbe = ref<SSOProbe | null>(null)
  const status = ref<InstallStatus | null>(null)

  /** What each tested step looked like when its test passed (a change asks for a new test). */
  const tested = reactive<Partial<Record<TestedStep, string>>>({})

  function personFor(mode: PersonMode, person: Person, chosen: DirectoryUser | null): Person {
    if (mode === 'existing') return { ...emptyPerson(), existing_id: chosen?.id ?? '' }
    return { ...person, existing_id: '' }
  }

  const administratorPlan = computed(() =>
    personFor(administratorMode.value, administrator, administratorChosen.value),
  )
  const userPlan = computed(() => personFor(userMode.value, user, userChosen.value))

  function snapshot(step: TestedStep): string {
    switch (step) {
      case 'welcome':
        return JSON.stringify([token.value, tokenAccepted.value])
      case 'database':
        return JSON.stringify(database)
      case 'organization':
        return JSON.stringify(organization)
      case 'keycloak':
        return JSON.stringify([sso, organization.public_origin])
      case 'administrator':
        return JSON.stringify([sso, administratorPlan.value])
      case 'user':
        return JSON.stringify([sso, userPlan.value, userSkipped.value])
    }
  }

  function isTestedStep(step: StepId): step is TestedStep {
    return (TESTED_STEPS as readonly string[]).includes(step)
  }

  /** The step may go on: its last test passed with the data now on screen. */
  function passed(step: StepId): boolean {
    if (step === 'user' && userSkipped.value) return true
    if (step === 'skills') return skills.value.every((s) => s.summary && !s.problem)
    if (!isTestedStep(step)) return true
    return tested[step] !== undefined && tested[step] === snapshot(step)
  }

  /** The step was tested but its data changed afterwards. */
  function stale(step: StepId): boolean {
    return isTestedStep(step) && tested[step] !== undefined && tested[step] !== snapshot(step)
  }

  const stepIndex = computed(() => STEP_IDS.indexOf(current.value))

  /** Steps before the review the person may open again (they were reached and nothing runs yet). */
  function reachable(step: StepId): boolean {
    if (status.value) return step === current.value
    const target = STEP_IDS.indexOf(step)
    if (target > STEP_IDS.indexOf('review')) return false
    return STEP_IDS.slice(0, target).every((s) => passed(s))
  }

  function goTo(step: StepId): void {
    if (reachable(step)) {
      current.value = step
      clearProblems()
    }
  }

  function next(): void {
    const following = STEP_IDS[stepIndex.value + 1]
    if (following && passed(current.value)) goTo(following)
  }

  function back(): void {
    const previous = STEP_IDS[stepIndex.value - 1]
    if (previous && !status.value) goTo(previous)
  }

  function clearProblems(): void {
    problems.value = []
    generalProblem.value = null
  }

  function fieldProblem(field: string): FieldProblem | undefined {
    return problems.value.find((p) => p.field === field)
  }

  async function guarded(action: () => Promise<void>): Promise<boolean> {
    busy.value = true
    clearProblems()
    try {
      await action()
      return true
    } catch (error) {
      if (error instanceof InstallerError && error.errors.length > 0) {
        problems.value = error.errors
      } else {
        generalProblem.value =
          error instanceof InstallerError ? error : new InstallerError(0, 'failed')
      }
      return false
    } finally {
      busy.value = false
    }
  }

  async function loadState(): Promise<void> {
    await guarded(async () => {
      const state = await installerApi.state()
      installedAtStart.value = state.installed
      version.value = state.version
    })
  }

  async function submitToken(): Promise<boolean> {
    token.value = token.value.trim()
    return guarded(async () => {
      try {
        await installerApi.checkToken(token.value)
      } catch (error) {
        tokenAccepted.value = false
        if (error instanceof InstallerError && error.status === 401) {
          throw new InstallerError(401, 'invalid_token', [
            { field: 'token', reason: 'invalid_token' },
          ])
        }
        throw error
      }
      tokenAccepted.value = true
      const [defaults, environment, native] = await Promise.all([
        installerApi.defaults(token.value),
        installerApi.checkEnvironment(token.value),
        installerApi.nativeSkills(token.value),
      ])
      bundled.value = defaults.bundled
      if (!tested.database) Object.assign(database, defaults.database)
      if (!tested.organization) {
        Object.assign(organization, {
          ...defaults.organization,
          name: defaults.organization.name || organization.name,
        })
      }
      if (!tested.keycloak) Object.assign(sso, defaults.sso)
      environmentOk.value = environment.checks.every((c) => c.ok)
      nativeSkills.value = native.skills ?? []
      if (environmentOk.value) tested.welcome = snapshot('welcome')
    })
  }

  async function testDatabase(): Promise<boolean> {
    databaseProbe.value = null
    return guarded(async () => {
      try {
        databaseProbe.value = await installerApi.checkDatabase(token.value, { ...database })
      } catch (error) {
        if (error instanceof InstallerError)
          databaseProbe.value = (error.found as DatabaseProbe | undefined) ?? null
        throw error
      }
      tested.database = snapshot('database')
    })
  }

  async function testOrganization(): Promise<boolean> {
    return guarded(async () => {
      organization.public_origin = organization.public_origin.trim().replace(/\/+$/, '')
      await installerApi.checkOrganization(token.value, { ...organization })
      tested.organization = snapshot('organization')
    })
  }

  async function testKeycloak(): Promise<boolean> {
    ssoProbe.value = null
    return guarded(async () => {
      try {
        ssoProbe.value = await installerApi.checkSSO(token.value, { ...sso }, { ...organization })
      } catch (error) {
        if (error instanceof InstallerError)
          ssoProbe.value = (error.found as SSOProbe | undefined) ?? null
        throw error
      }
      if (sso.mode === 'new_realm') {
        administratorMode.value = 'new'
        userMode.value = 'new'
      }
      tested.keycloak = snapshot('keycloak')
    })
  }

  async function testPerson(field: 'administrator' | 'user'): Promise<boolean> {
    return guarded(async () => {
      const person = field === 'administrator' ? administratorPlan.value : userPlan.value
      await installerApi.checkPerson(token.value, { ...sso }, person, field)
      tested[field] = snapshot(field)
    })
  }

  async function searchUsers(query: string): Promise<DirectoryUser[]> {
    let found: DirectoryUser[] = []
    await guarded(async () => {
      found = (await installerApi.searchUsers(token.value, { ...sso }, query)).users ?? []
    })
    return found
  }

  function skipUser(): void {
    userSkipped.value = true
    next()
  }

  async function addSkillFiles(files: File[]): Promise<void> {
    for (const file of files) {
      const draft: SkillDraft = { file_name: file.name, content: await fileToBase64(file) }
      skills.value.push(draft)
      const index = skills.value.length - 1
      try {
        const summary = await installerApi.checkSkill(token.value, {
          file_name: draft.file_name,
          content: draft.content,
        })
        skills.value[index] = { ...draft, summary }
      } catch (error) {
        const reason =
          error instanceof InstallerError
            ? ((error.errors as Array<FieldProblem | undefined>)[0]?.reason ?? error.code)
            : 'failed'
        skills.value[index] = { ...draft, problem: reason }
      }
    }
  }

  function removeSkill(index: number): void {
    skills.value.splice(index, 1)
  }

  const plan = computed<Plan>(() => ({
    database: { ...database },
    organization: { ...organization },
    sso: { ...sso },
    administrator: administratorPlan.value,
    user: userSkipped.value ? null : userPlan.value,
    skills: skills.value
      .filter((s) => s.summary && !s.problem)
      .map((s) => ({ file_name: s.file_name, content: s.content })),
  }))

  let poller: ReturnType<typeof setTimeout> | undefined

  async function poll(): Promise<void> {
    try {
      status.value = await installerApi.status(token.value)
    } catch {
      // A missed answer is retried on the next tick.
    }
    if (status.value?.installed) {
      current.value = 'done'
      return
    }
    if (status.value?.running !== false) {
      poller = setTimeout(() => void poll(), 1000)
    }
  }

  async function install(): Promise<boolean> {
    const ok = await guarded(async () => {
      status.value = await installerApi.install(token.value, plan.value)
      current.value = 'install'
    })
    if (ok) await poll()
    return ok
  }

  async function retry(): Promise<boolean> {
    const ok = await guarded(async () => {
      status.value = await installerApi.retry(token.value)
    })
    if (ok) {
      status.value = status.value ? { ...status.value, running: true } : status.value
      await poll()
    }
    return ok
  }

  async function finish(): Promise<void> {
    await installerApi.finish(token.value).catch(() => undefined)
  }

  function stopPolling(): void {
    if (poller) clearTimeout(poller)
  }

  return {
    token,
    tokenAccepted,
    installedAtStart,
    version,
    bundled,
    environmentOk,
    current,
    busy,
    problems,
    generalProblem,
    database,
    organization,
    sso,
    administrator,
    administratorMode,
    administratorChosen,
    user,
    userMode,
    userChosen,
    userSkipped,
    skills,
    nativeSkills,
    databaseProbe,
    ssoProbe,
    status,
    stepIndex,
    plan,
    passed,
    stale,
    reachable,
    goTo,
    next,
    back,
    fieldProblem,
    clearProblems,
    loadState,
    submitToken,
    testDatabase,
    testOrganization,
    testKeycloak,
    testPerson,
    searchUsers,
    skipUser,
    addSkillFiles,
    removeSkill,
    install,
    retry,
    finish,
    stopPolling,
  }
})
