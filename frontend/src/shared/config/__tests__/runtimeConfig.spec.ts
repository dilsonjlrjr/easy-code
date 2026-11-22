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

  it('offers the development sign-in only when the configuration says so (ESP-02 ER-02-9)', () => {
    expect(resolveRuntimeConfig(undefined, undefined).devSignIn).toBe(false)
    expect(resolveRuntimeConfig({ devSignIn: 'false' }, undefined).devSignIn).toBe(false)
    expect(resolveRuntimeConfig({ devSignIn: true }, undefined).devSignIn).toBe(true)
    expect(resolveRuntimeConfig({ devSignIn: 'true' }, undefined).devSignIn).toBe(true)
    expect(resolveRuntimeConfig(undefined, undefined, 'true').devSignIn).toBe(true)
  })
})
