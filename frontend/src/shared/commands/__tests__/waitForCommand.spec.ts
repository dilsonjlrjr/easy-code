import { describe, expect, it, vi } from 'vitest'
import type { Command } from '@/shared/api/contract'
import { waitForCommand } from '../waitForCommand'

function command(state: Command['state']): Command {
  return {
    id: 'c1',
    type: 'start',
    target: { type: 'project', id: 'p1' },
    state,
    requested_by: { id: 'dev', name: 'Dev' },
    origin: 'web',
    requested_at: '2026-11-22T10:00:00Z',
  }
}

describe('waitForCommand (ER-00-10)', () => {
  it('follows a pending command until it is applied', async () => {
    const read = vi
      .fn<(id: string) => Promise<Command>>()
      .mockResolvedValueOnce(command('pending'))
      .mockResolvedValueOnce(command('applied'))

    const result = await waitForCommand(command('pending'), read, {
      sleep: () => Promise.resolve(),
    })

    expect(result.state).toBe('applied')
    expect(read).toHaveBeenCalledTimes(2)
  })

  it('stops after the timeout and keeps the last known state', async () => {
    const read = vi.fn<(id: string) => Promise<Command>>().mockResolvedValue(command('pending'))
    const result = await waitForCommand(command('pending'), read, {
      intervalMs: 10,
      timeoutMs: 30,
      sleep: () => Promise.resolve(),
    })
    expect(result.state).toBe('pending')
    expect(read).toHaveBeenCalledTimes(3)
  })
})
