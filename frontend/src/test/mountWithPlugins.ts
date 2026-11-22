import { mount, type ComponentMountingOptions } from '@vue/test-utils'
import { createPinia } from 'pinia'
import type { Component } from 'vue'
import { vuetify } from '@/app/plugins/vuetify'
import { i18n } from '@/shared/i18n'

/** Mounts a component with Vuetify, vue-i18n (pt-BR) and a fresh Pinia, as the application does. */
export function mountWithPlugins<TComponent extends Component>(
  component: TComponent,
  options: ComponentMountingOptions<TComponent> = {},
) {
  return mount(component, {
    ...options,
    global: { plugins: [createPinia(), i18n, vuetify], ...options.global },
  })
}
