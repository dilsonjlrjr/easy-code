import '@fontsource/inter/400.css'
import '@fontsource/inter/500.css'
import '@fontsource/inter/600.css'
import '@fontsource/inter/700.css'
import '@fontsource/inter/800.css'
import '@/app/styles/global.css'

import { createPinia } from 'pinia'
import { createApp } from 'vue'
import { vuetify } from '@/app/plugins/vuetify'
// After Vuetify's styles, so the canvas rules win over the widget defaults.
import '@/app/styles/easycode.css'
import { i18n } from '@/shared/i18n'
import InstallerApp from './InstallerApp.vue'

// Same plugins as the web (theme, messages, preferences), without the router or the session.
createApp(InstallerApp).use(createPinia()).use(i18n).use(vuetify).mount('#app')
