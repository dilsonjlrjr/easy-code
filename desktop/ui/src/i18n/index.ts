import { createI18n, useI18n } from 'vue-i18n'
import en from './locales/en'
import ptBR from './locales/pt-BR'

export const SUPPORTED_LOCALES = ['pt-BR', 'en'] as const
export type Locale = (typeof SUPPORTED_LOCALES)[number]

export function createAppI18n(locale: Locale = 'pt-BR') {
  return createI18n({
    legacy: false,
    locale,
    fallbackLocale: 'pt-BR',
    messages: { 'pt-BR': ptBR, en },
  })
}

export function normalizeLocale(value: string | undefined): Locale {
  return value?.toLowerCase().startsWith('en') ? 'en' : 'pt-BR'
}

/** Label of a key when it exists, otherwise the raw value (unknown stage, agent or state). */
export function useLabel(): (key: string, raw: string) => string {
  const i18n = useI18n()
  return (key, raw) => (i18n.te(key) ? i18n.t(key) : raw)
}

/** Text of an untyped event field. */
export function fieldText(value: unknown): string {
  return typeof value === 'string' || typeof value === 'number' ? String(value) : ''
}
