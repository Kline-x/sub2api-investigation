<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AccountPatrolSettings } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{
  show: boolean
}>()

const emit = defineEmits<{
  close: []
  updated: [settings: AccountPatrolSettings]
}>()

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const saving = ref(false)
const form = ref<AccountPatrolSettings>({
  enabled: false,
  interval_minutes: 30,
  batch_size: 20,
  concurrency: 4
})

const canSave = computed(() => !loading.value && !saving.value)

const load = async () => {
  loading.value = true
  try {
    form.value = await adminAPI.accounts.getAccountPatrolSettings()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accounts.patrol.loadFailed')))
  } finally {
    loading.value = false
  }
}

const save = async () => {
  saving.value = true
  try {
    const updated = await adminAPI.accounts.updateAccountPatrolSettings({
      enabled: !!form.value.enabled,
      interval_minutes: Number(form.value.interval_minutes) || 30,
      batch_size: Number(form.value.batch_size) || 20,
      concurrency: Number(form.value.concurrency) || 4
    })
    form.value = updated
    emit('updated', updated)
    appStore.showSuccess(t('admin.accounts.patrol.saved'))
    emit('close')
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accounts.patrol.saveFailed')))
  } finally {
    saving.value = false
  }
}

watch(
  () => props.show,
  (v) => {
    if (v) load()
  }
)
</script>

<template>
  <div v-if="show" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" @click.self="emit('close')">
    <div class="w-full max-w-md rounded-xl bg-white p-5 shadow-xl dark:bg-dark-800">
      <div class="mb-4 flex items-center justify-between">
        <h3 class="text-lg font-semibold text-gray-900 dark:text-gray-100">
          {{ t('admin.accounts.patrol.title') }}
        </h3>
        <button class="text-gray-400 hover:text-gray-600" @click="emit('close')">×</button>
      </div>

      <p class="mb-4 text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.accounts.patrol.description') }}
      </p>

      <div v-if="loading" class="py-8 text-center text-sm text-gray-400">
        {{ t('common.loading') }}
      </div>

      <div v-else class="space-y-4">
        <label class="flex items-center justify-between gap-3">
          <span class="text-sm text-gray-700 dark:text-gray-200">{{ t('admin.accounts.patrol.enabled') }}</span>
          <input v-model="form.enabled" type="checkbox" class="h-4 w-4" />
        </label>

        <label class="block">
          <span class="mb-1 block text-sm text-gray-700 dark:text-gray-200">{{ t('admin.accounts.patrol.intervalMinutes') }}</span>
          <input
            v-model.number="form.interval_minutes"
            type="number"
            min="5"
            max="1440"
            class="input w-full"
          />
        </label>

        <label class="block">
          <span class="mb-1 block text-sm text-gray-700 dark:text-gray-200">{{ t('admin.accounts.patrol.batchSize') }}</span>
          <input
            v-model.number="form.batch_size"
            type="number"
            min="1"
            max="100"
            class="input w-full"
          />
        </label>

        <label class="block">
          <span class="mb-1 block text-sm text-gray-700 dark:text-gray-200">{{ t('admin.accounts.patrol.concurrency') }}</span>
          <input
            v-model.number="form.concurrency"
            type="number"
            min="1"
            max="20"
            class="input w-full"
          />
        </label>
      </div>

      <div class="mt-6 flex justify-end gap-2">
        <button class="btn btn-secondary" :disabled="saving" @click="emit('close')">
          {{ t('common.cancel') }}
        </button>
        <button class="btn btn-primary" :disabled="!canSave" @click="save">
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </div>
  </div>
</template>
