// Read-only source diagnostic. No browser or actual API is used.
import { afterEach, expect, it, vi } from '../../frontend/node_modules/vitest'
import { mount, flushPromises } from '../../frontend/node_modules/@vue/test-utils'
import { createPinia, setActivePinia } from '../../frontend/node_modules/pinia'
import { createI18n } from '../../frontend/node_modules/vue-i18n/dist/vue-i18n.runtime.esm-bundler.js'
import { createRouter, createMemoryHistory } from '../../frontend/node_modules/vue-router'
import KeyUsageView from '../../frontend/src/views/KeyUsageView.vue'
import { useAppStore } from '../../frontend/src/stores/app'
import en from '../../frontend/src/i18n/locales/en'

afterEach(() => { vi.unstubAllGlobals() })
it('initial mount uses real stores/i18n/LocaleSwitcher/Icon without query data', async () => {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }))
  const pinia = createPinia(); setActivePinia(pinia)
  const store = useAppStore()
  store.publicSettingsLoaded = true
  store.cachedPublicSettings = { site_name: 'Synthetic', site_logo: '', doc_url: '', subscription_enabled: true } as never
  const i18n = createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/key-usage', component: KeyUsageView }, { path: '/home', component: { template: '<div>home</div>' } }] })
  await router.push('/key-usage'); await router.isReady()
  const errors: string[] = []
  const wrapper = mount(KeyUsageView, { global: { plugins: [pinia, i18n, router], config: { errorHandler: error => { errors.push(String(error)) } } } })
  await flushPromises()
  expect(errors).toEqual([])
  expect(wrapper.find('main').exists()).toBe(true)
  expect(wrapper.find('input[type="password"]').exists()).toBe(true)
  expect(wrapper.text()).toContain('API Key')
  wrapper.unmount()
})
