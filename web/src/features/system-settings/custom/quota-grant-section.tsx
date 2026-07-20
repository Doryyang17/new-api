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
import { zodResolver } from '@hookform/resolvers/zod'
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { Gift, Loader2, Search, Users } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import * as z from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { USER_ROLE, USER_STATUS } from '@/features/users/constants'
import { useDebounce } from '@/hooks'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'

import { SettingsSection } from '../components/settings-section'
import {
  grantUserQuota,
  listQuotaGrantTargetIds,
  listQuotaGrantTargets,
  type QuotaGrantFilters,
} from './quota-grant-api'

const QUOTA_GRANT_PAGE_SIZE = 20

const quotaGrantFormSchema = z.object({
  amountUsd: z
    .string()
    .trim()
    .regex(/^\d+(?:\.\d{1,2})?$/, '请输入有效的美元金额，最多保留两位小数')
    .refine((value) => Number(value) > 0, '发放金额必须大于 $0'),
  reason: z
    .string()
    .trim()
    .min(1, '请填写发放原因')
    .max(200, '发放原因不能超过 200 个字符'),
})

type QuotaGrantFormValues = z.infer<typeof quotaGrantFormSchema>

type PendingQuotaGrant = QuotaGrantFormValues & {
  requestId: string
  userIds: number[]
}

const roleOptions = [
  {
    value: USER_ROLE.USER,
    label: '普通用户',
    description: '不包含管理员和 Root 用户',
  },
  {
    value: USER_ROLE.ADMIN,
    label: '管理员',
    description: '包含管理员，不包含 Root 用户',
  },
] as const

const statusOptions = [
  {
    value: USER_STATUS.ENABLED,
    label: '已启用',
    description: '当前可以正常使用服务的用户',
  },
  {
    value: USER_STATUS.DISABLED,
    label: '已禁用',
    description: '当前被停用但仍保留数据的用户',
  },
] as const

function FilterCheckbox({
  id,
  checked,
  label,
  description,
  disabled = false,
  onCheckedChange,
}: {
  id: string
  checked: boolean
  label: string
  description: string
  disabled?: boolean
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <label
      htmlFor={id}
      className={cn(
        'border-border flex items-start gap-3 rounded-lg border p-3 transition-colors',
        disabled
          ? 'cursor-not-allowed opacity-60'
          : 'hover:bg-muted/40 cursor-pointer'
      )}
    >
      <Checkbox
        id={id}
        checked={checked}
        disabled={disabled}
        onCheckedChange={(value) => onCheckedChange(Boolean(value))}
        className='mt-0.5'
      />
      <span className='min-w-0'>
        <span className='block text-sm font-medium'>{label}</span>
        <span className='text-muted-foreground block text-xs leading-5'>
          {description}
        </span>
      </span>
    </label>
  )
}

