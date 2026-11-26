import '@fontsource/inter/400.css'
import '@fontsource/inter/500.css'
import '@fontsource/inter/600.css'
import '@fontsource/inter/700.css'
import { createApp } from 'vue'
import App from './App.vue'
import { createAppI18n } from './i18n'
import './styles.css'

createApp(App).use(createAppI18n()).mount('#app')
