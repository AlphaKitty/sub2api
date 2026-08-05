<template>
  <BaseDialog
    :show="show"
    :title="t('admin.groups.healthPolicyTitle')"
    width="wide"
    @close="handleClose"
  >
    <div v-if="group" class="space-y-4">
      <div class="flex flex-wrap items-center gap-3 rounded-lg bg-gray-50 px-4 py-2.5 text-sm dark:bg-dark-700">
        <span class="inline-flex items-center gap-1.5">
          <PlatformIcon :platform="group.platform" size="sm" />
          {{ t('admin.groups.platforms.' + group.platform) }}
        </span>
        <span class="text-gray-400">|</span>
        <span class="font-medium text-gray-900 dark:text-white">{{ group.name }}</span>
      </div>

      <div v-if="loading" class="flex justify-center py-8">
        <svg class="h-6 w-6 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
      </div>

      <template v-else>
        <div class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-800 dark:border-amber-900/50 dark:bg-amber-900/20 dark:text-amber-200">
          {{ t('admin.groups.healthPolicyGlobalHint') }}
        </div>

        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <label class="flex items-center justify-between gap-3 rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-600 sm:col-span-2">
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.groups.healthPolicyEnabled') }}</span>
            <Toggle v-model="form.enabled" />
          </label>

          <div>
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
              {{ t('admin.groups.healthPolicyModel') }}
            </label>
            <Input v-model="form.model_id" :placeholder="defaultModelForPlatform(group.platform)" />
            <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
              {{ t('admin.groups.healthPolicyModelHint', { model: defaultModelForPlatform(group.platform) }) }}
            </p>
          </div>

          <div>
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
              {{ t('admin.groups.healthPolicyCron') }}
            </label>
            <Input v-model="form.cron_expression" :placeholder="'*/30 * * * *'" />
          </div>

          <div>
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
              {{ t('admin.groups.healthPolicyConcurrency') }}
            </label>
            <Input v-model.number="form.concurrency" type="number" min="1" max="50" />
          </div>

          <div>
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
              {{ t('admin.groups.healthPolicyTimeout') }}
            </label>
            <Input v-model.number="form.timeout_seconds" type="number" min="5" max="600" />
          </div>

          <div>
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
              {{ t('admin.groups.healthPolicyThreshold') }}
            </label>
            <Input v-model.number="form.consecutive_failure_threshold" type="number" min="1" max="20" />
          </div>

          <div>
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
              {{ t('admin.groups.healthPolicyOnFailure') }}
            </label>
            <Select
              v-model="form.on_failure_action"
              :options="failureActionOptions"
            />
          </div>

          <label class="flex items-center justify-between gap-3 rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-600">
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.groups.healthPolicyRecover') }}</span>
            <Toggle v-model="form.on_success_recover" />
          </label>

          <label class="flex items-center justify-between gap-3 rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-600">
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.groups.healthPolicyReenable') }}</span>
            <Toggle v-model="form.on_success_enable_if_disabled" />
          </label>
        </div>

        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn btn-primary" :disabled="saving" @click="handleSave">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
          <button type="button" class="btn btn-secondary" :disabled="running || !hasPolicy" @click="handleRunNow">
            {{ running ? t('admin.groups.healthPolicyRunning') : t('admin.groups.healthPolicyRunNow') }}
          </button>
          <button
            v-if="hasPolicy"
            type="button"
            class="btn btn-secondary text-red-600 dark:text-red-400"
            :disabled="deleting"
            @click="handleDelete"
          >
            {{ t('admin.groups.healthPolicyDelete') }}
          </button>
        </div>

        <div>
          <div class="mb-2 flex items-center justify-between">
            <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.groups.healthPolicyHistory') }}
            </h4>
            <button type="button" class="text-xs text-primary-600 hover:underline dark:text-primary-400" @click="loadRuns">
              {{ t('common.refresh') }}
            </button>
          </div>

          <div v-if="runsLoading" class="py-4 text-center text-sm text-gray-400">
            {{ t('common.loading') }}
          </div>
          <div v-else-if="runs.length === 0" class="py-4 text-center text-sm text-gray-400">
            {{ t('admin.groups.healthPolicyNoRuns') }}
          </div>
          <div v-else class="space-y-2">
            <div
              v-for="run in runs"
              :key="run.id"
              class="rounded-lg border border-gray-200 p-3 text-sm dark:border-dark-600"
            >
              <div class="flex flex-wrap items-center gap-2">
                <span class="font-medium text-gray-900 dark:text-white">#{{ run.id }}</span>
                <span class="rounded px-1.5 py-0.5 text-xs" :class="statusClass(run.status)">{{ run.status }}</span>
                <span class="text-xs text-gray-500">{{ run.trigger }}</span>
                <span class="text-xs text-gray-400">{{ formatDateTime(run.started_at) }}</span>
              </div>
              <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.groups.healthPolicyRunSummary', {
                  total: run.total_count,
                  ok: run.success_count,
                  fail: run.failure_count,
                  action: run.action_count
                }) }}
              </div>
              <button
                type="button"
                class="mt-1 text-xs text-primary-600 hover:underline dark:text-primary-400"
                @click="toggleRunDetail(run.id)"
              >
                {{ expandedRunId === run.id ? t('common.collapse') : t('admin.groups.healthPolicyViewItems') }}
              </button>
              <div v-if="expandedRunId === run.id" class="mt-2 max-h-56 overflow-auto rounded border border-gray-100 dark:border-dark-600">
                <div v-if="detailLoading" class="p-2 text-xs text-gray-400">{{ t('common.loading') }}</div>
                <table v-else-if="runDetail?.items?.length" class="w-full text-xs">
                  <thead class="bg-gray-50 dark:bg-dark-700">
                    <tr>
                      <th class="px-2 py-1 text-left">ID</th>
                      <th class="px-2 py-1 text-left">{{ t('admin.groups.healthPolicyAccount') }}</th>
                      <th class="px-2 py-1 text-left">{{ t('common.status') }}</th>
                      <th class="px-2 py-1 text-left">{{ t('admin.groups.healthPolicyAction') }}</th>
                      <th class="px-2 py-1 text-left">ms</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="item in runDetail.items" :key="item.id" class="border-t border-gray-100 dark:border-dark-600">
                      <td class="px-2 py-1">{{ item.account_id }}</td>
                      <td class="px-2 py-1">{{ item.account_name || '-' }}</td>
                      <td class="px-2 py-1">
                        <span :class="statusClass(item.status)">{{ item.status }}</span>
                        <div v-if="item.error_message" class="text-red-500">{{ item.error_message }}</div>
                      </td>
                      <td class="px-2 py-1">{{ item.action_taken }}</td>
                      <td class="px-2 py-1">{{ item.latency_ms }}</td>
                    </tr>
                  </tbody>
                </table>
                <div v-else class="p-2 text-xs text-gray-400">{{ t('admin.groups.healthPolicyNoItems') }}</div>
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Input from '@/components/common/Input.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'
import accountHealthPolicyAPI, {
  type AccountHealthPolicy,
  type AccountHealthRun
} from '@/api/admin/accountHealthPolicy'
import type { AdminGroup } from '@/types'

