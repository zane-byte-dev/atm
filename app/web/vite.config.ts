import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: { sourcemap: false },
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    cors: false,
    // Omit a browser host/port: Vite derives them from /@vite/client served
    // through Go, including when `atm serve --port 0` picks a free port.
    ws: { path: '/__atm_hmr' },
  },
})
