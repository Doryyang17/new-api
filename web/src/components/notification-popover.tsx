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
import type { TFunction } from 'i18next'
import { Bell, Check, Clock3, Megaphone } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { RichContent } from '@/components/rich-content'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Popover,
  PopoverContent,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from '@/components/ui/popover'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { AnnouncementDetailDialog } from '@/features/announcements/components/announcement-detail-dialog'
import { AnnouncementLevelBadge } from '@/features/announcements/components/announcement-level-badge'
import type { Announcement } from '@/features/announcements/types'
import { formatDateTimeObject } from '@/lib/time'
import { cn } from '@/lib/utils'

interface NotificationPopoverProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  unreadCount: number
  activeTab: 'notice' | 'announcements'
  onTabChange: (tab: 'notice' | 'announcements') => void
  notice: string
  announcements: Announcement[]
  loading: boolean
  showAnnouncementCenter: boolean
  onAnnouncementRead: (announcement: Announcement) => Promise<void>
  className?: string
}

/**
 * Get relative time string from a date
 */
function getRelativeTime(publishDate: string | Date, t: TFunction): string {
  if (!publishDate) return ''

  const now = new Date()
  const pubDate = new Date(publishDate)

  // If invalid date, return original string
  if (Number.isNaN(pubDate.getTime())) {
    return typeof publishDate === 'string' ? publishDate : ''
  }

  const diffMs = now.getTime() - pubDate.getTime()
  const diffSeconds = Math.floor(diffMs / 1000)
  const diffMinutes = Math.floor(diffSeconds / 60)
  const diffHours = Math.floor(diffMinutes / 60)
  const diffDays = Math.floor(diffHours / 24)
  const diffWeeks = Math.floor(diffDays / 7)
  const diffMonths = Math.floor(diffDays / 30)
  const diffYears = Math.floor(diffDays / 365)

  // If future time, show specific date
  if (diffMs < 0) return formatDateTimeObject(pubDate)

  // Return relative time based on difference
  if (diffSeconds < 60) return t('Just now')
  if (diffMinutes < 60) {
    return diffMinutes === 1
      ? t('1 minute ago')
      : t('{{count}} minutes ago', { count: diffMinutes })
  }
  if (diffHours < 24) {
    return diffHours === 1
      ? t('1 hour ago')
      : t('{{count}} hours ago', { count: diffHours })
  }
  if (diffDays < 7) {
    return diffDays === 1
      ? t('1 day ago')
      : t('{{count}} days ago', { count: diffDays })
  }
  if (diffWeeks < 4) {
    return diffWeeks === 1
      ? t('1 week ago')
      : t('{{count}} weeks ago', { count: diffWeeks })
  }
  if (diffMonths < 12) {
    return diffMonths === 1
      ? t('1 month ago')
      : t('{{count}} months ago', { count: diffMonths })
  }
  if (diffYears < 2) return t('1 year ago')

  // Over 2 years, show specific date
  return formatDateTimeObject(pubDate)
}

/**
 * Empty state component
 */
function EmptyState({
  icon,
  title,
  description,
}: {
  icon: React.ReactNode
  title: string
  description?: string
}) {
  return (
    <Empty className='min-h-48 border-0 p-4'>
      <EmptyHeader>
        <EmptyMedia variant='icon'>{icon}</EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        {description ? (
          <EmptyDescription>{description}</EmptyDescription>
        ) : null}
      </EmptyHeader>
    </Empty>
  )
}

/**
 * Notice tab content
 */
function NoticeContent({
  notice,
  loading,
  t,
}: {
  notice: string
  loading: boolean
  t: TFunction
}) {
  if (loading) {
    return (
      <EmptyState
        icon={<Bell />}
        title={t('Loading...')}
        description={t('Latest platform updates and notices')}
      />
    )
  }

  if (!notice) {
    return (
      <EmptyState icon={<Bell />} title={t('No announcements at this time')} />
    )
  }

  return (
    <ScrollArea className='h-[min(52vh,28rem)] pr-3'>
      <RichContent breaks content={notice} />
    </ScrollArea>
  )
}

/**
 * Announcements tab content
 */
