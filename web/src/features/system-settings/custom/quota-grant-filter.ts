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
import { USER_ROLE, USER_STATUS } from '@/features/users/constants'
import dayjs from '@/lib/dayjs'

import type { QuotaGrantFilters } from './quota-grant-api'

export const quotaGrantTimeZone = 'Asia/Shanghai'

export function formatQuotaGrantTimestamp(timestamp: number) {
  return dayjs
    .unix(timestamp)
    .tz(quotaGrantTimeZone)
    .format('YYYY-MM-DD HH:mm:ss')
}

export function createDefaultQuotaGrantFilters(): QuotaGrantFilters {
  const now = dayjs().tz(quotaGrantTimeZone)
  return {
    keyword: '',
    roles: [USER_ROLE.USER],
    statuses: [USER_STATUS.ENABLED],
    balance_mode: 'any',
    balance_amount: '',
    balance_max: '',
    time_start_at: now.subtract(6, 'day').startOf('day').unix(),
    time_end_at: now.endOf('day').unix(),
    recharge_mode: 'any',
    usage_mode: 'any',
    usage_models: [],
  }
}

export const quotaGrantCustomBalanceModes = new Set([
  'lt',
  'lte',
  'eq',
  'gte',
  'gt',
  'between',
])

const maxQuotaGrantUsageModels = 100

function usageModelsLabel(models: string[]) {
  const visibleModels = models.slice(0, 3)
  const label = visibleModels.join('、')
  if (models.length <= visibleModels.length) return label
  return `${label} 等 ${models.length} 个模型`
}

function behaviorTimeLabel(filters: QuotaGrantFilters) {
  if (!filters.time_start_at || !filters.time_end_at) return '未指定时间'
  const start = dayjs
    .unix(filters.time_start_at)
    .tz(quotaGrantTimeZone)
    .format('YYYY-MM-DD HH:mm')
  const end = dayjs
    .unix(filters.time_end_at)
    .tz(quotaGrantTimeZone)
    .format('YYYY-MM-DD HH:mm')
  return `${start} 至 ${end}（北京时间）`
}

export function quotaGrantFilterSummary(filters: QuotaGrantFilters) {
  let statusLabel = '已禁用'
  if (filters.statuses.length === 2) {
    statusLabel = '全部状态'
  } else if (filters.statuses[0] === USER_STATUS.ENABLED) {
    statusLabel = '已启用'
  }
  let roleLabel = '普通用户'
  if (filters.roles.length === 2) {
    roleLabel = '普通用户和管理员'
  } else if (filters.roles[0] === USER_ROLE.ADMIN) {
    roleLabel = '管理员'
  }
  const parts = [statusLabel, roleLabel]
  const balanceLabels: Record<string, string> = {
    low: '余额 < $10',
    negative: '负余额',
    zero: '零余额',
    positive: '余额 > $0',
    lt: `余额 < $${filters.balance_amount}`,
    lte: `余额 ≤ $${filters.balance_amount}`,
    eq: `余额 = $${filters.balance_amount}`,
    gte: `余额 ≥ $${filters.balance_amount}`,
    gt: `余额 > $${filters.balance_amount}`,
    between: `余额 $${filters.balance_amount}–$${filters.balance_max}`,
  }
  if (balanceLabels[filters.balance_mode]) {
    parts.push(balanceLabels[filters.balance_mode])
  }

  const hasBehaviorFilter =
    filters.recharge_mode !== 'any' || filters.usage_mode !== 'any'
  if (hasBehaviorFilter) {
    parts.push(`行为时间：${behaviorTimeLabel(filters)}`)
  }
  if (filters.recharge_mode === 'recharged') {
    parts.push('范围内有充值')
  } else if (filters.recharge_mode === 'unrecharged') {
    parts.push('范围内无充值')
  }
  if (filters.usage_mode !== 'any') {
    if (filters.usage_models.length > 0) {
      const action = filters.usage_mode === 'used' ? '使用过' : '未使用'
      parts.push(
        `范围内${action}任一模型：${usageModelsLabel(filters.usage_models)}`
      )
    } else {
      const action = filters.usage_mode === 'used' ? '有' : '无'
      parts.push(`范围内${action}模型消耗`)
    }
  }
  if (filters.keyword) parts.push(`关键词：${filters.keyword}`)
  return parts.join('；')
}

export function validateQuotaGrantFilters(filters: QuotaGrantFilters) {
  if (
    quotaGrantCustomBalanceModes.has(filters.balance_mode) &&
    !filters.balance_amount
  ) {
    return '请填写余额金额'
  }
  if (filters.balance_mode === 'between' && !filters.balance_max) {
    return '请填写余额区间上限'
  }
  const hasBehaviorFilter =
    filters.recharge_mode !== 'any' || filters.usage_mode !== 'any'
  const hasStart =
    Number.isFinite(filters.time_start_at) && filters.time_start_at > 0
  const hasEnd = Number.isFinite(filters.time_end_at) && filters.time_end_at > 0
  if (hasBehaviorFilter && (!hasStart || !hasEnd)) {
    return '请选择完整的行为时间范围'
  }
  if (hasStart !== hasEnd) {
    return '请选择完整的行为时间范围'
  }
  if (hasStart && filters.time_end_at < filters.time_start_at) {
    return '行为时间范围结束时间不能早于开始时间'
  }
  if (filters.usage_models.length > maxQuotaGrantUsageModels) {
    return `使用模型最多选择 ${maxQuotaGrantUsageModels} 个`
  }
  if (filters.usage_models.some((model) => model.trim().length > 100)) {
    return '单个使用模型名称不能超过 100 个字符'
  }
  return null
}
