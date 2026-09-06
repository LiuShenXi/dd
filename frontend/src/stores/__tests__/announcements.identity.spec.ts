import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const api = vi.hoisted(() => ({ list: vi.fn(), markRead: vi.fn(), getVersion: vi.fn() }))
vi.mock('@/api', () => ({ announcementsAPI: api }))

import { useAnnouncementStore } from '@/stores/announcements'

const announcement = {
  id: 1,
  title: 'Synthetic notice',
  content: 'Synthetic content',
  notify_mode: 'banner' as const,
  created_at: '2026-09-06T00:00:00Z',
  updated_at: '2026-09-06T00:00:00Z',
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

describe('announcement session isolation', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    api.list.mockResolvedValue([announcement])
    api.markRead.mockResolvedValue({ message: 'ok' })
    api.getVersion.mockResolvedValue({ version: 'v1', unread_count: 1 })
  })

  it('discards an in-flight list response after logout', async () => {
    const pending = deferred<typeof announcement[]>()
    api.list.mockReturnValueOnce(pending.promise)
    const store = useAnnouncementStore()
    store.setIdentity(101)
    const request = store.fetchAnnouncements(true)

    store.setIdentity(null)
    pending.resolve([announcement])
    await request

    expect(store.announcements).toEqual([])
    expect(store.loading).toBe(false)
  })

  it('does not apply an old mark-read completion to the next identity', async () => {
    const pending = deferred<{ message: string }>()
    const store = useAnnouncementStore()
    store.setIdentity(101)
    await store.fetchAnnouncements(true)
    api.markRead.mockReturnValueOnce(pending.promise)
    const request = store.markAsRead(1)

    store.setIdentity(202)
    pending.resolve({ message: 'ok' })
    await request

    expect(store.announcements).toEqual([])
  })

  it('refreshes the full list only when the lightweight version changes', async () => {
    const store = useAnnouncementStore()
    store.setIdentity(101)
    await store.fetchAnnouncements(true)
    api.list.mockClear()

    await store.checkForUpdates()
    expect(api.list).not.toHaveBeenCalled()

    api.getVersion.mockResolvedValueOnce({ version: 'v2', unread_count: 2 })
    await store.checkForUpdates()
    expect(api.list).toHaveBeenCalledTimes(1)
  })

  it('does not acknowledge a version until its full-list refresh succeeds', async () => {
    const store = useAnnouncementStore()
    store.setIdentity(101)
    await store.fetchAnnouncements(true)
    await store.checkForUpdates()
    api.list.mockClear()
    api.getVersion.mockResolvedValue({ version: 'v2', unread_count: 2 })
    api.list.mockRejectedValueOnce(new Error('temporary list failure')).mockResolvedValueOnce([announcement])

    await store.checkForUpdates()
    await store.checkForUpdates()

    expect(api.list).toHaveBeenCalledTimes(2)
  })

  it('keeps the newest same-session list when requests resolve out of order', async () => {
    const first = deferred<typeof announcement[]>()
    const second = deferred<typeof announcement[]>()
    const newer = { ...announcement, id: 2, title: 'Newer' }
    api.list.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const store = useAnnouncementStore()
    store.setIdentity(101)

    const firstRequest = store.fetchAnnouncements(true)
    const secondRequest = store.fetchAnnouncements(true)
    second.resolve([newer])
    await secondRequest
    first.resolve([announcement])
    await firstRequest

    expect(store.announcements.map((item) => item.id)).toEqual([2])
  })
})
