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
import { Crown02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import {
  StatusBadge,
  textColorMap,
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
  ceremonial?: boolean
}

function badgeVariant(color?: string): StatusVariant {
  if (color && validVariants.has(color as StatusVariant)) {
    return color as StatusVariant
  }
  return 'neutral'
}

export function LevelBadge(props: LevelBadgeProps) {
  const { name, color, ratio, showRatio, ceremonial, ...badgeProps } = props
  const variant = badgeVariant(color)
  const badge = (
    <StatusBadge
      {...badgeProps}
      label={name}
      variant={variant}
      copyable={badgeProps.copyable ?? false}
      className={cn(
        'min-w-0 shrink overflow-hidden',
        ceremonial && [
          'bg-current/10 font-semibold tracking-wide ring-1 ring-current/25',
          'motion-safe:transition-[filter,transform] motion-safe:duration-200',
          'motion-safe:hover:-translate-y-0.5 motion-safe:hover:brightness-105',
        ],
        badgeProps.className
      )}
    />
  )
  if (!showRatio && !ceremonial) return badge

  return (
    <span
      className={cn(
        'inline-flex max-w-full min-w-0 items-center gap-2 text-xs',
        ceremonial && [
          'group/level-badge relative isolate rounded-full px-1 py-0.5',
          textColorMap[variant],
        ]
      )}
    >
      {ceremonial && (
        <span
          className='pointer-events-none absolute -inset-1 rounded-full bg-current/10 opacity-70 blur-md transition-opacity duration-300 group-hover/level-badge:opacity-100 motion-reduce:transition-none'
          aria-hidden='true'
        />
      )}
      {ceremonial && (
        <HugeiconsIcon
          icon={Crown02Icon}
          className='relative size-3.5 shrink-0'
          strokeWidth={1.8}
          aria-hidden='true'
        />
      )}
      {badge}
      {showRatio && ratio != null && (
        <span
          className={cn(
            'inline-flex h-5 shrink-0 items-center rounded-full px-1.5 font-mono text-xs leading-none font-medium tabular-nums',
            ceremonial
              ? 'bg-current/10 text-current ring-1 ring-current/20'
              : 'bg-info/10 text-info'
          )}
        >
          ×{ratio.toFixed(2)}
        </span>
      )}
    </span>
  )
}
