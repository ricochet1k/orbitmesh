import { defineConfig } from 'vite'
import solid from 'vite-plugin-solid'
import { tanstackRouter } from '@tanstack/router-plugin/vite'

const disableWsProxy =
  process.env.VITE_DISABLE_WS_PROXY === "1" ||
  process.env.VITE_DISABLE_WS_PROXY === "true"

const apiProxyTarget = process.env.VITE_API_PROXY_TARGET || 'http://localhost:8080'

export default defineConfig({
  plugins: [
    tanstackRouter({
      target: 'solid',
      autoCodeSplitting: true,
    }),
    solid(),
  ],
  server: {
    port: 3000,
    host: '127.0.0.1',
    proxy: {
      '/api': {
        target: apiProxyTarget,
        changeOrigin: true,
        ws: !disableWsProxy,
      },
    },
  },
  build: {
    target: 'esnext',
  },
})
