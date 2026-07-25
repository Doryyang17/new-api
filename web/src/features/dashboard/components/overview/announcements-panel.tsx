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
import { Link } from '@tanstack/react-router'
import { ArrowRight, Check, Clock3, Megaphone } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { IconBadge } from '@/components/ui/icon-badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { AnnouncementDetailDialog } from '@/features/announcements/components/announcement-detail-dialog'
import { AnnouncementLevelBadge } from '@/features/announcements/components/announcement-level-badge'
import {
  useAnnouncements,
  useMarkAnnouncementRead,
} from '@/features/announcements/hooks'
import type { Announcement } from '@/features/announcements/types'
import { formatDateTimeObject } from '@/lib/time'
import { cn } from '@/lib/utils'

import { PanelWrapper } from '../ui/panel-wrapper'

export function AnnouncementsPanel() {
  const announcementsQuery = useAnnouncements(1, 5)
  const markReadMutation = useMarkAnnouncementRead()
  const list = announcementsQuery.data?.items ?? []
  const unreadCount = announcementsQuery.data?.unread_count ?? 0
  const [selectedAnnouncement, setSelectedAnnouncement] =
    useState<Announcement | null>(null)

  const handleAnnouncementClick = (item: Announcement) => {
    setSelectedAnnouncement(item)
    if (!item.read) {
      void markReadMutation.mutateAsync(item.key).catch(() => {
        toast.error('阅读状态保存失败，请稍后重试')
      })
    }
  }

  return (
    <PanelWrapper
      title={
        <span className='flex items-center gap-2'>
          <IconBadge tone='warning' size='sm'>
            <Megaphone />
          </IconBadge>
          最新公告
        </span>
      }
      description={
        unreadCount > 0
          ? `你有 ${unreadCount} 条未读公告`
          : '平台动态与重要通知'
      }
      loading={announcementsQuery.isLoading}
      empty={!list.length}
      emptyMessage='暂无公告'
      height='h-72'
      contentClassName='p-0'
      headerActions={
        <div className='flex items-center gap-1.5'>
          {unreadCount > 0 ? (
            <Badge variant='destructive'>{unreadCount} 未读</Badge>
          ) : null}
          <Button
            size='sm'
            variant='ghost'
            render={<Link to='/announcements' />}
          >
            查看更多
            <ArrowRight aria-hidden='true' />
          </Button>
        </div>
      }
    >
      <ScrollArea className='h-72'>
        <div>
          {list.map((item, idx) => {
            return (
              <button
                key={item.key}
                type='button'
                onClick={() => handleAnnouncementClick(item)}
                className={cn(
                  'group hover:bg-muted/40 focus-visible:ring-ring w-full px-3 py-3 text-left outline-none transition-colors focus-visible:ring-3 sm:px-5 sm:py-3.5',
                  idx < list.length - 1 && 'border-border/60 border-b'
                )}
              >
                <div className='flex items-start gap-2.5'>
                  <span
                    className={cn(
                      'mt-2 size-2 shrink-0 rounded-full',
                      item.read ? 'bg-muted-foreground/30' : 'bg-primary'
                    )}
                    aria-hidden='true'
                  />
                  <div className='flex min-w-0 flex-1 flex-col gap-1'>
                    <div className='flex flex-wrap items-center gap-1.5'>
                      <AnnouncementLevelBadge level={item.level} />
                      <span className='text-muted-foreground inline-flex items-center gap-1 text-xs'>
                        {item.read ? <Check className='size-3' /> : null}
                        {item.read ? '已阅读' : '未阅读'}
                      </span>
                    </div>
                    <p className='line-clamp-1 text-sm font-medium'>
                      {item.title}
                    </p>
                    <time className='text-muted-foreground/70 flex items-center gap-1 text-xs'>
                      <Clock3 className='size-3' aria-hidden='true' />
                      {formatDateTimeObject(new Date(item.publishDate))}
                    </time>
                  </div>
                </div>
              </button>
            )
          })}
        </div>
      </ScrollArea>

      <AnnouncementDetailDialog
        open={selectedAnnouncement != null}
        onOpenChange={(open) => {
          if (!open) setSelectedAnnouncement(null)
        }}
        announcement={selectedAnnouncement}
      />
    </PanelWrapper>
  )
}