function AnnouncementsContent({
  announcements,
  loading,
  showReadState,
  t,
  onOpen,
}: {
  announcements: Announcement[]
  loading: boolean
  showReadState: boolean
  t: TFunction
  onOpen: (announcement: Announcement) => void
}) {
  if (loading) {
    return (
      <EmptyState
        icon={<Megaphone />}
        title={t('Loading...')}
        description={t('Latest platform updates and notices')}
      />
    )
  }

  if (announcements.length === 0) {
    return (
      <EmptyState icon={<Megaphone />} title={t('No system announcements')} />
    )
  }

  return (
    <ScrollArea className='h-[min(52vh,28rem)] pr-3'>
      <div className='flex flex-col'>
        {announcements.map((item, idx) => {
          const publishDate = item.publishDate
            ? new Date(item.publishDate)
            : null
          const relativeTime = publishDate
            ? getRelativeTime(publishDate, t)
            : ''
          const absoluteTime = publishDate
            ? formatDateTimeObject(publishDate)
            : ''

          return (
            <div key={item.key}>
              <button
                type='button'
                onClick={() => onOpen(item)}
                className='hover:bg-muted/50 focus-visible:ring-ring w-full rounded-lg px-2 py-3 text-left transition-colors outline-none focus-visible:ring-3'
              >
                <div className='flex items-start gap-3'>
                  {showReadState ? (
                    <span
                      className={cn(
                        'mt-2 size-2 shrink-0 rounded-full',
                        item.read ? 'bg-muted-foreground/30' : 'bg-primary'
                      )}
                      aria-hidden='true'
                    />
                  ) : null}
                  <div className='flex min-w-0 flex-1 flex-col gap-2'>
                    <div className='flex flex-wrap items-center gap-1.5'>
                      <AnnouncementLevelBadge level={item.level} />
                      {showReadState ? (
                        <span className='text-muted-foreground inline-flex items-center gap-1 text-xs'>
                          {item.read ? <Check className='size-3' /> : null}
                          {item.read ? '已阅读' : '未阅读'}
                        </span>
                      ) : null}
                    </div>
                    <p className='line-clamp-2 text-sm leading-5 font-medium'>
                      {item.title}
                    </p>
                    {item.summary ? (
                      <p className='text-muted-foreground line-clamp-2 text-xs leading-5'>
                        {item.summary}
                      </p>
                    ) : null}
                    {absoluteTime ? (
                      <div className='text-muted-foreground flex items-center gap-1 text-xs'>
                        <Clock3 className='size-3' aria-hidden='true' />
                        {relativeTime ? `${relativeTime} · ` : null}
                        {absoluteTime}
                      </div>
                    ) : null}
                  </div>
                </div>
              </button>
              {idx < announcements.length - 1 ? <Separator /> : null}
            </div>
          )
        })}
      </div>
    </ScrollArea>
  )
}

/**
 * Notification popover with Notice and Announcements tabs
 */
export function NotificationPopover({
  open,
  onOpenChange,
  unreadCount,
  activeTab,
  onTabChange,
  notice,
  announcements,
  loading,
  showAnnouncementCenter,
  onAnnouncementRead,
  className,
}: NotificationPopoverProps) {
  const { t } = useTranslation()
  const [selectedAnnouncement, setSelectedAnnouncement] =
    useState<Announcement | null>(null)

  const handleOpenAnnouncement = (announcement: Announcement) => {
    onOpenChange(false)
    setSelectedAnnouncement(announcement)
    void onAnnouncementRead(announcement).catch(() => {
      toast.error('阅读状态保存失败，请稍后重试')
    })
  }

  return (
    <>
      <Popover open={open} onOpenChange={onOpenChange}>
        <PopoverTrigger
          render={
            <Button
              variant='ghost'
              size='icon'
              className={cn('relative size-9', className)}
              aria-label={t('Notifications')}
            />
          }
        >
          <Bell className='size-[1.2rem]' />
          {unreadCount > 0 ? (
            <Badge
              variant='destructive'
              className='absolute -top-1 -right-1 flex h-5 min-w-5 items-center justify-center px-1 text-[10px] font-semibold tabular-nums'
            >
              {unreadCount > 99 ? '99+' : unreadCount}
            </Badge>
          ) : null}
        </PopoverTrigger>

        <PopoverContent
          align='end'
          sideOffset={8}
          className='w-[min(26rem,calc(100vw-1rem))] gap-3 p-3'
        >
          <PopoverHeader className='gap-1 px-1'>
            <PopoverTitle>{t('System Announcements')}</PopoverTitle>
            <p className='text-muted-foreground text-xs'>
              {t('Latest platform updates and notices')}
            </p>
          </PopoverHeader>

          <Tabs
            value={activeTab}
            onValueChange={onTabChange as (value: string) => void}
          >
            <TabsList className='grid w-full grid-cols-2'>
              <TabsTrigger value='notice' className='gap-1.5'>
                <Bell className='size-3.5' />
                {t('Notice')}
              </TabsTrigger>
              <TabsTrigger value='announcements' className='gap-1.5'>
                <Megaphone className='size-3.5' />
                公告
              </TabsTrigger>
            </TabsList>

            <TabsContent value='notice' className='mt-2'>
              <NoticeContent notice={notice} loading={loading} t={t} />
            </TabsContent>

            <TabsContent value='announcements' className='mt-2'>
              <AnnouncementsContent
                announcements={announcements}
                loading={loading}
                showReadState={showAnnouncementCenter}
                t={t}
                onOpen={handleOpenAnnouncement}
              />
            </TabsContent>
          </Tabs>

          <div className='flex items-center justify-end gap-2'>
            {showAnnouncementCenter ? (
              <Button
                size='sm'
                variant='outline'
                render={<Link to='/announcements' />}
                onClick={() => onOpenChange(false)}
              >
                查看全部公告
              </Button>
            ) : null}
            <Button size='sm' onClick={() => onOpenChange(false)}>
              {t('Close')}
            </Button>
          </div>
        </PopoverContent>
      </Popover>

      <AnnouncementDetailDialog
        announcement={selectedAnnouncement}
        open={selectedAnnouncement != null}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) setSelectedAnnouncement(null)
        }}
      />
    </>
  )
}
