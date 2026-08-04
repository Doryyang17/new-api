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
import { z } from 'zod'

import type {
  UserLevelConfig,
  UserLevelDefinition,
} from '@/features/user-levels'

export const USER_LEVEL_BADGE_COLORS = [
  { value: 'neutral', label: '中性灰' },
  { value: 'info', label: '信息蓝' },
  { value: 'success', label: '成功绿' },
  { value: 'warning', label: '提醒黄' },
  { value: 'purple', label: '典雅紫' },
  { value: 'blue', label: '深海蓝' },
  { value: 'cyan', label: '青色' },
  { value: 'green', label: '绿色' },
  { value: 'orange', label: '橙色' },
  { value: 'pink', label: '粉色' },
] as const

const badgeColors = new Set<string>(
  USER_LEVEL_BADGE_COLORS.map((item) => item.value)
)
const levelIdPattern = /^[a-z0-9][a-z0-9_-]{0,31}$/
export const MAX_USER_LEVEL_THRESHOLD_QUOTA = Number.MAX_SAFE_INTEGER

const levelSchema = z.object({
  id: z
    .string()
    .trim()
    .min(1, '请输入等级 ID')
    .regex(levelIdPattern, '仅支持小写字母、数字、下划线和短横线'),
  name: z.string().trim().min(1, '请输入等级名称').max(20, '最多 20 个字符'),
  description: z.string().trim().max(80, '最多 80 个字符').optional(),
  threshold_quota: z
    .number()
    .max(MAX_USER_LEVEL_THRESHOLD_QUOTA, '累计消耗门槛超出可精确计算范围')
    .int('累计消耗门槛必须换算为整数额度')
    .nonnegative('累计消耗门槛不能小于 0'),
  ratio: z
    .number()
    .finite('请输入有效倍率')
    .positive('倍率必须大于 0')
    .max(1, '等级倍率不能超过 1'),
  badge_color: z.string().refine((value) => badgeColors.has(value), {
    message: '请选择标签颜色',
  }),
  archived: z.boolean(),
})

export const userLevelSettingsSchema = z
  .object({
    schema_version: z.literal(1),
    enabled: z.boolean(),
    levels: z.array(levelSchema).min(1).max(20, '最多配置 20 个等级'),
  })
  .superRefine((values, ctx) => {
    const ids = new Set<string>()
    const names = new Set<string>()
    let previousThreshold = -1
    let previousRatio = 1

    values.levels.forEach((level, index) => {
      if (ids.has(level.id)) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['levels', index, 'id'],
          message: '等级 ID 不能重复',
        })
      }
      ids.add(level.id)

      if (names.has(level.name)) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['levels', index, 'name'],
          message: '等级名称不能重复',
        })
      }
      names.add(level.name)

      if (level.threshold_quota <= previousThreshold) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['levels', index, 'threshold_quota'],
          message: '消耗门槛必须按等级严格递增',
        })
      }
      if (level.ratio > previousRatio) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['levels', index, 'ratio'],
          message: '更高等级的倍率不能高于前一等级',
        })
      }

      if (index === 0) {
        if (
          level.id !== 'base' ||
          level.threshold_quota !== 0 ||
          level.ratio !== 1 ||
          level.archived
        ) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            path: ['levels', 0],
            message: '基础等级必须保持 ID 为 base、门槛为 0、倍率为 1',
          })
        }
      } else if (level.threshold_quota === 0) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['levels', index, 'threshold_quota'],
          message: '非基础等级的消耗门槛必须大于 0',
        })
      }

      previousThreshold = level.threshold_quota
      previousRatio = level.ratio
    })
  })

export type UserLevelSettingsValues = z.infer<typeof userLevelSettingsSchema>

export const DEFAULT_USER_LEVEL_CONFIG: UserLevelConfig = {
  schema_version: 1,
  enabled: false,
  levels: [
    {
      id: 'base',
      name: '普通用户',
      description: '默认等级',
      threshold_quota: 0,
      ratio: 1,
      badge_color: 'neutral',
      archived: false,
    },
  ],
}

export function normalizeUserLevelConfig(
  values: UserLevelSettingsValues
): UserLevelConfig {
  return {
    schema_version: 1,
    enabled: values.enabled,
    levels: values.levels.map((level) => ({
      ...level,
      id: level.id.trim(),
      name: level.name.trim(),
      description: level.description?.trim() || undefined,
    })),
  }
}

export function getBillingImpactMessages(
  previous: UserLevelConfig,
  next: UserLevelConfig,
  memberCounts: Record<string, number>
): string[] {
  const messages: string[] = []
  if (previous.enabled && !next.enabled) {
    messages.push('关闭后，所有用户请求将立即停止应用等级倍率。')
  }
  if (!previous.enabled && next.enabled) {
    const affectedMembers = next.levels.reduce((total, level) => {
      if (level.ratio === 1) return total
      return total + (memberCounts[level.id] ?? 0)
    }, 0)
    if (affectedMembers > 0) {
      messages.push(
        `重新开启后，已有 ${affectedMembers} 名用户的新请求将立即恢复等级倍率。`
      )
    }
  }

  const previousById = new Map<string, UserLevelDefinition>(
    previous.levels.map((level) => [level.id, level])
  )
  for (const level of next.levels) {
    const oldLevel = previousById.get(level.id)
    const memberCount = memberCounts[level.id] ?? 0
    if (
      next.enabled &&
      oldLevel &&
      oldLevel.ratio !== level.ratio &&
      memberCount > 0
    ) {
      messages.push(
        `${level.name} 当前有 ${memberCount} 名用户，保存后其新请求将使用 ×${level.ratio.toFixed(2)}。`
      )
    }
  }
  return messages
}
