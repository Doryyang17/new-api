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
import { useQuery } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useRef } from 'react'
import {
  type Resolver,
  useFieldArray,
  useForm,
  useWatch,
} from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
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
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { getEnabledModels } from '@/features/channels/api'
import { formatTokens } from '@/features/rankings/lib/format'

import { getDailyUsageStatus } from '../api'
import {
  SettingsForm,
  SettingsFormGrid,
  SettingsFormGridItem,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'
import type {
  DailyUsageModelLimitConfig,
  ModelDailyUsageStatus,
} from './daily-usage-types'

const createDailyUsageLimitSchema = (t: (key: string) => string) =>
  z
    .object({
      daily_usage_setting: z.object({
        enabled: z.boolean(),
        limit_tokens: z.coerce.number().int().min(0),
        timezone: z.string().min(1),
        message: z.string().min(1),
        model_limits: z.array(
          z.object({
            model_name: z.string().trim().min(1, '请选择模型'),
            model_max_usage: z.coerce
              .number()
              .int()
              .positive('模型最大额度必须大于 0'),
            enabled: z.boolean(),
          })
        ),
      }),
    })
    .superRefine((values, ctx) => {
      const settings = values.daily_usage_setting
      if (settings.enabled && settings.limit_tokens <= 0) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['daily_usage_setting', 'limit_tokens'],
          message: t('Daily token limit must be greater than 0 when enabled'),
        })
      }

      const seen = new Set<string>()
      settings.model_limits.forEach((limit, index) => {
        if (seen.has(limit.model_name)) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            path: ['daily_usage_setting', 'model_limits', index, 'model_name'],
            message: '同一模型只能配置一条限制',
          })
        }
        seen.add(limit.model_name)
        if (
          settings.limit_tokens <= 0 ||
          limit.model_max_usage >= settings.limit_tokens
        ) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            path: [
              'daily_usage_setting',
              'model_limits',
              index,
              'model_max_usage',
            ],
            message: '模型限制必须小于全站限制',
          })
        }
      })
    })

type DailyUsageLimitFormValues = z.infer<
  ReturnType<typeof createDailyUsageLimitSchema>
>

export type FlatDailyUsageLimitDefaults = {
  'daily_usage_setting.enabled': boolean
  'daily_usage_setting.limit_tokens': number
  'daily_usage_setting.timezone': string
  'daily_usage_setting.message': string
  'daily_usage_setting.model_limits': DailyUsageModelLimitConfig[]
}

type DailyUsageOptionValue =
  | string
  | number
  | boolean
  | DailyUsageModelLimitConfig[]
type DailyUsageEntry = [
  keyof FlatDailyUsageLimitDefaults,
  DailyUsageOptionValue,
]

const normalizeModels = (models: string[]) =>
  [...new Set(models.map((model) => model.trim()).filter(Boolean))].sort(
    (a, b) => a.localeCompare(b)
  )

const normalizeModelLimits = (limits: DailyUsageModelLimitConfig[]) =>
  limits.map((limit) => ({
    model_name: limit.model_name.trim(),
    model_max_usage: Number(limit.model_max_usage),
    enabled: Boolean(limit.enabled),
  }))

const buildFormDefaults = (
  defaults: FlatDailyUsageLimitDefaults
): DailyUsageLimitFormValues => ({
  daily_usage_setting: {
    enabled: defaults['daily_usage_setting.enabled'] ?? false,
    limit_tokens: defaults['daily_usage_setting.limit_tokens'] ?? 0,
    timezone: defaults['daily_usage_setting.timezone'] || 'Asia/Shanghai',
    message:
      defaults['daily_usage_setting.message'] ||
      '当日系统使用量已超上限，请每天再来。',
    model_limits: normalizeModelLimits(
      defaults['daily_usage_setting.model_limits'] ?? []
    ),
  },
})

const normalizeFormValues = (
  values: DailyUsageLimitFormValues
): FlatDailyUsageLimitDefaults => ({
  'daily_usage_setting.enabled': values.daily_usage_setting.enabled,
  'daily_usage_setting.limit_tokens': values.daily_usage_setting.limit_tokens,
  'daily_usage_setting.timezone': values.daily_usage_setting.timezone,
  'daily_usage_setting.message': values.daily_usage_setting.message,
  'daily_usage_setting.model_limits': normalizeModelLimits(
    values.daily_usage_setting.model_limits
  ),
})

const dailyUsageValuesEqual = (
  left: DailyUsageOptionValue,
  right: DailyUsageOptionValue
) => JSON.stringify(left) === JSON.stringify(right)

