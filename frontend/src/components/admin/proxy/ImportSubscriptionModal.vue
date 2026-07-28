<template>
  <BaseDialog
    :show="show"
    :title="t('admin.proxies.importSubscriptionTitle')"
    width="normal"
    close-on-click-outside
    @close="handleClose"
  >
    <div class="space-y-4">
      <div class="text-sm text-gray-600 dark:text-dark-300">
        {{ t('admin.proxies.importSubscriptionHint') }}
      </div>

      <div>
        <label class="input-label">{{ t('admin.proxies.importSubscriptionUrlLabel') }}</label>
        <input
          v-model="url"
          type="text"
          autocomplete="off"
          :disabled="step !== 'input' || previewing"
          class="input"
          :placeholder="t('admin.proxies.importSubscriptionUrlPlaceholder')"
          @input="handleUrlEdited"
        />
      </div>

      <!-- Preview result -->
      <div
        v-if="step !== 'input' && previewResult"
        class="space-y-2 rounded-xl border border-gray-200 p-4 dark:border-dark-700"
      >
        <div class="text-sm text-gray-700 dark:text-dark-300">
          {{
            t('admin.proxies.importSubscriptionPreviewSummary', {
              created: previewResult.created,
              updated: previewResult.updated
            })
          }}
        </div>

        <div v-if="previewResult.skipped.length" class="mt-2">
          <div class="text-sm font-medium text-amber-600 dark:text-amber-400">
            {{ t('admin.proxies.importSubscriptionSkippedTitle', { count: previewResult.skipped.length }) }}
          </div>
          <div
            class="mt-2 max-h-48 overflow-auto rounded-lg bg-gray-50 p-3 font-mono text-xs dark:bg-dark-800"
          >
            <div v-for="(item, idx) in previewResult.skipped" :key="idx" class="whitespace-pre-wrap">
              {{ item.name }} — {{ item.reason }}
            </div>
          </div>
        </div>
      </div>

      <!-- Final import result -->
      <div
        v-if="step === 'done' && importResult"
        class="rounded-xl border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-700 dark:border-emerald-800 dark:bg-emerald-900/20 dark:text-emerald-400"
      >
        {{
          t('admin.proxies.importSubscriptionDone', {
            created: importResult.created,
            updated: importResult.updated
          })
        }}
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button
          v-if="step !== 'done'"
          class="btn btn-secondary"
          type="button"
          :disabled="previewing || importing"
          @click="handleClose"
        >
          {{ t('common.cancel') }}
        </button>

        <button
          v-if="step === 'input'"
          class="btn btn-primary"
          type="button"
          :disabled="!canPreview || previewing"
          @click="handlePreview"
        >
          {{ previewing ? t('admin.proxies.importSubscriptionPreviewing') : t('admin.proxies.importSubscriptionPreview') }}
        </button>

        <template v-if="step === 'preview'">
          <button class="btn btn-secondary" type="button" :disabled="importing" @click="handleBack">
            {{ t('admin.proxies.importSubscriptionBack') }}
          </button>
          <button
            class="btn btn-primary"
            type="button"
            :disabled="importing || !previewResult || previewResult.created === 0"
            @click="handleConfirmImport"
          >
            {{ importing ? t('admin.proxies.importSubscriptionImporting') : t('admin.proxies.importSubscriptionConfirm') }}
          </button>
        </template>

        <button v-if="step === 'done'" class="btn btn-primary" type="button" @click="handleClose">
          {{ t('admin.proxies.importSubscriptionClose') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { ImportProxySubscriptionResult } from '@/types'

interface Props {
  show: boolean
}

interface Emits {
  (e: 'close'): void
  (e: 'imported'): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { t } = useI18n()
const appStore = useAppStore()

// 订阅链接含机场分配的密钥，属敏感信息：只保存在组件本地状态里，
// 不打印到 console、不拼进 URL query、不写 localStorage。
const url = ref('')
const step = ref<'input' | 'preview' | 'done'>('input')
const previewing = ref(false)
const importing = ref(false)
const previewResult = ref<ImportProxySubscriptionResult | null>(null)
const importResult = ref<ImportProxySubscriptionResult | null>(null)

const canPreview = computed(() => url.value.trim().length > 0)

const resetState = () => {
  url.value = ''
  step.value = 'input'
  previewing.value = false
  importing.value = false
  previewResult.value = null
  importResult.value = null
}

watch(
  () => props.show,
  (open) => {
    if (open) {
      resetState()
    }
  }
)

// 预览后如果用户又改动了链接，预览结果视为过期，退回输入态
const handleUrlEdited = () => {
  if (step.value !== 'input') {
    step.value = 'input'
    previewResult.value = null
  }
}

const handlePreview = async () => {
  const trimmed = url.value.trim()
  if (!trimmed) {
    appStore.showError(t('admin.proxies.importSubscriptionUrlRequired'))
    return
  }
  previewing.value = true
  try {
    const result = await adminAPI.proxies.importSubscription(trimmed, true)
    previewResult.value = result
    step.value = 'preview'
  } catch (error: any) {
    appStore.showError(error.response?.data?.detail || t('admin.proxies.importSubscriptionPreviewFailed'))
  } finally {
    previewing.value = false
  }
}

const handleBack = () => {
  step.value = 'input'
  previewResult.value = null
}

const handleConfirmImport = async () => {
  const trimmed = url.value.trim()
  if (!trimmed) return
  importing.value = true
  try {
    const result = await adminAPI.proxies.importSubscription(trimmed, false)
    importResult.value = result
    step.value = 'done'
    appStore.showSuccess(
      t('admin.proxies.importSubscriptionDone', { created: result.created, updated: result.updated })
    )
    emit('imported')
  } catch (error: any) {
    appStore.showError(error.response?.data?.detail || t('admin.proxies.importSubscriptionFailed'))
  } finally {
    importing.value = false
  }
}

const handleClose = () => {
  if (previewing.value || importing.value) return
  emit('close')
}
</script>
