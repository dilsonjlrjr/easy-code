import { apiClient } from '@/shared/api/apiClient'
import type { ProviderId, UserRef } from '@/shared/api/contract'
import type { HttpClient } from '@/shared/api/httpClient'
import { idempotencyHeaders } from '@/shared/api/idempotencyKey'

export type KeyStatus = 'missing' | 'testing' | 'active'

/** Kind of a `claude-code` credential (US-58): an API key or a `claude setup-token` token. */
export type CredentialKind = 'api_key' | 'subscription_token'

/** Claude Code detection by easycode-sandbox, only on `claude-code` (US-58 CA-1, CA-2). */
export interface Detection {
  detected: boolean
  version: string | null
  checked_at: string | null
}

/** openapi `Provider`: the key itself never comes back, only its status and last 4 characters. */
export interface Provider {
  id: ProviderId
  name: string
  detection?: Detection
  key: {
    status: KeyStatus
    last_test?: {
      result: 'valid' | 'invalid' | 'no_answer'
      message?: string
      tested_at?: string
    } | null
    last_4?: string | null
    saved_at?: string | null
    saved_by?: UserRef | null
    tested_at?: string | null
    credential_kind?: CredentialKind
  }
}

export interface Capabilities {
  tool_use: boolean
  context_tokens: number
  prompt_cache: boolean
  image_input: boolean
}

export interface PricesPerMillion {
  input: string
  output: string
  read_cache: string
  write_cache: string
}

export interface NewModelPrice {
  effective_since: string
  currency: string
  per_million_tokens: PricesPerMillion
  source: string
}

export interface ModelPrice extends NewModelPrice {
  id: number
  table_version?: string
  used: boolean
  recorded_at: string
  recorded_by?: UserRef
}

export interface NewModel {
  display_name: string
  capabilities: Capabilities
  active: boolean
}

export interface Model extends NewModel {
  provider: ProviderId
  model: string
  current_price?: ModelPrice | null
  /** 'anthropic' when a claude-code model uses the price of the Anthropic model of the same id (ADR-23). */
  price_inherited_from?: 'anthropic' | null
}

export type ModelPriceModification = Partial<
  Pick<NewModelPrice, 'effective_since' | 'per_million_tokens' | 'source'>
>

export interface ProvidersApi {
  listProviders: () => Promise<Provider[]>
  saveProviderKey: (provider: ProviderId, key: string) => Promise<Provider>
  removeProviderKey: (provider: ProviderId) => Promise<void>
  listModels: (filter?: { provider?: ProviderId; active?: boolean }) => Promise<Model[]>
  recordModel: (provider: ProviderId, model: string, body: NewModel) => Promise<Model>
  listModelPrices: (provider: ProviderId, model: string) => Promise<ModelPrice[]>
  recordModelPrice: (
    provider: ProviderId,
    model: string,
    body: NewModelPrice,
    idempotencyKey: string,
  ) => Promise<ModelPrice>
  correctModelPrice: (
    provider: ProviderId,
    model: string,
    effectivePeriodId: number,
    body: ModelPriceModification,
    ifMatch: string,
  ) => Promise<ModelPrice>
}

function modelPath(provider: ProviderId, model: string): string {
  return `/providers/${provider}/models/${encodeURIComponent(model)}`
}

/**
 * `correctModelPrice` requires `If-Match`, but neither `listModelPrices` nor `recordModelPrice` returns
 * an ETag for a period (contract gap, reported). Until it does, `*` (RFC 9110: any current version)
 * satisfies the header; the API still refuses periods already used (409 `effective-period-in-use`).
 */
export const ANY_PRICE_VERSION = '*'

export function createProvidersApi(client: HttpClient = apiClient): ProvidersApi {
  return {
    listProviders: async () => (await client.get<{ items: Provider[] }>('/providers')).items,
    saveProviderKey: (provider, key) => client.put<Provider>(`/providers/${provider}/key`, { key }),
    removeProviderKey: async (provider) => {
      await client.delete(`/providers/${provider}/key`)
    },
    listModels: async (filter = {}) =>
      (await client.get<{ items: Model[] }>('/models', { query: filter })).items,
    recordModel: (provider, model, body) => client.put<Model>(modelPath(provider, model), body),
    listModelPrices: async (provider, model) =>
      (await client.get<{ items: ModelPrice[] }>(`${modelPath(provider, model)}/prices`)).items,
    recordModelPrice: (provider, model, body, idempotencyKey) =>
      client.post<ModelPrice>(`${modelPath(provider, model)}/prices`, body, {
        headers: idempotencyHeaders(idempotencyKey),
      }),
    correctModelPrice: (provider, model, effectivePeriodId, body, ifMatch) =>
      client.request<ModelPrice>(
        'PATCH',
        `${modelPath(provider, model)}/prices/${String(effectivePeriodId)}`,
        {
          body,
          contentType: 'application/merge-patch+json',
          headers: { 'If-Match': ifMatch },
        },
      ),
  }
}

export const providersApi = createProvidersApi()
