<template>
  <BaseDialog
    :show="show"
    :title="t('admin.modelMappingTemplates.title')"
    width="extra-wide"
    @close="$emit('close')"
  >
    <div class="space-y-5">
      <p class="text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.modelMappingTemplates.description') }}
      </p>

      <!-- ══════════ 模板管理区 ══════════ -->
      <div class="rounded-lg border border-gray-200 dark:border-dark-600">
        <div class="flex items-center justify-between border-b border-gray-200 px-3 py-2 dark:border-dark-600">
          <h4 class="text-sm font-medium text-gray-900 dark:text-white">
            {{ t('admin.modelMappingTemplates.manage') }}
          </h4>
          <button type="button" class="btn btn-secondary btn-sm" @click="startCreate">
            <Icon name="plus" size="sm" class="mr-1" />
            {{ t('admin.modelMappingTemplates.newTemplate') }}
          </button>
        </div>

        <div v-if="templatesLoading" class="flex items-center justify-center py-6">
          <Icon name="refresh" size="md" class="animate-spin text-gray-400" />
        </div>

        <!-- 模板列表 -->
        <div v-else-if="templates.length === 0" class="py-6 text-center text-sm text-gray-400">
          {{ t('admin.modelMappingTemplates.empty') }}
        </div>
        <ul v-else class="max-h-56 divide-y divide-gray-100 overflow-y-auto dark:divide-dark-700">
          <li
            v-for="tpl in templates"
            :key="tpl.id"
            class="flex items-center gap-2 px-3 py-2"
          >
            <button
              type="button"
              class="flex min-w-0 flex-1 items-center gap-2 rounded px-2 py-1 text-left hover:bg-gray-50 dark:hover:bg-dark-700"
              @click="editTemplate(tpl)"
            >
              <Icon name="edit" size="sm" class="shrink-0 text-gray-400" />
              <span class="min-w-0 flex-1 truncate text-sm text-gray-800 dark:text-gray-200">
                {{ tpl.name }}
              </span>
              <span v-if="tpl.platform" class="shrink-0 rounded-full bg-gray-100 px-2 py-0.5 text-[10px] text-gray-500 dark:bg-dark-600 dark:text-gray-400">
                {{ platformLabel(tpl.platform) }}
              </span>
              <span class="shrink-0 text-xs text-gray-400">
                {{ mappingCount(tpl) }} {{ t('admin.modelMappingTemplates.entries') }}
              </span>
            </button>
            <button
              type="button"
              class="rounded p-1 text-red-500 hover:bg-red-50 dark:hover:bg-red-900/20"
              :title="t('common.delete')"
              @click="removeTemplate(tpl.id)"
            >
              <Icon name="trash" size="sm" />
            </button>
          </li>
        </ul>
      </div>

      <!-- 模板编辑器 -->
      <div
        v-if="editing"
        class="rounded-lg border border-primary-200 bg-primary-50/30 p-3 dark:border-primary-800/50 dark:bg-primary-900/10"
      >
        <div class="mb-3 flex items-center gap-2">
          <input
            v-model="editor.name"
            type="text"
            class="input flex-1"
            :placeholder="t('admin.modelMappingTemplates.namePlaceholder')"
          />
          <Select
            v-model="editor.platform"
            :options="platformOptions"
            :clearable="true"
            :placeholder="t('admin.modelMappingTemplates.platformPlaceholder')"
            class="w-44 shrink-0"
          />
          <button type="button" class="btn btn-primary btn-sm shrink-0" :disabled="!canSaveEditor" @click="saveEditor">
            {{ t('common.save') }}
          </button>
          <button type="button" class="btn btn-secondary btn-sm shrink-0" @click="cancelEditor">
            {{ t('common.cancel') }}
          </button>
        </div>
        <div class="space-y-1.5">
          <div v-for="(row, idx) in editor.rows" :key="idx" class="flex items-center gap-2">
            <input
              v-model="row.from"
              type="text"
              class="input flex-1"
              :placeholder="t('admin.modelMappingTemplates.fromPlaceholder')"
            />
            <Icon name="arrowRight" size="sm" class="shrink-0 text-gray-400" />
            <input
              v-model="row.to"
              type="text"
              class="input flex-1"
              :placeholder="t('admin.modelMappingTemplates.toPlaceholder')"
            />
            <button
              type="button"
              class="shrink-0 rounded p-1 text-gray-400 hover:text-red-500"
              @click="editor.rows.splice(idx, 1)"
            >
              <Icon name="x" size="sm" />
            </button>
          </div>
          <button type="button" class="btn btn-secondary btn-sm" @click="editor.rows.push({ from: '', to: '' })">
            <Icon name="plus" size="sm" class="mr-1" />
            {{ t('admin.modelMappingTemplates.addRow') }}
          </button>
        </div>
      </div>

      <!-- ══════════ 应用区 ══════════ -->
      <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
        <h4 class="mb-2 text-sm font-medium text-gray-900 dark:text-white">
          {{ t('admin.modelMappingTemplates.applyTitle') }}
        </h4>

        <div class="grid gap-3 sm:grid-cols-2">
          <div>
            <label class="input-label">{{ t('admin.modelMappingTemplates.targetGroup') }}</label>
            <Select
              v-model="applyGroupId"
              :options="groupOptions"
              :placeholder="t('admin.modelMappingTemplates.selectGroupPlaceholder')"
              :searchable="true"
            />
          </div>
          <div>
            <label class="input-label">{{ t('admin.modelMappingTemplates.applyTemplate') }}</label>
            <Select
              v-model="applyTemplateId"
              :options="applyTemplateOptions"
              :placeholder="t('admin.modelMappingTemplates.selectTemplatePlaceholder')"
            />
          </div>
        </div>

        <!-- 映射预览 -->
        <div v-if="previewMapping.length > 0" class="mt-3">
          <p class="mb-1 text-xs font-medium text-gray-500 dark:text-gray-400">
            {{ t('admin.modelMappingTemplates.preview') }}（{{ previewMapping.length }}）
          </p>
          <div class="max-h-40 overflow-y-auto rounded-md bg-gray-50 p-2 font-mono text-[11px] dark:bg-dark-800">
            <div v-for="[from, to] in previewMapping" :key="from" class="flex items-center gap-1 py-0.5">
              <span class="min-w-0 flex-1 truncate text-gray-700 dark:text-gray-300">{{ from }}</span>
              <span class="text-gray-400">→</span>
              <span class="min-w-0 flex-1 truncate text-gray-700 dark:text-gray-300">{{ to }}</span>
            </div>
          </div>
        </div>

        <div class="mt-3 flex items-center gap-3">
          <button
            type="button"
            class="btn btn-primary"
            :disabled="!canApply || applying"
            @click="apply"
          >
            <Icon v-if="applying" name="refresh" size="sm" class="mr-1 animate-spin" />
            <Icon v-else name="bolt" size="sm" class="mr-1" />
            {{ t('admin.modelMappingTemplates.apply') }}
          </button>
          <span v-if="applyResult" class="text-xs">
            <span v-if="applyResult.failed === 0" class="text-emerald-600 dark:text-emerald-400">
              {{ t('admin.modelMappingTemplates.applySuccess', { count: applyResult.success }) }}
            </span>
            <span v-else class="text-amber-600 dark:text-amber-400">
              {{ t('admin.modelMappingTemplates.applyPartial', { success: applyResult.success, failed: applyResult.failed }) }}
            </span>
          </span>
        </div>
        <p class="mt-2 text-xs text-gray-400 dark:text-gray-500">
          {{ t('admin.modelMappingTemplates.applyHint') }}
        </p>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import {
  accountsAPI,
  type ModelMappingTemplate
} from '@/api/admin/accounts'
import { getAll as getAllGroups } from '@/api/admin/groups'
import type { AdminGroup, GroupPlatform } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { platformLabel } from '@/utils/platformColors'

