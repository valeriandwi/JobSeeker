/// <reference types="vitest/config" />
import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig(({ mode }) => {
  // Read the proxy target from web/.env* without needing process typings.
  const env = loadEnv(mode, '.', 'VITE_')
  const apiTarget = env.VITE_API_PROXY_TARGET || 'http://127.0.0.1:8080'

  return {
    plugins: [react()],
    server: {
      port: 5173,
      // Same-origin development: the browser only ever talks to
      // http://localhost:5173, so the session cookie is first-party and
      // SameSite=Lax works. Pointing the SPA at the API on a different host
      // (app on localhost, API on 127.0.0.1) makes every request cross-site and
      // the browser silently refuses to send the session cookie — which looks
      // exactly like "login does nothing".
      proxy: {
        '/api': { target: apiTarget, changeOrigin: false },
        '/healthz': { target: apiTarget, changeOrigin: false },
        '/readyz': { target: apiTarget, changeOrigin: false },
      },
    },
    test: {
      globals: true,
      environment: 'jsdom',
      setupFiles: ['./src/test/setup.ts'],
      css: false,
    },
  }
})