const props = defineProps<{
  show: boolean
  group: AdminGroup | null
}>()

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const saving = ref(false)
const running = ref(false)
const deleting = ref(false)
const runsLoading = ref(false)
const detailLoading = ref(false)
const hasPolicy = ref(false)
const runs = ref<AccountHealthRun[]>([])
const expandedRunId = ref<number | null>(null)
const runDetail = ref<AccountHealthRun | null>(null)

const form = reactive({
  enabled: false,
  model_id: '',
  cron_expression: '*/30 * * * *',
  concurrency: 3,
  timeout_seconds: 60,
  consecutive_failure_threshold: 2,
  on_failure_action: 'disable_schedulable' as 'none' | 'disable_schedulable',
  on_success_recover: true,
  on_success_enable_if_disabled: true
})

// 各平台默认测试模型，与后端 AccountTestService 保持一致。
// 必须填写该分组平台支持的模型，否则探测会失败（例如 Grok 分组填 gpt-5.4 会返回 404）。
const platformDefaultModels: Record<string, string> = {
  anthropic: 'claude-sonnet-4-5-20250929',
  openai: 'gpt-5.4',
  gemini: 'gemini-2.0-flash',
  antigravity: 'claude-sonnet-4-5',
  grok: 'grok-4.5'
}

function defaultModelForPlatform(platform: string): string {
  return platformDefaultModels[platform] ?? 'claude-sonnet-4-5-20250929'
}

