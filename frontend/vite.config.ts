import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { svelte } from '@sveltejs/vite-plugin-svelte'
import { defineConfig } from 'vite'

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')

// module.prop is the single source of truth for the module version. The
// frontend reads it at build time so the About panel can never drift away
// from the version that is actually flashed to the device.
function readModuleVersion(): { version: string; versionCode: string } {
  const fallback = { version: 'v0.0.0', versionCode: '0' }
  try {
    const raw = readFileSync(resolve(repoRoot, 'module.prop'), 'utf8')
    return {
      version: raw.match(/^version=(.+)$/m)?.[1]?.trim() ?? fallback.version,
      versionCode: raw.match(/^versionCode=(.+)$/m)?.[1]?.trim() ?? fallback.versionCode,
    }
  } catch {
    return fallback
  }
}

const moduleVersion = readModuleVersion()

// https://vite.dev/config/
export default defineConfig({
  base: './',
  plugins: [svelte()],
  define: {
    __APP_VERSION__: JSON.stringify(moduleVersion.version),
    __APP_VERSION_CODE__: JSON.stringify(moduleVersion.versionCode),
  },
  server: {
    port: 5173,
    host: true,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:80',
        changeOrigin: true,
        ws: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        // NOTE: [hash] alone is enough for cache-busting. A timestamp was
        // removed here to keep builds reproducible (identical content must
        // produce identical filenames, otherwise every release churns).
        entryFileNames: `assets/[name]-[hash].js`,
        chunkFileNames: `assets/[name]-[hash].js`,
        assetFileNames: `assets/[name]-[hash].[ext]`,
        manualChunks(id) {
          if (id.includes('@xterm')) {
            return 'xterm'
          }
          if (id.includes('@lucide')) {
            return 'lucide'
          }
        },
      },
    },
  },
})
