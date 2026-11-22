import { useLocalStorage } from '@vueuse/core'
import { defineStore } from 'pinia'
import { computed } from 'vue'
import { DEFAULT_LOCALE, isSupportedLocale, type SupportedLocale } from '@/shared/i18n'

export const THEME_PREFERENCES = ['system', 'light', 'dark'] as const
export type ThemePreference = (typeof THEME_PREFERENCES)[number]

/**
 * Keys in the browser's local storage. They hold interface preferences only, never personal or
 * project data (RNF-20). When authentication arrives the key gains the user id, so each user on
 * a shared browser keeps their own choice.
 */
export const PREFERENCE_STORAGE_KEYS = {
  locale: 'easy-code:preferences:locale',
  theme: 'easy-code:preferences:theme',
} as const

function isThemePreference(value: unknown): value is ThemePreference {
  return typeof value === 'string' && (THEME_PREFERENCES as readonly string[]).includes(value)
}

export const usePreferencesStore = defineStore('preferences', () => {
  const storedLocale = useLocalStorage<string>(PREFERENCE_STORAGE_KEYS.locale, DEFAULT_LOCALE, {
    flush: 'sync',
  })
  const storedTheme = useLocalStorage<string>(PREFERENCE_STORAGE_KEYS.theme, 'system', {
    flush: 'sync',
  })

  const locale = computed<SupportedLocale>(() =>
    isSupportedLocale(storedLocale.value) ? storedLocale.value : DEFAULT_LOCALE,
  )
  const themePreference = computed<ThemePreference>(() =>
    isThemePreference(storedTheme.value) ? storedTheme.value : 'system',
  )

  function changeLocale(nextLocale: SupportedLocale): void {
    storedLocale.value = nextLocale
  }

  function changeThemePreference(nextPreference: ThemePreference): void {
    storedTheme.value = nextPreference
  }

  return { locale, themePreference, changeLocale, changeThemePreference }
})
