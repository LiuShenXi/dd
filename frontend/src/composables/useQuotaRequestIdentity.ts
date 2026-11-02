import { onBeforeUnmount, watch } from 'vue'

/** Check the exact caller identity in success, catch and finally, including edits. */
export function useQuotaRequestIdentity(identity: () => string) {
  let generation = 0
  const sequences = new Map<string, number>()
  function invalidate() { generation++; sequences.clear() }
  // Returning A → B → A must not revive a response captured during the first A.
  watch(identity, invalidate, { flush: 'sync' })
  function capture(channel = 'default') {
    const scope = identity(), epoch = generation
    const sequence = (sequences.get(channel) ?? 0) + 1
    sequences.set(channel, sequence)
    return () => epoch === generation && scope === identity() && sequences.get(channel) === sequence
  }
  onBeforeUnmount(invalidate)
  return { capture, invalidate }
}
