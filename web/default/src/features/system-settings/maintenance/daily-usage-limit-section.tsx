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
import { useEffect, useMemo, useRef } from 'react'
import { type Resolver, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { Alert, AlertDescription } from '@/components/ui/alert'
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
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

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

const createDailyUsageLimitSchema = (t: (key: string) => string) =>
  z
    .object({
      daily_usage_setting: z.object({
        enabled: z.boolean(),
        limit_tokens: z.coerce.number().int().min(0),
        timezone: z.string().min(1),
        message: z.string().min(1),
      }),
    })
    .superRefine((values, ctx) => {
      if (
        values.daily_usage_setting.enabled &&
        values.daily_usage_setting.limit_tokens <= 0
      ) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['daily_usage_setting', 'limit_tokens'],
          message: t('Daily token limit must be greater than 0 when enabled'),
        })
      }
    })

type DailyUsageLimitFormValues = z.infer<
  ReturnType<typeof createDailyUsageLimitSchema>
>

export type FlatDailyUsageLimitDefaults = {
  'daily_usage_setting.enabled': boolean
  'daily_usage_setting.limit_tokens': number
  'daily_usage_setting.timezone': string
  'daily_usage_setting.message': string
}

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
  },
})

const normalizeFormValues = (
  values: DailyUsageLimitFormValues
): FlatDailyUsageLimitDefaults => ({
  'daily_usage_setting.enabled': values.daily_usage_setting.enabled,
  'daily_usage_setting.limit_tokens': values.daily_usage_setting.limit_tokens,
  'daily_usage_setting.timezone': values.daily_usage_setting.timezone,
  'daily_usage_setting.message': values.daily_usage_setting.message,
})

const orderDailyUsageEntries = (
  entries: [string, string | number | boolean][],
  enabled: boolean
) =>
  [...entries].sort(([leftKey], [rightKey]) => {
    const enabledKey = 'daily_usage_setting.enabled'
    if (leftKey === enabledKey && rightKey !== enabledKey) {
      return enabled ? 1 : -1
    }
    if (rightKey === enabledKey && leftKey !== enabledKey) {
      return enabled ? -1 : 1
    }
    return 0
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
    const entries = Object.entries(normalized).filter(
      ([key, value]) =>
        baselineRef.current[key as keyof FlatDailyUsageLimitDefaults] !== value
    )
    const orderedEntries = orderDailyUsageEntries(
      entries,
      normalized['daily_usage_setting.enabled']
    )

    try {
      for (const [key, value] of orderedEntries) {
        await updateOption.mutateAsync({ key, value })
      }
      baselineRef.current = normalized
      baselineSerializedRef.current = JSON.stringify(normalized)
      form.reset(buildFormDefaults(normalized))
      toast.success(t('Daily usage limit settings saved'))
    } catch (error) {
      toast.error(t('Failed to save settings'))
      throw error
    }
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
              {t(
                'The limiter uses a dedicated daily token counter and refreshes every 5 minutes. Rejections are recorded before any channel API call.'
              )}
            </AlertDescription>
          </Alert>

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
                      {t(
                        'When enabled, model requests are rejected after the five-minute usage snapshot reaches the daily token cap.'
                      )}
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
                  <FormLabel>{t('Daily token limit')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Set the maximum total tokens the whole system can use in one day.'
                    )}
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
                      {t(
                        "Message returned to API clients when today's system usage has reached the limit."
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SettingsFormGridItem>
          </SettingsFormGrid>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
