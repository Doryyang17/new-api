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

import {
  createTieredPricingEditorInitialState,
  resolveTieredPricingVisualConfig,
} from './tiered-pricing-editor-state'

describe('tiered pricing editor initialization', () => {
  test('keeps a complex probe expression intact and opens raw mode', () => {
    const expression =
      'len < 5000 ? tier("测活请求 · $0.2/次", 200000) : tier("normal", p * 1.5 + c * 9 + cr * 0.5)'

    const state = createTieredPricingEditorInitialState(expression, '')

    assert.equal(state.editorMode, 'raw')
    assert.equal(state.rawExpr, expression)
    assert.equal(state.visualConfig, null)
  })

  test('uses visual mode only when the expression round-trips safely', () => {
    const expression = 'tier("normal", p * 1.5 + c * 9 + cr * 0.5)'

    const state = createTieredPricingEditorInitialState(expression, '')

    assert.equal(state.editorMode, 'visual')
    assert.equal(state.visualConfig?.tiers[0]?.label, 'normal')
    assert.equal(state.rawExpr, expression)
  })

  test('restores the default visual form after the raw expression is cleared', () => {
    const visualConfig = resolveTieredPricingVisualConfig('   ')

    assert.ok(visualConfig)
    assert.ok(visualConfig.tiers.length > 0)
  })

  test('rejects a non-empty expression that cannot round-trip visually', () => {
    const visualConfig = resolveTieredPricingVisualConfig(
      'len < 5000 ? tier("probe", 200000) : tier("normal", p * 1 + c * 2)'
    )

    assert.equal(visualConfig, null)
  })
})
