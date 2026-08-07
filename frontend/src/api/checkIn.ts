/**
 * Check-in (每日签到) API endpoints
 * 签到赠送额度：每日签到领取小额余额奖励
 */

import { apiClient } from './client'
import type { CheckInStatus, CheckInResult } from '@/types'

/**
 * 获取当前用户签到状态
 * GET /api/v1/user/check-in
 */
export async function getCheckInStatus(): Promise<CheckInStatus> {
  const { data } = await apiClient.get<CheckInStatus>('/user/check-in')
  return data
}

/**
 * 执行签到
 * POST /api/v1/user/check-in
 */
export async function checkIn(): Promise<CheckInResult> {
  const { data } = await apiClient.post<CheckInResult>('/user/check-in')
  return data
}

export const checkInAPI = {
  getCheckInStatus,
  checkIn,
}
