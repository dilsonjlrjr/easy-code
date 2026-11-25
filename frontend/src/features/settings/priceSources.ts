import type { ProviderId } from '@/shared/api/contract'
import type { Capabilities, PricesPerMillion } from './api/providersApi'

/**
 * Public price lists the price table can be filled from. The browser reads them directly (GET, no
 * credential, no project data); the platform servers never reach them (ADR-16 egress). What comes
 * back is untrusted data: each entry is checked here, shown as text and saved only after the
 * administrator picks it. Prices in the lists are USD per token or per million tokens.
 */
export type PriceSourceId = 'models_dev' | 'litellm' | 'openrouter'

export interface PriceOffer {
  provider: ProviderId
  /** Model id as the provider API names it (ModelName). */
  model: string
  displayName: string
  /** USD per million tokens, as decimal strings the API accepts. */
  perMillionUsd: PricesPerMillion
  capabilities: Capabilities
  deprecated: boolean
}

export interface PriceSource {
  id: PriceSourceId
  name: string
  url: string
  homepage: string
  parse: (data: unknown, today: Date) => PriceOffer[]
}

const PROVIDERS: ProviderId[] = ['anthropic', 'openai']
/** openapi ModelName (pricing.modelNamePattern in the api). */
const MODEL_NAME = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/
const MAX_DISPLAY_NAME = 120
const DEFAULT_CONTEXT_TOKENS = 128_000

type Json = Record<string, unknown>

