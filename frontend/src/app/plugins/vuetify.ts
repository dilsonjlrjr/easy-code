import { createVuetify, type ThemeDefinition } from 'vuetify'
import { aliases, mdi } from 'vuetify/iconsets/mdi-svg'
import { createVueI18nAdapter } from 'vuetify/locale/adapters/vue-i18n'
import { useI18n } from 'vue-i18n'
import { i18n } from '@/shared/i18n'
import 'vuetify/styles'

type VueI18nInstance = Parameters<typeof createVueI18nAdapter>[0]['i18n']

// Colors chosen for WCAG 2.2 AA contrast on their surfaces (RNF-18).
const lightTheme: ThemeDefinition = {
  dark: false,
  colors: {
    primary: '#2f4ba0',
    secondary: '#4f5b75',
    background: '#f7f8fc',
    surface: '#ffffff',
    // Darker than the usual #b3261e: a focused field in error tints its background with this
    // color, and the floating label must keep 4.5:1 over it (RNF-18).
    error: '#98201a',
    info: '#1d5fa8',
    success: '#2e6b30',
    warning: '#8a5300',
  },
}

const darkTheme: ThemeDefinition = {
  dark: true,
  colors: {
    primary: '#b4c5ff',
    secondary: '#bfc6dc',
    background: '#111318',
    surface: '#1b1e25',
    error: '#ffb4ab',
    info: '#9ecaff',
    success: '#8fd68a',
    warning: '#ffb95c',
  },
}

export const vuetify = createVuetify({
  icons: { defaultSet: 'mdi', aliases, sets: { mdi } },
  // The adapter is typed for an untyped i18n instance; ours is typed by the catalog schema.
  locale: { adapter: createVueI18nAdapter({ i18n: i18n as unknown as VueI18nInstance, useI18n }) },
  theme: {
    defaultTheme: 'light',
    themes: { light: lightTheme, dark: darkTheme },
  },
  defaults: {
    // ESP-00 (PD-17): 44 px is the design height for buttons and fields.
    VBtn: { minHeight: 44 },
    VTextField: { density: 'comfortable' },
  },
})
