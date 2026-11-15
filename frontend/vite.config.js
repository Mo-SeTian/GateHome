import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { readFileSync } from 'node:fs'
import path from 'path'

export default defineConfig({
  base: './',
  plugins: [vue(), {
    name: 'vue-router-edge-focus-fix',
    enforce: 'pre',
    transform(code, id) {
      if (!id.split('?')[0].endsWith('/vue-router/dist/vue-router.mjs')) return
      // Backport vuejs/router#2704: hidden-page history writes restore Edge windows.
      const listener = 'document.addEventListener("visibilitychange", beforeUnloadListener);'
      if (!code.includes(listener)) throw new Error('Review the Vue Router Edge focus backport before upgrading')
      return {
        code: code.replace(listener, '').replace('document.removeEventListener("visibilitychange", beforeUnloadListener);', ''),
        map: null
      }
    }
  }],
  optimizeDeps: { exclude: ['vue-router'] },
  define: {
    __GATEHOME_VERSION__: JSON.stringify(readFileSync(new URL('../VERSION', import.meta.url), 'utf8').trim())
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src')
    }
  },
  server: {
    port: 5173,
    host: '127.0.0.1',
    proxy: {
      '/healthz': {
        target: process.env.GATEHOME_API_TARGET || 'http://127.0.0.1:16666',
        changeOrigin: false
      },
      '/china-outline.svg': {
        target: process.env.GATEHOME_API_TARGET || 'http://127.0.0.1:16666',
        changeOrigin: false
      },
      '/api/': {
        target: process.env.GATEHOME_API_TARGET || 'http://127.0.0.1:16666',
        changeOrigin: false
      },
      '/sunpanel/': {
        target: process.env.GATEHOME_API_TARGET || 'http://127.0.0.1:16666',
        changeOrigin: false
      },
      '/mcp': {
        target: process.env.GATEHOME_API_TARGET || 'http://127.0.0.1:16666',
        changeOrigin: false
      }
    }
  }
})
