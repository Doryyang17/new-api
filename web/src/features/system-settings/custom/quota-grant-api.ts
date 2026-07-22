/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { api } from '@/lib/api'

export type QuotaGrantFilters = {
  keyword: string
  roles: number[]
  statuses: number[]
  balance_mode: string
  balance_amount: string
  balance_max: string
  recharge_mode: string
  recharge_date: string
  usage_mode: string
  usage_period: string
}

export type QuotaGrantTarget = {
  id: number
  username: string
  display_name: string
  email: string
  quota: number
  role: number
  status: number
  group: string
  created_at: number
  last_used_at: number
  used_quota_7d: number
}

export type QuotaGrantTargetPage = {
  items: QuotaGrantTarget[]
  total: number
  page: number
  page_size: number
}

type ApiResponse<T> = {
  success: boolean
  message?: string
  data?: T
}

export type QuotaGrantBatchResult = {
  batch: {
    id: number
    request_id: string
    operator_user_id: number
    quota: number
    amount_usd: string
    reason: string
    filter_json: string
    filter_summary: string
    result: string
    target_count: number
    created_at: number
  }
  already_processed: boolean
  cache_sync_pending: boolean
}

function quotaGrantFilterParams(filters: QuotaGrantFilters) {
  return {
    keyword: filters.keyword,
    roles: filters.roles.join(','),
    statuses: filters.statuses.join(','),
    balance_mode: filters.balance_mode,
    balance_amount: filters.balance_amount,
    balance_max: filters.balance_max,
    recharge_mode: filters.recharge_mode,
    recharge_date: filters.recharge_date,
    usage_mode: filters.usage_mode,
    usage_period: filters.usage_period,
  }
}

export async function listQuotaGrantTargets(
  filters: QuotaGrantFilters,
  page: number,
  pageSize: number
) {
  const response = await api.get<ApiResponse<QuotaGrantTargetPage>>(
    '/api/user/quota-grants/targets',
    {
      params: {
        ...quotaGrantFilterParams(filters),
        p: page,
        page_size: pageSize,
      },
    }
  )
  return response.data
}

export async function listQuotaGrantTargetIds(filters: QuotaGrantFilters) {
  const response = await api.get<ApiResponse<{ ids: number[] }>>(
    '/api/user/quota-grants/targets/ids',
    { params: quotaGrantFilterParams(filters) }
  )
  return response.data
}

export async function grantUserQuota(request: {
  request_id: string
  user_ids: number[]
  amount_usd: string
  reason: string
  filters: QuotaGrantFilters
}) {
  const response = await api.post<ApiResponse<QuotaGrantBatchResult>>(
    '/api/user/quota-grants',
    request
  )
  return response.data
}
