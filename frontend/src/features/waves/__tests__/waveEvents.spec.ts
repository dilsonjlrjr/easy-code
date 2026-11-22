import { describe, expect, it } from 'vitest'
import type { CloudEvent } from '@/shared/events/eventStream'
import {
  belongsToWave,
  checkpointTotals,
  lastResumeNotice,
  mergeEvents,
  waveStateFrom,
} from '../composables/waveEvents'

function event(sequence: number, type: string, data: Record<string, unknown> = {}): CloudEvent {
  return {
    specversion: '1.0',
    id: `e-${String(sequence)}`,
    type: `br.easycode.${type}.v1`,
    source: '/projects/p1',
    subject: 'waves/REQ-01',
    time: '2026-11-22T10:00:00Z',
    sequence,
    waveid: 'REQ-01',
    data: { wave: 'REQ-01', ...data },
  }
}

describe('wave events', () => {
  it('US-09 CA-3: merges history and live events in sequence order without duplicates', () => {
    const merged = mergeEvents(
      [event(1, 'wave.created'), event(3, 'wave.started')],
      [event(2, 'wave.started'), event(3, 'wave.started')],
    )
    expect(merged.map((item) => item.sequence)).toEqual([1, 2, 3])
  })

  it('US-09 CA-2: reads the new wave state from the transition', () => {
    const started = event(2, 'wave.started', {
      transition: { machine: 'wave', from_state: 'queued', to_state: 'running' },
    })
    expect(waveStateFrom(started)).toBe('running')
    expect(
      waveStateFrom(
        event(3, 'stage.started', { transition: { machine: 'stage', to_state: 'running' } }),
      ),
    ).toBeUndefined()
  })

  it('US-36 CA-2: takes the running cost from each checkpoint', () => {
    const checkpoint = event(4, 'wave.checkpoint_written', {
      checkpoint: 5,
      after: 'model_call',
      accumulated_tokens: { input: 10, output: 2, read_cache: 0, write_cache: 0 },
      accumulated_cost: { value: '0.0123', currency: 'BRL' },
    })
    expect(checkpointTotals(checkpoint)).toEqual({
      checkpoint: 5,
      tokens: { input: 10, output: 2, read_cache: 0, write_cache: 0 },
      cost: { value: '0.0123', currency: 'BRL' },
    })
  })

  it('US-11 CA-1/CA-2: keeps the last resumption notice with the lost cost', () => {
    const history = [
      event(5, 'wave.checkpoint_written'),
      event(6, 'wave.resumed_after_crash', {
        checkpoint: 5,
        lease_epoch: 2,
        redone_call: true,
        lost_cost: { value: '0.01', currency: 'BRL' },
        estimated_lost_cost: true,
      }),
    ]
    expect(lastResumeNotice(history)).toEqual({
      checkpoint: 5,
      lostCost: { value: '0.01', currency: 'BRL' },
      estimated: true,
    })
  })

  it('filters events of other waves', () => {
    expect(belongsToWave(event(1, 'wave.started'), 'REQ-01')).toBe(true)
    expect(
      belongsToWave(
        { ...event(1, 'wave.started'), waveid: 'REQ-02', data: { wave: 'REQ-02' } },
        'REQ-01',
      ),
    ).toBe(false)
  })
})
