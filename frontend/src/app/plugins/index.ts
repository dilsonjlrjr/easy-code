import type { App } from 'vue'
import { createPinia } from 'pinia'
import { router } from '@/app/router'
import { i18n } from '@/shared/i18n'
import { vuetify } from './vuetify'

export function registerPlugins(app: App): void {
  app.use(createPinia()).use(i18n).use(router).use(vuetify)
}
