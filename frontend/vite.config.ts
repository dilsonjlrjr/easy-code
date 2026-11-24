import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'
import vuetify from 'vite-plugin-vuetify'

// https://vite.dev/config/
// `vite build --mode installer` builds only the installation wizard (installer.html, ADR-22) into
// dist-installer/, served by easycode-installer; the web image never carries it.
export default defineConfig(({ mode }) => ({
  plugins: [vue(), vuetify({ autoImport: true })],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build:
    mode === 'installer'
      ? {
          outDir: 'dist-installer',
          rollupOptions: { input: fileURLToPath(new URL('./installer.html', import.meta.url)) },
        }
      : {},
  server: {
    host: true,
    port: 5173,
  },
  preview: {
    host: true,
    port: 4173,
  },
}))
