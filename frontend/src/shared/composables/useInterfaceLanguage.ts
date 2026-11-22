import { storeToRefs } from 'pinia'
import { watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { SUPPORTED_LOCALES } from '@/shared/i18n'
import { usePreferencesStore } from '@/shared/preferences/preferencesStore'

/** Keeps vue-i18n and `<html lang>` in step with the stored language preference. */
export function useInterfaceLanguageSync(): void {
  const { locale: i18nLocale } = useI18n({ useScope: 'global' })
  const { locale: preferredLocale } = storeToRefs(usePreferencesStore())

  watchEffect(() => {
    i18nLocale.value = preferredLocale.value
    document.documentElement.lang = preferredLocale.value
  })
}

/** What a language switcher needs: the options, the current one and how to change it. */
export function useInterfaceLanguage() {
  const preferences = usePreferencesStore()
  const { locale: currentLocale } = storeToRefs(preferences)
  return {
    availableLocales: SUPPORTED_LOCALES,
    currentLocale,
    changeLocale: preferences.changeLocale,
  }
}
