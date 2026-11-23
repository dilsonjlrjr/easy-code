import { describe, expect, it } from 'vitest'
import type { AgentConfiguration } from '../api/agentsApi'
import {
  configurationChanges,
  formFromConfiguration,
  nextConfigurationFromForm,
  toolOptions,
  toolsFromKeys,
} from '../composables/agentConfigurationForm'

const current: AgentConfiguration = {
  version: 3,
  instructions_version: 'v1.0+p3',
  adjustment_instructions: 'Use o padrão de nomes.',
  tools: {
    read_file: ['docs/**'],
    write_file: ['docs/dados/**'],
    execute_command: { allowed: true, categories: ['validate_ddl'] },
  },
  skills: [{ skill: 'sql-review', version: 1, available_version: 2 }],
  output_criteria: ['schema.valid'],
  created_at: '2026-11-20T10:00:00-03:00',
}

const allowedByRole = {
  read_file: ['docs/**', 'specs/**'],
  write_file: ['docs/dados/**'],
  execute_command: { allowed: true, categories: ['validate_ddl', 'run_tests'] },
  browser: true,
}

describe('toolOptions (ESP-22 3.4, US-25 CA-3)', () => {
  it('offers what the role allows and marks what lies outside it', () => {
    const options = toolOptions({ ...current.tools, git: ['push'] }, allowedByRole)
    expect(options.filter((option) => option.allowed).map((option) => option.key)).toEqual([
      'read_file:docs/**',
      'read_file:specs/**',
      'write_file:docs/dados/**',
      'execute_command:validate_ddl',
      'execute_command:run_tests',
      'browser',
    ])
    expect(options.find((option) => option.key === 'git:push')).toEqual({
      key: 'git:push',
      tool: 'git',
      value: 'push',
      allowed: false,
    })
  })
})

describe('toolsFromKeys', () => {
  it('rebuilds the contract structure, with categories and the browser', () => {
    expect(toolsFromKeys(formFromConfiguration(current).tools)).toEqual(current.tools)
    expect(toolsFromKeys(['execute_command', 'browser', 'scanners:gitleaks'])).toEqual({
      execute_command: { allowed: true },
      browser: true,
      scanners: ['gitleaks'],
    })
    expect(toolsFromKeys([])).toEqual({})
  })
})

describe('nextConfigurationFromForm (US-25 CA-2)', () => {
  it('keeps the pinned skills and sends the trimmed adjustment', () => {
    const next = nextConfigurationFromForm(current, {
      adjustment: '  Nova regra.  ',
      tools: ['read_file:docs/**'],
    })
    expect(next).toEqual({
      adjustment_instructions: 'Nova regra.',
      tools: { read_file: ['docs/**'] },
      skills: [{ skill: 'sql-review', version: 1 }],
    })
    // A cleared adjustment is sent as "" so the api removes it (an absent field would keep it).
    expect(
      nextConfigurationFromForm(current, { adjustment: ' ', tools: [] }).adjustment_instructions,
    ).toBe('')
  })
})

describe('configurationChanges (ESP-22 4: summary before saving)', () => {
  it('lists the instructions change and each tool added or removed', () => {
    const options = toolOptions(current.tools, allowedByRole)
    const form = formFromConfiguration(current)
    expect(configurationChanges(current, form, options)).toEqual([])
    const changes = configurationChanges(
      current,
      {
        adjustment: 'Outra regra.',
        tools: [...form.tools.filter((key) => key !== 'write_file:docs/dados/**'), 'browser'],
      },
      options,
    )
    expect(changes.map((change) => [change.kind, change.option?.key])).toEqual([
      ['instructions', undefined],
      ['tool_added', 'browser'],
      ['tool_removed', 'write_file:docs/dados/**'],
    ])
  })
})
