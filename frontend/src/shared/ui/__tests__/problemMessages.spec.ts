import { describe, expect, it } from 'vitest'
import { ApiError } from '@/shared/api/problemDetails'
import { problemMessageKey } from '../problemMessages'

describe('problemMessageKey (ER-00-12: the message comes from the problem type, never the text)', () => {
  it('maps known problem types to their own message', () => {
    const error = new ApiError(409, {
      type: 'urn:easycode:problem:incomplete-configuration',
      title: 'texto técnico qualquer',
    })
    expect(problemMessageKey(error)).toBe('problems.incomplete-configuration')
  })

  it('falls back by status, then to the generic message, and to network for non-API errors', () => {
    expect(problemMessageKey(new ApiError(404, undefined))).toBe('problems.not-found')
    expect(problemMessageKey(new ApiError(502, undefined))).toBe('problems.internal')
    expect(problemMessageKey(new ApiError(418, { type: 'urn:easycode:problem:teapot' }))).toBe(
      'problems.generic',
    )
    expect(problemMessageKey(new TypeError('Failed to fetch'))).toBe('problems.network')
  })
})
