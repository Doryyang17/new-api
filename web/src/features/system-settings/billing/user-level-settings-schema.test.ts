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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { UserLevelConfig } from '@/features/user-levels'

import {
  getBillingImpactMessages,
  normalizeUserLevelConfig,
  userLevelSettingsSchema,
} from './user-level-settings-schema'

const enabledConfig: UserLevelConfig = {
  schema_version: 1,
  enabled: true,
  levels: [
    {
      id: 'base',
      name: '普通用户',
      threshold_quota: 0,
      ratio: 1,
      badge_color: 'neutral',
      archived: false,
    },
    {
      id: 'silver',
      name: '白银会员',
      threshold_quota: 500_000,
      ratio: 0.8,
      badge_color: 'blue',
      archived: false,
    },
  ],
}

describe('user level settings validation', () => {
  test('accepts an ordered discount ladder and normalizes display text', () => {
    const input = structuredClone(enabledConfig)
    input.levels[1].name = '  白银会员  '
    input.levels[1].description = '  累充权益  '

    const parsed = userLevelSettingsSchema.parse(input)
    const normalized = normalizeUserLevelConfig(parsed)

    assert.equal(normalized.levels[1].name, '白银会员')
    assert.equal(normalized.levels[1].description, '累充权益')
  })

  test('rejects non-increasing thresholds and a weaker higher-level discount', () => {
    const input = structuredClone(enabledConfig)
    input.levels.push({
      id: 'gold',
      name: '黄金会员',
      threshold_quota: 500_000,
      ratio: 0.9,
      badge_color: 'warning',
      archived: false,
    })

    const result = userLevelSettingsSchema.safeParse(input)
    assert.equal(result.success, false)
    if (result.success) return
    assert.deepEqual(
      result.error.issues.map((issue) => issue.message),
      ['消耗门槛必须按等级严格递增', '更高等级的倍率不能高于前一等级']
    )
  })

  test('warns only for changes that alter billing for existing members', () => {
    const ratioChange = structuredClone(enabledConfig)
    ratioChange.levels[1].ratio = 0.7
    assert.deepEqual(
      getBillingImpactMessages(enabledConfig, ratioChange, { silver: 3 }),
      ['白银会员 当前有 3 名用户，保存后其新请求将使用 ×0.70。']
    )

    const archived = structuredClone(enabledConfig)
    archived.levels[1].archived = true
    assert.deepEqual(
      getBillingImpactMessages(enabledConfig, archived, { silver: 3 }),
      []
    )

    const disabled = structuredClone(enabledConfig)
    disabled.enabled = false
    assert.deepEqual(
      getBillingImpactMessages(enabledConfig, disabled, { silver: 3 }),
      ['关闭后，所有用户请求将立即停止应用等级倍率。']
    )

    const reenabled = structuredClone(disabled)
    reenabled.enabled = true
    assert.deepEqual(
      getBillingImpactMessages(disabled, reenabled, { silver: 3 }),
      ['重新开启后，已有 3 名用户的新请求将立即恢复等级倍率。']
    )
  })

  test('rejects thresholds outside the exact client range', () => {
    const input = structuredClone(enabledConfig)
    input.levels[1].threshold_quota = Number.MAX_SAFE_INTEGER + 1

    const result = userLevelSettingsSchema.safeParse(input)
    assert.equal(result.success, false)
    if (result.success) return
    assert.equal(
      result.error.issues[0]?.message,
      '累计消耗门槛超出可精确计算范围'
    )
  })
})
