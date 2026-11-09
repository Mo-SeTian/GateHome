import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { readFileSync } from 'node:fs'
import path from 'path'

export default defineConfig({
  base: './',
  plugins: [vue()],
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
