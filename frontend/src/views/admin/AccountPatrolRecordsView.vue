<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="card p-4 sm:p-6">
          <div class="flex flex-wrap items-end justify-between gap-4">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('admin.accounts.patrol.recordsTitle') }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.patrol.recordsDescription') }}
              </p>
            </div>
            <div class="flex flex-wrap items-center gap-3">
              <button type="button" class="btn btn-secondary" :disabled="loading" @click="load">
                {{ t('common.refresh') }}
              </button>
              <button type="button" class="btn btn-primary" @click="openSettings">
                {{ t('admin.accounts.patrol.open') }}
              </button>
            </div>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="records" :loading="loading" row-key="id">
          <template #cell-finished_at="{ value }">
            <span class="whitespace-nowrap text-gray-600 dark:text-gray-300">{{ formatTime(value) }}</span>
          </template>
          <template #cell-started_at="{ value }">
            <span class="whitespace-nowrap text-gray-600 dark:text-gray-300">{{ formatTime(value) }}</span>
          </template>
          <template #cell-result="{ row }">
            <span class="text-sm">
              <span class="text-emerald-600 dark:text-emerald-400">{{ row.success_count }}</span>
              /
              <span :class="row.failed_count > 0 ? 'text-red-600 dark:text-red-400' : 'text-gray-500'">
                {{ row.failed_count }}
              </span>
              <span class="text-gray-400"> ({{ row.batch_size }})</span>
            </span>
          </template>
          <template #cell-failed_account_ids="{ value }">
            <span v-if="!value || value.length === 0" class="text-gray-400">-</span>
            <span v-else class="break-all text-xs text-red-600 dark:text-red-400" :title="value.join(', ')">
              {{ value.slice(0, 12).join(', ') }}<span v-if="value.length > 12">...</span>
            </span>
          </template>
          <template #cell-settings="{ row }">
            <span class="text-xs text-gray-500">
              {{ t('admin.accounts.patrol.recordsSettingsHint', {
                interval: row.interval_minutes,
                concurrency: row.concurrency
              }) }}
            </span>
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="total > 0"
          :total="total"
          :page="page"
          :page-size="pageSize"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <AccountPatrolSettingsModal
      :show="showSettings"
      @close="showSettings = false"
      @updated="onSettingsUpdated"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import type { Column } from '@/components/common/types'
import Pagination from '@/components/common/Pagination.vue'
import AccountPatrolSettingsModal from '@/components/admin/account/AccountPatrolSettingsModal.vue'
import { adminAPI } from '@/api/admin'
import type { AccountPatrolRecord } from '@/api/admin/accounts'
import { useAppStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const records = ref<AccountPatrolRecord[]>([])
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const showSettings = ref(false)

const columns = computed<Column[]>(() => [
  { key: 'id', label: 'ID', width: '80px' },
  { key: 'started_at', label: t('admin.accounts.patrol.recordsStartedAt') },
  { key: 'finished_at', label: t('admin.accounts.patrol.recordsFinishedAt') },
  { key: 'result', label: t('admin.accounts.patrol.recordsResult') },
  { key: 'cursor_after', label: t('admin.accounts.patrol.recordsCursor') },
  { key: 'failed_account_ids', label: t('admin.accounts.patrol.recordsFailedIds') },
  { key: 'settings', label: t('admin.accounts.patrol.recordsSettings') }
])

function formatTime(value: string) {
  if (!value) return '-'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  return d.toLocaleString()
}

async function load() {
  loading.value = true
  try {
    const res = await adminAPI.accounts.listAccountPatrolRecords({
      page: page.value,
      page_size: pageSize.value
    })
    records.value = res.items || []
    total.value = res.total || 0
    page.value = res.page || page.value
    pageSize.value = res.page_size || pageSize.value
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accounts.patrol.recordsLoadFailed')))
  } finally {
    loading.value = false
  }
}

function openSettings() {
  showSettings.value = true
}

function onSettingsUpdated() {
  showSettings.value = false
  void load()
}

function handlePageChange(p: number) {
  page.value = p
  void load()
}

function handlePageSizeChange(ps: number) {
  pageSize.value = ps
  page.value = 1
  void load()
}

onMounted(() => {
  void load()
})
</script>
