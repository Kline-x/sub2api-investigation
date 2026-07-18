<template>
  <div class="mb-4 flex items-center justify-between rounded-lg bg-primary-50 p-3 dark:bg-primary-900/20">
    <div class="flex flex-wrap items-center gap-2">
      <span v-if="selectedIds.length > 0" class="text-sm font-medium text-primary-900 dark:text-primary-100">
        {{ t('admin.accounts.bulkActions.selected', { count: selectedIds.length }) }}
      </span>
      <span v-else class="text-sm font-medium text-primary-900 dark:text-primary-100">
        {{ t('admin.accounts.bulkEdit.title') }}
      </span>
      <template v-if="selectedIds.length > 0">
        <button
          v-if="allVisibleSelected && !allFilteredSelected && filteredTotal > selectedIds.length"
          data-testid="select-all-filtered"
          :disabled="selectingAll"
          class="text-xs font-medium text-primary-700 hover:text-primary-800 disabled:cursor-wait disabled:opacity-60 dark:text-primary-300 dark:hover:text-primary-200"
          @click="$emit('select-filtered')"
        >
          {{ selectingAll
            ? t('admin.accounts.bulkActions.selectingAll')
            : t('admin.accounts.bulkActions.selectAllFiltered', { count: filteredTotal }) }}
        </button>
        <span v-else-if="allFilteredSelected" class="text-xs font-medium text-primary-700 dark:text-primary-300">
          {{ t('admin.accounts.bulkActions.allFilteredSelected', { count: filteredTotal }) }}
        </span>
        <button
          v-else
          class="text-xs font-medium text-primary-700 hover:text-primary-800 dark:text-primary-300 dark:hover:text-primary-200"
          @click="$emit('select-page')"
        >
          {{ t('admin.accounts.bulkActions.selectCurrentPage') }}
        </button>
        <span class="text-gray-300 dark:text-primary-800">&bull;</span>
        <button
          class="text-xs font-medium text-primary-700 hover:text-primary-800 dark:text-primary-300 dark:hover:text-primary-200"
          @click="$emit('clear')"
        >
          {{ t('admin.accounts.bulkActions.clear') }}
        </button>
      </template>
    </div>
    <div class="flex gap-2">
      <template v-if="selectedIds.length > 0">
        <button class="btn btn-danger btn-sm" @click="$emit('delete')">{{ t('admin.accounts.bulkActions.delete') }}</button>
        <button class="btn btn-secondary btn-sm" @click="$emit('reset-status')">{{ t('admin.accounts.bulkActions.resetStatus') }}</button>
        <button class="btn btn-secondary btn-sm" @click="$emit('set-error')">{{ t('admin.accounts.bulkActions.setError') }}</button>
        <button class="btn btn-secondary btn-sm" @click="$emit('test')">{{ t('admin.accounts.bulkActions.test') }}</button>
        <button class="btn btn-secondary btn-sm" @click="$emit('refresh-token')">{{ t('admin.accounts.bulkActions.refreshToken') }}</button>
        <button class="btn btn-success btn-sm" @click="$emit('toggle-schedulable', true)">{{ t('admin.accounts.bulkActions.enableScheduling') }}</button>
        <button class="btn btn-warning btn-sm" @click="$emit('toggle-schedulable', false)">{{ t('admin.accounts.bulkActions.disableScheduling') }}</button>
        <button class="btn btn-primary btn-sm" @click="$emit('edit-selected')">{{ t('admin.accounts.bulkActions.edit') }}</button>
      </template>
      <button class="btn btn-primary btn-sm" @click="$emit('edit-filtered')">
        {{ t('admin.accounts.bulkEdit.submit') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{
  selectedIds: number[]
  filteredTotal: number
  allVisibleSelected: boolean
  allFilteredSelected: boolean
  selectingAll: boolean
}>()
defineEmits(['delete', 'edit-selected', 'edit-filtered', 'clear', 'select-page', 'select-filtered', 'toggle-schedulable', 'reset-status', 'set-error', 'refresh-token', 'test'])
const { t } = useI18n()
</script>