export function QuotaGrantSection() {
  const queryClient = useQueryClient()
  const [roles, setRoles] = useState<number[]>([USER_ROLE.USER])
  const [statuses, setStatuses] = useState<number[]>([USER_STATUS.ENABLED])
  const [searchInput, setSearchInput] = useState('')
  const [page, setPage] = useState(1)
  const [selectedUserIds, setSelectedUserIds] = useState<Set<number>>(
    () => new Set()
  )
  const [pendingGrant, setPendingGrant] = useState<PendingQuotaGrant | null>(
    null
  )
  const keyword = useDebounce(searchInput.trim(), 300)

  const filters = useMemo<QuotaGrantFilters>(
    () => ({ keyword, roles, statuses }),
    [keyword, roles, statuses]
  )

  const form = useForm<QuotaGrantFormValues>({
    resolver: zodResolver(quotaGrantFormSchema),
    defaultValues: { amountUsd: '', reason: '' },
  })

  const targetsQuery = useQuery({
    queryKey: ['quota-grant-targets', filters, page, QUOTA_GRANT_PAGE_SIZE],
    queryFn: async () => {
      const response = await listQuotaGrantTargets(
        filters,
        page,
        QUOTA_GRANT_PAGE_SIZE
      )
      if (!response.success || !response.data) {
        throw new Error(response.message || '加载用户清单失败')
      }
      return response.data
    },
    placeholderData: keepPreviousData,
  })

  const selectAllMutation = useMutation({
    mutationFn: () => listQuotaGrantTargetIds(filters),
    onSuccess: (response) => {
      if (!response.success || !response.data) {
        toast.error(response.message || '选择筛选用户失败')
        return
      }
      setSelectedUserIds((current) => {
        const next = new Set(current)
        response.data?.ids.forEach((id) => next.add(id))
        return next
      })
      toast.success(`已选择当前筛选结果中的 ${response.data.ids.length} 位用户`)
    },
    onError: (error: Error) => {
      toast.error(error.message || '选择筛选用户失败')
    },
  })

  const grantMutation = useMutation({
    mutationFn: (grant: PendingQuotaGrant) =>
      grantUserQuota({
        request_id: grant.requestId,
        user_ids: grant.userIds,
        amount_usd: grant.amountUsd,
        reason: grant.reason,
      }),
    onSuccess: (response) => {
      if (!response.success || !response.data) {
        toast.error(response.message || '额度发放失败')
        return
      }
      const count = response.data.batch.target_count
      if (response.data.cache_sync_pending) {
        toast.warning(
          `额度已成功发放给 ${count} 位用户，余额缓存正在后台同步，显示可能短暂延迟`
        )
      } else if (response.data.already_processed) {
        toast.success(`该批次已处理，无需重复发放（${count} 人）`)
      } else {
        toast.success(`已成功为 ${count} 位用户发放额度`)
      }
      setPendingGrant(null)
      setSelectedUserIds(new Set())
      form.reset({ amountUsd: '', reason: '' })
      queryClient.invalidateQueries({ queryKey: ['quota-grant-targets'] })
      queryClient.invalidateQueries({ queryKey: ['users'] })
      queryClient.invalidateQueries({ queryKey: ['logs'] })
      queryClient.invalidateQueries({ queryKey: ['usage-logs-stats'] })
    },
    onError: (error: Error) => {
      toast.error(error.message || '额度发放失败')
    },
  })

  const targetPage = targetsQuery.data
  const pageUsers = targetPage?.items ?? []
  const total = targetPage?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / QUOTA_GRANT_PAGE_SIZE))
  const searchUpdatePending = searchInput.trim() !== keyword
  const filtersLocked = selectAllMutation.isPending
  const selectionLocked =
    searchUpdatePending ||
    targetsQuery.isFetching ||
    targetsQuery.isError ||
    filtersLocked
  const pageUserIds = pageUsers.map((user) => user.id)
  const allPageSelected =
    pageUserIds.length > 0 && pageUserIds.every((id) => selectedUserIds.has(id))
  const somePageSelected =
    !allPageSelected && pageUserIds.some((id) => selectedUserIds.has(id))

  const toggleFilter = (
    value: number,
    checked: boolean,
    current: number[],
    setCurrent: (values: number[]) => void,
    label: string
  ) => {
    if (!checked && current.length === 1 && current[0] === value) {
      toast.info(`请至少保留一个${label}`)
      return
    }
    const next = checked
      ? [...new Set([...current, value])]
      : current.filter((item) => item !== value)
    setCurrent(next)
    setPage(1)
  }

  const toggleUser = (userId: number, checked: boolean) => {
    setSelectedUserIds((current) => {
      const next = new Set(current)
      if (checked) next.add(userId)
      else next.delete(userId)
      return next
    })
  }

  const toggleCurrentPage = (checked: boolean) => {
    setSelectedUserIds((current) => {
      const next = new Set(current)
      pageUserIds.forEach((id) => {
        if (checked) next.add(id)
        else next.delete(id)
      })
      return next
    })
  }

  const onSubmit = (values: QuotaGrantFormValues) => {
    if (selectionLocked) {
      toast.info('用户清单或批量选择尚未完成，请稍候')
      return
    }
    if (selectedUserIds.size === 0) {
      toast.error('请至少勾选一位用户')
      return
    }
    setPendingGrant({
      ...values,
      requestId: crypto.randomUUID(),
      userIds: [...selectedUserIds].sort((a, b) => a - b),
    })
  }

  const pendingTotal = pendingGrant
    ? (Number(pendingGrant.amountUsd) * pendingGrant.userIds.length).toFixed(2)
    : '0.00'

  return (
    <SettingsSection title='批量发放额度'>
      <div className='grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]'>
        <Card>
          <CardHeader>
            <CardTitle className='flex items-center gap-2'>
              <Users className='size-4' />
              选择发放用户
            </CardTitle>
            <CardDescription>
              默认筛选“已启用的普通用户”。Root
              用户始终排除，筛选不会自动勾选，提交只以勾选清单为准。
            </CardDescription>
          </CardHeader>
          <CardContent className='space-y-4'>
            <div className='grid gap-3 sm:grid-cols-2'>
              <div className='space-y-2'>
                <div className='text-sm font-medium'>用户状态</div>
                {statusOptions.map((option) => (
                  <FilterCheckbox
                    key={option.value}
                    id={`quota-grant-status-${option.value}`}
                    checked={statuses.includes(option.value)}
                    label={option.label}
                    description={option.description}
                    disabled={filtersLocked}
                    onCheckedChange={(checked) =>
                      toggleFilter(
                        option.value,
                        checked,
                        statuses,
                        setStatuses,
                        '用户状态'
                      )
                    }
                  />
                ))}
              </div>
              <div className='space-y-2'>
                <div className='text-sm font-medium'>用户角色</div>
                {roleOptions.map((option) => (
                  <FilterCheckbox
                    key={option.value}
                    id={`quota-grant-role-${option.value}`}
                    checked={roles.includes(option.value)}
                    label={option.label}
                    description={option.description}
                    disabled={filtersLocked}
                    onCheckedChange={(checked) =>
                      toggleFilter(
                        option.value,
                        checked,
                        roles,
                        setRoles,
                        '用户角色'
                      )
                    }
                  />
                ))}
              </div>
            </div>

            <div className='flex flex-col gap-2 sm:flex-row sm:items-center'>
              <div className='relative min-w-0 flex-1'>
                <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2' />
                <Input
                  value={searchInput}
                  disabled={filtersLocked}
                  onChange={(event) => {
                    setSearchInput(event.target.value)
                    setPage(1)
                  }}
                  placeholder='按用户 ID、用户名、显示名或邮箱搜索'
                  className='pl-8'
                />
              </div>
              <Button
                type='button'
                variant='outline'
                disabled={total === 0 || selectionLocked}
                onClick={() => selectAllMutation.mutate()}
              >
                {selectAllMutation.isPending && (
                  <Loader2 className='size-4 animate-spin' />
                )}
                选择筛选结果（{total}）
              </Button>
              <Button
                type='button'
                variant='ghost'
                disabled={selectedUserIds.size === 0 || selectionLocked}
                onClick={() => setSelectedUserIds(new Set())}
              >
                清空已选
              </Button>
            </div>

            <div className='border-border overflow-hidden rounded-lg border'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className='w-10'>
                      <Checkbox
                        checked={allPageSelected}
                        indeterminate={somePageSelected}
                        disabled={selectionLocked}
                        onCheckedChange={(value) =>
                          toggleCurrentPage(Boolean(value))
                        }
                        aria-label='选择当前页用户'
                      />
                    </TableHead>
                    <TableHead>用户</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>角色</TableHead>
                    <TableHead>当前余额</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {targetsQuery.isLoading && (
                    <TableRow>
                      <TableCell colSpan={5} className='h-32 text-center'>
                        <Loader2 className='text-muted-foreground mx-auto size-5 animate-spin' />
                      </TableCell>
                    </TableRow>
                  )}
                  {targetsQuery.isError && (
                    <TableRow>
                      <TableCell colSpan={5} className='h-40 text-center'>
                        <div className='mx-auto flex max-w-md flex-col items-center gap-2'>
                          <div className='text-destructive font-medium'>
                            用户清单加载失败
                          </div>
                          <div className='text-muted-foreground text-sm'>
                            {targetsQuery.error.message ||
                              '请重新加载用户清单后再进行额度发放'}
                          </div>
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            disabled={targetsQuery.isFetching}
                            onClick={() => targetsQuery.refetch()}
                          >
                            {targetsQuery.isFetching && (
                              <Loader2 className='size-4 animate-spin' />
                            )}
                            重新加载
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  )}
                  {!targetsQuery.isLoading &&
                    !targetsQuery.isError &&
                    pageUsers.length === 0 && (
                      <TableRow>
                        <TableCell
                          colSpan={5}
                          className='text-muted-foreground h-32 text-center'
                        >
                          当前筛选条件下没有可发放用户
                        </TableCell>
                      </TableRow>
                    )}
                  {!targetsQuery.isLoading &&
                    !targetsQuery.isError &&
                    pageUsers.length > 0 &&
                    pageUsers.map((user) => (
                      <TableRow
                        key={user.id}
                        data-state={
                          selectedUserIds.has(user.id) ? 'selected' : undefined
                        }
                      >
                        <TableCell>
                          <Checkbox
                            checked={selectedUserIds.has(user.id)}
                            disabled={selectionLocked}
                            onCheckedChange={(value) =>
                              toggleUser(user.id, Boolean(value))
                            }
                            aria-label={`选择用户 ${user.username}`}
                          />
                        </TableCell>
                        <TableCell>
                          <div className='min-w-36'>
                            <div className='font-medium'>{user.username}</div>
                            <div className='text-muted-foreground max-w-64 truncate text-xs'>
                              ID {user.id}
                              {user.display_name
                                ? ` · ${user.display_name}`
                                : ''}
                              {user.email ? ` · ${user.email}` : ''}
                            </div>
                          </div>
                        </TableCell>
                        <TableCell>
                          <Badge
                            variant='outline'
                            className={
                              user.status === USER_STATUS.ENABLED
                                ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400'
                                : 'text-muted-foreground'
                            }
                          >
                            {user.status === USER_STATUS.ENABLED
                              ? '已启用'
                              : '已禁用'}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          {user.role === USER_ROLE.ADMIN
                            ? '管理员'
                            : '普通用户'}
                        </TableCell>
                        <TableCell>{formatQuota(user.quota)}</TableCell>
                      </TableRow>
                    ))}
                </TableBody>
              </Table>
            </div>

            <div className='flex flex-col gap-2 text-sm sm:flex-row sm:items-center sm:justify-between'>
              <div className='text-muted-foreground'>
                当前筛选 {total} 人，已勾选 {selectedUserIds.size} 人
                {targetsQuery.isFetching && !targetsQuery.isLoading
                  ? '，正在刷新…'
                  : ''}
              </div>
              <div className='flex items-center gap-2'>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={selectionLocked || page <= 1}
                  onClick={() => setPage((value) => Math.max(1, value - 1))}
                >
                  上一页
                </Button>
                <span className='text-muted-foreground min-w-20 text-center text-xs'>
                  第 {page} / {pageCount} 页
                </span>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={selectionLocked || page >= pageCount}
                  onClick={() =>
                    setPage((value) => Math.min(pageCount, value + 1))
                  }
                >
                  下一页
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card className='h-fit xl:sticky xl:top-4'>
          <CardHeader>
            <CardTitle className='flex items-center gap-2'>
              <Gift className='size-4' />
              填写发放内容
            </CardTitle>
            <CardDescription>
              金额固定以美元计价。原因会出现在每位用户自己的使用日志中。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Form {...form}>
              <form
                className='space-y-5'
                onSubmit={form.handleSubmit(onSubmit)}
              >
                <FormField
                  control={form.control}
                  name='amountUsd'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>每位用户发放金额</FormLabel>
                      <FormControl>
                        <div className='relative'>
                          <span className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-sm'>
                            $
                          </span>
                          <Input
                            {...field}
                            type='number'
                            min='0.01'
                            step='0.01'
                            inputMode='decimal'
                            placeholder='10.00'
                            className='pl-7'
                          />
                        </div>
                      </FormControl>
                      <FormDescription>
                        示例：填写 10，即每位选中用户增加 $10.00。
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='reason'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>发放原因</FormLabel>
                      <FormControl>
                        <Textarea
                          {...field}
                          rows={5}
                          maxLength={200}
                          placeholder='例如：服务器迁移补偿、活动奖励、服务故障补偿'
                        />
                      </FormControl>
                      <FormDescription>
                        必填，最多 200 个字符；用户可在自己的使用日志中看到。
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <div className='bg-muted/50 space-y-2 rounded-lg p-3 text-sm'>
                  <div className='flex items-center justify-between'>
                    <span className='text-muted-foreground'>已选用户</span>
                    <span className='font-semibold'>
                      {selectedUserIds.size} 人
                    </span>
                  </div>
                  <div className='text-muted-foreground text-xs leading-5'>
                    发放时会为每位用户写入一条可见日志，同时记录管理员批次审计；同一请求不会重复入账。
                  </div>
                </div>

                <Button
                  type='submit'
                  className='w-full'
                  disabled={selectedUserIds.size === 0 || selectionLocked}
                >
                  核对并发放
                </Button>
              </form>
            </Form>
          </CardContent>
        </Card>
      </div>

      <ConfirmDialog
        open={pendingGrant !== null}
        onOpenChange={(open) => {
          if (!open && !grantMutation.isPending) setPendingGrant(null)
        }}
        title='确认批量发放额度'
        desc={
          pendingGrant ? (
            <div className='space-y-3 text-left'>
              <p>
                即将为 <strong>{pendingGrant.userIds.length}</strong>{' '}
                位用户每人发放{' '}
                <strong>${Number(pendingGrant.amountUsd).toFixed(2)}</strong>
                ，合计 <strong>${pendingTotal}</strong>。
              </p>
              <div className='bg-muted rounded-lg p-3'>
                <div className='text-foreground text-xs font-medium'>
                  发放原因
                </div>
                <div className='text-muted-foreground mt-1 text-sm whitespace-pre-wrap'>
                  {pendingGrant.reason}
                </div>
              </div>
              <p className='text-muted-foreground text-xs'>
                提交后额度与用户日志会原子写入，不能在本页面撤销。
              </p>
            </div>
          ) : (
            ''
          )
        }
        confirmText={
          grantMutation.isPending ? (
            <>
              <Loader2 className='size-4 animate-spin' />
              正在发放
            </>
          ) : (
            '确认发放'
          )
        }
        isLoading={grantMutation.isPending}
        handleConfirm={() => {
          if (pendingGrant) grantMutation.mutate(pendingGrant)
        }}
      />
    </SettingsSection>
  )
}
