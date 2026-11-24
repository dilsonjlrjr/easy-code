import '@fontsource/inter/400.css'
import '@fontsource/inter/500.css'
import '@fontsource/inter/600.css'
import '@fontsource/inter/700.css'
import '@fontsource/inter/800.css'
import './app/styles/global.css'

import { createApp } from 'vue'
import App from './app/App.vue'
import { registerPlugins } from './app/plugins'
// After Vuetify's styles, so the canvas rules win over the widget defaults.
import './app/styles/easycode.css'

const app = createApp(App)
registerPlugins(app)
app.mount('#app')
