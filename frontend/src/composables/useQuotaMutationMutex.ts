import { computed, reactive } from 'vue'

// A closed dialog does not cancel a mutation. Keep its lock across unmounts.
const pending = reactive(new Map<string, symbol>())
export function useQuotaMutationMutex(namespace: string, identity: () => string) {
  const key = () => `${namespace}:${identity()}`
  const busy = computed(() => pending.has(key()))
  function acquire(): (() => void) | null {
    const scope = key()
    if (pending.has(scope)) return null
    const token = Symbol(scope)
    pending.set(scope, token)
    return () => { if (pending.get(scope) === token) pending.delete(scope) }
  }
  return { busy, acquire }
}
