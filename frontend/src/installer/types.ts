/** Shapes of the easycode-installer API (backend internal/application/install). */
export type SSOMode = 'new_realm' | 'existing_realm'
export type SSLMode = 'disable' | 'require' | 'verify-full'

export interface Database {
  host: string
  port: number
  admin_user: string
  admin_password: string
  name: string
  ssl_mode: SSLMode
}

export interface Organization {
  name: string
  currency: string
  public_origin: string
  locale: 'pt-BR' | 'en'
}

export interface SSO {
  public_url: string
  internal_url: string
  mode: SSOMode
  realm: string
  admin_user: string
  admin_password: string
}

export interface Person {
  existing_id: string
  username: string
  email: string
  first_name: string
  last_name: string
  password: string
}

export interface SkillPackage {
  file_name: string
  /** Base64 of the zip (Go []byte). */
  content: string
}

export interface Plan {
  database: Database
  organization: Organization
  sso: SSO
  administrator: Person
  user: Person | null
  skills: SkillPackage[]
}

export interface Defaults {
  database: Database
  sso: SSO
  organization: Organization
  bundled: boolean
}

export interface DatabaseProbe {
  server_version: string
  can_create: boolean
  exists: boolean
  has_platform_tables: boolean
  other_platform_database?: string
}

export interface DirectoryUser {
  id: string
  username: string
  email: string
  name: string
  enabled: boolean
}

export interface SSOProbe {
  realm_exists: boolean
  actions: string[] | null
  administrators: DirectoryUser[] | null
}

export interface SkillSummary {
  name: string
  description: string
}

export type StepStatus = 'pending' | 'running' | 'done' | 'failed' | 'skipped'

export interface StepState {
  name: string
  status: StepStatus
  reason?: string
}

export interface SkillResult {
  file_name: string
  name?: string
  status: 'received' | 'refused'
  reason?: string
}

export interface InstallStatus {
  installed: boolean
  running: boolean
  steps: StepState[] | null
  skills: SkillResult[] | null
  administrator?: DirectoryUser
  public_origin?: string
}

export interface FieldProblem {
  field: string
  reason: string
}
