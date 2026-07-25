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
import { Clock3 } from 'lucide-react'

import { Dialog } from '@/components/dialog'
import { RichContent } from '@/components/rich-content'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { formatDateTimeObject } from '@/lib/time'

import type { Announcement } from '../types'
import { AnnouncementLevelBadge } from './announcement-level-badge'

export function AnnouncementDetailDialog(props: {
  announcement: Announcement | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const announcement = props.announcement

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={announcement?.title || '公告详情'}
      description={
        announcement ? (
          <span className='flex flex-wrap items-center gap-2 pt-1'>
            <AnnouncementLevelBadge level={announcement.level} />
            {announcement.forceRead ? (
              <Badge variant='outline'>强制阅读</Badge>
            ) : null}
            <span className='inline-flex items-center gap-1'>
              <Clock3 className='size-3.5' aria-hidden='true' />
              {formatDateTimeObject(new Date(announcement.publishDate))}
            </span>
          </span>
        ) : null
      }
      contentClassName='sm:max-w-3xl'
      bodyClassName='py-2 sm:py-3'
      footer={<Button onClick={() => props.onOpenChange(false)}>关闭</Button>}
    >
      {announcement ? (
        <RichContent
          breaks
          content={announcement.content}
          className='prose-headings:text-balance max-w-none text-sm leading-7'
        />
      ) : null}
    </Dialog>
  )
}
