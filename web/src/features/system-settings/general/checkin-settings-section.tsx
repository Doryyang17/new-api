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
import { useMemo } from 'react'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

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
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import { parseQuotaFromDollars, quotaUnitsToDollars } from '@/lib/format'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const schema = z
  .object({
    enabled: z.boolean(),
    minQuota: z.coerce.number().min(0),
    maxQuota: z.coerce.number().min(0),
    bonusEnabled: z.boolean(),
    bonusMinAmount: z.coerce.number().min(0),
    bonusMaxAmount: z.coerce.number().min(0),
  })
  .refine((values) => values.bonusMinAmount <= values.bonusMaxAmount, {
    message: '最低赠金不能高于最高赠金',
    path: ['bonusMaxAmount'],
  })

type Values = z.infer<typeof schema>

export function CheckinSettingsSection({
  defaultValues,
}: {
  defaultValues: {
    enabled: boolean
    minQuota: number
    maxQuota: number
    bonusEnabled: boolean
    bonusMinAmount: number
    bonusMaxAmount: number
  }
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const quotaStep = currencyMeta.kind === 'tokens' ? 1 : 0.000001
  const quotaPlaceholder = currencyMeta.kind === 'tokens' ? '1000' : '0.01'
  const displayDefaults = useMemo(
    () => ({
      enabled: defaultValues.enabled,
      minQuota: quotaUnitsToDollars(defaultValues.minQuota),
      maxQuota: quotaUnitsToDollars(defaultValues.maxQuota),
      bonusEnabled: defaultValues.bonusEnabled,
      bonusMinAmount: quotaUnitsToDollars(defaultValues.bonusMinAmount),
      bonusMaxAmount: quotaUnitsToDollars(defaultValues.bonusMaxAmount),
    }),
    [
      defaultValues.bonusEnabled,
      defaultValues.bonusMaxAmount,
      defaultValues.bonusMinAmount,
      defaultValues.enabled,
      defaultValues.maxQuota,
      defaultValues.minQuota,
    ]
  )

  const form = useForm<Values>({
    resolver: zodResolver(schema) as unknown as Resolver<Values>,
    defaultValues: displayDefaults,
  })

  const { isDirty, isSubmitting } = form.formState
  const enabled = form.watch('enabled')
  const bonusEnabled = form.watch('bonusEnabled')

  async function onSubmit(values: Values) {
    const updates: Array<{ key: string; value: string }> = []

    if (values.enabled !== displayDefaults.enabled) {
      updates.push({
        key: 'checkin_setting.enabled',
        value: String(values.enabled),
      })
    }

    if (values.minQuota !== displayDefaults.minQuota) {
      updates.push({
        key: 'checkin_setting.min_quota',
        value: String(parseQuotaFromDollars(values.minQuota)),
      })
    }

    if (values.maxQuota !== displayDefaults.maxQuota) {
      updates.push({
        key: 'checkin_setting.max_quota',
        value: String(parseQuotaFromDollars(values.maxQuota)),
      })
    }

    if (values.bonusEnabled !== displayDefaults.bonusEnabled) {
      updates.push({
        key: 'checkin_bonus_setting.enabled',
        value: String(values.bonusEnabled),
      })
    }

    const bonusMinUpdate =
      values.bonusMinAmount !== displayDefaults.bonusMinAmount
        ? {
            key: 'checkin_bonus_setting.min_amount',
            value: String(parseQuotaFromDollars(values.bonusMinAmount)),
          }
        : null

    const bonusMaxUpdate =
      values.bonusMaxAmount !== displayDefaults.bonusMaxAmount
        ? {
            key: 'checkin_bonus_setting.max_amount',
            value: String(parseQuotaFromDollars(values.bonusMaxAmount)),
          }
        : null

    // Keep the persisted range valid between the two single-option requests.
    // Moving the whole range upward requires max first; every other change is
    // safe with min first.
    if (
      bonusMinUpdate &&
      bonusMaxUpdate &&
      values.bonusMinAmount > displayDefaults.bonusMaxAmount
    ) {
      updates.push(bonusMaxUpdate, bonusMinUpdate)
    } else {
      if (bonusMinUpdate) updates.push(bonusMinUpdate)
      if (bonusMaxUpdate) updates.push(bonusMaxUpdate)
    }

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }

    form.reset(values)
  }

  return (
    <SettingsSection title={t('Check-in Settings')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending || isSubmitting}
            isSaveDisabled={!isDirty}
            saveLabel='Save check-in settings'
          />
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable check-in feature')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Allow users to check in daily for random quota rewards'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={updateOption.isPending || isSubmitting}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          {enabled && (
            <div className='space-y-6'>
              <div className='grid gap-6 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='minQuota'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Minimum check-in quota')} ({currencyLabel})
                      </FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          step={quotaStep}
                          placeholder={quotaPlaceholder}
                          disabled={
                            bonusEnabled ||
                            updateOption.isPending ||
                            isSubmitting
                          }
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        仅在独立签到赠金关闭时使用
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='maxQuota'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Maximum check-in quota')} ({currencyLabel})
                      </FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          step={quotaStep}
                          placeholder={quotaPlaceholder}
                          disabled={
                            bonusEnabled ||
                            updateOption.isPending ||
                            isSubmitting
                          }
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        余额奖励与独立签到赠金不会同时发放
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
              <div className='border-t pt-6'>
                <FormField
                  control={form.control}
                  name='bonusEnabled'
                  render={({ field }) => (
                    <SettingsSwitchItem>
                      <SettingsSwitchContent>
                        <FormLabel>启用独立签到赠金</FormLabel>
                        <FormDescription>
                          开启后仅发放独立赠金，不再发放账户余额奖励；赠金当天
                          24:00 自动失效并优先抵扣消费
                        </FormDescription>
                      </SettingsSwitchContent>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                          disabled={updateOption.isPending || isSubmitting}
                        />
                      </FormControl>
                    </SettingsSwitchItem>
                  )}
                />
              </div>
              {bonusEnabled && (
                <div className='grid gap-6 sm:grid-cols-2'>
                  <FormField
                    control={form.control}
                    name='bonusMinAmount'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>最低签到赠金 ({currencyLabel})</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={0}
                            step={quotaStep}
                            placeholder='0.1'
                            {...field}
                          />
                        </FormControl>
                        <FormDescription>
                          每次签到随机赠金的最低金额
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name='bonusMaxAmount'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>最高签到赠金 ({currencyLabel})</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={0}
                            step={quotaStep}
                            placeholder='1'
                            {...field}
                          />
                        </FormControl>
                        <FormDescription>
                          每次签到随机赠金的最高金额
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>
              )}
            </div>
          )}
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
