import { describe, expect, it, vi } from 'vitest'
import {
  PRICE_SOURCES,
  convertDecimal,
  fetchPriceOffers,
  parseLiteLlm,
  parseModelsDev,
  parseOpenRouter,
  toDecimal,
} from '../priceSources'

const TODAY = new Date('2026-11-25T12:00:00Z')

describe('toDecimal', () => {
  it('writes the contract decimal: at most 8 decimal digits, no trailing zeros', () => {
    expect(toDecimal(3e-6 * 1_000_000)).toBe('3')
    expect(toDecimal(0.3)).toBe('0.3')
    expect(toDecimal(1.23456789123)).toBe('1.23456789')
    expect(toDecimal(0)).toBe('0')
    expect(toDecimal(-1)).toBeUndefined()
    expect(toDecimal(Number.NaN)).toBeUndefined()
  })

  it('converts by an exchange rate', () => {
    expect(convertDecimal('3', 5.5)).toBe('16.5')
  })
})

describe('parseModelsDev', () => {
  it('reads the Anthropic and OpenAI models with price and capabilities', () => {
    const offers = parseModelsDev({
      anthropic: {
        models: {
          'claude-x-1': {
            id: 'claude-x-1',
            name: 'Claude X 1',
            tool_call: true,
            modalities: { input: ['text', 'image'] },
            limit: { context: 200000 },
            cost: { input: 3, output: 15, cache_read: 0.3, cache_write: 3.75 },
          },
          'no-cost': { id: 'no-cost', name: 'No cost' },
        },
      },
      openai: {
        models: {
          'gpt-old': {
            id: 'gpt-old',
            status: 'deprecated',
            limit: { context: 8000 },
            cost: { input: 30, output: 60 },
          },
        },
      },
      google: { models: { gemini: { id: 'gemini', cost: { input: 1, output: 2 } } } },
    })

    expect(offers).toEqual([
      {
        provider: 'anthropic',
        model: 'claude-x-1',
        displayName: 'Claude X 1',
        perMillionUsd: { input: '3', output: '15', read_cache: '0.3', write_cache: '3.75' },
        capabilities: {
          tool_use: true,
          context_tokens: 200000,
          prompt_cache: true,
          image_input: true,
        },
        deprecated: false,
      },
      {
        provider: 'openai',
        model: 'gpt-old',
        displayName: 'gpt-old',
        perMillionUsd: { input: '30', output: '60', read_cache: '0', write_cache: '0' },
        capabilities: {
          tool_use: false,
          context_tokens: 8000,
          prompt_cache: false,
          image_input: false,
        },
        deprecated: true,
      },
    ])
  })

  it('ignores anything that is not the expected shape', () => {
    expect(parseModelsDev(null)).toEqual([])
    expect(
      parseModelsDev({ anthropic: { models: { 'bad id!': { cost: { input: 1, output: 1 } } } } }),
    ).toEqual([])
  })
})

describe('parseLiteLlm', () => {
  it('reads chat models of the two providers, per token to per million', () => {
    const offers = parseLiteLlm(
      {
        sample_spec: { litellm_provider: 'openai', mode: 'chat' },
        'claude-x-1': {
          litellm_provider: 'anthropic',
          mode: 'chat',
          input_cost_per_token: 3e-6,
          output_cost_per_token: 1.5e-5,
          cache_read_input_token_cost: 3e-7,
          cache_creation_input_token_cost: 3.75e-6,
          max_input_tokens: 200000,
          supports_function_calling: true,
          supports_vision: true,
          deprecation_date: '2026-11-30',
        },
        'gpt-y': {
          litellm_provider: 'openai',
          mode: 'chat',
          input_cost_per_token: 2.5e-6,
          output_cost_per_token: 1e-5,
          max_input_tokens: 128000,
          deprecation_date: '2026-01-01',
        },
        'ft:gpt-y': {
          litellm_provider: 'openai',
          mode: 'chat',
          input_cost_per_token: 1,
          output_cost_per_token: 1,
        },
        'openai/gpt-y': {
          litellm_provider: 'openai',
          mode: 'chat',
          input_cost_per_token: 1,
          output_cost_per_token: 1,
        },
        'text-embedding': {
          litellm_provider: 'openai',
          mode: 'embedding',
          input_cost_per_token: 1e-7,
        },
      },
      TODAY,
    )

    expect(offers.map((offer) => [offer.model, offer.deprecated])).toEqual([
      ['claude-x-1', false],
      ['gpt-y', true],
    ])
    expect(offers[0]?.perMillionUsd).toEqual({
      input: '3',
      output: '15',
      read_cache: '0.3',
      write_cache: '3.75',
    })
    expect(offers[0]?.capabilities).toEqual({
      tool_use: true,
      context_tokens: 200000,
      prompt_cache: true,
      image_input: true,
    })
  })
})

describe('parseOpenRouter', () => {
  it('maps OpenRouter ids to the provider API ids and skips variants', () => {
    const offers = parseOpenRouter({
      data: [
        {
          id: 'anthropic/claude-sonnet-4.5',
          name: 'Anthropic: Claude Sonnet 4.5',
          context_length: 1000000,
          architecture: { input_modalities: ['text', 'image'] },
          supported_parameters: ['tools', 'temperature'],
          pricing: {
            prompt: '0.000003',
            completion: '0.000015',
            input_cache_read: '0.0000003',
            input_cache_write: '0.00000375',
          },
        },
        {
          id: 'anthropic/claude-sonnet-4.5:batch',
          pricing: { prompt: '0.0000015', completion: '0.0000075' },
        },
        {
          id: 'openai/gpt-4.1',
          name: 'OpenAI: GPT-4.1',
          context_length: 1047576,
          pricing: { prompt: '0.000002', completion: '0.000008' },
        },
        { id: 'meta/llama', pricing: { prompt: '0', completion: '0' } },
      ],
    })

    expect(offers.map((offer) => [offer.provider, offer.model, offer.displayName])).toEqual([
      ['anthropic', 'claude-sonnet-4-5', 'Claude Sonnet 4.5'],
      ['openai', 'gpt-4.1', 'GPT-4.1'],
    ])
    expect(offers[0]?.perMillionUsd.write_cache).toBe('3.75')
    expect(offers[0]?.capabilities.tool_use).toBe(true)
  })
})

describe('fetchPriceOffers', () => {
  it('sends a GET without credentials and parses the answer', async () => {
    const source = PRICE_SOURCES.find((item) => item.id === 'models_dev')
    if (!source) throw new Error('missing source')
    const fetcher = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({ openai: { models: { m: { id: 'm', cost: { input: 1, output: 2 } } } } }),
        {
          status: 200,
        },
      ),
    )

    const offers = await fetchPriceOffers(source, { fetcher, today: TODAY })

    expect(offers.map((offer) => offer.model)).toEqual(['m'])
    expect(fetcher).toHaveBeenCalledWith(
      'https://models.dev/api.json',
      expect.objectContaining({
        method: 'GET',
        credentials: 'omit',
        referrerPolicy: 'no-referrer',
      }),
    )
  })

  it('fails on an error status', async () => {
    const source = PRICE_SOURCES[0]
    if (!source) throw new Error('missing source')
    const fetcher = vi.fn().mockResolvedValue(new Response('nope', { status: 503 }))
    await expect(fetchPriceOffers(source, { fetcher })).rejects.toThrow('HTTP 503')
  })
})
