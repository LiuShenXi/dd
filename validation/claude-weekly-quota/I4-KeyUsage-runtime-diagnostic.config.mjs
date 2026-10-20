import { defineConfig } from '../../frontend/node_modules/vitest/dist/config.js'
import vue from '../../frontend/node_modules/@vitejs/plugin-vue/dist/index.mjs'
import { fileURLToPath } from 'node:url'
const root = fileURLToPath(new URL('../../frontend', import.meta.url))
export default defineConfig({
  root,
  plugins: [vue()],
  resolve: { alias: { '@': `${root}/src`, 'vue-i18n': 'vue-i18n/dist/vue-i18n.runtime.esm-bundler.js' } },
  define: { __INTLIFY_JIT_COMPILATION__: true },
  test: { environment: 'jsdom', include: ['../validation/claude-weekly-quota/I4-KeyUsage-runtime-diagnostic.spec.ts'], testTimeout: 10000 }
})
