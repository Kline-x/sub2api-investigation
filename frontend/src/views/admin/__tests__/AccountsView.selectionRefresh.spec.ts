import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'

const {
  listAccounts,
  listIDs,
  refreshCredentials,
  getBatchTodayStats,
  getAllProxies,
  getAllGroups,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listIDs: vi.fn(),
  refreshCredentials: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroups: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getAccountPatrolSettings: vi.fn().mockResolvedValue({ enabled: false, interval_minutes: 30, batch_size: 20, concurrency: 4 }),
      list: listAccounts,
      listWithEtag: vi.fn().mockResolvedValue({ notModified: true, etag: null, data: null }),
      listIDs,
      refreshCredentials,
      getBatchTodayStats
    },
    proxies: { getAll: getAllProxies },
    groups: { getAll: getAllGroups }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError, showInfo: vi.fn(), showWarning: vi.fn() })
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ token: 'test-token' }) }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const BulkBarStub = {
  props: ['selectedIds', 'filteredTotal', 'allVisibleSelected', 'allFilteredSelected', 'selectingAll'],
  emits: ['select-filtered'],
  template: '<button data-test="select-filtered" @click="$emit(\'select-filtered\')">select</button>'
}
const ActionMenuStub = {
  emits: ['refresh-token'],
  setup() {
    return { account }
  },
  template: '<button data-test="refresh-token" @click="$emit(\'refresh-token\', account)">refresh</button>'
}

const account = {
  id: 1,
  name: 'grok-account',
  platform: 'grok',
  type: 'oauth',
  status: 'active',
  schedulable: true,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z'
}

function mountView() {
  return mount(AccountsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
        DataTable: { props: ['data'], template: '<div></div>' },
        AccountBulkActionsBar: BulkBarStub,
        AccountActionMenu: ActionMenuStub,
        AccountTableActions: { template: '<div><slot /></div>' },
        AccountTableFilters: true,
        Pagination: true,
        ConfirmDialog: true,
        ImportDataModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: true,
        AccountStatsModal: true,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: true,
        EditAccountModal: true,
        BulkEditAccountModal: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: true,
        AccountUsageCell: true,
        Icon: true
      }
    }
  })
}

describe('admin AccountsView filtered selection and token refresh', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    listAccounts.mockResolvedValue({ items: [account], total: 3, page: 1, page_size: 20, pages: 1 })
    listIDs.mockResolvedValue({ ids: [1, 2, 3], total: 3 })
    getBatchTodayStats.mockResolvedValue({ stats: {} })
    getAllProxies.mockResolvedValue([])
    getAllGroups.mockResolvedValue([])
  })

  it('selects every account matching the current filters', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="select-filtered"]').trigger('click')
    await flushPromises()

    expect(listIDs).toHaveBeenCalledWith(expect.objectContaining({
      platform: '', type: '', status: '', group: '', search: '', privacy_mode: ''
    }))
    expect(wrapper.getComponent(BulkBarStub).props('selectedIds')).toEqual([1, 2, 3])
    expect(wrapper.getComponent(BulkBarStub).props('allFilteredSelected')).toBe(true)
  })

  it('shows success and ignores a duplicate refresh while the first request is pending', async () => {
    let resolveRefresh!: (value: typeof account) => void
    refreshCredentials.mockReturnValue(new Promise(resolve => { resolveRefresh = resolve }))
    const wrapper = mountView()
    await flushPromises()

    const button = wrapper.get('[data-test="refresh-token"]')
    await button.trigger('click')
    await button.trigger('click')
    expect(refreshCredentials).toHaveBeenCalledTimes(1)

    resolveRefresh(account)
    await flushPromises()
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.refreshTokenSuccess')
  })

  it('shows the backend message when refreshing a token fails', async () => {
    refreshCredentials.mockRejectedValue({ message: 'invalid_grant' })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="refresh-token"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('invalid_grant')
  })
})
