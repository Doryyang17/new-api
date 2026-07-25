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
import { Check, Clock3, LockKeyhole, Pin } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { formatDateTimeObject } from '@/lib/time'
import { cn } from '@/lib/utils'

import type { Announcement } from '../types'
import { AnnouncementLevelBadge } from './announcement-level-badge'

const compactDateFormatter = new Intl.DateTimeFormat('zh-CN', {
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
})

export function AnnouncementListItem(props: {
  announcement: Announcement
  selected: boolean
  onSelect: (announcement: Announcement) => void
}) {
  const announcement = props.announcement
  const publishDate = new Date(announcement.publishDate)

  return (
    <button
      type='button'
      onClick={() => props.onSelect(announcement)}
      aria-label={`阅读公告：${announcement.title}（${announcement.read ? '已读' : '未读'}）`}
      aria-pressed={props.selected}
      className={cn(
        'focus-visible:ring-ring relative flex w-full gap-2 border-b px-4 py-3 text-left outline-none transition-colors hover:bg-muted/50 focus-visible:z-10 focus-visible:ring-3 lg:py-2',
        props.selected && 'bg-muted/70 hover:bg-muted/70'
      )}
    >
      <span
        className='flex w-2 shrink-0 justify-center pt-1.5'
        aria-hidden='true'
      >
        <span
          className={cn(
            'size-2 rounded-full',
            announcement.read ? 'bg-transparent' : 'bg-primary'
          )}
        />
      </span>

      <span className='min-w-0 flex-1'>
        <span className='flex items-center justify-between gap-3'>
          <span
            className={cn(
              'min-w-0 truncate text-sm',
              announcement.read ? 'font-medium' : 'font-semibold'
            )}
          >
            {announcement.title}
          </span>
          <time
            dateTime={announcement.publishDate}
            title={formatDateTimeObject(publishDate)}
            className='text-muted-foreground shrink-0 text-xs tabular-nums'
          >
            {compactDateFormatter.format(publishDate)}
          </time>
        </span>

        <span className='text-muted-foreground mt-1 block truncate text-xs leading-4'>
          {announcement.summary || announcement.content}
        </span>

        <span className='mt-1 flex min-w-0 items-center gap-1.5 overflow-hidden'>
          <span className='flex min-w-0 items-center gap-1.5 overflow-hidden'>
            <AnnouncementLevelBadge
              level={announcement.level}
              className='h-5 px-1.5 text-xs'
            />
            {announcement.forceRead ? (
              <Badge variant='outline' className='h-5 gap-1 px-1.5 text-xs'>
                <LockKeyhole aria-hidden='true' />
                强制
              </Badge>
            ) : null}
            {announcement.pinned ? (
              <Badge variant='outline' className='h-5 gap-1 px-1.5 text-xs'>
                <Pin aria-hidden='true' />
                置顶
              </Badge>
            ) : null}
          </span>
          <span
            className={cn(
              'ml-auto flex shrink-0 items-center gap-1 text-xs font-medium',
              announcement.read ? 'text-muted-foreground' : 'text-primary'
            )}
          >
            {announcement.read ? (
              <Check className='size-3.5' aria-hidden='true' />
            ) : (
              <Clock3 className='size-3.5' aria-hidden='true' />
            )}
            {announcement.read ? '已读' : '未读'}
          </span>
        </span>
      </span>
    </button>
  )
}
