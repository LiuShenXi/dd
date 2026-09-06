import { computed, onBeforeUnmount, ref } from 'vue'

export function useServerClock(tickMs = 1000) {
  const serverEpochAtSync = ref<number | null>(null)
  const monotonicAtSync = ref<number | null>(null)
  const tick = ref(0)
  let timer: ReturnType<typeof setInterval> | null = null

  const nowMs = computed(() => {
    void tick.value
    if (serverEpochAtSync.value === null || monotonicAtSync.value === null) return Date.now()
    return serverEpochAtSync.value + Math.max(0, performance.now() - monotonicAtSync.value)
  })

  function sync(serverNow: string) {
    const parsed = Date.parse(serverNow)
    if (!Number.isFinite(parsed)) return
    serverEpochAtSync.value = parsed
    monotonicAtSync.value = performance.now()
    if (!timer) {
      timer = setInterval(() => {
        tick.value += 1
      }, tickMs)
    }
  }

  function stop() {
    if (timer) clearInterval(timer)
    timer = null
  }

  onBeforeUnmount(stop)

  return { nowMs, sync, stop }
}
