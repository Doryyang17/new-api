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
import { CircleAlert, Info, TriangleAlert } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

import type { AnnouncementLevel } from '../types'

const levelConfig = {
  normal: { label: '普通', icon: Info },
  important: { label: '重要', icon: CircleAlert },
  urgent: { label: '紧急', icon: TriangleAlert },
} as const

export function AnnouncementLevelBadge(props: {
  level?: AnnouncementLevel
  className?: string
}) {
  const level = props.level ?? 'normal'
  const config = levelConfig[level]
  const Icon = config.icon

  return (
    <Badge
      variant='outline'
      className={cn(
        'gap-1',
        level === 'important' && 'border-warning/30 bg-warning/10 text-warning',
        level === 'urgent' &&
          'border-destructive/30 bg-destructive/10 text-destructive',
        props.className
      )}
    >
      <Icon aria-hidden='true' />
      {config.label}
    </Badge>
  )
}
