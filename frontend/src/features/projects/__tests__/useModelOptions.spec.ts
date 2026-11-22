import { describe, expect, it } from 'vitest'
import type { Model } from '@/features/settings/api/providersApi'
import { missingCapabilities, modelRefKey, parseModelKey } from '../composables/useModelOptions'

const model: Model = {
  provider: 'anthropic',
  model: 'execution-model',
  display_name: 'Execution',
  active: true,
  capabilities: { tool_use: false, context_tokens: 200000, prompt_cache: true, image_input: false },
}

describe('model options helpers', () => {
  it('US-16 CA-2: names the capability a model lacks for a stage', () => {
    expect(missingCapabilities(model, ['tool_use', 'prompt_cache'])).toEqual(['tool_use'])
    expect(missingCapabilities(model, [])).toEqual([])
  })

  it('round-trips a model reference through its option key', () => {
    const key = modelRefKey({ provider: 'openai', model: 'gpt-x:2026' })
    expect(key).toBe('openai/gpt-x:2026')
    expect(parseModelKey(key ?? '')).toEqual({ provider: 'openai', model: 'gpt-x:2026' })
  })
})
