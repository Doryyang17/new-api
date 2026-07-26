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
import { CircleAlert, Clock3, LoaderCircle, Megaphone } from 'lucide-react'
import { toast } from 'sonner'

import { RichContent } from '@/components/rich-content'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { formatDateTimeObject } from '@/lib/time'

import { useMandatoryAnnouncements, useMarkAnnouncementRead } from '../hooks'
import { AnnouncementLevelBadge } from './announcement-level-badge'

export function MandatoryAnnouncementDialog() {
  const mandatoryQuery = useMandatoryAnnouncements()
  const markReadMutation = useMarkAnnouncementRead()
  const announcements = mandatoryQuery.data ?? []
  const current = announcements[0]

  if (mandatoryQuery.isLoading) {
    return (
      <Dialog open onOpenChange={() => undefined}>
        <DialogContent
          showCloseButton={false}
          overlayClassName='bg-transparent supports-backdrop-filter:backdrop-blur-none'
          className='pointer-events-none size-px gap-0 overflow-hidden border-0 bg-transparent p-0 opacity-0 shadow-none'
          aria-busy='true'
        >
          <DialogHeader>
            <DialogTitle>正在检查系统公告</DialogTitle>
            <DialogDescription>
              请稍候，正在同步你的阅读状态。
            </DialogDescription>
          </DialogHeader>
        </DialogContent>
      </Dialog>
    )
  }

  if (mandatoryQuery.isError && !current) {
    return (
      <Dialog open onOpenChange={() => undefined}>
        <DialogContent
          showCloseButton={false}
          overlayClassName='bg-black/55 supports-backdrop-filter:backdrop-blur-sm'
          className='gap-0 overflow-hidden p-0 sm:max-w-md'
        >
          <DialogHeader className='border-b px-5 py-4 text-left'>
            <DialogTitle>公告检查失败</DialogTitle>
            <DialogDescription>
              暂时无法确认是否存在必须阅读的公告，请重新检查。
            </DialogDescription>
          </DialogHeader>
          <div className='flex items-start gap-3 px-5 py-5'>
            <CircleAlert
              className='text-destructive mt-0.5 size-5 shrink-0'
              aria-hidden='true'
            />
            <p className='text-muted-foreground text-sm leading-6'>
              检查成功前无法继续使用系统，以免遗漏重要通知。
            </p>
          </div>
          <DialogFooter className='m-0 rounded-none border-t px-5 py-4'>
            <Button
              disabled={mandatoryQuery.isFetching}
              onClick={() => void mandatoryQuery.refetch()}
            >
              {mandatoryQuery.isFetching ? (
                <LoaderCircle className='animate-spin' aria-hidden='true' />
              ) : null}
              重新检查
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    )
  }
  if (!current) return null

  const handleAcknowledge = async () => {
    try {
      await markReadMutation.mutateAsync(current.key)
    } catch {
      const refreshed = await mandatoryQuery.refetch()
      if (refreshed.isError) {
        toast.error('阅读状态保存失败，公告状态也无法刷新，请重试')
      } else if (refreshed.data?.some((item) => item.key === current.key)) {
        toast.error('阅读状态保存失败，请重试')
      }
    }
  }

  return (
    <Dialog open onOpenChange={() => undefined}>
      <DialogContent
        showCloseButton={false}
        overlayClassName='bg-black/55 supports-backdrop-filter:backdrop-blur-sm'
        className='max-h-[calc(100dvh-2rem)] gap-0 overflow-hidden p-0 sm:max-w-2xl'
      >
        <DialogHeader className='border-b px-5 py-4 text-left sm:px-6'>
          <div className='flex items-start gap-3'>
            <div className='bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-xl'>
              <Megaphone className='size-5' aria-hidden='true' />
            </div>
            <div className='min-w-0 flex-1 space-y-2'>
              <div className='flex flex-wrap items-center gap-2'>
                <DialogTitle className='text-lg'>系统公告</DialogTitle>
                <AnnouncementLevelBadge level={current.level} />
              </div>
              <DialogDescription className='flex flex-wrap items-center gap-x-3 gap-y-1'>
                <span className='inline-flex items-center gap-1'>
                  <Clock3 className='size-3.5' aria-hidden='true' />
                  {formatDateTimeObject(new Date(current.publishDate))}
                </span>
                <span>第 1 条，共 {announcements.length} 条待确认</span>
              </DialogDescription>
            </div>
          </div>
        </DialogHeader>

        <div className='max-h-[min(58dvh,34rem)] overflow-y-auto overscroll-contain px-5 py-5 sm:px-6'>
          <h2 className='mb-4 text-lg font-semibold text-balance'>
            {current.title}
          </h2>
          <RichContent
            breaks
            content={current.content}
            className='max-w-none text-sm leading-7'
          />
        </div>

        <DialogFooter className='m-0 rounded-none border-t px-5 py-4 sm:px-6'>
          <Button
            size='lg'
            disabled={markReadMutation.isPending || mandatoryQuery.isFetching}
            onClick={handleAcknowledge}
            className='w-full sm:w-auto sm:min-w-32'
          >
            {markReadMutation.isPending || mandatoryQuery.isFetching ? (
              <LoaderCircle className='animate-spin' aria-hidden='true' />
            ) : null}
            我已知晓
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
