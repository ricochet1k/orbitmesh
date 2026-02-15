import { defineConfig } from 'vitest/config'
import solid from 'vite-plugin-solid'

export default defineConfig({
  plugins: [solid()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: [],
    include: ['src/**/*.test.{ts,tsx}'],
    exclude: ['tests/**'],
    testTimeout: 4000,
    hookTimeout: 4000,
    server: {
      deps: {
        inline: [/^@tanstack\//],
      },
    },
  },
})
