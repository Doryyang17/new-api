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
import { Delete02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import type { Control } from 'react-hook-form'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
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
import { LevelBadge, type UserLevelDefinition } from '@/features/user-levels'
import { parseQuotaFromDollars, quotaUnitsToDollars } from '@/lib/format'

import {
  MAX_USER_LEVEL_THRESHOLD_QUOTA,
  USER_LEVEL_BADGE_COLORS,
  type UserLevelSettingsValues,
} from './user-level-settings-schema'

type UserLevelEditorRowProps = {
  control: Control<UserLevelSettingsValues>
  index: number
  level: UserLevelDefinition
  persisted: boolean
  memberCount: number
  currencyLabel: string
  quotaStep: number
  disabled: boolean
  onRemove: () => void
}

export function UserLevelEditorRow(props: UserLevelEditorRowProps) {
  const isBase = props.index === 0
  const canRemove = !isBase && !props.persisted

  return (
    <article
      className='border-border/70 space-y-4 rounded-xl border p-4'
      aria-label={`等级 ${props.level.name || props.index + 1}`}
    >
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div className='flex min-w-0 flex-wrap items-center gap-2'>
          <LevelBadge
            name={props.level.name || '未命名等级'}
            color={props.level.badge_color}
            ratio={props.level.ratio}
            showRatio
          />
          {isBase && <Badge variant='outline'>基础等级</Badge>}
          {props.persisted && (
            <Badge variant='secondary'>{props.memberCount} 名用户</Badge>
          )}
          {props.level.archived && <Badge variant='warning'>停止发放</Badge>}
        </div>

        <div className='flex items-center gap-3'>
          {!isBase && (
            <FormField
              control={props.control}
              name={`levels.${props.index}.archived`}
              render={({ field }) => (
                <FormItem className='flex items-center gap-2 space-y-0'>
                  <FormLabel className='text-muted-foreground font-normal'>
                    停止发放
                  </FormLabel>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={props.disabled}
                    />
                  </FormControl>
                </FormItem>
              )}
            />
          )}
          {canRemove && (
            <Button
              type='button'
              variant='ghost'
              size='icon'
              aria-label={`删除 ${props.level.name || '新等级'}`}
              title='删除未保存等级'
              disabled={props.disabled}
              onClick={props.onRemove}
            >
              <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
            </Button>
          )}
        </div>
      </div>

      <div className='grid min-w-0 gap-4 md:grid-cols-2 xl:grid-cols-4'>
        <FormField
          control={props.control}
          name={`levels.${props.index}.id`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>等级 ID</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  disabled={props.disabled || props.persisted || isBase}
                  placeholder='例如 silver'
                  autoComplete='off'
                  onChange={(event) =>
                    field.onChange(event.target.value.toLowerCase())
                  }
                />
              </FormControl>
              <FormDescription>
                保存后不可修改，用于稳定识别等级。
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={props.control}
          name={`levels.${props.index}.name`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>等级名称</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  disabled={props.disabled}
                  placeholder='例如 白银会员'
                  autoComplete='off'
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={props.control}
          name={`levels.${props.index}.threshold_quota`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>累计消耗门槛（{props.currencyLabel}）</FormLabel>
              <FormControl>
                <Input
                  type='number'
                  min={isBase ? 0 : props.quotaStep}
                  max={quotaUnitsToDollars(MAX_USER_LEVEL_THRESHOLD_QUOTA)}
                  step={props.quotaStep}
                  value={quotaUnitsToDollars(field.value)}
                  disabled={props.disabled || isBase}
                  onBlur={field.onBlur}
                  name={field.name}
                  ref={field.ref}
                  onChange={(event) => {
                    const amount = event.target.valueAsNumber
                    if (Number.isFinite(amount)) {
                      field.onChange(parseQuotaFromDollars(amount))
                    }
                  }}
                />
              </FormControl>
              <FormDescription>统计账户已结算的实际使用消耗。</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={props.control}
          name={`levels.${props.index}.ratio`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>等级倍率</FormLabel>
              <FormControl>
                <Input
                  type='number'
                  min={0.01}
                  max={1}
                  step={0.01}
                  value={Number.isFinite(field.value) ? field.value : ''}
                  disabled={props.disabled || isBase}
                  onBlur={field.onBlur}
                  name={field.name}
                  ref={field.ref}
                  onChange={(event) => {
                    if (Number.isFinite(event.target.valueAsNumber)) {
                      field.onChange(event.target.valueAsNumber)
                    }
                  }}
                />
              </FormControl>
              <FormDescription>
                越高等级倍率应越低，范围 0.01–1。
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={props.control}
          name={`levels.${props.index}.badge_color`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>标签颜色</FormLabel>
              <Select
                items={USER_LEVEL_BADGE_COLORS}
                value={field.value}
                onValueChange={field.onChange}
                disabled={props.disabled}
              >
                <FormControl>
                  <SelectTrigger className='w-full'>
                    <SelectValue placeholder='选择标签颜色' />
                  </SelectTrigger>
                </FormControl>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {USER_LEVEL_BADGE_COLORS.map((color) => (
                      <SelectItem key={color.value} value={color.value}>
                        <LevelBadge name={color.label} color={color.value} />
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={props.control}
          name={`levels.${props.index}.description`}
          render={({ field }) => (
            <FormItem className='md:col-span-2 xl:col-span-3'>
              <FormLabel>等级说明</FormLabel>
              <FormControl>
                <Textarea
                  {...field}
                  value={field.value ?? ''}
                  rows={2}
                  maxLength={80}
                  disabled={props.disabled}
                  placeholder='向用户说明该等级权益'
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      </div>

      {props.persisted && !isBase && (
        <p className='text-muted-foreground text-xs leading-5'>
          已保存等级不可删除或修改
          ID。停止发放后，新用户不能再领取；已领取用户仍保留该等级与倍率。
        </p>
      )}
    </article>
  )
}
