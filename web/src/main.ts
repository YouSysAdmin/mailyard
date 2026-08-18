import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
// Self-hosted, because the CSP is font-src 'self' and a font CDN
// would simply be blocked. The wght entry carries the five upright
// faces only - the default entry adds five italics the console never
// uses, and these bytes ship inside the Go binary.
import '@fontsource-variable/geist/wght.css'
import '@fontsource-variable/geist-mono/wght.css'
import './assets/styles.css'

const app = createApp(App)
app.use(createPinia())
app.use(router)

app.mount('#app')
