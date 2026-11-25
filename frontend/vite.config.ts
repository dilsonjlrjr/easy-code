import { readFileSync } from 'node:fs'
import process from 'node:process'
import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'
import vuetify from 'vite-plugin-vuetify'

/** Placeholder the web image's nginx swaps for the nonce of each response (docker/nginx.conf). */
const CSP_NONCE_PLACEHOLDER = '__CSP_NONCE__'

/**
 * The SPA policy of the web image (docker/csp-policy.txt, ADR-19), also sent by `vite preview` so
 * the e2e suite runs under it. Preview has no per-response nonce: it uses the placeholder as the
 * nonce, and the fake content origin of e2e/support/fakeApi.ts as frame-src.
 */
function previewContentSecurityPolicy(): string {
  const template = readFileSync(new URL('./docker/csp-policy.txt', import.meta.url), 'utf8').trim()
  return template
    .replaceAll('{nonce}', CSP_NONCE_PLACEHOLDER)
    .replaceAll('{connect_src}', "'self'")
    .replaceAll('{frame_src}', process.env.E2E_CONTENT_ORIGIN ?? 'http://content.easycode.test')
}

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
  // Every script and style of index.html carries the nonce (ADR-19); the installer page is served
  // by easycode-installer, not by the web image, so it keeps none.
  html: mode === 'installer' ? {} : { cspNonce: CSP_NONCE_PLACEHOLDER },
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
    headers:
      mode === 'installer' ? {} : { 'Content-Security-Policy': previewContentSecurityPolicy() },
  },
}))
