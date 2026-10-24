import { defineConfig } from 'vitest/config'
import { onigurumaDataUrl, sharedResolve } from './vite.shared'

// Unit tests for the web client (currently the public site reader). DOM tests
// opt into jsdom per file with `// @vitest-environment jsdom`, as app-core does.
export default defineConfig({
  root: __dirname,
  plugins: [onigurumaDataUrl()],
  resolve: sharedResolve,
  test: {
    include: ['src/**/*.test.ts']
  }
})
