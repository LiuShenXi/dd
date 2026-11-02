import { describe, expect, it } from 'vitest'
import { ref } from 'vue'
import { useQuotaMutationMutex } from '../useQuotaMutationMutex'
describe('mutation lock lifetime exceeds dialog lifetime', () => {
  it('shares the auth/target lock across replacement dialog instances and releases only its captured owner', () => {
    const target = ref('admin:1:user:A')
    const oldDialog = useQuotaMutationMutex('test', () => target.value), releaseA = oldDialog.acquire()!
    const newDialog = useQuotaMutationMutex('test', () => target.value)
    expect(newDialog.busy.value).toBe(true); expect(newDialog.acquire()).toBeNull()
    target.value = 'admin:1:user:B'; const releaseB = newDialog.acquire()!
    releaseA(); releaseA(); expect(newDialog.busy.value).toBe(true)
    releaseB(); expect(newDialog.busy.value).toBe(false)
    target.value = 'admin:1:user:A'; expect(newDialog.busy.value).toBe(false)
  })
})
