import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import 'katex/dist/katex.min.css'
import './style.css'

createApp(App).use(router).mount('#app')
