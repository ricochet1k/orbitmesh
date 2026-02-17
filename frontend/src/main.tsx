import { render, Show } from 'solid-js/web'
import { ErrorComponentProps, RouterProvider, createRouter } from '@tanstack/solid-router'
import './index.css'

const root = document.getElementById('root')

import { routeTree } from './routeTree.gen'

const router = createRouter({ routeTree })

declare module '@tanstack/solid-router' {
  interface Register {
    router: typeof router
  }
}

function ErrorComponent(props: ErrorComponentProps) {
  return <div style={{ background: "white", border: "1px solid red", "border-radius": "10px", margin: "20px", padding: "20px" }}>
    <h2 style={{ "color": "red" }}>Error</h2>
    <pre>{'' + props.error}</pre>
    <Show when={props.info}><pre>{'' + props.info}</pre></Show>
    <button onClick={() => props.reset()}>Reset</button>
  </ div>
}

render(() => <RouterProvider router={router} defaultErrorComponent={ErrorComponent} />, root!)
