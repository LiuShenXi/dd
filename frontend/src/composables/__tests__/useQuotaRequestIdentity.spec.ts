import { describe, expect, it } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { useQuotaRequestIdentity } from '../useQuotaRequestIdentity'
describe('quota caller identities', () => {
  it('does not revive old A after synchronous A → B → A and isolates request channels', () => {
    const scope = ref('A')
    let identity!: ReturnType<typeof useQuotaRequestIdentity>
    const wrapper = mount(defineComponent({ setup() { identity = useQuotaRequestIdentity(() => scope.value); return () => h('div') } }))
    const firstA = identity.capture('preview'), read = identity.capture('read')
    expect(firstA()).toBe(true); expect(read()).toBe(true)
    const newerA = identity.capture('preview'); expect(firstA()).toBe(false); expect(read()).toBe(true)
    scope.value = 'B'; scope.value = 'A'; expect(newerA()).toBe(false); expect(read()).toBe(false)
    const final = identity.capture(); wrapper.unmount(); expect(final()).toBe(false)
  })
})