const { t } = useI18n()
const appStore = useAppStore()

const props = defineProps<{ show: boolean }>()

const templatesLoading = ref(false)
const templates = ref<ModelMappingTemplate[]>([])
const groups = ref<AdminGroup[]>([])

const applying = ref(false)
const applyGroupId = ref<number | null>(null)
const applyTemplateId = ref<string | null>(null)
const applyResult = ref<{ success: number; failed: number } | null>(null)

let loadedOnce = false

// 弹窗打开时才加载数据（组件常驻挂载，避免页面初始化时发出无关请求）。
watch(
  () => props.show,
  (show) => {
    if (!show || loadedOnce) return
    loadedOnce = true
    loadTemplates()
    loadGroups()
  },
  { immediate: true }
)

// ── 模板编辑器 ──
interface EditorRow {
  from: string
  to: string
}
const editing = ref(false)
const editorId = ref<string | null>(null)
const editor = ref<{ name: string; platform: string; rows: EditorRow[] }>({
  name: '',
  platform: '',
  rows: []
})

const platformOptions = [
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' },
  { value: 'grok', label: 'Grok' }
]

const groupOptions = computed(() =>
  groups.value.map((g) => ({
    value: g.id,
    label: `${g.name}（${platformLabel(g.platform)}）`,
    description: g.description || undefined
  }))
)

