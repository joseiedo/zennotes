import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import {
  excalidrawFonts,
  onigurumaDataUrl,
  rendererManualChunk,
  resolveRendererModulePreloads,
  sharedResolve
} from './vite.shared'

export default defineConfig({
  root: __dirname,
  // Emit relative paths in index.html so the same bundle works at the
  // domain root and under a reverse-proxy subpath (e.g. /zennotes/).
  // Runtime API + WebSocket calls derive the prefix from
  // window.__ZN_BASE_PATH__, which the Go server injects into the SPA
  // shell when ZENNOTES_BASE_PATH is set.
  base: './',
  resolve: sharedResolve,
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:7878',
        changeOrigin: true,
        ws: true
      },
      '/vault': {
        target: 'http://127.0.0.1:7878',
        changeOrigin: true
      },
      '/fs': {
        target: 'http://127.0.0.1:7878',
        changeOrigin: true
      },
      '/notes': {
        target: 'http://127.0.0.1:7878',
        changeOrigin: true
      },
      '/comments': {
        target: 'http://127.0.0.1:7878',
        changeOrigin: true
      },
      '/folders': {
        target: 'http://127.0.0.1:7878',
        changeOrigin: true
      },
      '/assets': {
        target: 'http://127.0.0.1:7878',
        changeOrigin: true
      },
      '/search': {
        target: 'http://127.0.0.1:7878',
        changeOrigin: true
      },
      '/tasks': {
        target: 'http://127.0.0.1:7878',
        changeOrigin: true
      },
      '/demo': {
        target: 'http://127.0.0.1:7878',
        changeOrigin: true
      },
      '/watch': {
        target: 'http://127.0.0.1:7878',
        changeOrigin: true,
        ws: true
      },
      '/capabilities': {
        target: 'http://127.0.0.1:7878',
        changeOrigin: true
      },
      '/version': {
        target: 'http://127.0.0.1:7878',
        changeOrigin: true
      },
      '/platform': {
        target: 'http://127.0.0.1:7878',
        changeOrigin: true
      },
      '/healthz': {
        target: 'http://127.0.0.1:7878',
        changeOrigin: true
      },
      '/assets-data': {
        target: 'http://127.0.0.1:7878',
        changeOrigin: true
      }
    }
  },
  plugins: [onigurumaDataUrl(), react(), excalidrawFonts()],
  // Typst ships a WASM compiler loaded lazily via `?url` + dynamic import; keep
  // it out of the esbuild dep pre-bundler so the wasm glue stays intact.
  optimizeDeps: {
    exclude: [
      '@myriaddreamin/typst.ts',
      '@myriaddreamin/typst-ts-web-compiler',
      '@myriaddreamin/typst-ts-renderer'
    ]
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 3500,
    modulePreload: {
      resolveDependencies: resolveRendererModulePreloads
    },
    sourcemap: false,
    rollupOptions: {
      output: {
        manualChunks: rendererManualChunk
      }
    }
  }
})
