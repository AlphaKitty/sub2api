<template>
  <div v-if="status?.enabled" class="card overflow-hidden">
    <div class="flex flex-col gap-5 p-5 lg:flex-row lg:items-center lg:gap-8">
      <!-- 左侧：标题 / 状态 / 按钮 -->
      <div class="shrink-0 space-y-2.5 lg:w-72">
        <div class="flex items-center gap-2">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('checkin.title') }}</h2>
          <span
            v-if="status.streak_days > 0"
            class="rounded-full bg-amber-100 px-2.5 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400"
          >
            🔥 {{ t('checkin.streakDays', { days: status.streak_days }) }}
          </span>
        </div>

        <p class="text-sm text-gray-500 dark:text-dark-400">
          <template v-if="status.today_checked">
            {{ t('checkin.doneToday', { reward: formatReward(status.today_reward) }) }}
          </template>
          <template v-else-if="status.reg_age_remaining_hours > 0">
            {{ t('checkin.regAgeLock', { hours: status.reg_age_remaining_hours }) }}
          </template>
          <template v-else-if="status.monthly_cap_remaining === 0">
            {{ t('checkin.monthlyCapReached') }}
          </template>
          <template v-else>
            {{ t('checkin.todayReward', { reward: formatReward(status.today_reward) }) }}
          </template>
        </p>

        <button
          :disabled="!status.can_check_in || submitting"
          class="w-full rounded-xl py-2.5 text-sm font-semibold transition-all disabled:cursor-not-allowed disabled:opacity-60 lg:w-auto lg:px-8"
          :class="
            status.today_checked
              ? 'bg-gray-100 text-gray-500 dark:bg-dark-800 dark:text-dark-400'
              : 'bg-primary-600 text-white shadow-sm hover:bg-primary-700 active:scale-[0.98]'
          "
          @click="doCheckIn"
        >
          <span v-if="submitting">{{ t('checkin.submitting') }}</span>
          <span v-else-if="status.today_checked">{{ t('checkin.checked') }}</span>
          <span v-else>{{ t('checkin.button') }}</span>
        </button>

        <Transition name="fade">
          <p v-if="successMessage" class="rounded-lg bg-emerald-50 px-3 py-2 text-xs font-medium text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-400">
            {{ successMessage }}
          </p>
        </Transition>
        <Transition name="fade">
          <p v-if="errorMessage" class="rounded-lg bg-red-50 px-3 py-2 text-xs font-medium text-red-600 dark:bg-red-900/20 dark:text-red-400">
            {{ errorMessage }}
          </p>
        </Transition>
      </div>

      <!-- 右侧：7 天周期奖励条 -->
      <div class="min-w-0 flex-1">
        <div class="flex items-end justify-between gap-1.5 sm:gap-2">
          <div
            v-for="(reward, i) in status.rewards"
            :key="i"
            class="flex min-w-0 flex-1 flex-col items-center gap-1.5"
          >
            <div
              class="flex h-9 w-full items-center justify-center rounded-lg text-xs font-semibold transition-all sm:h-10"
              :class="pillClass(i)"
            >
              <span v-if="isDone(i)" class="text-white">✓</span>
              <span v-else-if="isToday(i)" class="text-white">{{ formatReward(reward) }}</span>
              <span v-else class="truncate text-gray-500 dark:text-dark-400">{{ formatReward(reward) }}</span>
            </div>
            <span class="text-[10px] text-gray-400 dark:text-dark-500">{{ t('checkin.day', { n: i + 1 }) }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { checkInAPI } from '@/api/checkIn'
import { useAuthStore } from '@/stores/auth'
import type { CheckInStatus } from '@/types'

const { t } = useI18n()
const authStore = useAuthStore()

const status = ref<CheckInStatus | null>(null)
const submitting = ref(false)
const successMessage = ref('')
const errorMessage = ref('')

// 当前周期进度：已签 = 今天已完成（含今天），未签 = 今天待签
const progress = computed(() => {
  if (!status.value) return 0
  return status.value.streak_days % Math.max(status.value.rewards.length, 1)
})

const isToday = (i: number) => {
  if (!status.value?.rewards.length) return false
  return i === progress.value
}

const isDone = (i: number) => {
  if (!status.value) return false
  if (status.value.today_checked) return i <= progress.value
  return i < progress.value
}

const pillClass = (i: number) => {
  if (isDone(i)) return 'bg-emerald-500'
  if (isToday(i)) return status.value?.today_checked ? 'bg-emerald-500' : 'bg-primary-500'
  return 'bg-gray-100 dark:bg-dark-800'
}

const formatReward = (v: number) => {
  if (v >= 0.01) return `$${v.toFixed(2)}`
  return `$${v.toFixed(3)}`
}

const loadStatus = async () => {
  try {
    status.value = await checkInAPI.getCheckInStatus()
  } catch (e) {
    console.error('Failed to load check-in status:', e)
  }
}

const doCheckIn = async () => {
  if (!status.value?.can_check_in || submitting.value) return
  submitting.value = true
  successMessage.value = ''
  errorMessage.value = ''
  try {
    const result = await checkInAPI.checkIn()
    await loadStatus()
    // 刷新用户余额
    await authStore.refreshUser()
    successMessage.value = t('checkin.success', {
      reward: formatReward(result.reward),
      streak: result.streak_days,
    })
  } catch (e: any) {
    errorMessage.value = e?.response?.data?.message || t('checkin.error')
    await loadStatus()
  } finally {
    submitting.value = false
  }
}

onMounted(loadStatus)
</script>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.3s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
