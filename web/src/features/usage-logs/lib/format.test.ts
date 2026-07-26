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

import { getTieredBillingSummary, isProbePenaltyLog } from './format'

const probePenaltyExpr =
  'len < 5000 ? tier("测活请求 · $0.2/次", 200000) : tier("normal", p * 0.3 + c * 0.5)'

describe('probe penalty log marker', () => {
  test('marks only requests whose settled tier is the probe penalty tier', () => {
    assert.equal(
      isProbePenaltyLog({
        billing_mode: 'tiered_expr',
        matched_tier: '测活请求 · $0.2/次',
      }),
      true
    )
    assert.equal(
      isProbePenaltyLog({
        billing_mode: 'tiered_expr',
        matched_tier: 'normal',
      }),
      false
    )
    assert.equal(
      isProbePenaltyLog({
        billing_mode: 'per-token',
        matched_tier: '测活请求 · $0.2/次',
      }),
      false
    )
  })

  test('exposes the matched fixed fee for log detail rendering', () => {
    const summary = getTieredBillingSummary({
      billing_mode: 'tiered_expr',
      expr_b64: Buffer.from(probePenaltyExpr).toString('base64'),
      matched_tier: '测活请求 · $0.2/次',
    })

    assert.equal(summary?.tier.label, '测活请求 · $0.2/次')
    assert.equal(summary?.fixedPriceUSD, 0.2)
    assert.deepEqual(summary?.priceEntries, [])
  })
})
