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

import type { PricingModel } from '../types'
import { parseTiersFromExpr } from './billing-expr'
import {
  getDynamicPricingSummary,
  selectPreferredDynamicPricingTier,
} from './dynamic-price'

const probePenaltyExpr =
  'len < 5000 ? tier("测活请求 · $0.2/次", 200000) : tier("normal", p * 0.3 + c * 0.5 + cr * 0.1)'

function createDynamicModel(billingExpr: string): PricingModel {
  return {
    id: 1,
    model_name: 'deepseek-v4-flash',
    quota_type: 0,
    model_ratio: 0,
    completion_ratio: 0,
    enable_groups: ['default'],
    group_ratio: { default: 1 },
    billing_mode: 'tiered_expr',
    billing_expr: billingExpr,
  }
}

describe('dynamic pricing headline tier', () => {
  test('uses the normal tier instead of the fixed probe penalty tier', () => {
    const summary = getDynamicPricingSummary(
      createDynamicModel(probePenaltyExpr),
      { tokenUnit: 'M' }
    )

    assert.ok(summary)
    assert.equal(summary.tier?.label, 'normal')
    assert.deepEqual(
      summary.entries.map((entry) => [entry.field, entry.value]),
      [
        ['inputPrice', 0.3],
        ['outputPrice', 0.5],
        ['cacheReadPrice', 0.1],
      ]
    )
  })

  test('falls back to the first tier with token prices when normal is absent', () => {
    const tiers = parseTiersFromExpr(
      'len < 5000 ? tier("probe", 200000) : tier("standard", p * 1 + c * 2)'
    )

    assert.equal(selectPreferredDynamicPricingTier(tiers)?.label, 'standard')
  })

  test('parses a numeric-only tier as a fixed per-request USD price', () => {
    const tiers = parseTiersFromExpr(probePenaltyExpr)

    assert.equal(tiers[0]?.label, '测活请求 · $0.2/次')
    assert.equal(tiers[0]?.fixedPriceUSD, 0.2)
    assert.deepEqual(tiers[0]?.conditions, [
      { var: 'len', op: '<', value: 5000 },
    ])
  })
})
