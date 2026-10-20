import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import SubscriptionQuotaRolloutModal from '../SubscriptionQuotaRolloutModal.vue'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types'
import type { QuotaRolloutAllocation, QuotaRolloutConversion, QuotaRolloutState } from '@/types/subscriptionQuotaRollout'
import { deferred, quotaTerm } from '@/__tests__/subscriptionQuotaFixtures'
import { rolloutState } from '@/__tests__/subscriptionQuotaRolloutFixtures'
import en from '@/i18n/locales/en/subscriptionQuota'
import zh from '@/i18n/locales/zh/subscriptionQuota'
const api = vi.hoisted(() => ({ state: vi.fn(), maintenance: vi.fn(), preview: vi.fn(), convert: vi.fn() }))
const translations = vi.hoisted(() => ({ locale: 'en' }))
vi.mock('@/api/admin/subscriptionQuotaRollout', () => ({ adminSubscriptionQuotaRolloutAPI: api }))
function message(key: string): string | undefined {
  const source: unknown = translations.locale === 'en' ? en : zh
  return key.split('.').reduce<unknown>((value, part) => (value as Record<string, unknown>)?.[part], source) as string | undefined
}
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => message(key) || key, te: (key: string) => !!message(key) }) }))
const group = { id: 6, name: 'Claude', platform: 'anthropic' as const }
let wrappers: VueWrapper[] = []
async function setup() {
  const wrapper = mount(SubscriptionQuotaRolloutModal, { props: { show: true, group }, global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } } } })
  wrappers.push(wrapper); await flushPromises(); return wrapper
}
async function allocate(wrapper: VueWrapper, quota = '100.00000001') {
  await wrapper.get('[data-testid="rollout-add-user-42"]').trigger('click')
  await wrapper.get('[data-testid="rollout-user-quota"]').setValue(quota)
}
async function previewAndConfirm(wrapper: VueWrapper) {
  await wrapper.get('[data-testid="rollout-allocation-form"]').trigger('submit'); await flushPromises()
  await wrapper.get('[data-testid="rollout-confirmed"]').setValue(true)
}
const converted = (): QuotaRolloutConversion => ({ state: rolloutState({ quota_mode: 'term', converted: true, revision: 2, allocations: [{ user_id: 42, quota_usd: '100.00000001' }] }), terms: [quotaTerm()], replayed: false })
beforeEach(() => {
  vi.resetAllMocks(); translations.locale = 'en'; setActivePinia(createPinia()); useAuthStore().user = { id: 1 } as User
  api.state.mockResolvedValue(rolloutState())
  api.preview.mockImplementation(async (_: number, allocations: QuotaRolloutAllocation[]) => rolloutState({ allocations, preview_token: 'preview-1' }))
  api.convert.mockResolvedValue(converted())
})
afterEach(() => { wrappers.forEach(wrapper => wrapper.unmount()); wrappers = [] })
describe('controlled Claude rollout', () => {
  it('lists Key owners without selecting or copying any allowance, even if GET contains an old preview', async () => {
    api.state.mockResolvedValue(rolloutState({ allocations: [{ user_id: 42, quota_usd: '999.00000000' }] }))
    const wrapper = await setup()
    expect(wrapper.findAll('[data-testid="rollout-allocation-row"]')).toHaveLength(0)
    expect(wrapper.find('[data-testid="rollout-legacy-orders-warning"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('User A'); expect(wrapper.text()).toContain('User B')
    await wrapper.get('[data-testid="rollout-add-user-42"]').trigger('click')
    expect((wrapper.get('[data-testid="rollout-user-quota"]').element as HTMLInputElement).value).toBe('')
    expect(wrapper.get('[data-testid="rollout-preview"]').attributes('disabled')).toBeDefined()
    expect(api.preview).not.toHaveBeenCalled(); expect(api.convert).not.toHaveBeenCalled()
  })
  it('requires explicit review even for a zero-card conversion and sends [] rather than omitting allocations', async () => {
    const wrapper = await setup()
    await wrapper.get('[data-testid="rollout-allocation-form"]').trigger('submit'); await flushPromises()
    expect(api.preview).toHaveBeenCalledWith(6, [])
    expect(wrapper.get('[data-testid="rollout-convert"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="rollout-preview-result"]').text()).toContain('zero cards')
    await wrapper.get('[data-testid="rollout-confirmed"]').setValue(true); await wrapper.get('[data-testid="rollout-convert"]').trigger('click'); await flushPromises()
    expect(api.convert.mock.calls[0][1]).toEqual({ allocations: [], expected_revision: 1, preview_token: 'preview-1' })
  })
  it.each(['0', '1e2', '1.123456789'])('rejects invalid allocation %s without preview', async quota => {
    const wrapper = await setup(); await allocate(wrapper, quota)
    await wrapper.get('[data-testid="rollout-allocation-form"]').trigger('submit')
    expect(api.preview).not.toHaveBeenCalled()
  })
  it('rejects duplicate and unsafe user IDs before a preview', async () => {
    const wrapper = await setup(); await allocate(wrapper)
    await wrapper.get('[data-testid="rollout-add-row"]').trigger('click')
    await wrapper.findAll('[data-testid="rollout-user-id"]')[1].setValue('42'); await wrapper.findAll('[data-testid="rollout-user-quota"]')[1].setValue('300')
    await wrapper.get('[data-testid="rollout-allocation-form"]').trigger('submit'); expect(api.preview).not.toHaveBeenCalled()
    await wrapper.findAll('[data-testid="rollout-user-id"]')[1].setValue('9007199254740992')
    await wrapper.get('[data-testid="rollout-allocation-form"]').trigger('submit'); expect(api.preview).not.toHaveBeenCalled()
  })
  it('preserves the exact payload/key on network retry; edited allocations require a new preview and key', async () => {
    api.convert.mockRejectedValueOnce(new Error('offline')).mockRejectedValueOnce(new Error('offline again'))
    const wrapper = await setup(); await allocate(wrapper); await previewAndConfirm(wrapper)
    await wrapper.get('[data-testid="rollout-convert"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-testid="rollout-convert"]').trigger('click'); await flushPromises()
    expect(api.convert.mock.calls[0]).toEqual(api.convert.mock.calls[1])
    expect(api.convert.mock.calls[0][1]).toEqual({ allocations: [{ user_id: 42, quota_usd: '100.00000001' }], expected_revision: 1, preview_token: 'preview-1' })
    await wrapper.get('[data-testid="rollout-user-quota"]').setValue('300.00000003')
    expect(wrapper.find('[data-testid="rollout-preview-result"]').exists()).toBe(false)
    await previewAndConfirm(wrapper); await wrapper.get('[data-testid="rollout-convert"]').trigger('click'); await flushPromises()
    expect(api.convert.mock.calls[2][2]).not.toBe(api.convert.mock.calls[0][2])
  })
  it('keeps converted rights under maintenance until a separate explicit release', async () => {
    const wrapper = await setup(); await allocate(wrapper); await previewAndConfirm(wrapper)
    await wrapper.get('[data-testid="rollout-convert"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-testid="rollout-converted"]').text()).toContain('maintenance remains enabled')
    expect(api.maintenance).not.toHaveBeenCalled()
    api.maintenance.mockResolvedValue(rolloutState({ quota_mode: 'term', converted: true, maintenance: false, group_status: 'active', revision: 3 }))
    await wrapper.get('[data-testid="rollout-reason"]').setValue('verified activation')
    await wrapper.get('[data-testid="rollout-maintenance"]').trigger('click'); await flushPromises()
    expect(api.maintenance.mock.calls[0][1]).toEqual({ enabled: false, expected_revision: 2, reason: 'verified activation' })
  })
  it.each(['en', 'zh'])('renders known blockers in %s and preserves unknown values; blocked previews cannot convert', async locale => {
    translations.locale = locale
    api.preview.mockResolvedValue(rolloutState({ protected_pending: 2, pending_orders: 1, blockers: ['PROTECTED_REQUESTS_PENDING', 'ORDERS_PENDING', 'REPLICA_CONFIG_MISMATCH', 'FUTURE_BLOCKER'], preview_token: 'blocked-token' }))
    const wrapper = await setup(); await allocate(wrapper)
    await wrapper.get('[data-testid="rollout-allocation-form"]').trigger('submit'); await flushPromises()
    expect(wrapper.get('[data-testid="rollout-blockers"]').text()).toContain(locale === 'en' ? 'Protected requests are still waiting' : '仍有受保护请求')
    expect(wrapper.text()).toContain('FUTURE_BLOCKER')
    expect(wrapper.get('[data-testid="rollout-confirmed"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="rollout-convert"]').trigger('click'); expect(api.convert).not.toHaveBeenCalled()
  })
  it('preserves input after a stale-preview conflict and requires fresh confirmation', async () => {
    api.convert.mockRejectedValueOnce({ status: 409 })
    const wrapper = await setup(); await allocate(wrapper, '333.00000003'); await previewAndConfirm(wrapper)
    api.state.mockResolvedValue(rolloutState({ revision: 2 }))
    await wrapper.get('[data-testid="rollout-convert"]').trigger('click'); await flushPromises()
    expect((wrapper.get('[data-testid="rollout-user-quota"]').element as HTMLInputElement).value).toBe('333.00000003')
    expect(wrapper.find('[data-testid="rollout-preview-result"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="rollout-error"]').text()).toContain('state changed')
    expect(api.state).toHaveBeenCalledTimes(2)
  })
  it('keeps both live instances visible when replica IDs repeat and preserves the duplicate blocker', async () => {
    const replica = rolloutState().replicas[0]
    const duplicateState = rolloutState({
      replicas: [
        { ...replica, heartbeat_at: '2026-10-20T00:01:00Z', claude_default_concurrency: 3 },
        { ...replica, heartbeat_at: '2026-10-20T00:02:00Z', claude_default_concurrency: 7 }
      ],
      blockers: ['REPLICA_DUPLICATE']
    })
    api.state.mockResolvedValue(duplicateState)
    api.preview.mockResolvedValue(duplicateState)
    const wrapper = await setup()
    let instances = wrapper.findAll('[data-testid="rollout-replica"]')
    expect(instances).toHaveLength(2)
    expect(instances[0].text()).toContain('test-one'); expect(instances[1].text()).toContain('test-one')
    expect(instances[0].text()).toContain(':01'); expect(instances[1].text()).toContain(':02')
    expect(instances[0].text()).toContain('Independent Claude default concurrency: 3')
    expect(instances[1].text()).toContain('Independent Claude default concurrency: 7')
    await wrapper.get('[data-testid="rollout-allocation-form"]').trigger('submit'); await flushPromises()
    instances = wrapper.findAll('[data-testid="rollout-replica"]')
    expect(instances).toHaveLength(2)
    expect(wrapper.get('[data-testid="rollout-blockers"]').text()).toContain('same replica ID')
    expect(wrapper.get('[data-testid="rollout-convert"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="rollout-convert"]').trigger('click')
    expect(api.convert).not.toHaveBeenCalled()
  })
  it.each(['en', 'zh'])('shows historical terminal orders as a non-blocking warning in %s, without inferring cards', async locale => {
    translations.locale = locale
    api.state.mockResolvedValue(rolloutState({ legacy_terminal_orders: 4 }))
    api.preview.mockResolvedValue(rolloutState({ legacy_terminal_orders: 4, preview_token: 'reviewed-empty-set' }))
    const wrapper = await setup()
    const warning = wrapper.get('[data-testid="rollout-legacy-orders-warning"]')
    expect(warning.text()).toContain('4')
    expect(warning.text()).toContain(locale === 'en' ? 'manual review' : '人工核查')
    expect(warning.text()).toContain(locale === 'en' ? 'will not infer an allowance or automatically issue a card' : '不会自动推算额度或发卡')
    expect(wrapper.findAll('[data-testid="rollout-allocation-row"]')).toHaveLength(0)
    await previewAndConfirm(wrapper)
    expect(wrapper.get('[data-testid="rollout-preview-result"]').text()).toContain(locale === 'en' ? 'manual review' : '人工核查')
    expect(wrapper.get('[data-testid="rollout-convert"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="rollout-convert"]').trigger('click'); await flushPromises()
    expect(api.convert.mock.calls[0][1].allocations).toEqual([])
  })
  it.each(['success', 'failure'])('ignores old preview %s/finally after changing groups', async outcome => {
    const old = deferred<QuotaRolloutState>(), newer = deferred<QuotaRolloutState>()
    api.preview.mockReturnValueOnce(old.promise).mockReturnValueOnce(newer.promise)
    const wrapper = await setup(); await allocate(wrapper); await wrapper.get('[data-testid="rollout-allocation-form"]').trigger('submit')
    api.state.mockResolvedValue(rolloutState({ group_id: 7 }))
    await wrapper.setProps({ group: { ...group, id: 7 } }); await flushPromises(); await allocate(wrapper, '300'); await wrapper.get('[data-testid="rollout-allocation-form"]').trigger('submit')
    if (outcome === 'success') old.resolve(rolloutState({ preview_token: 'old' })); else old.reject(new Error('old failure'))
    await flushPromises()
    expect(wrapper.find('[data-testid="rollout-preview-result"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="rollout-preview"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).not.toContain('old failure')
    newer.reject(new Error('current failure')); await flushPromises()
    expect(wrapper.get('[data-testid="rollout-error"]').text()).toContain('current failure')
  })
  it('does not unlock a sent mutation on close/reopen; refreshes committed state once it settles', async () => {
    const pending = deferred<QuotaRolloutConversion>(); api.convert.mockReturnValueOnce(pending.promise)
    const wrapper = await setup(); await allocate(wrapper); await previewAndConfirm(wrapper); await wrapper.get('[data-testid="rollout-convert"]').trigger('click')
    await wrapper.setProps({ show: false }); await wrapper.setProps({ show: true }); await flushPromises()
    expect(wrapper.get('[data-testid="rollout-refresh"]').attributes('disabled')).toBeDefined()
    api.state.mockResolvedValue(converted().state); pending.resolve(converted()); await flushPromises()
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(wrapper.get('[data-testid="rollout-converted"]').text()).toContain('maintenance remains enabled')
    expect(api.state).toHaveBeenCalledTimes(2)
  })
  it('retries an unchanged maintenance request with its original operation key', async () => {
    api.state.mockResolvedValue(rolloutState({ maintenance: false, group_status: 'active' }))
    api.maintenance.mockRejectedValue(new Error('offline'))
    const wrapper = await setup(); await wrapper.get('[data-testid="rollout-reason"]').setValue('approved maintenance')
    await wrapper.get('[data-testid="rollout-maintenance"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-testid="rollout-maintenance"]').trigger('click'); await flushPromises()
    expect(api.maintenance.mock.calls[0]).toEqual(api.maintenance.mock.calls[1])
  })
})