const failureActionOptions = computed(() => [
  { value: 'disable_schedulable', label: t('admin.groups.healthPolicyActionDisable') },
  { value: 'none', label: t('admin.groups.healthPolicyActionNone') }
])

function resetForm(policy: AccountHealthPolicy | null) {
  hasPolicy.value = !!policy
  const defaultModel = defaultModelForPlatform(props.group?.platform ?? '')
  form.enabled = policy?.enabled ?? false
  form.model_id = policy?.model_id || defaultModel
  form.cron_expression = policy?.cron_expression || '*/30 * * * *'
  form.concurrency = policy?.concurrency || 3
  form.timeout_seconds = policy?.timeout_seconds || 60
  form.consecutive_failure_threshold = policy?.consecutive_failure_threshold || 2
  form.on_failure_action = (policy?.on_failure_action as 'none' | 'disable_schedulable') || 'disable_schedulable'
  form.on_success_recover = policy?.on_success_recover ?? true
  form.on_success_enable_if_disabled = policy?.on_success_enable_if_disabled ?? true
}

async function loadPolicy() {
  if (!props.group) return
  loading.value = true
  try {
    const policy = await accountHealthPolicyAPI.getByGroup(props.group.id)
    resetForm(policy)
    await loadRuns()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.groups.healthPolicyLoadFailed')))
  } finally {
    loading.value = false
  }
}

async function loadRuns() {
  if (!props.group) return
  runsLoading.value = true
  try {
    runs.value = await accountHealthPolicyAPI.listRuns(props.group.id, 20)
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.groups.healthPolicyLoadFailed')))
  } finally {
    runsLoading.value = false
  }
}

async function handleSave() {
  if (!props.group) return
  if (!form.model_id.trim()) {
    appStore.showError(t('admin.groups.healthPolicyModelRequired'))
    return
  }
  saving.value = true
  try {
    const policy = await accountHealthPolicyAPI.upsert(props.group.id, {
      enabled: form.enabled,
      model_id: form.model_id.trim(),
      cron_expression: form.cron_expression.trim(),
      concurrency: Number(form.concurrency) || 3,
      timeout_seconds: Number(form.timeout_seconds) || 60,
      consecutive_failure_threshold: Number(form.consecutive_failure_threshold) || 2,
      on_failure_action: form.on_failure_action,
      on_success_recover: form.on_success_recover,
      on_success_enable_if_disabled: form.on_success_enable_if_disabled
    })
    resetForm(policy)
    appStore.showSuccess(t('admin.groups.healthPolicySaved'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.groups.healthPolicySaveFailed')))
  } finally {
    saving.value = false
  }
}

async function handleRunNow() {
  if (!props.group) return
  running.value = true
  try {
    await accountHealthPolicyAPI.runNow(props.group.id)
    appStore.showSuccess(t('admin.groups.healthPolicyRunDone'))
    await loadRuns()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.groups.healthPolicyRunFailed')))
  } finally {
    running.value = false
  }
}

async function handleDelete() {
  if (!props.group) return
  deleting.value = true
  try {
    await accountHealthPolicyAPI.remove(props.group.id)
    resetForm(null)
    runs.value = []
    appStore.showSuccess(t('admin.groups.healthPolicyDeleted'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.groups.healthPolicyDeleteFailed')))
  } finally {
    deleting.value = false
  }
}

async function toggleRunDetail(runId: number) {
  if (expandedRunId.value === runId) {
    expandedRunId.value = null
    runDetail.value = null
    return
  }
  expandedRunId.value = runId
  detailLoading.value = true
  try {
    runDetail.value = await accountHealthPolicyAPI.getRun(runId)
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.groups.healthPolicyLoadFailed')))
  } finally {
    detailLoading.value = false
  }
}

function statusClass(status: string) {
  if (status === 'success' || status === 'operational') return 'text-green-600 dark:text-green-400'
  if (status === 'partial') return 'text-amber-600 dark:text-amber-400'
  if (status === 'failed' || status === 'error') return 'text-red-600 dark:text-red-400'
  return 'text-gray-500'
}

function handleClose() {
  emit('close')
}

watch(
  () => props.show,
  (show) => {
    if (show) {
      expandedRunId.value = null
      runDetail.value = null
      loadPolicy()
    }
  }
)
</script>
