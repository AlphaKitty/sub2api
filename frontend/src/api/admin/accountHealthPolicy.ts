/**
 * Admin Account Health Policy API
 * Group-scoped automatic account connectivity probing and remediation.
 */

import { apiClient } from '../client'

export interface AccountHealthPolicy {
  id: number
  group_id: number
  enabled: boolean
  cron_expression: string
  model_id: string
  preferred_models: string[]
  concurrency: number
  timeout_seconds: number
  consecutive_failure_threshold: number
  on_failure_action: 'none' | 'disable_schedulable'
  allow_delete: boolean
  on_success_recover: boolean
  on_success_enable_if_disabled: boolean
  max_run_history: number
  last_run_at: string | null
  next_run_at: string | null
  created_at: string
  updated_at: string
}

export interface AccountHealthPolicyUpsert {
  enabled?: boolean
  cron_expression?: string
  model_id?: string
  preferred_models?: string[]
  concurrency?: number
  timeout_seconds?: number
  consecutive_failure_threshold?: number
  on_failure_action?: 'none' | 'disable_schedulable'
  on_success_recover?: boolean
  on_success_enable_if_disabled?: boolean
  max_run_history?: number
}

export interface AccountHealthRunItem {
  id: number
  run_id: number
  account_id: number
  account_name: string
  model_id: string
  status: string
  latency_ms: number
  error_message: string
  consecutive_failures: number
  action_taken: string
  response_excerpt: string
  started_at: string
  finished_at: string
  created_at: string
}

export interface AccountHealthRun {
  id: number
  policy_id: number
  group_id: number
  trigger: string
  status: string
  total_count: number
  success_count: number
  failure_count: number
  skipped_count: number
  action_count: number
  error_message: string
  started_at: string
  finished_at: string | null
  created_at: string
  items?: AccountHealthRunItem[]
}

export async function getByGroup(groupId: number): Promise<AccountHealthPolicy | null> {
  const { data } = await apiClient.get<AccountHealthPolicy | null>(
    `/admin/groups/${groupId}/health-policy`
  )
  return data ?? null
}

export async function upsert(
  groupId: number,
  req: AccountHealthPolicyUpsert
): Promise<AccountHealthPolicy> {
  const { data } = await apiClient.put<AccountHealthPolicy>(
    `/admin/groups/${groupId}/health-policy`,
    req
  )
  return data
}

export async function remove(groupId: number): Promise<void> {
  await apiClient.delete(`/admin/groups/${groupId}/health-policy`)
}

export async function runNow(groupId: number): Promise<AccountHealthRun> {
  const { data } = await apiClient.post<AccountHealthRun>(
    `/admin/groups/${groupId}/health-policy/run`
  )
  return data
}

export async function listRuns(groupId: number, limit = 20): Promise<AccountHealthRun[]> {
  const { data } = await apiClient.get<AccountHealthRun[]>(
    `/admin/groups/${groupId}/health-policy/runs`,
    { params: { limit } }
  )
  return data ?? []
}

export async function getRun(runId: number): Promise<AccountHealthRun> {
  const { data } = await apiClient.get<AccountHealthRun>(
    `/admin/account-health-runs/${runId}`
  )
  return data
}

export const accountHealthPolicyAPI = {
  getByGroup,
  upsert,
  remove,
  runNow,
  listRuns,
  getRun
}

export default accountHealthPolicyAPI
