import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { ProviderId } from '@/shared/api/contract'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import {
  ANY_PRICE_VERSION,
  providersApi,
  type Model,
  type ModelPrice,
  type ModelPriceModification,
  type NewModel,
  type NewModelPrice,
  type Provider,
  type ProvidersApi,
} from '../api/providersApi'

let api: ProvidersApi = providersApi

/** Tests replace the API; the application uses the default one. */
export function setProvidersApi(replacement: ProvidersApi): void {
  api = replacement
}

/** One model of a price list to save: the model first when it is new, then a new effective period. */
export interface PriceImportItem {
  provider: ProviderId
  model: string
  newModel?: NewModel
  price: NewModelPrice
}

export interface PriceImportResult {
  provider: ProviderId
  model: string
  error?: unknown
}

export function modelKey(provider: ProviderId, model: string): string {
  return `${provider}/${model}`
}

/**
 * Organization settings for providers, models and the price table (ESP-23; US-15, US-37, US-16
 * capability matrix). Keys are sent once and dropped: nothing here keeps the key value (US-15 CA-2).
 */
export const useProvidersStore = defineStore('settings-providers', () => {
  const providers = ref<Provider[]>([])
  const models = ref<Model[]>([])
  const prices = ref<Record<string, ModelPrice[]>>({})

  async function loadProviders(): Promise<void> {
    providers.value = await api.listProviders()
  }

  async function loadModels(): Promise<void> {
    models.value = await api.listModels()
  }

  function replaceProvider(updated: Provider): void {
    providers.value = providers.value.map((item) => (item.id === updated.id ? updated : item))
  }

  /** Saves and tests a key (up to 15 s). On refusal the previous key stays active (CA-23-3). */
  async function saveKey(provider: ProviderId, key: string): Promise<Provider> {
    const saved = await api.saveProviderKey(provider, key)
    replaceProvider(saved)
    return saved
  }

  async function removeKey(provider: ProviderId): Promise<void> {
    await api.removeProviderKey(provider)
    await loadProviders()
  }

  async function saveModel(provider: ProviderId, model: string, body: NewModel): Promise<Model> {
    const saved = await api.recordModel(provider, model, body)
    const key = modelKey(saved.provider, saved.model)
    const others = models.value.filter((item) => modelKey(item.provider, item.model) !== key)
    models.value = [...others, saved]
    return saved
  }

  /** Periods newest first (ESP-23 3.2: decreasing `effective_since`). */
  async function loadPrices(provider: ProviderId, model: string): Promise<ModelPrice[]> {
    const items = await api.listModelPrices(provider, model)
    const sorted = [...items].sort((left, right) =>
      right.effective_since.localeCompare(left.effective_since),
    )
    prices.value = { ...prices.value, [modelKey(provider, model)]: sorted }
    return sorted
  }

  /**
   * Records a new effective period. The idempotency key belongs to the user's intention, so the
   * dialog keeps it across retries of the same confirmation.
   */
  async function recordPrice(
    provider: ProviderId,
    model: string,
    body: NewModelPrice,
    idempotencyKey: string = newIdempotencyKey(),
  ): Promise<ModelPrice> {
    const created = await api.recordModelPrice(provider, model, body, idempotencyKey)
    await Promise.all([loadPrices(provider, model), loadModels()])
    return created
  }

  /** Only unused periods can be corrected (US-37 CA-3); the API refuses used ones with 409. */
  async function correctPrice(
    provider: ProviderId,
    model: string,
    price: ModelPrice,
    body: ModelPriceModification,
  ): Promise<ModelPrice> {
    const corrected = await api.correctModelPrice(
      provider,
      model,
      price.id,
      body,
      ANY_PRICE_VERSION,
    )
    await Promise.all([loadPrices(provider, model), loadModels()])
    return corrected
  }

  /**
   * Saves the picked models of a price list, one by one: a failure stops only its own model and is
   * returned with it. Models and the cached periods are read again once at the end.
   */
  async function importPrices(items: PriceImportItem[]): Promise<PriceImportResult[]> {
    const results: PriceImportResult[] = []
    for (const item of items) {
      try {
        if (item.newModel) await api.recordModel(item.provider, item.model, item.newModel)
        await api.recordModelPrice(item.provider, item.model, item.price, newIdempotencyKey())
        results.push({ provider: item.provider, model: item.model })
      } catch (error) {
        results.push({ provider: item.provider, model: item.model, error })
      }
    }
    const touched = new Set(items.map((item) => modelKey(item.provider, item.model)))
    prices.value = Object.fromEntries(
      Object.entries(prices.value).filter(([key]) => !touched.has(key)),
    )
    await loadModels()
    return results
  }

  return {
    providers,
    models,
    prices,
    loadProviders,
    loadModels,
    saveKey,
    removeKey,
    saveModel,
    loadPrices,
    recordPrice,
    correctPrice,
    importPrices,
  }
})
