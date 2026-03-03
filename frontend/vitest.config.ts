import { defineConfig } from 'vitest/config'
import solid from 'vite-plugin-solid'

export default defineConfig({
  plugins: [solid()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['src/tests/setup.ts'],
    globalSetup: ['src/tests/global-setup.ts'],
    include: ['src/**/*.test.{ts,tsx}', 'tests/helpers/**/*.test.ts'],
    exclude: ['tests/e2e/**', 'tests/ui/**'],
    testTimeout: 4000,
    hookTimeout: 4000,
    server: {
      deps: {
        inline: [/^@tanstack\//],
      },
    },
  },
})
