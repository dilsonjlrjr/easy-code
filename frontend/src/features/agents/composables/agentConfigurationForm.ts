import type { AgentConfiguration, AgentTools, NewAgentConfiguration } from '../api/agentsApi'

/** Tools whose entries are lists in `AgentTools` (paths, git operations, scanners). */
const LIST_TOOLS = ['read_file', 'write_file', 'git', 'scanners'] as const
type ListTool = (typeof LIST_TOOLS)[number]

/**
 * One checkbox of the tools editor (ESP-22 3.4): a path, a command category, a git operation, a
 * scanner or the browser. `key` is unique; `value` is the entry shown in monospace.
 */
export interface ToolOption {
  key: string
  tool: ListTool | 'execute_command' | 'browser'
  value?: string
  /** Inside what the agent role allows; anything else can only be unchecked (US-25 CA-3). */
  allowed: boolean
}

export interface AgentConfigurationForm {
  adjustment: string
  tools: string[]
}

/** The `execute_command` entry without categories (the role allows commands, none listed). */
const ANY_COMMAND = 'execute_command'

function keysOf(tools: AgentTools | undefined): string[] {
  if (!tools) return []
  const keys: string[] = []
  for (const tool of LIST_TOOLS)
    for (const value of tools[tool] ?? []) keys.push(`${tool}:${value}`)
  if (tools.execute_command?.allowed) {
    const categories = tools.execute_command.categories ?? []
    if (categories.length === 0) keys.push(ANY_COMMAND)
    for (const category of categories) keys.push(`execute_command:${category}`)
  }
  if (tools.browser) keys.push('browser')
  return keys
}

function optionOf(key: string, allowed: boolean): ToolOption {
  if (key === 'browser') return { key, tool: 'browser', allowed }
  if (key === ANY_COMMAND) return { key, tool: 'execute_command', allowed }
  const separator = key.indexOf(':')
  return {
    key,
    tool: key.slice(0, separator) as ToolOption['tool'],
    value: key.slice(separator + 1),
    allowed,
  }
}

/**
 * The checkboxes: what the role allows (`allowed_by_role`), plus what the current version has
 * outside it, so it shows and can be removed. Without `allowed_by_role` only the current ones.
 */
export function toolOptions(
  current: AgentTools | undefined,
  allowedByRole: AgentTools | undefined,
): ToolOption[] {
  const allowed = new Set(keysOf(allowedByRole ?? current))
  const keys = [...new Set([...keysOf(allowedByRole ?? current), ...keysOf(current)])]
  return keys.map((key) => optionOf(key, allowed.has(key)))
}

export function formFromConfiguration(configuration: AgentConfiguration): AgentConfigurationForm {
  return {
    adjustment: configuration.adjustment_instructions ?? '',
    tools: keysOf(configuration.tools),
  }
}

/** The checked keys back to the contract structure (`AgentTools`). */
export function toolsFromKeys(keys: string[]): AgentTools {
  const tools: AgentTools = {}
  for (const tool of LIST_TOOLS) {
    const values = keys
      .filter((key) => key.startsWith(`${tool}:`))
      .map((key) => key.slice(tool.length + 1))
    if (values.length) tools[tool] = values
  }
  const categories = keys
    .filter((key) => key.startsWith('execute_command:'))
    .map((key) => key.slice('execute_command:'.length))
  if (categories.length || keys.includes(ANY_COMMAND)) {
    tools.execute_command = { allowed: true, ...(categories.length ? { categories } : {}) }
  }
  if (keys.includes('browser')) tools.browser = true
  return tools
}

/** The next version: instructions adjustment and tools from the form, the pinned skills kept. */
export function nextConfigurationFromForm(
  current: AgentConfiguration,
  form: AgentConfigurationForm,
): NewAgentConfiguration {
  const adjustment = form.adjustment.trim()
  return {
    ...(adjustment ? { adjustment_instructions: adjustment } : {}),
    tools: toolsFromKeys(form.tools),
    skills: current.skills.map(({ skill, version }) => ({ skill, version })),
  }
}

export interface ConfigurationChange {
  kind: 'instructions' | 'tool_added' | 'tool_removed'
  option?: ToolOption
}

/** ESP-22 4 "Salvar configuração": the summary of the changes shown before saving. */
export function configurationChanges(
  current: AgentConfiguration,
  form: AgentConfigurationForm,
  options: ToolOption[],
): ConfigurationChange[] {
  const changes: ConfigurationChange[] = []
  if ((current.adjustment_instructions ?? '').trim() !== form.adjustment.trim()) {
    changes.push({ kind: 'instructions' })
  }
  const before = new Set(keysOf(current.tools))
  const after = new Set(form.tools)
  const find = (key: string) => options.find((option) => option.key === key) ?? optionOf(key, true)
  for (const key of form.tools)
    if (!before.has(key)) changes.push({ kind: 'tool_added', option: find(key) })
  for (const key of before)
    if (!after.has(key)) changes.push({ kind: 'tool_removed', option: find(key) })
  return changes
}
