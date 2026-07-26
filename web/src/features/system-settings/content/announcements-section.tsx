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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  BarChart3,
  CircleAlert,
  LockKeyhole,
  Plus,
  Save,
  Trash2,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useForm, type SubmitErrorHandler } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { DateTimePicker } from '@/components/datetime-picker'
import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { getAllAnnouncementStats } from '@/features/announcements/api'
import { AnnouncementLevelBadge } from '@/features/announcements/components/announcement-level-badge'
import { announcementQueryKeys } from '@/features/announcements/hooks'
import type {
  AnnouncementLevel,
  AnnouncementStat,
} from '@/features/announcements/types'
import { formatDateTimeObject } from '@/lib/time'

import {
  SettingsFormGrid,
  SettingsSwitchField,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  normalizeAnnouncementImmediate,
  resolveAnnouncementPublishDate,
} from './announcement-publish-state'
import { AnnouncementUnreadUsersDialog } from './announcement-unread-users-dialog'

type AnnouncementConfig = {
  rowKey: string
  id?: number | string
  title: string
  content: string
  publishDate: string
  level: AnnouncementLevel
  forceRead: boolean
  immediate: boolean
  extra?: string
  type?: string
  category?: string
  pinned?: boolean
  offlineAt?: string
}

type AnnouncementsSectionProps = {
  enabled: boolean
  data: string
}

const announcementSchema = z.object({
  title: z
    .string()
    .trim()
    .min(1, '请输入公告标题')
    .max(120, '标题不能超过 120 个字符'),
  content: z
    .string()
    .trim()
    .min(1, '请输入公告内容')
    .max(10000, '内容不能超过 10000 个字符'),
  extra: z.string().max(300, '摘要不能超过 300 个字符').optional(),
  publishDate: z.string().min(1, '请选择发布时间'),
  level: z.enum(['normal', 'important', 'urgent']),
  forceRead: z.boolean(),
  immediate: z.boolean(),
})

type AnnouncementFormValues = z.infer<typeof announcementSchema>

const ANNOUNCEMENT_FORM_ID = 'announcement-form'
const ANNOUNCEMENT_LEVEL_OPTIONS = [
  { value: 'normal', label: '普通' },
  { value: 'important', label: '重要' },
  { value: 'urgent', label: '紧急' },
] satisfies Array<{ value: AnnouncementLevel; label: string }>

function fallbackTitle(content: string): string {
  const firstLine = content.trim().split('\n', 1)[0]
  return firstLine.slice(0, 80) || '未命名公告'
}

function legacyLevel(type?: string): AnnouncementLevel {
  if (type === 'error') return 'urgent'
  if (type === 'warning' || type === 'ongoing') return 'important'
  return 'normal'
}

function normalizeAnnouncement(
  value: Record<string, unknown>
): AnnouncementConfig {
  const content = typeof value.content === 'string' ? value.content : ''
  const publishDate =
    typeof value.publishDate === 'string'
      ? value.publishDate
      : new Date().toISOString()
  const id =
    typeof value.id === 'string' || typeof value.id === 'number'
      ? value.id
      : crypto.randomUUID()
  const type = typeof value.type === 'string' ? value.type : undefined
  const level =
    value.level === 'normal' ||
    value.level === 'important' ||
    value.level === 'urgent'
      ? value.level
      : legacyLevel(type)

  return {
    rowKey: `id:${id}`,
    id,
    title:
      typeof value.title === 'string' && value.title.trim()
        ? value.title
        : fallbackTitle(content),
    content,
    publishDate,
    level,
    forceRead: value.forceRead === true,
    immediate: normalizeAnnouncementImmediate(value.immediate, publishDate),
    extra: typeof value.extra === 'string' ? value.extra : undefined,
    type,
    category: typeof value.category === 'string' ? value.category : undefined,
    pinned: value.pinned === true,
    offlineAt:
      typeof value.offlineAt === 'string' ? value.offlineAt : undefined,
  }
}

function announcementMatchKey(value: {
  id?: number | string | null
  publishDate: string
  content: string
}): string {
  if (value.id != null) return `id:${value.id}`
  return `legacy:${value.publishDate}:${value.content}`
}

