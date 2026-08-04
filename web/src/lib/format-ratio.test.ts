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

import { formatRatioMultiplier } from './format'

describe('ratio multiplier formatting', () => {
  test('shows no more than four decimal places without float noise', () => {
    assert.equal(formatRatioMultiplier(0.9900000000000001), '0.99')
    assert.equal(formatRatioMultiplier(1.1700000000000002), '1.17')
    assert.equal(formatRatioMultiplier(1.23456), '1.2346')
    assert.equal(formatRatioMultiplier(1.2), '1.2')
    assert.equal(formatRatioMultiplier(1), '1')
    assert.equal(formatRatioMultiplier(0), '0')
    assert.equal(formatRatioMultiplier(0.00001), '<0.0001')
    assert.equal(formatRatioMultiplier(-0.00001), '>-0.0001')
  })

  test('uses a placeholder for missing or invalid ratios', () => {
    assert.equal(formatRatioMultiplier(undefined), '-')
    assert.equal(formatRatioMultiplier(null), '-')
    assert.equal(formatRatioMultiplier(Number.NaN), '-')
    assert.equal(formatRatioMultiplier(Number.POSITIVE_INFINITY), '-')
  })
})
