import '@fontsource/roboto/400.css'
import '@fontsource/roboto/500.css'
import '@fontsource/roboto/700.css'
import '@/app/styles/global.css'

import { createPinia } from 'pinia'
import { createApp } from 'vue'
import { vuetify } from '@/app/plugins/vuetify'
import { i18n } from '@/shared/i18n'
import InstallerApp from './InstallerApp.vue'

// Same plugins as the web (theme, messages, preferences), without the router or the session.
createApp(InstallerApp).use(createPinia()).use(i18n).use(vuetify).mount('#app')
