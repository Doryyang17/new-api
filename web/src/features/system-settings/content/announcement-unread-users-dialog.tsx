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
import { useQuery } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight, CircleAlert, Users } from 'lucide-react'
import { useState } from 'react'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { getAnnouncementUnreadUsers } from '@/features/announcements/api'
import { formatDateTimeObject } from '@/lib/time'

const PAGE_SIZE = 20

export function AnnouncementUnreadUsersDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  announcementKey: string
  announcementTitle: string
}) {
  const [page, setPage] = useState(1)
  const usersQuery = useQuery({
    queryKey: [
      'announcements',
      'admin',
      'unread-users',
      props.announcementKey,
      page,
    ],
    queryFn: () =>
      getAnnouncementUnreadUsers(props.announcementKey, page, PAGE_SIZE),
    enabled: props.open && props.announcementKey !== '',
  })
  const data = usersQuery.data
  const totalPages = Math.max(1, Math.ceil((data?.total ?? 0) / PAGE_SIZE))

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        props.onOpenChange(open)
        if (!open) setPage(1)
      }}
      title='未读用户'
      description={props.announcementTitle}
      contentClassName='sm:max-w-4xl'
      bodyClassName='space-y-3'
      footer={
        usersQuery.isError ? null : (
          <div className='flex w-full items-center justify-between gap-3'>
            <span className='text-muted-foreground text-xs tabular-nums'>
              共 {data?.total ?? 0} 人
            </span>
            <div className='flex items-center gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={page <= 1 || usersQuery.isFetching}
                onClick={() => setPage((current) => current - 1)}
              >
                <ChevronLeft aria-hidden='true' />
                上一页
              </Button>
              <span className='text-muted-foreground min-w-16 text-center text-xs tabular-nums'>
                {page} / {totalPages}
              </span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={page >= totalPages || usersQuery.isFetching}
                onClick={() => setPage((current) => current + 1)}
              >
                下一页
                <ChevronRight aria-hidden='true' />
              </Button>
            </div>
          </div>
        )
      }
    >
      {usersQuery.isLoading ? (
        <Skeleton className='h-72 w-full rounded-xl' />
      ) : null}
      {usersQuery.isError ? (
        <div className='flex min-h-72 flex-col items-center justify-center gap-3 text-center'>
          <CircleAlert className='text-destructive size-7' aria-hidden='true' />
          <div>
            <p className='font-medium'>未读用户加载失败</p>
            <p className='text-muted-foreground mt-1 text-sm'>
              暂时无法获取阅读统计，请稍后重试。
            </p>
          </div>
          <Button
            type='button'
            variant='outline'
            disabled={usersQuery.isFetching}
            onClick={() => void usersQuery.refetch()}
          >
            重新加载
          </Button>
        </div>
      ) : null}
      {!usersQuery.isLoading && !usersQuery.isError ? (
        <StaticDataTable
          data={data?.items ?? []}
          getRowKey={(user) => user.id}
          emptyContent={
            <div className='text-muted-foreground flex min-h-48 flex-col items-center justify-center gap-2'>
              <Users className='size-6' aria-hidden='true' />
              <span>所有可用用户均已阅读</span>
            </div>
          }
          columns={[
            {
              id: 'user',
              header: '用户',
              cell: (user) => (
                <div className='min-w-0'>
                  <p className='truncate font-medium'>
                    {user.display_name || user.username}
                  </p>
                  <p className='text-muted-foreground truncate text-xs'>
                    @{user.username}
                  </p>
                </div>
              ),
            },
            {
              id: 'email',
              header: '邮箱',
              cell: (user) => user.email || '-',
            },
            {
              id: 'created',
              header: '注册时间',
              cell: (user) =>
                user.created_at
                  ? formatDateTimeObject(new Date(user.created_at * 1000))
                  : '-',
            },
            {
              id: 'last-login',
              header: '最近登录',
              cell: (user) =>
                user.last_login_at
                  ? formatDateTimeObject(new Date(user.last_login_at * 1000))
                  : '尚未登录',
            },
          ]}
        />
      ) : null}
    </Dialog>
  )
}
