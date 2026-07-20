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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import * as z from 'zod'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'

import { updateRequestRiskOptions } from '../api'
import {
  SettingsForm,
  SettingsFormGrid,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import type { UpdateRequestRiskOptionsRequest } from '../types'
import { RequestRiskLogsPanel } from './request-risk-logs-panel'

const requestRiskSchema = z.object({
  request_risk_setting: z.object({
    enabled: z.boolean(),
    mode: z.enum(['observe', 'enforce']),
    concurrency_mode: z.enum(['observe', 'enforce']),
    log_matches: z.boolean(),
    medium_cooldown_seconds: z.number().int().min(1).max(86400),
    token_block_seconds: z.number().int().min(1).max(86400),
    user_block_seconds: z.number().int().min(1).max(86400),
    ip_block_seconds: z.number().int().min(1).max(86400),
    user_concurrency_limit: z.number().int().min(0).max(1000),
    token_concurrency_limit: z.number().int().min(0).max(1000),
    group_whitelist: z.string(),
  }),
})

type RequestRiskFormValues = z.output<typeof requestRiskSchema>
type RequestRiskFormInput = z.input<typeof requestRiskSchema>

export type RequestRiskDefaults = {
  'request_risk_setting.enabled': boolean
  'request_risk_setting.mode': 'observe' | 'enforce'
  'request_risk_setting.concurrency_mode': '' | 'observe' | 'enforce'
  'request_risk_setting.log_matches': boolean
  'request_risk_setting.medium_cooldown_seconds': number
  'request_risk_setting.token_block_seconds': number
  'request_risk_setting.user_block_seconds': number
  'request_risk_setting.ip_block_seconds': number
  'request_risk_setting.user_concurrency_limit': number
  'request_risk_setting.token_concurrency_limit': number
  'request_risk_setting.group_whitelist': string[]
}

type RequestRiskSectionProps = {
  defaultValues: RequestRiskDefaults
}

const modeItems = [
  { value: 'observe', label: '观察模式' },
  { value: 'enforce', label: '拦截模式' },
] as const

const concurrencyModeItems = [
  { value: 'observe', label: '观察模式' },
  { value: 'enforce', label: '限制模式' },
] as const

function resolveMode(
  value: '' | 'observe' | 'enforce',
  fallback: 'observe' | 'enforce'
): 'observe' | 'enforce' {
  if (value === 'observe' || value === 'enforce') {
    return value
  }
  return fallback
}

function buildFormDefaults(
  defaults: RequestRiskDefaults
): RequestRiskFormInput {
  return {
    request_risk_setting: {
      enabled: defaults['request_risk_setting.enabled'],
      mode: defaults['request_risk_setting.mode'],
      concurrency_mode: resolveMode(
        defaults['request_risk_setting.concurrency_mode'],
        defaults['request_risk_setting.mode']
      ),
      log_matches: defaults['request_risk_setting.log_matches'],
      medium_cooldown_seconds:
        defaults['request_risk_setting.medium_cooldown_seconds'],
      token_block_seconds: defaults['request_risk_setting.token_block_seconds'],
      user_block_seconds: defaults['request_risk_setting.user_block_seconds'],
      ip_block_seconds: defaults['request_risk_setting.ip_block_seconds'],
      user_concurrency_limit:
        defaults['request_risk_setting.user_concurrency_limit'],
      token_concurrency_limit:
        defaults['request_risk_setting.token_concurrency_limit'],
      group_whitelist:
        defaults['request_risk_setting.group_whitelist'].join('\n'),
    },
  }
}

function parseGroupWhitelist(value: string): string[] {
  return [
    ...new Set(
      value
        .split(/[\n,]/)
        .map((item) => item.trim())
        .filter(Boolean)
    ),
  ]
}

function normalizeFormValues(
  values: RequestRiskFormValues
): RequestRiskDefaults {
  return {
    'request_risk_setting.enabled': values.request_risk_setting.enabled,
    'request_risk_setting.mode': values.request_risk_setting.mode,
    'request_risk_setting.concurrency_mode':
      values.request_risk_setting.concurrency_mode,
    'request_risk_setting.log_matches': values.request_risk_setting.log_matches,
    'request_risk_setting.medium_cooldown_seconds':
      values.request_risk_setting.medium_cooldown_seconds,
    'request_risk_setting.token_block_seconds':
      values.request_risk_setting.token_block_seconds,
    'request_risk_setting.user_block_seconds':
      values.request_risk_setting.user_block_seconds,
    'request_risk_setting.ip_block_seconds':
      values.request_risk_setting.ip_block_seconds,
    'request_risk_setting.user_concurrency_limit':
      values.request_risk_setting.user_concurrency_limit,
    'request_risk_setting.token_concurrency_limit':
      values.request_risk_setting.token_concurrency_limit,
    'request_risk_setting.group_whitelist': parseGroupWhitelist(
      values.request_risk_setting.group_whitelist
    ),
  }
}

function valuesEqual(
  left: RequestRiskDefaults[keyof RequestRiskDefaults],
  right: RequestRiskDefaults[keyof RequestRiskDefaults]
): boolean {
  if (Array.isArray(left) && Array.isArray(right)) {
    return JSON.stringify(left) === JSON.stringify(right)
  }
  return left === right
}

export function RequestRiskSection(props: RequestRiskSectionProps) {
  const queryClient = useQueryClient()
  const updateRequestRisk = useMutation({
    mutationFn: async (request: UpdateRequestRiskOptionsRequest) => {
      const response = await updateRequestRiskOptions(request)
      if (!response.success) {
        throw new Error(response.message || '批量测活与并发防护设置保存失败')
      }
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['system-options'] })
      toast.success('批量测活与并发防护设置已保存')
    },
    onError: (error: Error) => {
      toast.error(error.message || '批量测活与并发防护设置保存失败')
    },
  })
  const form = useForm<RequestRiskFormInput, unknown, RequestRiskFormValues>({
    resolver: zodResolver(requestRiskSchema),
    mode: 'onChange',
    defaultValues: buildFormDefaults(props.defaultValues),
  })

  useEffect(() => {
    form.reset(buildFormDefaults(props.defaultValues))
  }, [form, props.defaultValues])

  const enabled = form.watch('request_risk_setting.enabled')
  const riskMode = form.watch('request_risk_setting.mode')
  const concurrencyMode = form.watch('request_risk_setting.concurrency_mode')

  const onSubmit = async (values: RequestRiskFormValues) => {
    const normalized = normalizeFormValues(values)
    const updates = Object.entries(normalized)
      .filter(([key, value]) => {
        const typedKey = key as keyof RequestRiskDefaults
        return !valuesEqual(value, props.defaultValues[typedKey])
      })
      .map(([key, value]) => ({
        key,
        value: Array.isArray(value) ? JSON.stringify(value) : String(value),
      }))

    if (updates.length === 0) {
      return
    }

    try {
      await updateRequestRisk.mutateAsync({ updates })
    } catch {
      // The mutation's onError callback owns the user-facing error message.
    }
  }

  return (
    <SettingsSection title='批量测活与并发防护'>
      <SettingsPageFormActions
        onSave={form.handleSubmit(onSubmit)}
        isSaving={form.formState.isSubmitting}
        saveLabel='保存批量测活与并发防护'
      />

      <Tabs defaultValue='settings'>
        <TabsList className='grid w-full max-w-md grid-cols-2'>
          <TabsTrigger value='settings'>防护设置</TabsTrigger>
          <TabsTrigger value='logs'>触发日志</TabsTrigger>
        </TabsList>

        <TabsContent value='settings'>
          <Form {...form}>
            <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
              <Alert>
                <AlertTitle>批量测活建议先使用观察模式</AlertTitle>
                <AlertDescription>
                  批量测活与并发限制可分别设置运行模式。建议先观察行为评分，同时保留并发限制，确认没有误命中后再启用测活拦截。
                </AlertDescription>
              </Alert>

              <FormField
                control={form.control}
                name='request_risk_setting.enabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>启用批量测活与并发防护</FormLabel>
                      <FormDescription>
                        根据请求频率、模型轮询和在途并发进行保护。重复内容只作为辅助证据，业务失败和失败重试不参与拦截。
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

              <SettingsFormGrid>
                <FormField
                  control={form.control}
                  name='request_risk_setting.mode'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>批量测活模式</FormLabel>
                      <Select
                        items={modeItems}
                        value={field.value}
                        onValueChange={(value) =>
                          field.onChange(value as 'observe' | 'enforce')
                        }
                        disabled={!enabled}
                      >
                        <FormControl>
                          <SelectTrigger className='w-full'>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            <SelectItem value='observe'>观察模式</SelectItem>
                            <SelectItem value='enforce'>拦截模式</SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormDescription>
                        {riskMode === 'observe'
                          ? '只记录满足强异常条件的测活风险，不返回 429。'
                          : '仅当风险分达到阈值且命中高频或模型轮询等强异常因子时返回 429。'}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='request_risk_setting.concurrency_mode'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>并发限制模式</FormLabel>
                      <Select
                        items={concurrencyModeItems}
                        value={field.value}
                        onValueChange={(value) =>
                          field.onChange(value as 'observe' | 'enforce')
                        }
                        disabled={!enabled}
                      >
                        <FormControl>
                          <SelectTrigger className='w-full'>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            <SelectItem value='observe'>观察模式</SelectItem>
                            <SelectItem value='enforce'>限制模式</SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormDescription>
                        {concurrencyMode === 'observe'
                          ? '只记录并发超限，不改变请求结果。'
                          : '用户或 API 密钥在途请求超限时直接返回 429。'}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='request_risk_setting.medium_cooldown_seconds'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>中风险冷却时间（秒）</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          max={86400}
                          disabled={!enabled}
                          {...field}
                          onChange={(event) =>
                            field.onChange(
                              Number.parseInt(event.target.value, 10) || 1
                            )
                          }
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='request_risk_setting.token_block_seconds'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>令牌高风险限制时间（秒）</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          max={86400}
                          disabled={!enabled}
                          {...field}
                          onChange={(event) =>
                            field.onChange(
                              Number.parseInt(event.target.value, 10) || 1
                            )
                          }
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='request_risk_setting.user_block_seconds'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>用户高风险限制时间（秒）</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          max={86400}
                          disabled={!enabled}
                          {...field}
                          onChange={(event) =>
                            field.onChange(
                              Number.parseInt(event.target.value, 10) || 1
                            )
                          }
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='request_risk_setting.ip_block_seconds'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>IP 高风险限制时间（秒）</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          max={86400}
                          disabled={!enabled}
                          {...field}
                          onChange={(event) =>
                            field.onChange(
                              Number.parseInt(event.target.value, 10) || 1
                            )
                          }
                        />
                      </FormControl>
                      <FormDescription>
                        只有显式配置可信代理后才会启用 IP 评分和限制。
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='request_risk_setting.user_concurrency_limit'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>单用户在途请求上限</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          max={1000}
                          disabled={!enabled}
                          {...field}
                          onChange={(event) =>
                            field.onChange(
                              Number.parseInt(event.target.value, 10) || 0
                            )
                          }
                        />
                      </FormControl>
                      <FormDescription>
                        同一用户所有令牌、流式响应和 Realtime 连接共享此上限，0
                        表示不限制。
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='request_risk_setting.token_concurrency_limit'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>单令牌在途请求上限</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          max={1000}
                          disabled={!enabled}
                          {...field}
                          onChange={(event) =>
                            field.onChange(
                              Number.parseInt(event.target.value, 10) || 0
                            )
                          }
                        />
                      </FormControl>
                      <FormDescription>
                        超限时不排队，并发限制模式为“限制模式”时直接返回 429；0
                        表示不限制。
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </SettingsFormGrid>

              <FormField
                control={form.control}
                name='request_risk_setting.group_whitelist'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>防护豁免分组</FormLabel>
                    <FormControl>
                      <Textarea
                        rows={4}
                        placeholder={'trusted\nvip'}
                        disabled={!enabled}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      每行或使用逗号填写一个分组。豁免分组不会执行行为评分或在途并发限制。
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='request_risk_setting.log_matches'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>记录风控日志</FormLabel>
                      <FormDescription>
                        记录风险分、命中项、并发超限、评分文本和完整请求体。完整请求体仅管理员可见，不保存令牌原文或请求头。
                      </FormDescription>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                        disabled={!enabled}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
            </SettingsForm>
          </Form>
        </TabsContent>

        <TabsContent value='logs'>
          <RequestRiskLogsPanel />
        </TabsContent>
      </Tabs>
    </SettingsSection>
  )
}
