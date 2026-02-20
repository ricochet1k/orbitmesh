import { createFileRoute, redirect } from '@tanstack/solid-router'

export const Route = createFileRoute('/settings/')({
  beforeLoad: () => {
    throw redirect({ to: '/settings/providers' })
  },
  component: () => null,
})
