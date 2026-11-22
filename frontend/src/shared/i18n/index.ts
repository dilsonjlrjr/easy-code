import { createI18n } from 'vue-i18n'
import { en as vuetifyEn, pt as vuetifyPt } from 'vuetify/locale'
import { en } from './locales/en'
import { ptBR, type MessageSchema } from './locales/pt-BR'
import { DEFAULT_LOCALE, type SupportedLocale } from './supportedLocales'

/** Vuetify's own component strings live under `$vuetify` so one adapter serves both. */
type CatalogWithVuetify = MessageSchema & { $vuetify: typeof vuetifyEn }

export const i18n = createI18n<[CatalogWithVuetify], SupportedLocale, false>({
  // Composition API mode. The option is deprecated only because v12 drops the legacy mode itself.
  // eslint-disable-next-line @typescript-eslint/no-deprecated
  legacy: false,
  locale: DEFAULT_LOCALE,
  fallbackLocale: DEFAULT_LOCALE,
  messages: {
    'pt-BR': { ...ptBR, $vuetify: vuetifyPt },
    en: { ...en, $vuetify: vuetifyEn },
  },
})

export { DEFAULT_LOCALE, SUPPORTED_LOCALES, isSupportedLocale } from './supportedLocales'
export type { SupportedLocale } from './supportedLocales'
