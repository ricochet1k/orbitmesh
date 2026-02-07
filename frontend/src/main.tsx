import { render } from 'solid-js/web'
import App from './App'
import './index.css'
import './shell.css'

const root = document.getElementById('root')

if (import.meta.env.DEV && !(root instanceof HTMLElement)) {
  throw new Error(
    'Root element not found. Did you forget to add it to your index.html? Or if using the Vue template, did you overwrite the default App.vue?',
  )
}

render(() => <App />, root!)
