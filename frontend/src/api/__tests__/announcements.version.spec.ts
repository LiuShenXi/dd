import { describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, post: vi.fn() } }))

import { announcementsAPI } from '@/api'

describe('announcement version API', () => {
  it('uses the lightweight version endpoint', async () => {
    get.mockResolvedValue({ data: { version: 'v3', unread_count: 2 } })
    await announcementsAPI.getVersion()
    expect(get).toHaveBeenCalledWith('/announcements/version')
  })
})
