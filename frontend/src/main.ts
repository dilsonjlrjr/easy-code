import '@fontsource/roboto/400.css'
import '@fontsource/roboto/500.css'
import '@fontsource/roboto/700.css'
import './app/styles/global.css'

import { createApp } from 'vue'
import App from './app/App.vue'
import { registerPlugins } from './app/plugins'

const app = createApp(App)
registerPlugins(app)
app.mount('#app')
