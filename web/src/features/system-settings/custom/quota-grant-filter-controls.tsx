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
import { useMemo } from 'react'

import { CompactDateTimeRangePicker } from '@/components/compact-date-time-range-picker'
import { MultiSelect } from '@/components/multi-select'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
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
import { getEnabledModels } from '@/features/channels/api'
import { USER_ROLE, USER_STATUS } from '@/features/users/constants'

import type { QuotaGrantFilters } from './quota-grant-api'
import {
  quotaGrantCustomBalanceModes,
  quotaGrantTimeZone,
} from './quota-grant-filter'

type QuotaGrantFilterControlsProps = {
  value: QuotaGrantFilters
  disabled: boolean
  onChange: (filters: QuotaGrantFilters) => void
}

export function QuotaGrantFilterControls(props: QuotaGrantFilterControlsProps) {
  const enabledModelsQuery = useQuery({
    queryKey: ['enabled-models'],
    queryFn: getEnabledModels,
    enabled: props.value.usage_mode !== 'any',
    staleTime: 5 * 60 * 1000,
  })
  const modelOptions = useMemo(() => {
    const models = enabledModelsQuery.data?.data ?? []
    return [...new Set(models.map((model) => model.trim()).filter(Boolean))]
      .sort((left, right) => left.localeCompare(right))
      .map((model) => ({ value: model, label: model }))
  }, [enabledModelsQuery.data?.data])

  return (
    <div className='bg-muted/30 flex flex-col gap-4 rounded-lg border p-3'>
      <FieldSet>
        <FieldLegend variant='label'>基础条件</FieldLegend>
        <FieldGroup className='grid gap-3 sm:grid-cols-2 xl:grid-cols-3'>
          <Field>
            <FieldLabel htmlFor='quota-grant-status'>用户状态</FieldLabel>
            <Select
              value={
                props.value.statuses.length === 2
                  ? 'all'
                  : String(props.value.statuses[0])
              }
              disabled={props.disabled}
              onValueChange={(value) =>
                props.onChange({
                  ...props.value,
                  statuses:
                    value === 'all'
                      ? [USER_STATUS.ENABLED, USER_STATUS.DISABLED]
                      : [Number(value)],
                })
              }
            >
              <SelectTrigger id='quota-grant-status' className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value='all'>全部用户</SelectItem>
                  <SelectItem value={String(USER_STATUS.ENABLED)}>
                    已启用用户
                  </SelectItem>
                  <SelectItem value={String(USER_STATUS.DISABLED)}>
                    已禁用用户
                  </SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>

          <Field>
            <FieldLabel htmlFor='quota-grant-role'>用户角色</FieldLabel>
            <Select
              value={
                props.value.roles.length === 2
                  ? 'all'
                  : String(props.value.roles[0])
              }
              disabled={props.disabled}
              onValueChange={(value) =>
                props.onChange({
                  ...props.value,
                  roles:
                    value === 'all'
                      ? [USER_ROLE.USER, USER_ROLE.ADMIN]
                      : [Number(value)],
                })
              }
            >
              <SelectTrigger id='quota-grant-role' className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value={String(USER_ROLE.USER)}>
                    普通用户
                  </SelectItem>
                  <SelectItem value={String(USER_ROLE.ADMIN)}>
                    管理员
                  </SelectItem>
                  <SelectItem value='all'>普通用户和管理员</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>

          <Field className='sm:col-span-2 xl:col-span-1'>
            <FieldLabel htmlFor='quota-grant-balance'>当前余额</FieldLabel>
            <div className='flex min-w-0 flex-wrap gap-2 sm:flex-nowrap'>
              <Select
                value={props.value.balance_mode}
                disabled={props.disabled}
                onValueChange={(value) => {
                  if (!value) return
                  props.onChange({ ...props.value, balance_mode: value })
                }}
              >
                <SelectTrigger
                  id='quota-grant-balance'
                  className='min-w-36 flex-1'
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value='any'>不限余额</SelectItem>
                    <SelectItem value='low'>额度不足（&lt; $10）</SelectItem>
                    <SelectItem value='negative'>负余额</SelectItem>
                    <SelectItem value='zero'>零余额</SelectItem>
                    <SelectItem value='positive'>正余额</SelectItem>
                    <SelectItem value='lt'>小于</SelectItem>
                    <SelectItem value='lte'>小于等于</SelectItem>
                    <SelectItem value='eq'>等于</SelectItem>
                    <SelectItem value='gte'>大于等于</SelectItem>
                    <SelectItem value='gt'>大于</SelectItem>
                    <SelectItem value='between'>区间</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
              {quotaGrantCustomBalanceModes.has(props.value.balance_mode) && (
                <Input
                  type='number'
                  min='0'
                  step='0.01'
                  value={props.value.balance_amount}
                  disabled={props.disabled}
                  onChange={(event) =>
                    props.onChange({
                      ...props.value,
                      balance_amount: event.target.value,
                    })
                  }
                  aria-label='余额金额（美元）'
                  placeholder='美元'
                  className='w-28'
                />
              )}
              {props.value.balance_mode === 'between' && (
                <Input
                  type='number'
                  min='0'
                  step='0.01'
                  value={props.value.balance_max}
                  disabled={props.disabled}
                  onChange={(event) =>
                    props.onChange({
                      ...props.value,
                      balance_max: event.target.value,
                    })
                  }
                  aria-label='余额区间上限（美元）'
                  placeholder='上限'
                  className='w-28'
                />
              )}
            </div>
          </Field>
        </FieldGroup>
      </FieldSet>

      <FieldSet>
        <FieldLegend variant='label'>行为条件</FieldLegend>
        <FieldDescription>
          时间范围同时约束充值和使用条件；未选择行为条件时不会单独筛选用户。
        </FieldDescription>
        <FieldGroup className='grid gap-3 sm:grid-cols-2 xl:grid-cols-3'>
          <Field className='sm:col-span-2 xl:col-span-1'>
            <FieldLabel htmlFor='quota-grant-time-range'>
              行为时间（北京时间）
            </FieldLabel>
            <CompactDateTimeRangePicker
              id='quota-grant-time-range'
              start={
                props.value.time_start_at
                  ? new Date(props.value.time_start_at * 1000)
                  : undefined
              }
              end={
                props.value.time_end_at
                  ? new Date(props.value.time_end_at * 1000)
                  : undefined
              }
              disabled={props.disabled}
              timeZone={quotaGrantTimeZone}
              onChange={({ start, end }) => {
                props.onChange({
                  ...props.value,
                  time_start_at: start ? Math.floor(start.getTime() / 1000) : 0,
                  time_end_at: end ? Math.floor(end.getTime() / 1000) : 0,
                })
              }}
            />
          </Field>

          <Field>
            <FieldLabel htmlFor='quota-grant-recharge'>充值情况</FieldLabel>
            <Select
              value={props.value.recharge_mode}
              disabled={props.disabled}
              onValueChange={(value) => {
                if (!value) return
                props.onChange({ ...props.value, recharge_mode: value })
              }}
            >
              <SelectTrigger id='quota-grant-recharge' className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value='any'>不限充值情况</SelectItem>
                  <SelectItem value='recharged'>范围内有充值</SelectItem>
                  <SelectItem value='unrecharged'>范围内无充值</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>

          <Field>
            <FieldLabel htmlFor='quota-grant-usage'>使用情况</FieldLabel>
            <Select
              value={props.value.usage_mode}
              disabled={props.disabled}
              onValueChange={(value) => {
                if (!value) return
                props.onChange({
                  ...props.value,
                  usage_mode: value,
                  usage_models: value === 'any' ? [] : props.value.usage_models,
                })
              }}
            >
              <SelectTrigger id='quota-grant-usage' className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value='any'>不限使用情况</SelectItem>
                  <SelectItem value='used'>范围内有模型消耗</SelectItem>
                  <SelectItem value='unused'>范围内无模型消耗</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>

          {props.value.usage_mode !== 'any' && (
            <Field className='sm:col-span-2 xl:col-span-2'>
              <FieldLabel htmlFor='quota-grant-usage-model'>
                使用模型
              </FieldLabel>
              <MultiSelect
                id='quota-grant-usage-model'
                options={modelOptions}
                selected={props.value.usage_models}
                onChange={(values) =>
                  props.onChange({
                    ...props.value,
                    usage_models: values,
                  })
                }
                placeholder='全部模型，可搜索或输入历史模型名'
                emptyText='未找到模型，可直接添加精确名称'
                allowCreate
                createLabel='添加历史模型“{{value}}”'
                maxVisibleChips={3}
                disabled={props.disabled}
                className='w-full'
              />
              <FieldDescription>
                留空统计全部模型；多选按“或”匹配，命中任一模型的用户都会被筛选出来。
              </FieldDescription>
            </Field>
          )}
        </FieldGroup>
      </FieldSet>
    </div>
  )
}
