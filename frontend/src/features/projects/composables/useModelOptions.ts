import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Capacity, ModelRef } from '@/shared/api/contract'
import { useLoader } from '@/shared/composables/useLoader'
import {
  providersApi,
  type Model,
  type Provider,
  type ProvidersApi,
} from '@/features/settings/api/providersApi'

let api: ProvidersApi = providersApi

/** Tests replace the API; the application uses the default one. */
export function setModelOptionsApi(replacement: ProvidersApi): void {
  api = replacement
}

export interface ModelOption {
  value: string
  title: string
  subtitle: string
  model: Model
}

export function modelRefKey(ref: ModelRef | null | undefined): string | undefined {
  return ref ? `${ref.provider}/${ref.model}` : undefined
}

export function parseModelKey(key: string): ModelRef {
  const separator = key.indexOf('/')
  return {
    provider: key.slice(0, separator) as ModelRef['provider'],
    model: key.slice(separator + 1),
  }
}

/** Whether a model has every capability a stage requires (US-16 CA-2, ESP-29 ER-29-25). */
export function missingCapabilities(model: Model, required: Capacity[] = []): Capacity[] {
  return required.filter((capacity) => {
    if (capacity === 'context_tokens') return model.capabilities.context_tokens <= 0
    return !model.capabilities[capacity]
  })
}

/**
 * Model choices for the model pair: active models of providers whose key is active, crossed in the
 * client (ESP-05 3.2 and ESP-29 3.1, decided in PENDENCIAS PL-71). Each option shows its capabilities
 * and the current price, or "no current price" (CA-29-5).
 */
export function useModelOptions() {
  const { t } = useI18n()
  const models = ref<Model[]>([])
  const providers = ref<Provider[]>([])
  const loader = useLoader(async () => {
    const [loadedModels, loadedProviders] = await Promise.all([
      api.listModels({ active: true }),
      api.listProviders(),
    ])
    models.value = loadedModels
    providers.value = loadedProviders
  })

  const providersWithKey = computed(
    () => new Set(providers.value.filter((p) => p.key.status === 'active').map((p) => p.id)),
  )

  function describe(model: Model): string {
    const capabilities = [
      model.capabilities.tool_use ? t('settings.models.toolUse') : undefined,
      model.capabilities.prompt_cache ? t('settings.models.promptCache') : undefined,
      model.capabilities.image_input ? t('settings.models.imageInput') : undefined,
    ].filter((item) => item !== undefined)
    const provider =
      providers.value.find((item) => item.id === model.provider)?.name ?? model.provider
    const price = model.current_price ? '' : ` · ${t('settings.prices.noCurrentPrice')}`
    return `${provider} · ${capabilities.join(', ')}${price}`
  }

  const options = computed<ModelOption[]>(() =>
    models.value
      .filter((model) => model.active && providersWithKey.value.has(model.provider))
      .map((model) => ({
        value: `${model.provider}/${model.model}`,
        title: model.display_name,
        subtitle: describe(model),
        model,
      })),
  )

  function providerName(id: string): string {
    return providers.value.find((provider) => provider.id === id)?.name ?? id
  }

  return { options, loader, providerName }
}
