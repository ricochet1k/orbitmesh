import { render, Show } from 'solid-js/web'
import { ErrorComponentProps, RouterProvider, createRouter } from '@tanstack/solid-router'
import './index.css'
// import './shell.css'

const root = document.getElementById('root')

// Import the generated route tree
import { routeTree } from './routeTree.gen'

// Create a new router instance
const router = createRouter({ routeTree })

// Register the router instance for type safety
declare module '@tanstack/solid-router' {
  interface Register {
    router: typeof router
  }
}

// if (import.meta.env.DEV && !(root instanceof HTMLElement)) {
//   throw new Error(
//     'Root element not found. Did you forget to add it to your index.html? Or if using the Vue template, did you overwrite the default App.vue?',
//   )
// }

function ErrorComponent(props: ErrorComponentProps) {
  return <div style={{ background: "white", border: "1px solid red", "border-radius": "10px", margin: "20px", padding: "20px" }}>
    <h2 style={{ "color": "red" }}>Error</h2>
    <pre>{'' + props.error}</pre>
    <Show when={props.info}><pre>{'' + props.info}</pre></Show>
    <button onClick={() => props.reset()}>Reset</button>
  </ div>
}

render(() => <RouterProvider router={router} defaultErrorComponent={ErrorComponent} />, root!)
