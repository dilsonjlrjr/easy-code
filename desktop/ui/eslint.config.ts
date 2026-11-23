import pluginVitest from '@vitest/eslint-plugin'
import { defineConfigWithVueTs, vueTsConfigs } from '@vue/eslint-config-typescript'
import skipFormatting from 'eslint-config-prettier/flat'
import { globalIgnores } from 'eslint/config'
import pluginVue from 'eslint-plugin-vue'
import pluginVueA11y from 'eslint-plugin-vuejs-accessibility'

export default defineConfigWithVueTs(
  { name: 'desktop/files-to-lint', files: ['**/*.{vue,ts,mts}'] },
  globalIgnores(['**/dist/**', '**/coverage/**']),
  ...pluginVue.configs['flat/recommended'],
  vueTsConfigs.strictTypeChecked,
  ...pluginVueA11y.configs['flat/recommended'],
  {
    name: 'desktop/rules',
    rules: {
      // RNF-21: every user-facing text comes from the i18n catalogs.
      'vue/no-bare-strings-in-template': 'error',
      'vue/multi-word-component-names': 'error',
      // A label tied by `for` or by nesting is enough (WCAG 1.3.1); the default asks for both.
      'vuejs-accessibility/label-has-for': ['error', { required: { some: ['nesting', 'id'] } }],
    },
  },
  { ...pluginVitest.configs.recommended, files: ['src/**/__tests__/*'] },
  skipFormatting,
)