function serializeAnnouncements(announcements: AnnouncementConfig[]) {
  return announcements.map((announcement) => ({
    id: announcement.id,
    title: announcement.title,
    content: announcement.content,
    publishDate: announcement.publishDate,
    level: announcement.level,
    forceRead: announcement.forceRead,
    immediate: announcement.immediate,
    extra: announcement.extra || undefined,
    type: announcement.type,
    category: announcement.category,
    pinned: announcement.pinned,
    offlineAt: announcement.offlineAt,
  }))
}

export function AnnouncementsSection(props: AnnouncementsSectionProps) {
  const queryClient = useQueryClient()
  const updateOption = useUpdateOption()
  const [announcements, setAnnouncements] = useState<AnnouncementConfig[]>([])
  const [isEnabled, setIsEnabled] = useState(props.enabled)
  const [hasChanges, setHasChanges] = useState(false)
  const [selectedRows, setSelectedRows] = useState<string[]>([])
  const [showEditor, setShowEditor] = useState(false)
  const [editingAnnouncement, setEditingAnnouncement] =
    useState<AnnouncementConfig | null>(null)
  const [pendingDeleteRows, setPendingDeleteRows] = useState<string[]>([])
  const [selectedStat, setSelectedStat] = useState<AnnouncementStat | null>(
    null
  )

  const form = useForm<AnnouncementFormValues>({
    resolver: zodResolver(announcementSchema),
    defaultValues: {
      title: '',
      content: '',
      extra: '',
      publishDate: new Date().toISOString(),
      level: 'normal',
      forceRead: false,
      immediate: true,
    },
  })
  const immediate = form.watch('immediate')
  const formErrors = form.formState.errors
  const firstFormErrorMessage = [
    formErrors.title?.message,
    formErrors.content?.message,
    formErrors.extra?.message,
    formErrors.publishDate?.message,
    formErrors.level?.message,
  ].find((message): message is string => typeof message === 'string')

  const statsQuery = useQuery({
    queryKey: announcementQueryKeys.stats,
    queryFn: getAllAnnouncementStats,
    enabled: isEnabled,
  })
  const statsError = statsQuery.isError
  const refetchStats = statsQuery.refetch

  useEffect(() => {
    try {
      const parsed = JSON.parse(props.data || '[]') as unknown
      if (!Array.isArray(parsed)) {
        setAnnouncements([])
        return
      }
      setAnnouncements(
        parsed.map((item) =>
          normalizeAnnouncement(item as Record<string, unknown>)
        )
      )
    } catch {
      setAnnouncements([])
    }
  }, [props.data])

  useEffect(() => {
    setIsEnabled(props.enabled)
  }, [props.enabled])

  const statsByAnnouncement = useMemo(() => {
    const stats = statsQuery.data ?? []
    return new Map(stats.map((stat) => [announcementMatchKey(stat), stat]))
  }, [statsQuery.data])

  const sortedAnnouncements = useMemo(
    () =>
      [...announcements].sort(
        (left, right) =>
          new Date(right.publishDate).getTime() -
          new Date(left.publishDate).getTime()
      ),
    [announcements]
  )

  const openCreateDialog = () => {
    setEditingAnnouncement(null)
    form.reset({
      title: '',
      content: '',
      extra: '',
      publishDate: new Date().toISOString(),
      level: 'normal',
      forceRead: false,
      immediate: true,
    })
    setShowEditor(true)
  }

  const openEditDialog = useCallback(
    (announcement: AnnouncementConfig) => {
      setEditingAnnouncement(announcement)
      form.reset({
        title: announcement.title,
        content: announcement.content,
        extra: announcement.extra || '',
        publishDate: announcement.publishDate,
        level: announcement.level,
        forceRead: announcement.forceRead,
        immediate: announcement.immediate,
      })
      setShowEditor(true)
    },
    [form]
  )

  const handleSubmit = (values: AnnouncementFormValues) => {
    const publishDate = resolveAnnouncementPublishDate({
      immediate: values.immediate,
      selectedPublishDate: values.publishDate,
      editingAnnouncement,
    })
    if (editingAnnouncement) {
      setAnnouncements((current) =>
        current.map((announcement) =>
          announcement.rowKey === editingAnnouncement.rowKey
            ? { ...announcement, ...values, publishDate }
            : announcement
        )
      )
      toast.success('公告列表项已更新，请点击“保存设置”完成发布')
    } else {
      const id = crypto.randomUUID()
      setAnnouncements((current) => [
        ...current,
        {
          rowKey: `id:${id}`,
          id,
          ...values,
          publishDate,
        },
      ])
      toast.success('公告已加入列表，请点击“保存设置”完成发布')
    }
    setHasChanges(true)
    setShowEditor(false)
  }

  const handleInvalidSubmit: SubmitErrorHandler<AnnouncementFormValues> = (
    errors
  ) => {
    const errorMessage = [
      errors.title?.message,
      errors.content?.message,
      errors.extra?.message,
      errors.publishDate?.message,
      errors.level?.message,
    ].find((message): message is string => typeof message === 'string')

    toast.error('公告信息未填写完整', {
      description: errorMessage || '请检查标红字段后再次添加',
    })
  }

  const handleToggleEnabled = async (checked: boolean) => {
    try {
      const result = await updateOption.mutateAsync({
        key: 'console_setting.announcements_enabled',
        value: checked,
      })
      if (!result.success) return
      setIsEnabled(checked)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['status'] }),
        queryClient.invalidateQueries({
          queryKey: announcementQueryKeys.listRoot,
        }),
        queryClient.invalidateQueries({
          queryKey: announcementQueryKeys.mandatory,
        }),
        queryClient.invalidateQueries({
          queryKey: announcementQueryKeys.unreadCount,
        }),
      ])
      toast.success('公告中心状态已更新')
    } catch {
      toast.error('公告中心状态更新失败')
    }
  }

  const handleSave = async () => {
    try {
      const result = await updateOption.mutateAsync({
        key: 'console_setting.announcements',
        value: JSON.stringify(serializeAnnouncements(announcements)),
      })
      if (!result.success) return
      setHasChanges(false)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['status'] }),
        queryClient.invalidateQueries({
          queryKey: announcementQueryKeys.listRoot,
        }),
        queryClient.invalidateQueries({
          queryKey: announcementQueryKeys.mandatory,
        }),
        queryClient.invalidateQueries({
          queryKey: announcementQueryKeys.stats,
        }),
        queryClient.invalidateQueries({
          queryKey: announcementQueryKeys.unreadCount,
        }),
      ])
      toast.success('公告设置已保存')
    } catch {
      toast.error('公告设置保存失败')
    }
  }

  const confirmDelete = () => {
    setAnnouncements((current) =>
      current.filter(
        (announcement) => !pendingDeleteRows.includes(announcement.rowKey)
      )
    )
    setSelectedRows((current) =>
      current.filter((rowKey) => !pendingDeleteRows.includes(rowKey))
    )
    setPendingDeleteRows([])
    setHasChanges(true)
    toast.success('公告已移除，保存设置后生效')
  }

  const columns = useMemo<StaticDataTableColumn<AnnouncementConfig>[]>(
    () => [
      {
        id: 'select',
        header: (
          <Checkbox
            checked={
              announcements.length > 0 &&
              selectedRows.length === announcements.length
            }
            onCheckedChange={(checked) =>
              setSelectedRows(
                checked ? announcements.map((item) => item.rowKey) : []
              )
            }
            aria-label='选择全部公告'
          />
        ),
        className: 'w-10',
        cell: (announcement) => (
          <Checkbox
            checked={selectedRows.includes(announcement.rowKey)}
            onCheckedChange={(checked) =>
              setSelectedRows((current) =>
                checked
                  ? [...current, announcement.rowKey]
                  : current.filter((rowKey) => rowKey !== announcement.rowKey)
              )
            }
            aria-label={`选择公告：${announcement.title}`}
          />
        ),
      },
      {
        id: 'announcement',
        header: '公告',
        className: 'w-80 max-w-80 xl:w-128 xl:max-w-128',
        cellClassName: 'w-80 max-w-80 xl:w-128 xl:max-w-128',
        cell: (announcement) => (
          <div className='min-w-0 overflow-hidden'>
            <p className='truncate font-medium'>{announcement.title}</p>
            <p className='text-muted-foreground line-clamp-1 text-xs'>
              {announcement.extra || announcement.content}
            </p>
          </div>
        ),
      },
      {
        id: 'level',
        header: '等级',
        cell: (announcement) => (
          <AnnouncementLevelBadge level={announcement.level} />
        ),
      },
      {
        id: 'publish',
        header: '发布时间',
        cell: (announcement) => {
          const published = new Date(announcement.publishDate) <= new Date()
          return (
            <div className='space-y-1'>
              <p className='text-xs tabular-nums'>
                {formatDateTimeObject(new Date(announcement.publishDate))}
              </p>
              <Badge variant={published ? 'outline' : 'secondary'}>
                {published ? '已发布' : '待发布'}
              </Badge>
            </div>
          )
        },
      },
      {
        id: 'required',
        header: '阅读要求',
        cell: (announcement) =>
          announcement.forceRead ? (
            <Badge variant='destructive' className='gap-1'>
              <LockKeyhole aria-hidden='true' />
              强制阅读
            </Badge>
          ) : (
            <Badge variant='outline'>普通公告</Badge>
          ),
      },
      {
        id: 'stats',
        header: '阅读统计',
        cellClassName: 'min-w-44',
        cell: (announcement) => {
          if (statsError) {
            return (
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={() => void refetchStats()}
              >
                统计加载失败，重试
              </Button>
            )
          }
          const stat = statsByAnnouncement.get(
            announcementMatchKey(announcement)
          )
          if (!stat) {
            return (
              <span className='text-muted-foreground text-xs'>保存后统计</span>
            )
          }
          if (!stat.published) {
            return (
              <span className='text-muted-foreground text-xs'>尚未发布</span>
            )
          }
          return (
            <Button
              type='button'
              variant='ghost'
              className='h-auto justify-start px-0 py-1 text-left'
              onClick={() => setSelectedStat(stat)}
            >
              <BarChart3 aria-hidden='true' />
              <span className='leading-5'>
                {stat.read_count} 已读 · {stat.unread_count} 未读
                <br />
                阅读率 {stat.read_rate.toFixed(1)}%
              </span>
            </Button>
          )
        },
      },
      {
        id: 'actions',
        header: '操作',
        className: 'w-24',
        cell: (announcement) => (
          <StaticRowActions
            editLabel='编辑'
            deleteLabel='删除'
            menuLabel='公告操作'
            onEdit={() => openEditDialog(announcement)}
            onDelete={() => setPendingDeleteRows([announcement.rowKey])}
          />
        ),
      },
    ],
    [
      announcements,
      openEditDialog,
      selectedRows,
      statsByAnnouncement,
      refetchStats,
      statsError,
    ]
  )

  return (
    <SettingsSection title='公告中心'>
      <SettingsSwitchField
        checked={isEnabled}
        onCheckedChange={handleToggleEnabled}
        label='启用公告中心'
        description='开启后，已发布公告会出现在首页、顶部通知入口和公告中心。'
      />

      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex flex-wrap items-center gap-2'>
          <Button type='button' onClick={openCreateDialog}>
            <Plus aria-hidden='true' />
            发布公告
          </Button>
          {selectedRows.length > 0 ? (
            <Button
              type='button'
              variant='destructive'
              onClick={() => setPendingDeleteRows(selectedRows)}
            >
              <Trash2 aria-hidden='true' />
              删除选中（{selectedRows.length}）
            </Button>
          ) : null}
        </div>
        <Button
          type='button'
          variant={hasChanges ? 'default' : 'outline'}
          disabled={!hasChanges || updateOption.isPending}
          onClick={handleSave}
        >
          <Save aria-hidden='true' />
          保存设置
        </Button>
      </div>

      <StaticDataTable
        data={sortedAnnouncements}
        columns={columns}
        tableClassName='table-fixed'
        getRowKey={(announcement) => announcement.rowKey}
        emptyContent={
          <div className='text-muted-foreground flex min-h-48 items-center justify-center'>
            暂无公告，点击“发布公告”创建第一条公告。
          </div>
        }
      />

      <Dialog
        open={showEditor}
        onOpenChange={setShowEditor}
        title={editingAnnouncement ? '编辑公告' : '发布公告'}
        description='先添加到列表，再点击页面上的“保存设置”才会写入服务端。'
        contentClassName='sm:max-w-3xl'
        footer={
          <Button type='submit' form={ANNOUNCEMENT_FORM_ID}>
            {editingAnnouncement ? '更新列表项' : '添加到列表'}
          </Button>
        }
      >
        <Form {...form}>
          <form
            id={ANNOUNCEMENT_FORM_ID}
            onSubmit={form.handleSubmit(handleSubmit, handleInvalidSubmit)}
          >
            {form.formState.submitCount > 0 && firstFormErrorMessage ? (
              <Alert variant='destructive' className='mb-4'>
                <CircleAlert aria-hidden='true' />
                <AlertTitle>公告信息未填写完整</AlertTitle>
                <AlertDescription>
                  {firstFormErrorMessage}。请修正标红字段后再次添加。
                </AlertDescription>
              </Alert>
            ) : null}

            <SettingsFormGrid>
              <FormField
                control={form.control}
                name='title'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>公告标题</FormLabel>
                    <FormControl>
                      <Input {...field} placeholder='请输入公告标题' />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='level'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>公告等级</FormLabel>
                    <Select
                      items={ANNOUNCEMENT_LEVEL_OPTIONS}
                      value={field.value}
                      onValueChange={field.onChange}
                    >
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue placeholder='选择公告等级' />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        <SelectGroup>
                          <SelectItem value='normal'>普通</SelectItem>
                          <SelectItem value='important'>重要</SelectItem>
                          <SelectItem value='urgent'>紧急</SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='extra'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>公告摘要</FormLabel>
                    <FormControl>
                      <Input
                        {...field}
                        placeholder='用于首页和公告卡片，可留空自动截取正文'
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='content'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>公告内容</FormLabel>
                    <FormControl>
                      <Textarea
                        {...field}
                        rows={9}
                        placeholder='支持 Markdown 或安全 HTML'
                      />
                    </FormControl>
                    <FormDescription>
                      建议正文保持清晰、可扫描。
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='immediate'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <div className='min-w-0 space-y-0.5'>
                      <FormLabel>立即发布</FormLabel>
                      <FormDescription>
                        关闭后可选择未来发布时间。
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={(checked) => {
                          field.onChange(checked)
                          if (checked) {
                            form.setValue(
                              'publishDate',
                              new Date().toISOString()
                            )
                          }
                        }}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />

              {!immediate ? (
                <FormField
                  control={form.control}
                  name='publishDate'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>发布时间</FormLabel>
                      <FormControl>
                        <DateTimePicker
                          value={
                            field.value ? new Date(field.value) : undefined
                          }
                          onChange={(date) =>
                            field.onChange(date?.toISOString() || '')
                          }
                          placeholder='选择发布时间'
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              ) : null}

              <FormField
                control={form.control}
                name='forceRead'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <div className='min-w-0 space-y-0.5'>
                      <FormLabel>强制阅读</FormLabel>
                      <FormDescription>
                        用户登录后必须点击“我已知晓”，阅读状态才会保存。
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
            </SettingsFormGrid>
          </form>
        </Form>
      </Dialog>

      <AlertDialog
        open={pendingDeleteRows.length > 0}
        onOpenChange={(open) => {
          if (!open) setPendingDeleteRows([])
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除公告？</AlertDialogTitle>
            <AlertDialogDescription>
              将移除 {pendingDeleteRows.length} 条公告。保存设置后正式生效。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant='destructive' onClick={confirmDelete}>
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AnnouncementUnreadUsersDialog
        key={selectedStat?.key || 'unread-users'}
        open={selectedStat != null}
        onOpenChange={(open) => {
          if (!open) setSelectedStat(null)
        }}
        announcementKey={selectedStat?.key || ''}
        announcementTitle={selectedStat?.title || ''}
      />
    </SettingsSection>
  )
}
