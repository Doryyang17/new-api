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
import { CalendarClockIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo } from 'react'
import { Controller, useForm, useWatch } from 'react-hook-form'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldContent,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'

import { updateChannelAvailabilitySchedule } from '../../api'
import { useChannelAvailabilityNow } from '../../hooks/use-channel-availability-now'
import {
  CHANNEL_AVAILABILITY_TIMEZONES,
  channelAvailabilitySchema,
  channelsQueryKeys,
  DEFAULT_CHANNEL_AVAILABILITY_SCHEDULE,
  evaluateChannelAvailability,
  mergeChannelAvailabilitySchedule,
  parseChannelAvailabilitySchedule,
  type ChannelAvailabilityFormValues,
} from '../../lib'
import type {
  Channel,
  GetChannelsResponse,
  SearchChannelsResponse,
} from '../../types'
import { useChannels } from '../channels-provider'

type ChannelAvailabilityDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ChannelAvailabilityDialog(
  props: ChannelAvailabilityDialogProps
) {
  const queryClient = useQueryClient()
  const { currentRow, setCurrentRow } = useChannels()
  const form = useForm<ChannelAvailabilityFormValues>({
    resolver: zodResolver(channelAvailabilitySchema),
    defaultValues: DEFAULT_CHANNEL_AVAILABILITY_SCHEDULE,
  })

  useEffect(() => {
    if (!props.open) return
    const schedule = parseChannelAvailabilitySchedule(currentRow?.settings)
    form.reset(schedule ?? DEFAULT_CHANNEL_AVAILABILITY_SCHEDULE)
  }, [currentRow?.id, currentRow?.settings, form, props.open])

  const enabled = useWatch({ control: form.control, name: 'enabled' })
  const start = useWatch({ control: form.control, name: 'start' })
  const end = useWatch({ control: form.control, name: 'end' })
  const timezone = useWatch({ control: form.control, name: 'timezone' })
  const availabilityNow = useChannelAvailabilityNow(
    props.open && enabled === true
  )
  const previewSchedule = useMemo(
    () => ({ enabled, start, end, timezone }),
    [enabled, end, start, timezone]
  )
  const evaluation = useMemo(
    () =>
      evaluateChannelAvailability(previewSchedule, new Date(availabilityNow)),
    [availabilityNow, previewSchedule]
  )
  const timezoneOptions = useMemo(() => {
    const options: Array<{ value: string; label: string }> =
      CHANNEL_AVAILABILITY_TIMEZONES.map((option) => ({ ...option }))
    if (timezone && !options.some((option) => option.value === timezone)) {
      options.unshift({ value: timezone, label: timezone })
    }
    return options
  }, [timezone])

  const mutation = useMutation({
    mutationFn: async (values: ChannelAvailabilityFormValues) => {
      if (!currentRow) throw new Error('未选择渠道')
      const response = await updateChannelAvailabilitySchedule(
        currentRow.id,
        values
      )
      if (!response.success || !response.data) {
        throw new Error(response.message || '保存渠道可用时间失败')
      }
      return response.data
    },
    onSuccess: (result) => {
      if (!currentRow) return
      const nextSettings = mergeChannelAvailabilitySchedule(
        currentRow.settings,
        result.schedule
      )
      const updatedChannel: Channel = {
        ...currentRow,
        settings: nextSettings,
        status: result.status,
      }

      queryClient.setQueriesData<GetChannelsResponse | SearchChannelsResponse>(
        { queryKey: channelsQueryKeys.lists() },
        (previous) => {
          if (!previous?.data) return previous
          return {
            ...previous,
            data: {
              ...previous.data,
              items: previous.data.items.map((channel) =>
                channel.id === updatedChannel.id ? updatedChannel : channel
              ),
            },
          }
        }
      )
      setCurrentRow(updatedChannel)
      void queryClient.invalidateQueries({
        queryKey: channelsQueryKeys.lists(),
      })

      if (result.status_changed) {
        const statusText = result.status === 1 ? '启用' : '禁用'
        toast.success(`可用时间已保存，渠道已同步${statusText}`)
      } else {
        toast.success('渠道可用时间已保存')
      }
      props.onOpenChange(false)
    },
    onError: (error: unknown) => {
      const responseMessage = (
        error as { response?: { data?: { message?: string } } }
      )?.response?.data?.message
      toast.error(
        responseMessage ||
          (error instanceof Error ? error.message : '保存渠道可用时间失败')
      )
    },
  })

  const handleOpenChange = (open: boolean) => {
    if (mutation.isPending) return
    props.onOpenChange(open)
  }

  let currentStatus = '定时配置已关闭'
  let nextAction = '渠道状态保持不变'
  let statusVariant: StatusVariant = 'neutral'
  if (enabled && evaluation?.inWindow) {
    currentStatus = '当前处于可用时段'
    nextAction = `${evaluation.nextTime} 自动禁用`
    statusVariant = 'success'
  } else if (enabled && evaluation) {
    currentStatus = '当前处于禁用时段'
    nextAction = `${evaluation.nextTime} 自动启用`
    statusVariant = 'warning'
  } else if (enabled) {
    currentStatus = '配置待修正'
    nextAction = '保存前请修正时间或时区'
    statusVariant = 'danger'
  }

  const crossesMidnight = enabled && start > end

  return (
    <Dialog
      open={props.open}
      onOpenChange={handleOpenChange}
      title='渠道可用时间'
      description={currentRow ? `渠道：${currentRow.name}` : undefined}
      contentClassName='sm:max-w-lg'
      contentHeight='auto'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            disabled={mutation.isPending}
            onClick={() => handleOpenChange(false)}
          >
            取消
          </Button>
          <Button
            type='submit'
            form='channel-availability-form'
            disabled={mutation.isPending || !currentRow}
          >
            {mutation.isPending && <Spinner data-icon='inline-start' />}
            保存
          </Button>
        </>
      }
    >
      <form
        id='channel-availability-form'
        onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
      >
        <FieldGroup>
          <Field orientation='horizontal' data-disabled={mutation.isPending}>
            <FieldContent>
              <FieldLabel htmlFor='channel-availability-enabled'>
                定时启禁用
              </FieldLabel>
            </FieldContent>
            <Controller
              control={form.control}
              name='enabled'
              render={({ field }) => (
                <Switch
                  id='channel-availability-enabled'
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  disabled={mutation.isPending}
                  aria-label='定时启禁用'
                />
              )}
            />
          </Field>

          <div className='grid grid-cols-2 gap-3'>
            <Controller
              control={form.control}
              name='start'
              render={({ field, fieldState }) => (
                <Field
                  data-invalid={fieldState.invalid}
                  data-disabled={!enabled}
                >
                  <FieldLabel htmlFor='channel-availability-start'>
                    开始时间
                  </FieldLabel>
                  <Input
                    {...field}
                    id='channel-availability-start'
                    type='time'
                    disabled={!enabled || mutation.isPending}
                    aria-invalid={fieldState.invalid}
                  />
                  <FieldError errors={[fieldState.error]} />
                </Field>
              )}
            />

            <Controller
              control={form.control}
              name='end'
              render={({ field, fieldState }) => (
                <Field
                  data-invalid={fieldState.invalid}
                  data-disabled={!enabled}
                >
                  <FieldLabel htmlFor='channel-availability-end'>
                    结束时间
                  </FieldLabel>
                  <Input
                    {...field}
                    id='channel-availability-end'
                    type='time'
                    disabled={!enabled || mutation.isPending}
                    aria-invalid={fieldState.invalid}
                  />
                  <FieldError errors={[fieldState.error]} />
                </Field>
              )}
            />
          </div>

          <Controller
            control={form.control}
            name='timezone'
            render={({ field, fieldState }) => (
              <Field data-invalid={fieldState.invalid} data-disabled={!enabled}>
                <FieldLabel htmlFor='channel-availability-timezone'>
                  时区
                </FieldLabel>
                <Select
                  items={timezoneOptions}
                  value={field.value}
                  onValueChange={(value) => field.onChange(value ?? '')}
                  disabled={!enabled || mutation.isPending}
                >
                  <SelectTrigger
                    id='channel-availability-timezone'
                    className='w-full'
                    aria-invalid={fieldState.invalid}
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {timezoneOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FieldError errors={[fieldState.error]} />
              </Field>
            )}
          />

          <div className='border-border flex min-w-0 items-start justify-between gap-3 border-y py-3'>
            <div className='min-w-0 space-y-1.5'>
              <StatusBadge variant={statusVariant} size='sm' copyable={false}>
                <HugeiconsIcon
                  icon={CalendarClockIcon}
                  strokeWidth={2}
                  className='size-3.5 shrink-0'
                  aria-hidden='true'
                />
                <span className='truncate'>{currentStatus}</span>
              </StatusBadge>
              <p className='text-muted-foreground text-sm'>{nextAction}</p>
            </div>
            {crossesMidnight && (
              <StatusBadge variant='info' size='sm' copyable={false}>
                跨日时段
              </StatusBadge>
            )}
          </div>
        </FieldGroup>
      </form>
    </Dialog>
  )
}
