import { describe, expect, it } from 'vitest'
import { resolveRuntimeConfig } from '../runtimeConfig'

describe('resolveRuntimeConfig', () => {
  it('prefers the configuration injected at container start', () => {
    const config = resolveRuntimeConfig({ apiBaseUrl: 'https://api.company.test/' }, '/build-time')
    expect(config.apiBaseUrl).toBe('https://api.company.test')
  })

  it('falls back to the build-time variable, then to the same-origin versioned path', () => {
    expect(resolveRuntimeConfig({ apiBaseUrl: '  ' }, '/build-time').apiBaseUrl).toBe('/build-time')
    expect(resolveRuntimeConfig(undefined, undefined).apiBaseUrl).toBe('/api/v1')
  })
})
