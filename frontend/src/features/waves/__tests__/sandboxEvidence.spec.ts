import { describe, expect, it } from 'vitest'
import type { CallRecord, SandboxExecution } from '../api/wavesApi'
import {
  endedByLimit,
  executionOutcome,
  outputFileName,
  outputIsPartial,
  refusedToolCalls,
} from '../composables/sandboxEvidence'

function execution(overrides: Partial<SandboxExecution> = {}): SandboxExecution {
  return {
    id: '0193a5c0-0000-7000-8000-000000000001',
    command: 'go test ./...',
    category: 'tests',
    start: '2026-11-22T10:00:00-03:00',
    duration_ms: 1200,
    exit_code: 0,
    end_reason: 'completed',
    output_start: 'ok',
    output_bytes: 2,
    ...overrides,
  }
}

describe('sandbox evidence (US-41)', () => {
  it('CA-1: exit code 0 succeeds and any other code fails', () => {
    expect(executionOutcome(execution())).toBe('succeeded')
    expect(executionOutcome(execution({ exit_code: 1 }))).toBe('failed')
    expect(executionOutcome(execution({ exit_code: null }))).toBe('failed')
  })

  it('CA-3: a command stopped at the time limit reads as a limit, whatever its exit code', () => {
    const stopped = execution({ end_reason: 'time_limit', exit_code: 137 })
    expect(endedByLimit(stopped)).toBe(true)
    expect(executionOutcome(stopped)).toBe('limit')
    expect(executionOutcome(execution({ end_reason: 'memory_limit' }))).toBe('limit')
    expect(executionOutcome(execution({ end_reason: 'aborted' }))).toBe('aborted')
    expect(executionOutcome(execution({ end_reason: 'executor_error' }))).toBe('executor_error')
  })

  it('CA-1: the output start is partial when the full output is larger', () => {
    expect(outputIsPartial(execution())).toBe(false)
    expect(outputIsPartial(execution({ output_bytes: 70000 }))).toBe(true)
    expect(outputIsPartial(execution({ output_start: 'çã', output_bytes: 4 }))).toBe(false)
  })

  it('names the downloaded file by wave and position, never by the command', () => {
    expect(outputFileName('DEV-03', 7)).toBe('DEV-03-exec-7.txt')
  })
})

describe('refused tool calls (US-30 CA-1)', () => {
  it('keeps only the tool calls the least-privilege guard refused', () => {
    const calls: CallRecord[] = [
      { number: 1, type: 'model', start: '', duration_ms: 1, result: 'success' },
      {
        number: 2,
        type: 'tool',
        start: '',
        duration_ms: 1,
        result: 'refused',
        tool: 'run_command',
      },
      { number: 3, type: 'tool', start: '', duration_ms: 1, result: 'success', tool: 'read_file' },
    ]
    expect(refusedToolCalls(calls).map((call) => call.number)).toEqual([2])
  })
})
