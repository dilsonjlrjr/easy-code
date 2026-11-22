import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'
import { PREFERENCE_STORAGE_KEYS, usePreferencesStore } from '../preferencesStore'

describe('usePreferencesStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('defaults to pt-BR and the system theme', () => {
    const preferences = usePreferencesStore()
    expect(preferences.locale).toBe('pt-BR')
    expect(preferences.themePreference).toBe('system')
  })

  it('keeps the chosen language in local storage', () => {
    usePreferencesStore().changeLocale('en')
    expect(window.localStorage.getItem(PREFERENCE_STORAGE_KEYS.locale)).toBe('en')
  })

  it('ignores an unsupported stored language', () => {
    window.localStorage.setItem(PREFERENCE_STORAGE_KEYS.locale, 'xx-YY')
    expect(usePreferencesStore().locale).toBe('pt-BR')
  })
})
