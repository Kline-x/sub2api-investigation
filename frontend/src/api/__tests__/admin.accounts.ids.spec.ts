import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get } }))

import { listIDs } from '@/api/admin/accounts'

describe('admin accounts ids api', () => {
  beforeEach(() => get.mockReset())

  it('requests every account id matching the current filters', async () => {
    get.mockResolvedValue({ data: { ids: [3, 7], total: 2 } })

    const result = await listIDs({
      platform: 'grok',
      type: 'oauth',
      status: 'active',
      group: '12',
      search: 'mail',
      privacy_mode: 'unset'
    })

    expect(get).toHaveBeenCalledWith('/admin/accounts/ids', {
      params: {
        platform: 'grok',
        type: 'oauth',
        status: 'active',
        group: '12',
        search: 'mail',
        privacy_mode: 'unset'
      }
    })
    expect(result).toEqual({ ids: [3, 7], total: 2 })
  })
})
