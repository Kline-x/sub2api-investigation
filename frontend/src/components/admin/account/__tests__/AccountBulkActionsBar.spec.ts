import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountBulkActionsBar from '../AccountBulkActionsBar.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, params?: { count?: number }) => `${key}:${params?.count ?? ''}` })
}))

describe('AccountBulkActionsBar filtered selection', () => {
  it('offers selecting every filtered account after the visible page is selected', async () => {
    const wrapper = mount(AccountBulkActionsBar, {
      props: {
        selectedIds: [1, 2],
        filteredTotal: 27,
        allVisibleSelected: true,
        allFilteredSelected: false,
        selectingAll: false
      }
    })

    const button = wrapper.get('[data-testid="select-all-filtered"]')
    expect(button.text()).toContain('27')
    await button.trigger('click')
    expect(wrapper.emitted('select-filtered')).toHaveLength(1)
  })
})
