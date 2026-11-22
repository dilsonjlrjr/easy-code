import { usePreferredDark } from '@vueuse/core'
import { storeToRefs } from 'pinia'
import { computed, watchEffect } from 'vue'
import { useTheme } from 'vuetify'
import { THEME_PREFERENCES, usePreferencesStore } from '@/shared/preferences/preferencesStore'

export type ResolvedTheme = 'light' | 'dark'

/** Applies the stored theme preference to Vuetify, following the system when asked to. */
export function useThemeSync(): void {
  const theme = useTheme()
  const { themePreference } = storeToRefs(usePreferencesStore())
  const systemPrefersDark = usePreferredDark()

  const resolvedTheme = computed<ResolvedTheme>(() => {
    if (themePreference.value === 'system') {
      return systemPrefersDark.value ? 'dark' : 'light'
    }
    return themePreference.value
  })

  watchEffect(() => {
    theme.change(resolvedTheme.value)
    document.documentElement.style.colorScheme = resolvedTheme.value
  })
}

/** What a theme switcher needs. */
export function useThemePreference() {
  const preferences = usePreferencesStore()
  const { themePreference } = storeToRefs(preferences)
  return {
    availablePreferences: THEME_PREFERENCES,
    themePreference,
    changeThemePreference: preferences.changeThemePreference,
  }
}
