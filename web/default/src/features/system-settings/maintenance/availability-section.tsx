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
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

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

const timePattern = /^([01]\d|2[0-3]):[0-5]\d$/

const availabilitySchema = z.object({
  availability_setting: z.object({
    enabled: z.boolean(),
    unavailable_start: z.string().regex(timePattern),
    unavailable_end: z.string().regex(timePattern),
    timezone: z.string().min(1),
    message: z.string().min(1),
  }),
})

type AvailabilityFormInput = z.input<typeof availabilitySchema>
type AvailabilityFormValues = z.output<typeof availabilitySchema>

export type FlatAvailabilityDefaults = {
  'availability_setting.enabled': boolean
  'availability_setting.unavailable_start': string
  'availability_setting.unavailable_end': string
  'availability_setting.timezone': string
  'availability_setting.message': string
}

const buildFormDefaults = (
  defaults: FlatAvailabilityDefaults
): AvailabilityFormInput => ({
  availability_setting: {
    enabled: defaults['availability_setting.enabled'] ?? false,
    unavailable_start:
      defaults['availability_setting.unavailable_start'] || '22:00',
    unavailable_end:
      defaults['availability_setting.unavailable_end'] || '08:00',
    timezone: defaults['availability_setting.timezone'] || 'Asia/Shanghai',
    message:
      defaults['availability_setting.message'] ||
      '当前处于宵禁状态，22:00-8:00期间服务不可用，敬请谅解~',
  },
})

const normalizeFormValues = (
  values: AvailabilityFormValues
): FlatAvailabilityDefaults => ({
  'availability_setting.enabled': values.availability_setting.enabled,
  'availability_setting.unavailable_start':
    values.availability_setting.unavailable_start,
  'availability_setting.unavailable_end':
    values.availability_setting.unavailable_end,
  'availability_setting.timezone': values.availability_setting.timezone,
  'availability_setting.message': values.availability_setting.message,
})

type Props = {
  defaultValues: FlatAvailabilityDefaults
}

export function AvailabilitySection(props: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const formDefaults = useMemo(
    () => buildFormDefaults(props.defaultValues),
    [props.defaultValues]
  )

  const form = useForm<AvailabilityFormInput, unknown, AvailabilityFormValues>({
    resolver: zodResolver(availabilitySchema),
    defaultValues: formDefaults,
  })

  const baselineRef = useRef<FlatAvailabilityDefaults>(props.defaultValues)
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

  const enabled = form.watch('availability_setting.enabled')

  const onSubmit = async (values: AvailabilityFormValues) => {
    const normalized = normalizeFormValues(values)
    const entries = Object.entries(normalized).filter(
      ([key, value]) =>
        baselineRef.current[key as keyof FlatAvailabilityDefaults] !== value
    )

    try {
      for (const [key, value] of entries) {
        await updateOption.mutateAsync({ key, value })
      }
      baselineRef.current = normalized
      baselineSerializedRef.current = JSON.stringify(normalized)
      form.reset(buildFormDefaults(normalized))
      toast.success(t('System availability settings saved'))
    } catch (error) {
      toast.error(t('Failed to save settings'))
      throw error
    }
  }

  return (
    <SettingsSection title={t('System Availability')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save availability settings'
          />
          <SettingsFormGrid>
            <FormField
              control={form.control}
              name='availability_setting.enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable curfew')}</FormLabel>
                    <FormDescription>
                      {t('Reject requests during the configured time window.')}
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
              name='availability_setting.unavailable_start'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Unavailable start')}</FormLabel>
                  <FormControl>
                    <Input type='time' {...field} disabled={!enabled} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='availability_setting.unavailable_end'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Unavailable end')}</FormLabel>
                  <FormControl>
                    <Input type='time' {...field} disabled={!enabled} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='availability_setting.timezone'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Timezone')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='Asia/Shanghai'
                      {...field}
                      disabled={!enabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <SettingsFormGridItem span='full'>
              <FormField
                control={form.control}
                name='availability_setting.message'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Curfew message')}</FormLabel>
                    <FormControl>
                      <Textarea rows={3} {...field} disabled={!enabled} />
                    </FormControl>
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
