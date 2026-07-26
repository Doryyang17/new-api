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
import { combineBillingExpr } from '@/features/pricing/lib/billing-expr'
import {
  createDefaultVisualConfig,
  tryParseVisualConfig,
  type VisualConfig,
} from '@/features/pricing/lib/tier-expr'

export type TieredPricingEditorMode = 'visual' | 'raw'

export type TieredPricingEditorInitialState = {
  editorMode: TieredPricingEditorMode
  visualConfig: VisualConfig | null
  rawExpr: string
}

/**
 * Empty expressions start from the default visual form. Non-empty expressions
 * are accepted only when they round-trip through the visual representation.
 */
export function resolveTieredPricingVisualConfig(
  billingExpr: string
): VisualConfig | null {
  if (!billingExpr.trim()) return createDefaultVisualConfig()
  return tryParseVisualConfig(billingExpr)
}

/**
 * Complex expressions must start in raw mode with their exact source intact.
 * A default visual config is only created for a genuinely empty expression.
 */
export function createTieredPricingEditorInitialState(
  billingExpr: string,
  requestRuleExpr: string
): TieredPricingEditorInitialState {
  const visualConfig = resolveTieredPricingVisualConfig(billingExpr)
  const hasBillingExpr = billingExpr.trim().length > 0

  return {
    editorMode: hasBillingExpr && !visualConfig ? 'raw' : 'visual',
    visualConfig,
    rawExpr: combineBillingExpr(billingExpr, requestRuleExpr),
  }
}
