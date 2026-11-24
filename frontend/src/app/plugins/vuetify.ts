import { createVuetify, type ThemeDefinition } from 'vuetify'
import { aliases, mdi } from 'vuetify/iconsets/mdi-svg'
import { createVueI18nAdapter } from 'vuetify/locale/adapters/vue-i18n'
import { useI18n } from 'vue-i18n'
import { i18n } from '@/shared/i18n'
import 'vuetify/styles'

type VueI18nInstance = Parameters<typeof createVueI18nAdapter>[0]['i18n']

/**
 * The EasyCode look from the interface canvas ("Fundamentos visuais"): dark, with a blue and
 * violet glow, translucent surfaces and an indigo-violet accent. Strong color only for state.
 * The state colors are the light tints the canvas uses as chip text, so tonal chips, alerts and
 * text buttons keep 4.5:1 over the dark surfaces (RNF-18).
 */
const darkTheme: ThemeDefinition = {
  dark: true,
  colors: {
    primary: '#A78BFA',
    'on-primary': '#0B0E2A',
    secondary: '#A9B0D6',
    background: '#080B24',
    surface: '#151B45',
    'surface-bright': '#1C2357',
    'surface-variant': '#2A3170',
    'on-surface-variant': '#F1F3FF',
    'on-background': '#F1F3FF',
    'on-surface': '#F1F3FF',
    error: '#FCA5A5',
    info: '#C7D2FE',
    success: '#86EFAC',
    warning: '#FCD34D',
  },
  variables: {
    'border-color': '#8C96FF',
    'border-opacity': 0.16,
    'high-emphasis-opacity': 1,
    'medium-emphasis-opacity': 0.78,
    'theme-overlay-multiplier': 1,
  },
}

/** The same accent and state families on a light ground, for people who choose the light theme. */
const lightTheme: ThemeDefinition = {
  dark: false,
  colors: {
    primary: '#4F46E5',
    secondary: '#4B5280',
    background: '#F5F6FD',
    surface: '#FFFFFF',
    // Darker than the usual #b3261e: a focused field in error tints its background with this
    // color, and the floating label must keep 4.5:1 over it (RNF-18).
    error: '#98201a',
    info: '#3730A3',
    success: '#2e6b30',
    warning: '#8a5300',
  },
}

export const vuetify = createVuetify({
  icons: { defaultSet: 'mdi', aliases, sets: { mdi } },
  // The adapter is typed for an untyped i18n instance; ours is typed by the catalog schema.
  locale: { adapter: createVueI18nAdapter({ i18n: i18n as unknown as VueI18nInstance, useI18n }) },
  theme: {
    defaultTheme: 'dark',
    themes: { light: lightTheme, dark: darkTheme },
  },
  defaults: {
    // ESP-00 (PD-17): 44 px is the design height for buttons and fields.
    VBtn: { minHeight: 44 },
    VCard: { elevation: 0 },
    VTextField: { density: 'comfortable', variant: 'outlined' },
    VTextarea: { variant: 'outlined' },
    VSelect: { variant: 'outlined' },
    VAutocomplete: { variant: 'outlined' },
    VCombobox: { variant: 'outlined' },
    VFileInput: { variant: 'outlined' },
    VTabs: { color: 'primary' },
    VChip: { rounded: 'pill' },
  },
})
