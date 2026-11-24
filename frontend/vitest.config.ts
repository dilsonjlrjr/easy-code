import { fileURLToPath } from 'node:url'
import { configDefaults, defineConfig, mergeConfig } from 'vitest/config'
import viteConfig from './vite.config.ts'

export default mergeConfig(
  viteConfig({ mode: 'test', command: 'serve' }),
  defineConfig({
    test: {
      environment: 'jsdom',
      // Generous limit, as in playwright.config.ts: the shared CI host runs other builds meanwhile and
      // component tests that mount Vuetify went past the 5 s default under load.
      testTimeout: 20_000,
      exclude: [...configDefaults.exclude, 'e2e/**'],
      root: fileURLToPath(new URL('./', import.meta.url)),
      setupFiles: ['./src/test/setup.ts'],
      server: { deps: { inline: ['vuetify'] } },
    },
  }),
)
