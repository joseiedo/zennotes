import { resolve } from 'node:path'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import {
  excalidrawFonts,
  onigurumaDataUrl,
  rendererManualChunk,
  resolveRendererModulePreloads,
  sharedResolve
} from './vite.shared'

// Separate bundle for the opt-in public read-only site. The Go server serves it
// at /_zn/static/ and injects SEO content into public.html. No service worker,
// no manifest, and it must never import the admin entry (src/main.tsx).
export default defineConfig({
  root: __dirname,
  base: '/_zn/static/',
  publicDir: false,
  resolve: sharedResolve,
  plugins: [onigurumaDataUrl(), react(), excalidrawFonts('dist-public')],
  optimizeDeps: {
    exclude: [
      '@myriaddreamin/typst.ts',
      '@myriaddreamin/typst-ts-web-compiler',
      '@myriaddreamin/typst-ts-renderer'
    ]
  },
  build: {
    outDir: 'dist-public',
    emptyOutDir: true,
    chunkSizeWarningLimit: 3500,
    modulePreload: { resolveDependencies: resolveRendererModulePreloads },
    sourcemap: false,
    rollupOptions: {
      input: resolve(__dirname, 'public.html'),
      output: { manualChunks: rendererManualChunk }
    }
  }
})
