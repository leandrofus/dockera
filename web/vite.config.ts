import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// `bun run dev` proxies the API to a running `dockera serve` on :9900.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': { target: 'http://127.0.0.1:9900', ws: true, changeOrigin: false },
    },
  },
  build: { outDir: 'dist', emptyOutDir: true, chunkSizeWarningLimit: 1200 },
})