const applyTemplateOptions = computed(() =>
  templates.value.map((tpl) => ({
    value: tpl.id,
    label: `${tpl.name}（${mappingCount(tpl)} ${t('admin.modelMappingTemplates.entries')}）`
  }))
)

const previewMapping = computed<[string, string][]>(() => {
  const tpl = templates.value.find((x) => x.id === applyTemplateId.value)
  if (!tpl) return []
  return Object.entries(tpl.mapping).filter(([from, to]) => from && to)
})

const canApply = computed(() => applyGroupId.value != null && applyTemplateId.value != null)

const canSaveEditor = computed(() => editor.value.name.trim() !== '')

function mappingCount(tpl: ModelMappingTemplate): number {
  return Object.entries(tpl.mapping).filter(([f, t]) => f && t).length
}

async function loadTemplates() {
  templatesLoading.value = true
  try {
    templates.value = await accountsAPI.getModelMappingTemplates()
  } catch (err) {
    // 静默降级：列表页加载失败不打断账号管理主流程。
    console.error('Failed to load model mapping templates:', err)
  } finally {
    templatesLoading.value = false
  }
}

async function loadGroups() {
  try {
    groups.value = await getAllGroups()
  } catch (err) {
    console.error('Failed to load groups for template dialog:', err)
  }
}

watch(
  () => applyGroupId.value,
  () => {
    applyResult.value = null
  }
)

// ── 模板编辑器操作 ──
function startCreate() {
  editorId.value = null
  editor.value = { name: '', platform: '', rows: [{ from: '', to: '' }] }
  editing.value = true
}

function editTemplate(tpl: ModelMappingTemplate) {
  editorId.value = tpl.id
  editor.value = {
    name: tpl.name,
    platform: tpl.platform || '',
    rows: Object.entries(tpl.mapping).map(([from, to]) => ({ from, to }))
  }
  editing.value = true
}

function cancelEditor() {
  editing.value = false
  editorId.value = null
}

function saveEditor() {
  const mapping: Record<string, string> = {}
  for (const row of editor.value.rows) {
    const from = row.from.trim()
    const to = row.to.trim()
    if (from && to) mapping[from] = to
  }
  const tpl: ModelMappingTemplate = {
    id: editorId.value || crypto.randomUUID(),
    name: editor.value.name.trim(),
    platform: (editor.value.platform as GroupPlatform) || undefined,
    mapping
  }
  const idx = templates.value.findIndex((x) => x.id === tpl.id)
  if (idx >= 0) {
    templates.value[idx] = tpl
  } else {
    templates.value.push(tpl)
  }
  persistTemplates()
  editing.value = false
  editorId.value = null
}

function removeTemplate(id: string) {
  templates.value = templates.value.filter((x) => x.id !== id)
  if (applyTemplateId.value === id) applyTemplateId.value = null
  persistTemplates()
}

async function persistTemplates() {
  try {
    await accountsAPI.saveModelMappingTemplates(templates.value)
    appStore.showSuccess(t('admin.modelMappingTemplates.saved'))
  } catch (err) {
    appStore.showError(extractApiErrorMessage(err, t('admin.modelMappingTemplates.saveFailed')))
  }
}

// ── 应用 ──
async function apply() {
  if (!canApply.value) return
  const tpl = templates.value.find((x) => x.id === applyTemplateId.value)
  if (!tpl || !applyGroupId.value) return
  applying.value = true
  applyResult.value = null
  try {
    const result = await accountsAPI.applyModelMappingTemplate(applyGroupId.value, tpl.mapping)
    applyResult.value = { success: result.success, failed: result.failed }
    if (result.failed === 0) {
      appStore.showSuccess(t('admin.modelMappingTemplates.applySuccess', { count: result.success }))
    } else {
      appStore.showWarning(
        t('admin.modelMappingTemplates.applyPartial', { success: result.success, failed: result.failed })
      )
    }
  } catch (err) {
    appStore.showError(extractApiErrorMessage(err, t('admin.modelMappingTemplates.applyFailed')))
  } finally {
    applying.value = false
  }
}
</script>
