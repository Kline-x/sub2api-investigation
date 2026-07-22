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

  it('edits only selected accounts when selection is non-empty', async () => {
    const wrapper = mount(AccountBulkActionsBar, {
      props: {
        selectedIds: [1, 2],
        filteredTotal: 27,
        allVisibleSelected: false,
        allFilteredSelected: false,
        selectingAll: false
      }
    })

    const buttons = wrapper.findAll('button')
    const editSelected = buttons.find((b) => b.text().includes('bulkActions.edit'))
    expect(editSelected).toBeTruthy()
    // Must not show filter-based bulk edit while selection exists
    const editFiltered = buttons.find((b) => b.text().includes('bulkActions.editFiltered'))
    expect(editFiltered).toBeUndefined()
    await editSelected!.trigger('click')
    expect(wrapper.emitted('edit-selected')).toHaveLength(1)
    expect(wrapper.emitted('edit-filtered')).toBeUndefined()
  })

  it('offers filter-based bulk edit only when nothing is selected', async () => {
    const wrapper = mount(AccountBulkActionsBar, {
      props: {
        selectedIds: [],
        filteredTotal: 27,
        allVisibleSelected: false,
        allFilteredSelected: false,
        selectingAll: false
      }
    })

    const editFiltered = wrapper.findAll('button').find((b) => b.text().includes('bulkActions.editFiltered'))
    expect(editFiltered).toBeTruthy()
    await editFiltered!.trigger('click')
    expect(wrapper.emitted('edit-filtered')).toHaveLength(1)
  })
