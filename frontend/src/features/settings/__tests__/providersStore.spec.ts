import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ANY_PRICE_VERSION, type ModelPrice, type ProvidersApi } from '../api/providersApi'
import { setProvidersApi, useProvidersStore } from '../stores/providersStore'

function price(id: number, effectiveSince: string, used = false): ModelPrice {
  return {
    id,
    effective_since: effectiveSince,
    currency: 'BRL',
    per_million_tokens: { input: '1', output: '2', read_cache: '0.1', write_cache: '0.2' },
    source: 'página oficial',
    used,
    recorded_at: effectiveSince,
  }
}

function fakeApi(overrides: Partial<ProvidersApi> = {}): ProvidersApi {
  return {
    listProviders: vi.fn().mockResolvedValue([]),
    saveProviderKey: vi.fn(),
    removeProviderKey: vi.fn().mockResolvedValue(undefined),
    listModels: vi.fn().mockResolvedValue([]),
    recordModel: vi.fn(),
    listModelPrices: vi.fn().mockResolvedValue([]),
    recordModelPrice: vi.fn().mockResolvedValue(price(3, '2026-12-01T00:00:00Z')),
    correctModelPrice: vi.fn().mockResolvedValue(price(2, '2026-11-01T00:00:00Z')),
    ...overrides,
  }
}

describe('providers store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('US-15 CA-1/CA-2: keeps only what the API returns (status and last 4), never the key', async () => {
    const saved = {
      id: 'anthropic' as const,
      name: 'Anthropic',
      key: { status: 'active' as const, last_4: 'wxyz' },
    }
    const api = fakeApi({
      listProviders: vi.fn().mockResolvedValue([{ ...saved, key: { status: 'missing' } }]),
      saveProviderKey: vi.fn().mockResolvedValue(saved),
    })
    setProvidersApi(api)
    const store = useProvidersStore()
    await store.loadProviders()

    await store.saveKey('anthropic', 'sk-secret-value-1234wxyz')

    expect(api.saveProviderKey).toHaveBeenCalledWith('anthropic', 'sk-secret-value-1234wxyz')
    expect(JSON.stringify(store.$state)).not.toContain('sk-secret')
    expect(store.providers[0]?.key.last_4).toBe('wxyz')
  })

  it('US-37 CA-1: lists price periods newest first after recording a new one', async () => {
    const api = fakeApi({
      listModelPrices: vi
        .fn()
        .mockResolvedValue([
          price(1, '2026-10-01T00:00:00Z', true),
          price(3, '2026-12-01T00:00:00Z'),
        ]),
    })
    setProvidersApi(api)
    const store = useProvidersStore()

    await store.recordPrice(
      'anthropic',
      'm1',
      {
        effective_since: '2026-12-01T00:00:00Z',
        currency: 'BRL',
        per_million_tokens: { input: '1', output: '2', read_cache: '0.1', write_cache: '0.2' },
        source: 'página oficial',
      },
      'key-1',
    )

    expect(api.recordModelPrice).toHaveBeenCalledWith(
      'anthropic',
      'm1',
      expect.any(Object),
      'key-1',
    )
    expect(store.prices['anthropic/m1']?.map((item) => item.id)).toEqual([3, 1])
  })

  it('US-37 CA-3: corrects an unused period with If-Match and lets the API refuse used ones', async () => {
    const api = fakeApi()
    setProvidersApi(api)
    const store = useProvidersStore()

    await store.correctPrice('anthropic', 'm1', price(2, '2026-11-01T00:00:00Z'), {
      source: 'nova fonte',
    })

    expect(api.correctModelPrice).toHaveBeenCalledWith(
      'anthropic',
      'm1',
      2,
      { source: 'nova fonte' },
      ANY_PRICE_VERSION,
    )
  })
})