const orderDailyUsageEntries = (
  entries: DailyUsageEntry[],
  current: FlatDailyUsageLimitDefaults,
  target: FlatDailyUsageLimitDefaults
) =>
  [...entries].sort(([leftKey], [rightKey]) => {
    const priority = (key: keyof FlatDailyUsageLimitDefaults) => {
      if (key === 'daily_usage_setting.enabled') {
        return target['daily_usage_setting.enabled'] ? 100 : -100
      }
      if (
        key === 'daily_usage_setting.limit_tokens' ||
        key === 'daily_usage_setting.model_limits'
      ) {
        const increasingGlobalLimit =
          target['daily_usage_setting.limit_tokens'] >=
          current['daily_usage_setting.limit_tokens']
        if (key === 'daily_usage_setting.limit_tokens') {
          return increasingGlobalLimit ? -20 : -10
        }
        return increasingGlobalLimit ? -10 : -20
      }
      return 0
    }
    return priority(leftKey) - priority(rightKey)
  })

type Props = {
  defaultValues: FlatDailyUsageLimitDefaults
}

export function DailyUsageLimitSection(props: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const dailyUsageLimitSchema = useMemo(
    () => createDailyUsageLimitSchema(t),
    [t]
  )
  const formDefaults = useMemo(
    () => buildFormDefaults(props.defaultValues),
    [props.defaultValues]
  )
  const form = useForm<DailyUsageLimitFormValues>({
    resolver: zodResolver(dailyUsageLimitSchema) as Resolver<
      DailyUsageLimitFormValues,
      unknown,
      DailyUsageLimitFormValues
    >,
    defaultValues: formDefaults,
  })
  const modelLimitsFieldArray = useFieldArray({
    control: form.control,
    name: 'daily_usage_setting.model_limits',
  })
  const watchedModelLimits =
    useWatch({
      control: form.control,
      name: 'daily_usage_setting.model_limits',
    }) ?? []
  const watchedGlobalLimit =
    useWatch({
      control: form.control,
      name: 'daily_usage_setting.limit_tokens',
    }) ?? 0

  const enabledModelsQuery = useQuery({
    queryKey: ['enabled-models'],
    queryFn: getEnabledModels,
    staleTime: 5 * 60 * 1000,
  })
  const usageStatusQuery = useQuery({
    queryKey: ['daily-usage-status'],
    queryFn: getDailyUsageStatus,
    refetchInterval: 60 * 1000,
    staleTime: 30 * 1000,
  })
  const modelOptions = normalizeModels([
    ...(enabledModelsQuery.data?.data ?? []),
    ...watchedModelLimits.map((limit) => limit.model_name),
  ])
  const status = usageStatusQuery.data?.data
  const modelStatusByName = useMemo(
    () =>
      new Map(
        (status?.model_limits ?? []).map((modelStatus) => [
          modelStatus.model_name,
          modelStatus,
        ])
      ),
    [status?.model_limits]
  )

  const baselineRef = useRef<FlatDailyUsageLimitDefaults>(props.defaultValues)
  const baselineSerializedRef = useRef<string>(
    JSON.stringify(props.defaultValues)
  )

  useEffect(() => {
    const serialized = JSON.stringify(props.defaultValues)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = props.defaultValues
    baselineSerializedRef.current = serialized
    form.reset(buildFormDefaults(props.defaultValues))
  }, [props.defaultValues, form])

  const onSubmit = async (values: DailyUsageLimitFormValues) => {
    const normalized = normalizeFormValues(values)
    const entries = (Object.entries(normalized) as DailyUsageEntry[]).filter(
      ([key, value]) => !dailyUsageValuesEqual(baselineRef.current[key], value)
    )
    const orderedEntries = orderDailyUsageEntries(
      entries,
      baselineRef.current,
      normalized
    )

    try {
      for (const [key, value] of orderedEntries) {
        const result = await updateOption.mutateAsync({
          key,
          value: Array.isArray(value) ? JSON.stringify(value) : value,
        })
        if (!result.success) return
      }
      baselineRef.current = normalized
      baselineSerializedRef.current = JSON.stringify(normalized)
      form.reset(buildFormDefaults(normalized))
      toast.success(t('Daily usage limit settings saved'))
    } catch {
      // useUpdateOption owns the error toast; keep the form dirty for retry.
    }
  }

  const addModelLimit = () => {
    const selected = new Set(
      watchedModelLimits.map((limit) => limit.model_name).filter(Boolean)
    )
    const modelName = modelOptions.find((model) => !selected.has(model)) ?? ''
    const suggestedMax =
      watchedGlobalLimit > 1 ? Math.min(watchedGlobalLimit - 1, 20_000) : 1
    modelLimitsFieldArray.append({
      model_name: modelName,
      model_max_usage: suggestedMax,
      enabled: true,
    })
  }

  return (
    <SettingsSection title={t('Daily Usage Limit')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save daily usage limit settings'
          />

          <Alert>
            <AlertDescription className='text-xs'>
              使用量每 5
              分钟从消费日志刷新一次。全站超限会停止所有模型；模型超限只拒绝对应模型，不影响其他模型。
            </AlertDescription>
          </Alert>

          <section className='space-y-5' aria-labelledby='global-limit-heading'>
            <div className='space-y-1'>
              <h2 id='global-limit-heading' className='text-base font-semibold'>
                全站限制
              </h2>
              <p className='text-muted-foreground text-xs leading-5'>
                统计所有模型的当日累计使用量，达到额度后整个模型服务不可用。
              </p>
            </div>

            <UsageMetrics
              enabled={status?.enabled ?? false}
              exceeded={status?.exceeded ?? false}
              used={status?.used_tokens}
              max={status?.limit_tokens}
              remaining={status?.remaining_tokens}
              loading={usageStatusQuery.isLoading}
            />

            <SettingsFormGrid>
              <FormField
                control={form.control}
                name='daily_usage_setting.enabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>
                        {t('Enable daily system usage limit')}
                      </FormLabel>
                      <FormDescription>
                        启用后，全站使用量达到最大额度时拒绝所有模型请求。
                      </FormDescription>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />

              <FormField
                control={form.control}
                name='daily_usage_setting.limit_tokens'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>全站最大额度</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        step={1}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      模型独立额度必须严格小于该值。
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='daily_usage_setting.timezone'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Accounting timezone')}</FormLabel>
                    <FormControl>
                      <Input placeholder='Asia/Shanghai' {...field} />
                    </FormControl>
                    <FormDescription>
                      {t('The day resets at midnight in this timezone.')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <SettingsFormGridItem span='full'>
                <FormField
                  control={form.control}
                  name='daily_usage_setting.message'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Limit exceeded message')}</FormLabel>
                      <FormControl>
                        <Textarea rows={3} {...field} />
                      </FormControl>
                      <FormDescription>
                        全站额度达到上限时返回给 API 客户端的提示。
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </SettingsFormGridItem>
            </SettingsFormGrid>
          </section>

          <Separator />

          <section className='space-y-4' aria-labelledby='model-limit-heading'>
            <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
              <div className='space-y-1'>
                <h2
                  id='model-limit-heading'
                  className='text-base font-semibold'
                >
                  模型独立限制
                </h2>
                <p className='text-muted-foreground text-xs leading-5'>
                  未配置的模型没有独立额度，只受全站限制影响。
                </p>
              </div>
              <Button type='button' variant='outline' onClick={addModelLimit}>
                <Plus data-icon='inline-start' />
                添加模型限制
              </Button>
            </div>

            {modelLimitsFieldArray.fields.length === 0 ? (
              <Empty className='rounded-md border'>
                <EmptyHeader>
                  <EmptyTitle>暂未配置模型独立限制</EmptyTitle>
                  <EmptyDescription>
                    所有模型当前只受全站额度控制。
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <div className='space-y-3'>
                {modelLimitsFieldArray.fields.map((field, index) => {
                  const modelName = watchedModelLimits[index]?.model_name ?? ''
                  const modelStatus = modelStatusByName.get(modelName)
                  return (
                    <div
                      key={field.id}
                      className='border-border/70 space-y-4 rounded-md border p-4'
                    >
                      <div className='grid min-w-0 gap-4 lg:grid-cols-[minmax(0,1.5fr)_minmax(150px,0.8fr)_auto_auto] lg:items-start'>
                        <FormField
                          control={form.control}
                          name={`daily_usage_setting.model_limits.${index}.model_name`}
                          render={({ field: modelField }) => (
                            <FormItem className='min-w-0'>
                              <FormLabel>模型名称</FormLabel>
                              <Select
                                value={modelField.value}
                                onValueChange={modelField.onChange}
                              >
                                <FormControl>
                                  <SelectTrigger className='w-full'>
                                    <SelectValue placeholder='选择模型' />
                                  </SelectTrigger>
                                </FormControl>
                                <SelectContent alignItemWithTrigger={false}>
                                  <SelectGroup>
                                    {modelOptions.map((model) => (
                                      <SelectItem key={model} value={model}>
                                        {model}
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
                          control={form.control}
                          name={`daily_usage_setting.model_limits.${index}.model_max_usage`}
                          render={({ field: maxField }) => (
                            <FormItem>
                              <FormLabel>最大额度</FormLabel>
                              <FormControl>
                                <Input
                                  type='number'
                                  min={1}
                                  step={1}
                                  {...safeNumberFieldProps(maxField)}
                                />
                              </FormControl>
                              <FormMessage />
                            </FormItem>
                          )}
                        />

                        <FormField
                          control={form.control}
                          name={`daily_usage_setting.model_limits.${index}.enabled`}
                          render={({ field: enabledField }) => (
                            <FormItem className='flex min-h-16 items-center gap-3 pt-6 lg:justify-end'>
                              <FormLabel className='font-normal'>
                                启用
                              </FormLabel>
                              <FormControl>
                                <Switch
                                  checked={enabledField.value}
                                  onCheckedChange={enabledField.onChange}
                                />
                              </FormControl>
                            </FormItem>
                          )}
                        />

                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          className='mt-6'
                          aria-label={`删除 ${modelName || '模型'} 限制`}
                          title='删除模型限制'
                          onClick={() => modelLimitsFieldArray.remove(index)}
                        >
                          <Trash2 />
                        </Button>
                      </div>

                      <ModelUsageMetrics
                        config={watchedModelLimits[index]}
                        status={modelStatus}
                        loading={usageStatusQuery.isLoading}
                      />
                    </div>
                  )
                })}
              </div>
            )}

            {enabledModelsQuery.isError ||
            enabledModelsQuery.data?.success === false ? (
              <p className='text-destructive text-xs'>
                模型列表加载失败，已保存的模型仍会保留。
              </p>
            ) : null}
          </section>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}

function UsageMetrics(props: {
  enabled: boolean
  exceeded: boolean
  used?: number
  max?: number
  remaining?: number
  loading: boolean
}) {
  return (
    <div className='border-border/70 grid overflow-hidden rounded-md border sm:grid-cols-4'>
      <UsageMetric
        label='当前使用量'
        value={formatUsage(props.used, props.loading)}
      />
      <UsageMetric
        label='最大额度'
        value={formatUsage(props.max, props.loading)}
      />
      <UsageMetric
        label='剩余额度'
        value={formatUsage(props.remaining, props.loading)}
      />
      <UsageMetric
        label='状态'
        value={
          <LimitStatusBadge
            enabled={props.enabled}
            exceeded={props.exceeded}
            loading={props.loading}
          />
        }
      />
    </div>
  )
}

function ModelUsageMetrics(props: {
  config?: DailyUsageModelLimitConfig
  status?: ModelDailyUsageStatus
  loading: boolean
}) {
  const enabled = props.config?.enabled ?? false
  return (
    <div className='bg-muted/20 grid rounded-md sm:grid-cols-3'>
      <UsageMetric
        label='当前使用量'
        value={formatUsage(props.status?.model_current_usage, props.loading)}
      />
      <UsageMetric
        label='剩余额度'
        value={formatUsage(props.status?.remaining_usage, props.loading)}
      />
      <UsageMetric
        label='状态'
        value={
          <LimitStatusBadge
            enabled={enabled}
            exceeded={props.status?.exceeded ?? false}
            loading={props.loading}
            pending={!props.status && Boolean(props.config?.model_name)}
          />
        }
      />
    </div>
  )
}

function UsageMetric(props: { label: string; value: React.ReactNode }) {
  return (
    <div className='border-border/60 min-w-0 space-y-1 px-4 py-3 sm:border-r sm:last:border-r-0'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='truncate text-sm font-semibold'>{props.value}</div>
    </div>
  )
}

function LimitStatusBadge(props: {
  enabled: boolean
  exceeded: boolean
  loading: boolean
  pending?: boolean
}) {
  if (props.loading) return <Badge variant='outline'>刷新中</Badge>
  if (props.pending) return <Badge variant='outline'>保存后统计</Badge>
  if (!props.enabled) return <Badge variant='secondary'>未启用</Badge>
  if (props.exceeded) return <Badge variant='destructive'>已限制</Badge>
  return (
    <Badge
      variant='outline'
      className='border-emerald-500/40 text-emerald-700 dark:text-emerald-300'
    >
      正常
    </Badge>
  )
}

function formatUsage(value: number | undefined, loading: boolean) {
  if (loading) return '—'
  if (value === undefined || !Number.isFinite(value)) return '—'
  return `${formatTokens(value)} tokens`
}
