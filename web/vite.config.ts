import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  // The Go server embeds web/dist and serves it under /app
  // (env.ConsolePath). Router paths stay relative to this base, so the
  // admin/* routes resolve to /app/admin/* without being rewritten.
  base: '/app/',
  build: {
    rollupOptions: {
      output: {
        // Split the two heavyweight editors into their own chunks so
        // they are not pulled into the initial bundle. Vite 8 bundles
        // with Rolldown, whose manualChunks only accepts a function --
        // these groups are the Rolldown-native equivalent.
        advancedChunks: {
          groups: [
            { name: 'codemirror', test: /[\\/]node_modules[\\/](?:@codemirror[\\/]|codemirror[\\/])/ },
            { name: 'grapesjs', test: /[\\/]node_modules[\\/]grapesjs/ },
          ],
        },
      },
    },
  },
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      // The console's own api sits UNDER the SPA base, so it has to be
      // proxied explicitly - otherwise vite's history fallback answers
      // /app/api/auth/login with index.html and the login screen sees
      // HTML where it expected JSON.
      '/app/api': {
        target: 'http://localhost:3000',
        changeOrigin: true,
      },
      // Both /api/v1 (the product surface the console calls) and the
      // admin routes still on /api.
      '/api': {
        target: 'http://localhost:3000',
        changeOrigin: true,
      },
      '/tracking': {
        target: 'http://localhost:3000',
        changeOrigin: true,
      },
    },
  },
})
