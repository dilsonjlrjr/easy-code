import { describe, expect, it, vi } from 'vitest'
import {
  canUnblock,
  createSkillLibraryApi,
  followImport,
  importInProgress,
  isHttpsUrl,
  type SkillImport,
} from '../api/skillLibraryApi'
import type { HttpClient } from '@/shared/api/httpClient'

function anImport(overrides: Partial<SkillImport> = {}): SkillImport {
  return { id: 'imp-1', state: 'received', received_at: '2026-11-23T10:00:00Z', ...overrides }
}

describe('importInProgress (ESP-23 3.3)', () => {
  it('is in progress while received or while the version is imported or scanning', () => {
    expect(importInProgress(anImport())).toBe(true)
    expect(
      importInProgress(anImport({ state: 'version_created', version_status: 'scanning' })),
    ).toBe(true)
    expect(
      importInProgress(anImport({ state: 'version_created', version_status: 'unblocked' })),
    ).toBe(false)
    expect(importInProgress(anImport({ state: 'refused' }))).toBe(false)
  })
})

describe('followImport (US-44 CA-1)', () => {
  it('polls until the version settles', async () => {
    const read = vi
      .fn()
      .mockResolvedValueOnce(anImport({ state: 'version_created', version_status: 'scanning' }))
      .mockResolvedValueOnce(
        anImport({ state: 'version_created', version_status: 'unblocked', version: 1 }),
      )
    const result = await followImport(anImport(), read, { sleep: () => Promise.resolve() })
    expect(result.version_status).toBe('unblocked')
    expect(read).toHaveBeenCalledTimes(2)
  })

  it('stops at the timeout with the last state', async () => {
    const read = vi.fn().mockResolvedValue(anImport())
    const result = await followImport(anImport(), read, {
      intervalMs: 10,
      timeoutMs: 30,
      sleep: () => Promise.resolve(),
    })
    expect(result.state).toBe('received')
    expect(read).toHaveBeenCalledTimes(3)
  })
})

describe('canUnblock (ER-23-10, US-44 CA-4)', () => {
  it('releases only a version blocked for suspicious content', () => {
    expect(canUnblock(['malicious_content'])).toBe(true)
    expect(canUnblock(['format'])).toBe(false)
    expect(canUnblock(['malicious_content', 'size'])).toBe(false)
    expect(canUnblock(undefined)).toBe(false)
  })
})

describe('isHttpsUrl', () => {
  it('accepts only https addresses', () => {
    expect(isHttpsUrl('https://git.empresa.local/skills.git')).toBe(true)
    expect(isHttpsUrl('http://git.empresa.local/skills.git')).toBe(false)
    expect(isHttpsUrl('git@git.empresa.local:skills.git')).toBe(false)
  })
})

describe('createSkillLibraryApi', () => {
  it('sends the zip as multipart `package` with an Idempotency-Key', async () => {
    const post = vi.fn().mockResolvedValue(anImport())
    const api = createSkillLibraryApi({ post } as unknown as HttpClient)
    await api.importPackage(new File(['zip'], 'skill.zip'), 'key-1')
    const [path, body, options] = post.mock.calls[0] as [string, FormData, { headers: object }]
    expect(path).toBe('/skills/imports')
    expect(body.get('package')).toBeInstanceOf(File)
    expect(options.headers).toEqual({ 'Idempotency-Key': 'key-1' })
  })

  it('sends only the git fields that were given', async () => {
    const post = vi.fn().mockResolvedValue(anImport())
    const api = createSkillLibraryApi({ post } as unknown as HttpClient)
    await api.importGit({ git_url: 'https://git.empresa.local/s.git', reference: '' }, 'k')
    expect(post.mock.calls[0]?.[1]).toEqual({ git_url: 'https://git.empresa.local/s.git' })
  })
})
