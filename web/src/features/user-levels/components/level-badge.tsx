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
import {
  StatusBadge,
  type StatusBadgeProps,
  type StatusVariant,
} from '@/components/status-badge'
import { cn } from '@/lib/utils'

const validVariants = new Set<StatusVariant>([
  'neutral',
  'info',
  'success',
  'warning',
  'purple',
  'blue',
  'cyan',
  'green',
  'orange',
  'pink',
])

type LevelBadgeProps = Omit<
  StatusBadgeProps,
  'label' | 'variant' | 'autoColor'
> & {
  name: string
  color?: string
  ratio?: number
  showRatio?: boolean
}

function badgeVariant(color?: string): StatusVariant {
  if (color && validVariants.has(color as StatusVariant)) {
    return color as StatusVariant
  }
  return 'neutral'
}

export function LevelBadge(props: LevelBadgeProps) {
  const { name, color, ratio, showRatio, ...badgeProps } = props
  const badge = (
    <StatusBadge
      {...badgeProps}
      label={name}
      variant={badgeVariant(color)}
      copyable={badgeProps.copyable ?? false}
      className={cn('min-w-0 shrink overflow-hidden', badgeProps.className)}
    />
  )
  if (!showRatio || ratio == null) return badge

  return (
    <span className='inline-flex max-w-full min-w-0 items-center gap-2 text-xs'>
      {badge}
      <span className='bg-info/10 text-info inline-flex h-5 shrink-0 items-center rounded-full px-1.5 font-mono text-xs leading-none font-medium tabular-nums'>
        ×{ratio.toFixed(2)}
      </span>
    </span>
  )
}
