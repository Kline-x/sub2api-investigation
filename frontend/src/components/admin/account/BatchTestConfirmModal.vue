<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.bulkActions.testDialogTitle')"
    width="normal"
    close-on-click-outside
    @close="emit('close')"
  >
    <div class="space-y-4">
      <div class="text-sm text-gray-600 dark:text-dark-300">
        {{ t('admin.accounts.bulkActions.testDialogHint') }}
        <span class="ml-1 font-medium">{{ t('admin.accounts.bulkActions.selected', { count: accountIds.length }) }}</span>
      </div>

      <div v-if="loading" class="text-sm text-gray-500 dark:text-dark-400">
        {{ t('admin.accounts.bulkActions.testDialogLoadingModels') }}
      </div>

      <div v-else class="space-y-3">
        <div
          v-for="row in platformRows"
          :key="row.platform"
          class="flex flex-col gap-1 sm:flex-row sm:items-center sm:gap-3"
        >
          <label class="w-32 shrink-0 text-sm font-medium text-gray-700 dark:text-dark-200">
            {{ row.platform }}
            <span class="text-xs font-normal text-gray-400">({{ row.count }})</span>
          </label>
          <select
            v-model="row.modelId"
            class="input w-full"
            :disabled="row.models.length === 0"
          >
            <option value="">
              {{ row.models.length === 0 ? t('admin.accounts.bulkActions.testDialogNoModels') : '—' }}
            </option>
            <option v-for="m in row.models" :key="m.id" :value="m.id">
              {{ m.display_name || m.id }}
            </option>
          </select>
        </div>
      </div>
    </div>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">
        {{ t('common.cancel') }}
      </button>
      <button type="button" class="btn btn-primary" :disabled="loading || submitting" @click="handleConfirm">
        {{ t('admin.accounts.bulkActions.testDialogConfirm') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { adminAPI } from '@/api/admin'
import type { ClaudeModel } from '@/types'

interface SelectedAccount {
  id: number
  platform: string
}

interface PlatformRow {
  platform: string
  count: number
  sampleAccountId: number
  models: ClaudeModel[]
  modelId: string
}

const props = defineProps<{
  show: boolean
  accounts: SelectedAccount[]
  accountIds: number[]
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'confirm', modelsByPlatform: Record<string, string>): void
}>()

const { t } = useI18n()
const loading = ref(false)
const submitting = ref(false)
const platformRows = ref<PlatformRow[]>([])

const loadPlatformModels = async () => {
  const grouped = new Map<string, SelectedAccount[]>()
  for (const acc of props.accounts) {
    const platform = (acc.platform || '').trim() || 'unknown'
    const list = grouped.get(platform) || []
    list.push(acc)
    grouped.set(platform, list)
  }

  const rows: PlatformRow[] = []
  for (const [platform, list] of grouped.entries()) {
    rows.push({
      platform,
      count: list.length,
      sampleAccountId: list[0]!.id,
      models: [],
      modelId: ''
    })
  }
  platformRows.value = rows
  loading.value = true
  try {
    await Promise.all(
      platformRows.value.map(async (row) => {
        try {
          const models = await adminAPI.accounts.getAvailableModels(row.sampleAccountId)
          row.models = models || []
          if (row.platform === 'grok') {
            const grok45 = row.models.find((m) => m.id === 'grok-4.5')
            row.modelId = grok45?.id || row.models[0]?.id || ''
          } else if (row.models.length > 0) {
            row.modelId = row.models[0]?.id || ''
          }
        } catch (error) {
          console.error('Failed to load models for platform', row.platform, error)
          row.models = []
          row.modelId = ''
        }
      })
    )
  } finally {
    loading.value = false
  }
}

watch(
  () => props.show,
  (open) => {
    if (open) {
      submitting.value = false
      void loadPlatformModels()
    } else {
      platformRows.value = []
    }
  }
)

const handleConfirm = () => {
  if (loading.value || submitting.value) return
  submitting.value = true
  const modelsByPlatform: Record<string, string> = {}
  for (const row of platformRows.value) {
    const modelId = (row.modelId || '').trim()
    if (modelId) {
      modelsByPlatform[row.platform] = modelId
    }
  }
  emit('confirm', modelsByPlatform)
}
</script>