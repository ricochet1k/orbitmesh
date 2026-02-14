import { createRootRoute, Outlet } from '@tanstack/solid-router'
import { TanStackRouterDevtools } from '@tanstack/solid-router-devtools'
import Sidebar from '../components/Sidebar'
import AgentDock from '../components/AgentDock'
import { ErrorBoundary } from 'solid-js'

const RootLayout = () => (
  <div class="app-shell">
    <Sidebar />
    <Outlet />
    {/* <ErrorBoundary fallback={(err, reset) => <>
      <pre>{'' + err}</pre>
      <button onClick={reset}>Reset</button>
    </>}>
      <Outlet />
    </ErrorBoundary> */}
    <TanStackRouterDevtools />
    <AgentDock />
  </div>
)

export const Route = createRootRoute({ component: RootLayout })