function isObject(value: unknown): value is Json {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function asNumber(value: unknown): number | undefined {
  const parsed = typeof value === 'string' ? Number(value) : value
  return typeof parsed === 'number' && Number.isFinite(parsed) && parsed >= 0 ? parsed : undefined
}

function asText(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() !== '' ? value.trim() : undefined
}

/** A non-negative number as a decimal string of the contract: up to 10 integer and 8 decimal digits. */
export function toDecimal(value: number): string | undefined {
  if (!Number.isFinite(value) || value < 0 || value >= 1e10) return undefined
  const fixed = value.toFixed(8)
  return fixed.includes('.') ? fixed.replace(/\.?0+$/, '') : fixed
}

/** Converts a decimal string by an exchange rate (both positive). */
export function convertDecimal(value: string, rate: number): string | undefined {
  return toDecimal(Number(value) * rate)
}

function perMillion(
  input: number | undefined,
  output: number | undefined,
  readCache: number | undefined,
  writeCache: number | undefined,
  scale: number,
): PricesPerMillion | undefined {
  if (input === undefined || output === undefined) return undefined
  const values = [input, output, readCache ?? 0, writeCache ?? 0].map((value) =>
    toDecimal(value * scale),
  )
  const [inputText, outputText, readText, writeText] = values
  if (!inputText || !outputText || !readText || !writeText) return undefined
  return { input: inputText, output: outputText, read_cache: readText, write_cache: writeText }
}

function offer(
  provider: ProviderId,
  model: string,
  displayName: string | undefined,
  prices: PricesPerMillion | undefined,
  capabilities: Capabilities,
  deprecated: boolean,
): PriceOffer | undefined {
  if (!prices || !MODEL_NAME.test(model)) return undefined
  return {
    provider,
    model,
    displayName: (displayName ?? model).slice(0, MAX_DISPLAY_NAME),
    perMillionUsd: prices,
    capabilities: {
      ...capabilities,
      context_tokens:
        capabilities.context_tokens >= 1
          ? Math.floor(capabilities.context_tokens)
          : DEFAULT_CONTEXT_TOKENS,
    },
    deprecated,
  }
}

/** One offer per provider and model (the first wins), sorted by provider, then model. */
function unique(offers: Array<PriceOffer | undefined>): PriceOffer[] {
  const seen = new Map<string, PriceOffer>()
  for (const item of offers) {
    if (!item) continue
    const key = `${item.provider}/${item.model}`
    if (!seen.has(key)) seen.set(key, item)
  }
  return [...seen.values()].sort(
    (left, right) =>
      left.provider.localeCompare(right.provider) || left.model.localeCompare(right.model),
  )
}

/** models.dev `api.json`: `{ <provider>: { models: { <id>: { cost: USD per million, limit, ... } } } }`. */
export function parseModelsDev(data: unknown): PriceOffer[] {
  if (!isObject(data)) return []
  const offers: Array<PriceOffer | undefined> = []
  for (const provider of PROVIDERS) {
    const entry = data[provider]
    const models = isObject(entry) && isObject(entry.models) ? entry.models : {}
    for (const [id, raw] of Object.entries(models)) {
      if (!isObject(raw) || !isObject(raw.cost)) continue
      const limit = isObject(raw.limit) ? raw.limit : {}
      const modalities = isObject(raw.modalities) ? raw.modalities : {}
      const inputs = Array.isArray(modalities.input) ? modalities.input : []
      offers.push(
        offer(
          provider,
          asText(raw.id) ?? id,
          asText(raw.name),
          perMillion(
            asNumber(raw.cost.input),
            asNumber(raw.cost.output),
            asNumber(raw.cost.cache_read),
            asNumber(raw.cost.cache_write),
            1,
          ),
          {
            tool_use: raw.tool_call === true,
            context_tokens: asNumber(limit.context) ?? 0,
            prompt_cache: asNumber(raw.cost.cache_read) !== undefined,
            image_input: inputs.includes('image'),
          },
          raw.status === 'deprecated',
        ),
      )
    }
  }
  return unique(offers)
}

/** LiteLLM `model_prices_and_context_window.json`: `{ <model>: { litellm_provider, *_cost_per_token, ... } }`. */
export function parseLiteLlm(data: unknown, today: Date): PriceOffer[] {
  if (!isObject(data)) return []
  const offers: Array<PriceOffer | undefined> = []
  for (const [id, raw] of Object.entries(data)) {
    if (!isObject(raw)) continue
    const provider = PROVIDERS.find((item) => item === raw.litellm_provider)
    if (!provider || raw.mode !== 'chat' || id.includes('/') || id.startsWith('ft:')) continue
    const deprecation = asText(raw.deprecation_date)
    offers.push(
      offer(
        provider,
        id,
        undefined,
        perMillion(
          asNumber(raw.input_cost_per_token),
          asNumber(raw.output_cost_per_token),
          asNumber(raw.cache_read_input_token_cost),
          asNumber(raw.cache_creation_input_token_cost),
          1_000_000,
        ),
        {
          tool_use: raw.supports_function_calling === true,
          context_tokens: asNumber(raw.max_input_tokens) ?? 0,
          prompt_cache:
            raw.supports_prompt_caching === true ||
            asNumber(raw.cache_read_input_token_cost) !== undefined,
          image_input: raw.supports_vision === true,
        },
        deprecation !== undefined && new Date(deprecation).getTime() <= today.getTime(),
      ),
    )
  }
  return unique(offers)
}

/**
 * OpenRouter `/api/v1/models`: `{ data: [{ id: "<provider>/<model>", pricing: USD per token, ... }] }`.
 * Its Anthropic ids use dots where the Anthropic API uses dashes (claude-sonnet-4.5 → claude-sonnet-4-5).
 */
export function parseOpenRouter(data: unknown): PriceOffer[] {
  const list = isObject(data) && Array.isArray(data.data) ? data.data : []
  const offers: Array<PriceOffer | undefined> = []
  for (const raw of list) {
    if (!isObject(raw)) continue
    const id = asText(raw.id) ?? ''
    const [prefix, rest] = [id.slice(0, id.indexOf('/')), id.slice(id.indexOf('/') + 1)]
    const provider = PROVIDERS.find((item) => item === prefix)
    if (!provider || rest === '' || rest.includes(':') || rest.includes('/')) continue
    const model = provider === 'anthropic' ? rest.split('.').join('-') : rest
    const pricing = isObject(raw.pricing) ? raw.pricing : {}
    const architecture = isObject(raw.architecture) ? raw.architecture : {}
    const inputs = Array.isArray(architecture.input_modalities) ? architecture.input_modalities : []
    const parameters = Array.isArray(raw.supported_parameters) ? raw.supported_parameters : []
    const name = asText(raw.name)?.replace(/^[^:]+:\s*/, '')
    offers.push(
      offer(
        provider,
        model,
        name,
        perMillion(
          asNumber(pricing.prompt),
          asNumber(pricing.completion),
          asNumber(pricing.input_cache_read),
          asNumber(pricing.input_cache_write),
          1_000_000,
        ),
        {
          tool_use: parameters.includes('tools'),
          context_tokens: asNumber(raw.context_length) ?? 0,
          prompt_cache: asNumber(pricing.input_cache_read) !== undefined,
          image_input: inputs.includes('image'),
        },
        false,
      ),
    )
  }
  return unique(offers)
}

export const PRICE_SOURCES: PriceSource[] = [
  {
    id: 'models_dev',
    name: 'models.dev',
    url: 'https://models.dev/api.json',
    homepage: 'https://models.dev',
    parse: (data) => parseModelsDev(data),
  },
  {
    id: 'litellm',
    name: 'LiteLLM',
    url: 'https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json',
    homepage: 'https://github.com/BerriAI/litellm',
    parse: (data, today) => parseLiteLlm(data, today),
  },
  {
    id: 'openrouter',
    name: 'OpenRouter',
    url: 'https://openrouter.ai/api/v1/models',
    homepage: 'https://openrouter.ai/models',
    parse: (data) => parseOpenRouter(data),
  },
]

/** GET of a source list, without credentials or referrer; the answer is parsed as untrusted data. */
export async function fetchPriceOffers(
  source: PriceSource,
  options: { fetcher?: typeof fetch; signal?: AbortSignal; today?: Date } = {},
): Promise<PriceOffer[]> {
  const fetcher = options.fetcher ?? fetch
  const response = await fetcher(source.url, {
    method: 'GET',
    credentials: 'omit',
    referrerPolicy: 'no-referrer',
    headers: { Accept: 'application/json' },
    signal: options.signal,
  })
  if (!response.ok) throw new Error(`HTTP ${String(response.status)}`)
  return source.parse(await response.json(), options.today ?? new Date())
}
