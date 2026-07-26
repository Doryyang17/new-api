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
  ArrowLeft,
  BellRing,
  Check,
  ChevronLeft,
  ChevronRight,
  Clock3,
  LockKeyhole,
  Pin,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import { EmptyState } from '@/components/empty-state'
import { SectionPageLayout } from '@/components/layout'
import { RichContent } from '@/components/rich-content'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Skeleton } from '@/components/ui/skeleton'
import { useMediaQuery } from '@/hooks'
import { formatDateTimeObject } from '@/lib/time'
import { cn } from '@/lib/utils'

import { AnnouncementLevelBadge } from './components/announcement-level-badge'
import { AnnouncementListItem } from './components/announcement-list-item'
import { useAnnouncements, useMarkAnnouncementRead } from './hooks'
import { clampAnnouncementPage, getAnnouncementTotalPages } from './pagination'
import type { Announcement } from './types'

const PAGE_SIZE = 12

export function AnnouncementCenter(props: {
  page: number
  onPageChange: (page: number) => void
}) {
  const requestedPage = props.page
  const onPageChange = props.onPageChange
  const announcementsQuery = useAnnouncements(requestedPage, PAGE_SIZE)
  const { mutate: markRead } = useMarkAnnouncementRead()
  const isDesktop = useMediaQuery('(min-width: 1024px)')
  const attemptedReadKeysRef = useRef(new Set<string>())
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const [mobileDetailOpen, setMobileDetailOpen] = useState(false)
  const data = announcementsQuery.data
  const totalPages = getAnnouncementTotalPages(data?.total ?? 0, PAGE_SIZE)
  const selectedAnnouncement =
    data?.items.find((announcement) => announcement.key === selectedKey) ??
    data?.items[0] ??
    null

  useEffect(() => {
    if (
      !selectedAnnouncement ||
      selectedAnnouncement.read ||
      announcementsQuery.isPlaceholderData ||
      (!isDesktop && !mobileDetailOpen) ||
      attemptedReadKeysRef.current.has(selectedAnnouncement.key)
    ) {
      return
    }

    attemptedReadKeysRef.current.add(selectedAnnouncement.key)
    markRead(selectedAnnouncement.key, {
      onError: () => {
        attemptedReadKeysRef.current.delete(selectedAnnouncement.key)
        toast.error('阅读状态保存失败，请稍后重试')
      },
    })
  }, [
    announcementsQuery.isPlaceholderData,
    isDesktop,
    markRead,
    mobileDetailOpen,
    selectedAnnouncement,
  ])

  useEffect(() => {
    if (!data || announcementsQuery.isPlaceholderData) return
    const nextPage = clampAnnouncementPage(requestedPage, totalPages)
    if (nextPage === requestedPage) return

    setSelectedKey(null)
    setMobileDetailOpen(false)
    onPageChange(nextPage)
  }, [
    announcementsQuery.isPlaceholderData,
    data,
    onPageChange,
    requestedPage,
    totalPages,
  ])

  const handleSelectAnnouncement = (announcement: Announcement) => {
    setSelectedKey(announcement.key)
    setMobileDetailOpen(true)
  }

  const handlePageChange = (page: number) => {
    setSelectedKey(null)
    setMobileDetailOpen(false)
    onPageChange(page)
  }

  let announcementListContent
  if (announcementsQuery.isLoading) {
    announcementListContent = (
      <div aria-label='公告加载中'>
        {[0, 1, 2, 3, 4].map((item) => (
          <div key={item} className='space-y-2 border-b px-4 py-3 lg:py-2'>
            <div className='flex items-center justify-between gap-3'>
              <Skeleton className='h-4 w-1/2' />
              <Skeleton className='h-3 w-20' />
            </div>
            <Skeleton className='h-3 w-3/4' />
            <div className='flex items-center justify-between gap-3'>
              <Skeleton className='h-5 w-28' />
              <Skeleton className='h-3 w-10' />
            </div>
          </div>
        ))}
      </div>
    )
  } else if (announcementsQuery.isError) {
    announcementListContent = (
      <EmptyState
        icon={BellRing}
        title='公告加载失败'
        description='暂时无法读取公告，请检查网络后重试。'
        action={
          <Button onClick={() => void announcementsQuery.refetch()}>
            重新加载
          </Button>
        }
      />
    )
  } else if (data?.items.length) {
    announcementListContent = (
      <div role='list' aria-label='公告列表'>
        {data.items.map((announcement) => (
          <div key={announcement.key} role='listitem'>
            <AnnouncementListItem
              announcement={announcement}
              selected={announcement.key === selectedAnnouncement?.key}
              onSelect={handleSelectAnnouncement}
            />
          </div>
        ))}
      </div>
    )
  } else {
    announcementListContent = (
      <EmptyState
        icon={BellRing}
        title='暂无公告'
        description='新公告发布后会显示在这里。'
      />
    )
  }

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>公告中心</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Badge variant={data?.unread_count ? 'destructive' : 'outline'}>
          {data?.unread_count ? `未读 ${data.unread_count}` : '全部已读'}
        </Badge>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='bg-card grid h-full min-h-0 overflow-hidden rounded-xl border shadow-xs lg:grid-cols-12'>
          <section
            aria-label='公告列表'
            className={cn(
              'min-h-0 flex-col lg:col-span-4 lg:flex lg:border-r xl:col-span-3',
              mobileDetailOpen ? 'hidden' : 'flex'
            )}
          >
            <header className='flex shrink-0 items-center justify-between gap-3 border-b px-4 py-3'>
              <div className='min-w-0'>
                <h3 className='text-sm font-semibold'>公告列表</h3>
                <p className='text-muted-foreground mt-0.5 truncate text-xs'>
                  最新发布优先 · 阅读状态跨设备同步
                </p>
              </div>
              <span className='text-muted-foreground shrink-0 text-xs tabular-nums'>
                共 {data?.total ?? 0} 条
              </span>
            </header>

            <ScrollArea className='min-h-0 flex-1'>
              {announcementListContent}
            </ScrollArea>

            {totalPages > 1 ? (
              <nav
                aria-label='公告分页'
                className='flex shrink-0 items-center justify-between gap-2 border-t px-3 py-2.5'
              >
                <Button
                  size='sm'
                  variant='ghost'
                  disabled={requestedPage <= 1 || announcementsQuery.isFetching}
                  onClick={() => handlePageChange(requestedPage - 1)}
                >
                  <ChevronLeft aria-hidden='true' />
                  上一页
                </Button>
                <span className='text-muted-foreground text-xs tabular-nums'>
                  {requestedPage} / {totalPages}
                </span>
                <Button
                  size='sm'
                  variant='ghost'
                  disabled={
                    requestedPage >= totalPages || announcementsQuery.isFetching
                  }
                  onClick={() => handlePageChange(requestedPage + 1)}
                >
                  下一页
                  <ChevronRight aria-hidden='true' />
                </Button>
              </nav>
            ) : null}
          </section>

          <section
            aria-label='公告详情'
            className={cn(
              'min-h-0 flex-col lg:col-span-8 lg:flex xl:col-span-9',
              mobileDetailOpen ? 'flex' : 'hidden'
            )}
          >
            {selectedAnnouncement ? (
              <>
                <header className='shrink-0 border-b px-4 py-4 sm:px-6 sm:py-5'>
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    className='mb-3 -ml-2 lg:hidden'
                    onClick={() => setMobileDetailOpen(false)}
                  >
                    <ArrowLeft aria-hidden='true' />
                    返回公告列表
                  </Button>

                  <div className='flex flex-wrap items-center gap-1.5'>
                    <AnnouncementLevelBadge
                      level={selectedAnnouncement.level}
                    />
                    {selectedAnnouncement.forceRead ? (
                      <Badge variant='outline' className='gap-1'>
                        <LockKeyhole aria-hidden='true' />
                        强制阅读
                      </Badge>
                    ) : null}
                    {selectedAnnouncement.pinned ? (
                      <Badge variant='outline' className='gap-1'>
                        <Pin aria-hidden='true' />
                        置顶
                      </Badge>
                    ) : null}
                  </div>

                  <h1 className='mt-3 text-xl leading-tight font-semibold text-balance sm:text-2xl'>
                    {selectedAnnouncement.title}
                  </h1>
                  <div className='text-muted-foreground mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs'>
                    <span className='inline-flex items-center gap-1.5'>
                      <BellRing className='size-3.5' aria-hidden='true' />
                      系统公告
                    </span>
                    <span className='inline-flex items-center gap-1.5 tabular-nums'>
                      <Clock3 className='size-3.5' aria-hidden='true' />
                      {formatDateTimeObject(
                        new Date(selectedAnnouncement.publishDate)
                      )}
                    </span>
                    <span
                      className={cn(
                        'inline-flex items-center gap-1.5 font-medium',
                        selectedAnnouncement.read
                          ? 'text-muted-foreground'
                          : 'text-primary'
                      )}
                    >
                      {selectedAnnouncement.read ? (
                        <Check className='size-3.5' aria-hidden='true' />
                      ) : (
                        <span
                          className='bg-primary size-2 rounded-full'
                          aria-hidden='true'
                        />
                      )}
                      {selectedAnnouncement.read
                        ? '已阅读'
                        : '正在同步阅读状态'}
                    </span>
                  </div>
                </header>

                <ScrollArea className='min-h-0 flex-1'>
                  <article className='w-full max-w-4xl px-4 py-6 text-left sm:px-6 sm:py-8'>
                    <RichContent
                      breaks
                      content={selectedAnnouncement.content}
                      className='prose-headings:text-balance max-w-none text-sm leading-7 sm:text-base sm:leading-8'
                    />
                  </article>
                </ScrollArea>
              </>
            ) : (
              <div className='flex h-full items-center justify-center p-6'>
                <EmptyState
                  icon={BellRing}
                  title={
                    announcementsQuery.isLoading ? '公告加载中' : '选择一条公告'
                  }
                  description={
                    announcementsQuery.isLoading
                      ? '正在同步最新公告。'
                      : '从左侧列表选择公告后，可在这里连续阅读完整内容。'
                  }
                />
              </div>
            )}
          </section>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
