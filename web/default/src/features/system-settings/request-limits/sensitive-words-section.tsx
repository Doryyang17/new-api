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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertCircle,
  CheckCircle2,
  Eye,
  FileText,
  Pencil,
  Plus,
  RefreshCcw,
  Save,
  Search,
  ShieldCheck,
  Trash2,
  Upload,
  Wand2,
  X,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { type Resolver, type UseFormReturn, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
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
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'

import {
  clearPromptFilterLogs,
  deletePromptFilterLexicon,
  getPromptFilterLogs,
  getPromptFilterLexicons,
  getPromptFilterRules,
  getPromptFilterStatus,
  previewPromptFilterLexicon,
  savePromptFilterLexiconWords,
  testPromptFilter,
  updatePromptFilterLexicon,
  uploadPromptFilterLexicon,
} from '../api'
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
import type {
  PromptFilterCustomRule,
  PromptFilterLexiconFile,
  PromptFilterLexiconPreviewData,
  PromptFilterLog,
  PromptFilterMode,
  PromptFilterRule,
  PromptFilterVerdict,
} from '../types'
import { safeNumberFieldProps } from '../utils/numeric-field'

type FlatPromptFilterDefaults = {
  CheckSensitiveEnabled: boolean
  CheckSensitiveOnPromptEnabled: boolean
  SensitiveWords: string
  'prompt_filter_setting.mode': PromptFilterMode
  'prompt_filter_setting.threshold': number
  'prompt_filter_setting.strict_threshold': number
  'prompt_filter_setting.log_matches': boolean
  'prompt_filter_setting.max_text_length': number
  'prompt_filter_setting.message': string
  'prompt_filter_setting.block_status_code': number
  'prompt_filter_setting.block_error_code': string
  'prompt_filter_setting.group_whitelist': string[]
  'prompt_filter_setting.channel_whitelist': number[]
  'prompt_filter_setting.custom_patterns': string
  'prompt_filter_setting.disabled_patterns': string
  'prompt_filter_setting.review_enabled': boolean
  'prompt_filter_setting.review_base_url': string
  'prompt_filter_setting.review_model': string
  'prompt_filter_setting.review_timeout_seconds': number
  'prompt_filter_setting.review_fail_closed': boolean
}

type PromptFilterFormValues = {
  CheckSensitiveEnabled: boolean
  CheckSensitiveOnPromptEnabled: boolean
  SensitiveWords: string
  prompt_filter_setting: {
    mode: PromptFilterMode
    threshold: number
    strict_threshold: number
    log_matches: boolean
    max_text_length: number
    message: string
    block_status_code: number
    block_error_code: string
    group_whitelist: string
    channel_whitelist: string
    custom_patterns: string
    disabled_patterns: string
    review_enabled: boolean
    review_base_url: string
    review_model: string
    review_timeout_seconds: number
    review_fail_closed: boolean
    review_api_key: string
  }
}

type PromptTestState = {
  endpoint: string
  model: string
  text: string
  verdict?: PromptFilterVerdict
}

type LogsFilterState = {
  source: string
  action: string
  endpoint: string
  model: string
  apiKeyId: string
  query: string
}

const defaultPromptFilterMessage =
  'Request contains content blocked by prompt filter'
const defaultPromptFilterBlockStatusCode = 460
const defaultPromptFilterBlockErrorCode = 'prompt_blocked'
const defaultReviewBaseUrl = 'https://api.openai.com'
const defaultReviewModel = 'omni-moderation-latest'

const isPromptFilterMode = (value: string): value is PromptFilterMode =>
  value === 'block' || value === 'warn' || value === 'monitor'

const safeMode = (value: string): PromptFilterMode =>
  isPromptFilterMode(value) ? value : 'block'

const formatJson = (value: unknown) => JSON.stringify(value, null, 2)

const formatJsonTextarea = (value: string, fallback: string) => {
  const trimmed = value.trim()
  if (trimmed === '') return fallback
  try {
    return formatJson(JSON.parse(trimmed))
  } catch {
    return value
  }
}

const csvToStringList = (value: string) =>
  value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)

const csvToNumberList = (value: string) =>
  csvToStringList(value)
    .map((item) => Number(item))
    .filter((item) => Number.isInteger(item) && item > 0)

const createPromptFilterSchema = (t: (key: string) => string) =>
  z
    .object({
      CheckSensitiveEnabled: z.boolean(),
      CheckSensitiveOnPromptEnabled: z.boolean(),
      SensitiveWords: z.string(),
      prompt_filter_setting: z.object({
        mode: z.enum(['block', 'warn', 'monitor']),
        threshold: z.coerce.number().int().min(1),
        strict_threshold: z.coerce.number().int().min(1),
        log_matches: z.boolean(),
        max_text_length: z.coerce.number().int().min(1024).max(1048576),
        message: z.string().min(1),
        block_status_code: z.coerce
          .number()
          .int()
          .min(400)
          .max(499)
          .refine((value) => value !== 401, {
            message: t('Prompt block status code cannot be 401'),
          }),
        block_error_code: z
          .string()
          .min(1)
          .max(64)
          .regex(/^[a-z][a-z0-9_:-]*$/, {
            message: t(
              'Prompt block error code must use lowercase letters, numbers, underscores, colons, or hyphens'
            ),
          }),
        group_whitelist: z.string(),
        channel_whitelist: z.string(),
        custom_patterns: z.string().refine(
          (value) => {
            try {
              return Array.isArray(JSON.parse(value || '[]'))
            } catch {
              return false
            }
          },
          { message: t('Custom prompt rules must be a JSON array') }
        ),
        disabled_patterns: z.string().refine(
          (value) => {
            try {
              return Array.isArray(JSON.parse(value || '[]'))
            } catch {
              return false
            }
          },
          { message: t('Disabled built-in rules must be a JSON array') }
        ),
        review_enabled: z.boolean(),
        review_base_url: z.string().min(1),
        review_model: z.string().min(1),
        review_timeout_seconds: z.coerce.number().int().min(1).max(60),
        review_fail_closed: z.boolean(),
        review_api_key: z.string(),
      }),
    })
    .superRefine((values, ctx) => {
      if (
        values.prompt_filter_setting.strict_threshold <
        values.prompt_filter_setting.threshold
      ) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['prompt_filter_setting', 'strict_threshold'],
          message: t(
            'Strict threshold must be greater than or equal to threshold'
          ),
        })
      }
      const invalidChannel = csvToStringList(
        values.prompt_filter_setting.channel_whitelist
      ).find((item) => !/^\d+$/.test(item) || Number(item) <= 0)
      if (invalidChannel) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['prompt_filter_setting', 'channel_whitelist'],
          message: t('Channel whitelist must contain positive numeric IDs'),
        })
      }
    })

const buildFormDefaults = (
  defaults: FlatPromptFilterDefaults
): PromptFilterFormValues => ({
  CheckSensitiveEnabled: defaults.CheckSensitiveEnabled,
  CheckSensitiveOnPromptEnabled: defaults.CheckSensitiveOnPromptEnabled,
  SensitiveWords: defaults.SensitiveWords || '',
  prompt_filter_setting: {
    mode: safeMode(defaults['prompt_filter_setting.mode']),
    threshold: defaults['prompt_filter_setting.threshold'] ?? 50,
    strict_threshold: defaults['prompt_filter_setting.strict_threshold'] ?? 90,
    log_matches: defaults['prompt_filter_setting.log_matches'] ?? true,
    max_text_length:
      defaults['prompt_filter_setting.max_text_length'] ?? 80 * 1024,
    message:
      defaults['prompt_filter_setting.message'] || defaultPromptFilterMessage,
    block_status_code:
      defaults['prompt_filter_setting.block_status_code'] ??
      defaultPromptFilterBlockStatusCode,
    block_error_code:
      defaults['prompt_filter_setting.block_error_code'] ||
      defaultPromptFilterBlockErrorCode,
    group_whitelist: (
      defaults['prompt_filter_setting.group_whitelist'] ?? []
    ).join(', '),
    channel_whitelist: (
      defaults['prompt_filter_setting.channel_whitelist'] ?? []
    ).join(', '),
    custom_patterns: formatJsonTextarea(
      defaults['prompt_filter_setting.custom_patterns'],
      '[]'
    ),
    disabled_patterns: formatJsonTextarea(
      defaults['prompt_filter_setting.disabled_patterns'],
      '[]'
    ),
    review_enabled: defaults['prompt_filter_setting.review_enabled'] ?? false,
    review_base_url:
      defaults['prompt_filter_setting.review_base_url'] || defaultReviewBaseUrl,
    review_model:
      defaults['prompt_filter_setting.review_model'] || defaultReviewModel,
    review_timeout_seconds:
      defaults['prompt_filter_setting.review_timeout_seconds'] ?? 10,
    review_fail_closed:
      defaults['prompt_filter_setting.review_fail_closed'] ?? true,
    review_api_key: '',
  },
})

const normalizeFormValues = (
  values: PromptFilterFormValues
): FlatPromptFilterDefaults & {
  'prompt_filter_setting.review_api_key'?: string
} => ({
  CheckSensitiveEnabled: values.CheckSensitiveEnabled,
  CheckSensitiveOnPromptEnabled: values.CheckSensitiveOnPromptEnabled,
  SensitiveWords: values.SensitiveWords ?? '',
  'prompt_filter_setting.mode': values.prompt_filter_setting.mode,
  'prompt_filter_setting.threshold': values.prompt_filter_setting.threshold,
  'prompt_filter_setting.strict_threshold':
    values.prompt_filter_setting.strict_threshold,
  'prompt_filter_setting.log_matches': values.prompt_filter_setting.log_matches,
  'prompt_filter_setting.max_text_length':
    values.prompt_filter_setting.max_text_length,
  'prompt_filter_setting.message': values.prompt_filter_setting.message,
  'prompt_filter_setting.block_status_code':
    values.prompt_filter_setting.block_status_code,
  'prompt_filter_setting.block_error_code':
    values.prompt_filter_setting.block_error_code.trim(),
  'prompt_filter_setting.group_whitelist': csvToStringList(
    values.prompt_filter_setting.group_whitelist
  ),
  'prompt_filter_setting.channel_whitelist': csvToNumberList(
    values.prompt_filter_setting.channel_whitelist
  ),
  'prompt_filter_setting.custom_patterns': formatJsonTextarea(
    values.prompt_filter_setting.custom_patterns,
    '[]'
  ),
  'prompt_filter_setting.disabled_patterns': formatJsonTextarea(
    values.prompt_filter_setting.disabled_patterns,
    '[]'
  ),
  'prompt_filter_setting.review_enabled':
    values.prompt_filter_setting.review_enabled,
  'prompt_filter_setting.review_base_url':
    values.prompt_filter_setting.review_base_url.trim(),
  'prompt_filter_setting.review_model':
    values.prompt_filter_setting.review_model.trim(),
  'prompt_filter_setting.review_timeout_seconds':
    values.prompt_filter_setting.review_timeout_seconds,
  'prompt_filter_setting.review_fail_closed':
    values.prompt_filter_setting.review_fail_closed,
  ...(values.prompt_filter_setting.review_api_key.trim()
    ? {
        'prompt_filter_setting.review_api_key':
          values.prompt_filter_setting.review_api_key.trim(),
      }
    : {}),
})

const valuesEqual = (left: unknown, right: unknown) =>
  JSON.stringify(left) === JSON.stringify(right)

const uniqueCategories = (rules: PromptFilterRule[]) => {
  const categories = new Set<string>()
  rules.forEach((rule) => {
    if (rule.category) categories.add(rule.category)
  })
  return [...categories].sort((a, b) => a.localeCompare(b))
}

const formatFileSize = (bytes: number) => {
  if (!bytes || Number.isNaN(bytes)) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let value = bytes
  let unitIndex = 0
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024
    unitIndex += 1
  }
  return `${Number.parseFloat(value.toFixed(1))} ${units[unitIndex]}`
}

const formatUploadedAt = (timestamp: number) => {
  if (!timestamp) return '-'
  return new Date(timestamp * 1000).toLocaleString()
}

const selectedLexiconFileLabel = (files: File[]) => {
  if (files.length === 0) return '未选择文件'
  if (files.length === 1) return files[0].name
  return `已选择 ${files.length} 个文件`
}

const lexiconPreviewLimit = 200
const lexiconEditLimit = 200_000

const promptFilterLexiconSourceLabel = (source?: string) => {
  if (source === 'preset') return '预设'
  return '上传'
}

type SensitiveWordsSectionProps = {
  defaultValues: FlatPromptFilterDefaults
}

export function SensitiveWordsSection(props: SensitiveWordsSectionProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const updateOption = useUpdateOption()
  const [activeTab, setActiveTab] = useState('overview')
  const promptFilterSchema = useMemo(() => createPromptFilterSchema(t), [t])
  const formDefaults = useMemo(
    () => buildFormDefaults(props.defaultValues),
    [props.defaultValues]
  )
  const baselineRef = useRef(normalizeFormValues(formDefaults))
  const baselineSerializedRef = useRef(JSON.stringify(baselineRef.current))

  const form = useForm<PromptFilterFormValues, unknown, PromptFilterFormValues>(
    {
      resolver: zodResolver(promptFilterSchema) as Resolver<
        PromptFilterFormValues,
        unknown,
        PromptFilterFormValues
      >,
      defaultValues: formDefaults,
    }
  )

  const statusQuery = useQuery({
    queryKey: ['prompt-filter', 'status'],
    queryFn: getPromptFilterStatus,
  })
  const rulesQuery = useQuery({
    queryKey: ['prompt-filter', 'rules'],
    queryFn: getPromptFilterRules,
  })

  useEffect(() => {
    const normalized = normalizeFormValues(
      buildFormDefaults(props.defaultValues)
    )
    const serialized = JSON.stringify(normalized)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = normalized
    baselineSerializedRef.current = serialized
    form.reset(buildFormDefaults(props.defaultValues))
  }, [props.defaultValues, form])

  const onSubmit = async (values: PromptFilterFormValues) => {
    const normalized = normalizeFormValues(values)
    const updates = Object.entries(normalized).filter(([key, value]) => {
      const baseline =
        baselineRef.current[key as keyof typeof baselineRef.current]
      return !valuesEqual(value, baseline)
    })

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const [key, value] of updates) {
      const serializedValue =
        Array.isArray(value) || typeof value === 'object'
          ? JSON.stringify(value)
          : value
      await updateOption.mutateAsync({ key, value: serializedValue })
    }

    baselineRef.current = normalized
    baselineSerializedRef.current = JSON.stringify(normalized)
    form.reset(buildFormDefaults(normalized))
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['prompt-filter'] }),
      queryClient.invalidateQueries({ queryKey: ['system-options'] }),
    ])
    toast.success(t('Prompt filter settings saved'))
  }

  return (
    <SettingsSection title={t('Prompt Filter')}>
      <SettingsPageFormActions
        onSave={form.handleSubmit(onSubmit)}
        isSaving={updateOption.isPending}
        saveLabel='Save Prompt Filter'
      />

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList className='grid w-full max-w-4xl grid-cols-5'>
          <TabsTrigger value='overview'>{t('Overview')}</TabsTrigger>
          <TabsTrigger value='logs'>{t('Trigger Logs')}</TabsTrigger>
          <TabsTrigger value='rules'>{t('Rule Set')}</TabsTrigger>
          <TabsTrigger value='custom'>自定义规则</TabsTrigger>
          <TabsTrigger value='lexicons'>词库文件</TabsTrigger>
        </TabsList>

        <TabsContent value='overview' className='space-y-4'>
          <OverviewPanel
            form={form}
            onSubmit={onSubmit}
            status={statusQuery.data?.data}
            builtinRuleCount={
              rulesQuery.data?.data.builtin_patterns.length ??
              statusQuery.data?.data.builtin_rule_count ??
              0
            }
          />
        </TabsContent>

        <TabsContent value='logs'>
          <PromptFilterLogsPanel />
        </TabsContent>

        <TabsContent value='rules'>
          <PromptFilterRulesPanel
            rules={rulesQuery.data?.data.builtin_patterns ?? []}
            disabledRules={rulesQuery.data?.data.disabled_patterns ?? []}
            disabled={updateOption.isPending}
            onSaveOption={(key, value) =>
              updateOption.mutateAsync({ key, value })
            }
          />
        </TabsContent>

        <TabsContent value='custom'>
          <PromptFilterCustomRulesPanel
            customRules={rulesQuery.data?.data.custom_patterns ?? []}
            disabled={updateOption.isPending}
            onSaveOption={(key, value) =>
              updateOption.mutateAsync({ key, value })
            }
          />
        </TabsContent>

        <TabsContent value='lexicons'>
          <PromptFilterLexiconsPanel disabled={updateOption.isPending} />
        </TabsContent>
      </Tabs>
    </SettingsSection>
  )
}

type OverviewPanelProps = {
  form: UseFormReturn<PromptFilterFormValues, unknown, PromptFilterFormValues>
  onSubmit: (values: PromptFilterFormValues) => Promise<void>
  status?: {
    enabled: boolean
    mode: PromptFilterMode
    log_total: number
    lexicon_word_count: number
    review_api_key_configured: boolean
  }
  builtinRuleCount: number
}

function OverviewPanel(props: OverviewPanelProps) {
  const { t } = useTranslation()
  const [testState, setTestState] = useState<PromptTestState>({
    endpoint: '/v1/responses',
    model: 'gpt-5.5',
    text: '',
  })
  const testMutation = useMutation({
    mutationFn: testPromptFilter,
    onSuccess: (data) => {
      if (data.success) {
        setTestState((current) => ({
          ...current,
          verdict: data.data.verdict,
        }))
      } else {
        toast.error(data.message || t('Prompt filter test failed'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Prompt filter test failed'))
    },
  })

  return (
    <>
      <div className='grid gap-3 md:grid-cols-5'>
        <StatusCard
          label={t('Status')}
          value={props.status?.enabled ? t('Enabled') : t('Disabled')}
        />
        <StatusCard
          label={t('Current Mode')}
          value={t(modeLabel(props.status?.mode ?? 'block'))}
        />
        <StatusCard
          label={t('Built-in Rules')}
          value={props.builtinRuleCount.toString()}
        />
        <StatusCard
          label='词库词条'
          value={(props.status?.lexicon_word_count ?? 0).toString()}
        />
        <StatusCard
          label={t('Log Count')}
          value={(props.status?.log_total ?? 0).toString()}
        />
      </div>

      <div className='grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(360px,0.9fr)]'>
        <Card>
          <CardHeader>
            <CardTitle>{t('System Rules')}</CardTitle>
            <CardDescription>
              {t(
                'Configure local prompt scoring before requests reach upstream channels.'
              )}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Form {...props.form}>
              <SettingsForm onSubmit={props.form.handleSubmit(props.onSubmit)}>
                <SettingsFormGrid>
                  <FormField
                    control={props.form.control}
                    name='CheckSensitiveEnabled'
                    render={({ field }) => (
                      <SettingsSwitchItem>
                        <SettingsSwitchContent>
                          <FormLabel>{t('Enable Prompt Check')}</FormLabel>
                          <FormDescription>
                            {t(
                              'Turn on the shared sensitive keyword and prompt rule scanner.'
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
                    control={props.form.control}
                    name='CheckSensitiveOnPromptEnabled'
                    render={({ field }) => (
                      <SettingsSwitchItem>
                        <SettingsSwitchContent>
                          <FormLabel>
                            {t('Inspect prompts before relay')}
                          </FormLabel>
                          <FormDescription>
                            {t(
                              'Requests are checked after curfew and usage gates, before the upstream channel call.'
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
                    control={props.form.control}
                    name='prompt_filter_setting.log_matches'
                    render={({ field }) => (
                      <SettingsSwitchItem>
                        <SettingsSwitchContent>
                          <FormLabel>{t('Record matched logs')}</FormLabel>
                          <FormDescription>
                            {t(
                              'Save redacted trigger records for monitor, warn, and block decisions.'
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
                    control={props.form.control}
                    name='prompt_filter_setting.mode'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Processing Mode')}</FormLabel>
                        <Select
                          value={field.value}
                          onValueChange={field.onChange}
                        >
                          <FormControl>
                            <SelectTrigger className='w-full'>
                              <SelectValue />
                            </SelectTrigger>
                          </FormControl>
                          <SelectContent alignItemWithTrigger={false}>
                            <SelectGroup>
                              <SelectItem value='block'>
                                {t('Block request')}
                              </SelectItem>
                              <SelectItem value='warn'>
                                {t('Warn only')}
                              </SelectItem>
                              <SelectItem value='monitor'>
                                {t('Monitor only')}
                              </SelectItem>
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={props.form.control}
                    name='prompt_filter_setting.threshold'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Block Threshold')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={1}
                            step={1}
                            {...safeNumberFieldProps(field)}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={props.form.control}
                    name='prompt_filter_setting.strict_threshold'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Strict Rule Threshold')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={1}
                            step={1}
                            {...safeNumberFieldProps(field)}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={props.form.control}
                    name='prompt_filter_setting.max_text_length'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Max Scanned Characters')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={1024}
                            max={1048576}
                            step={1024}
                            {...safeNumberFieldProps(field)}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <SettingsFormGridItem span='full'>
                    <FormField
                      control={props.form.control}
                      name='prompt_filter_setting.message'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Blocked Response Message')}</FormLabel>
                          <FormControl>
                            <Textarea rows={2} {...field} />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </SettingsFormGridItem>

                  <FormField
                    control={props.form.control}
                    name='prompt_filter_setting.block_status_code'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>
                          {t('Prompt Block HTTP Status Code')}
                        </FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={400}
                            max={499}
                            step={1}
                            {...safeNumberFieldProps(field)}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Use a dedicated 4xx code so clients and audits can identify prompt policy hits.'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={props.form.control}
                    name='prompt_filter_setting.block_error_code'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Prompt Block Error Code')}</FormLabel>
                        <FormControl>
                          <Input placeholder='prompt_blocked' {...field} />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Stable machine-readable code returned in response bodies and trigger logs.'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <SettingsFormGridItem span='full'>
                    <FormField
                      control={props.form.control}
                      name='prompt_filter_setting.group_whitelist'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Group Whitelist')}</FormLabel>
                          <FormControl>
                            <Input placeholder='admin, internal' {...field} />
                          </FormControl>
                          <FormDescription>
                            {t(
                              'Comma-separated groups that bypass prompt checks.'
                            )}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </SettingsFormGridItem>

                  <SettingsFormGridItem span='full'>
                    <FormField
                      control={props.form.control}
                      name='prompt_filter_setting.channel_whitelist'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Channel Whitelist')}</FormLabel>
                          <FormControl>
                            <Input placeholder='28, 12, 11' {...field} />
                          </FormControl>
                          <FormDescription>
                            {t(
                              'Comma-separated channel IDs that bypass prompt checks after routing.'
                            )}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </SettingsFormGridItem>

                  <SettingsFormGridItem span='full'>
                    <FormField
                      control={props.form.control}
                      name='SensitiveWords'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Sensitive Keywords')}</FormLabel>
                          <FormControl>
                            <Textarea
                              rows={5}
                              placeholder={t('Enter one keyword per line')}
                              {...field}
                            />
                          </FormControl>
                          <FormDescription>
                            临时少量关键词仍可在这里维护；大词库请上传文件。
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </SettingsFormGridItem>
                </SettingsFormGrid>

                <Separator />

                <div className='grid gap-4 lg:grid-cols-2'>
                  <FormField
                    control={props.form.control}
                    name='prompt_filter_setting.review_enabled'
                    render={({ field }) => (
                      <SettingsSwitchItem>
                        <SettingsSwitchContent>
                          <FormLabel>{t('Enable Secondary Review')}</FormLabel>
                          <FormDescription>
                            {t(
                              'Run a moderation-compatible API review after local rules match.'
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
                    control={props.form.control}
                    name='prompt_filter_setting.review_fail_closed'
                    render={({ field }) => (
                      <SettingsSwitchItem>
                        <SettingsSwitchContent>
                          <FormLabel>{t('Review fail-closed')}</FormLabel>
                          <FormDescription>
                            {t(
                              'If review fails, keep the request blocked instead of allowing it.'
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
                    control={props.form.control}
                    name='prompt_filter_setting.review_base_url'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Review Base URL')}</FormLabel>
                        <FormControl>
                          <Input {...field} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={props.form.control}
                    name='prompt_filter_setting.review_model'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Review Model')}</FormLabel>
                        <FormControl>
                          <Input {...field} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={props.form.control}
                    name='prompt_filter_setting.review_timeout_seconds'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Review Timeout Seconds')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={1}
                            max={60}
                            step={1}
                            {...safeNumberFieldProps(field)}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={props.form.control}
                    name='prompt_filter_setting.review_api_key'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Review API Key')}</FormLabel>
                        <FormControl>
                          <Input
                            type='password'
                            placeholder={
                              props.status?.review_api_key_configured
                                ? t('Configured; leave blank to keep unchanged')
                                : t('Paste API key')
                            }
                            {...field}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>
              </SettingsForm>
            </Form>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{t('Rule Test')}</CardTitle>
            <CardDescription>
              {t(
                'Run the current local and review policy against a sample prompt.'
              )}
            </CardDescription>
          </CardHeader>
          <CardContent className='space-y-3'>
            <div className='grid gap-3 sm:grid-cols-2'>
              <Input
                value={testState.endpoint}
                onChange={(event) =>
                  setTestState((current) => ({
                    ...current,
                    endpoint: event.target.value,
                  }))
                }
              />
              <Input
                value={testState.model}
                onChange={(event) =>
                  setTestState((current) => ({
                    ...current,
                    model: event.target.value,
                  }))
                }
              />
            </div>
            <Textarea
              rows={8}
              value={testState.text}
              placeholder={t('Enter a prompt to inspect')}
              onChange={(event) =>
                setTestState((current) => ({
                  ...current,
                  text: event.target.value,
                }))
              }
            />
            <Button
              type='button'
              onClick={() =>
                testMutation.mutate({
                  endpoint: testState.endpoint,
                  model: testState.model,
                  text: testState.text,
                })
              }
              disabled={testMutation.isPending || !testState.text.trim()}
            >
              <Wand2 data-icon='inline-start' />
              <span>{t('Run Test')}</span>
            </Button>
            {testState.verdict ? (
              <VerdictCard verdict={testState.verdict} />
            ) : (
              <div className='text-muted-foreground rounded-lg border border-dashed p-4 text-sm'>
                {t('No test result yet.')}
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </>
  )
}

type StatusCardProps = {
  label: string
  value: string
}

function StatusCard(props: StatusCardProps) {
  return (
    <Card size='sm'>
      <CardHeader className='gap-2'>
        <CardDescription>{props.label}</CardDescription>
        <CardTitle>{props.value}</CardTitle>
      </CardHeader>
    </Card>
  )
}

function VerdictCard(props: { verdict: PromptFilterVerdict }) {
  const { t } = useTranslation()
  const blocked = props.verdict.action === 'block'
  return (
    <Alert variant={blocked ? 'destructive' : 'default'}>
      {blocked ? <AlertCircle /> : <CheckCircle2 />}
      <AlertTitle>
        {t('Action')}: {t(actionLabel(props.verdict.action))}
      </AlertTitle>
      <AlertDescription className='space-y-2'>
        <div>
          {t('Score')}: {props.verdict.score} / {props.verdict.threshold}
          {props.verdict.strict_hit ? ` · ${t('Strict hit')}` : ''}
        </div>
        {props.verdict.reason ? <div>{props.verdict.reason}</div> : null}
        {props.verdict.text_preview ? (
          <pre className='bg-muted/50 overflow-x-auto rounded-md p-2 text-xs whitespace-pre-wrap'>
            {props.verdict.text_preview}
          </pre>
        ) : null}
        {props.verdict.matched.length > 0 ? (
          <div className='flex flex-wrap gap-1'>
            {props.verdict.matched.map((match) => (
              <Badge key={`${match.name}-${match.weight}`} variant='outline'>
                {match.name} · {match.weight}
              </Badge>
            ))}
          </div>
        ) : null}
      </AlertDescription>
    </Alert>
  )
}

function PromptFilterLogsPanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [filters, setFilters] = useState<LogsFilterState>({
    source: 'all',
    action: 'all',
    endpoint: '',
    model: '',
    apiKeyId: '',
    query: '',
  })
  const logsQuery = useQuery({
    queryKey: ['prompt-filter', 'logs', page, filters],
    queryFn: () =>
      getPromptFilterLogs({
        page,
        page_size: 50,
        source: filters.source === 'all' ? undefined : filters.source,
        action: filters.action === 'all' ? undefined : filters.action,
        endpoint: filters.endpoint || undefined,
        model: filters.model || undefined,
        api_key_id: filters.apiKeyId || undefined,
        q: filters.query || undefined,
      }),
  })
  const clearLogsMutation = useMutation({
    mutationFn: clearPromptFilterLogs,
    onSuccess: async (data) => {
      if (data.success) {
        await queryClient.invalidateQueries({ queryKey: ['prompt-filter'] })
        toast.success(t('Prompt filter logs cleared'))
      } else {
        toast.error(data.message || t('Failed to clear prompt filter logs'))
      }
    },
  })
  const rows = logsQuery.data?.data.items ?? []
  const total = logsQuery.data?.data.total ?? 0
  const canPrevious = page > 1
  const canNext = page * 50 < total

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Trigger Logs')}</CardTitle>
        <CardDescription>
          {t(
            'Filter local prompt check decisions and upstream policy records.'
          )}
        </CardDescription>
        <CardAction className='flex gap-2'>
          <Button
            variant='outline'
            size='sm'
            onClick={() => logsQuery.refetch()}
          >
            <RefreshCcw data-icon='inline-start' />
            <span>{t('Refresh')}</span>
          </Button>
          <Button
            variant='destructive'
            size='sm'
            onClick={() => clearLogsMutation.mutate()}
            disabled={clearLogsMutation.isPending || total === 0}
          >
            <Trash2 data-icon='inline-start' />
            <span>{t('Clear Logs')}</span>
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className='space-y-4'>
        <div className='grid gap-3 md:grid-cols-6'>
          <Select
            value={filters.action}
            onValueChange={(value) =>
              setFilters((current) => ({ ...current, action: value ?? 'all' }))
            }
          >
            <SelectTrigger className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectItem value='all'>{t('All actions')}</SelectItem>
              <SelectItem value='block'>{t('Block')}</SelectItem>
              <SelectItem value='warn'>{t('Warn')}</SelectItem>
              <SelectItem value='monitor'>{t('Monitor')}</SelectItem>
              <SelectItem value='allow'>{t('Allow')}</SelectItem>
            </SelectContent>
          </Select>
          <Select
            value={filters.source}
            onValueChange={(value) =>
              setFilters((current) => ({ ...current, source: value ?? 'all' }))
            }
          >
            <SelectTrigger className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectItem value='all'>{t('All sources')}</SelectItem>
              <SelectItem value='local_filter'>{t('Local Filter')}</SelectItem>
              <SelectItem value='upstream_cyber_policy'>
                {t('Upstream Policy')}
              </SelectItem>
            </SelectContent>
          </Select>
          <Input
            value={filters.endpoint}
            placeholder={t('Endpoint')}
            onChange={(event) =>
              setFilters((current) => ({
                ...current,
                endpoint: event.target.value,
              }))
            }
          />
          <Input
            value={filters.model}
            placeholder={t('Model')}
            onChange={(event) =>
              setFilters((current) => ({
                ...current,
                model: event.target.value,
              }))
            }
          />
          <Input
            value={filters.apiKeyId}
            placeholder={t('API Key ID')}
            onChange={(event) =>
              setFilters((current) => ({
                ...current,
                apiKeyId: event.target.value,
              }))
            }
          />
          <div className='flex gap-2'>
            <Input
              value={filters.query}
              placeholder={t('Search')}
              onChange={(event) =>
                setFilters((current) => ({
                  ...current,
                  query: event.target.value,
                }))
              }
            />
            <Button
              type='button'
              size='icon'
              onClick={() => {
                setPage(1)
                logsQuery.refetch()
              }}
            >
              <Search />
            </Button>
          </div>
        </div>

        {rows.length === 0 ? (
          <div className='grid min-h-44 place-items-center rounded-lg border border-dashed'>
            <div className='text-center'>
              <FileText className='text-muted-foreground mx-auto mb-2 size-6' />
              <div className='font-medium'>{t('No trigger records')}</div>
              <div className='text-muted-foreground text-sm'>
                {t('There is nothing to show for the current filters.')}
              </div>
            </div>
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Action')}</TableHead>
                <TableHead>{t('Source')}</TableHead>
                <TableHead>{t('Endpoint')}</TableHead>
                <TableHead>{t('Model')}</TableHead>
                <TableHead>{t('API Key')}</TableHead>
                <TableHead>{t('Score')}</TableHead>
                <TableHead className='whitespace-nowrap'>
                  {t('Status Code')}
                </TableHead>
                <TableHead className='whitespace-nowrap'>
                  {t('Error Code')}
                </TableHead>
                <TableHead>{t('Preview')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((log) => (
                <PromptFilterLogRow key={log.id} log={log} />
              ))}
            </TableBody>
          </Table>
        )}

        <div className='flex items-center justify-between gap-3'>
          <div className='text-muted-foreground text-sm'>
            {t('Total')}: {total}
          </div>
          <div className='flex gap-2'>
            <Button
              variant='outline'
              size='sm'
              disabled={!canPrevious}
              onClick={() => setPage((current) => current - 1)}
            >
              {t('Previous')}
            </Button>
            <Button
              variant='outline'
              size='sm'
              disabled={!canNext}
              onClick={() => setPage((current) => current + 1)}
            >
              {t('Next')}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function PromptFilterLogRow(props: { log: PromptFilterLog }) {
  const { t } = useTranslation()
  return (
    <TableRow>
      <TableCell>
        <Badge
          variant={props.log.action === 'block' ? 'destructive' : 'outline'}
        >
          {t(actionLabel(props.log.action))}
        </Badge>
      </TableCell>
      <TableCell>{t(sourceLabel(props.log.source))}</TableCell>
      <TableCell className='max-w-48 truncate'>{props.log.endpoint}</TableCell>
      <TableCell className='max-w-40 truncate'>
        {props.log.model || '-'}
      </TableCell>
      <TableCell>
        {props.log.api_key_id > 0 ? `#${props.log.api_key_id}` : '-'}
      </TableCell>
      <TableCell>
        {props.log.score}/{props.log.threshold}
      </TableCell>
      <TableCell className='font-mono text-xs whitespace-nowrap'>
        {props.log.status_code > 0 ? props.log.status_code : '-'}
      </TableCell>
      <TableCell className='max-w-44 truncate font-mono text-xs'>
        {props.log.error_code || '-'}
      </TableCell>
      <TableCell className='max-w-[34rem] whitespace-normal'>
        <div className='line-clamp-2'>{props.log.text_preview || '-'}</div>
        {props.log.matched.length > 0 ? (
          <div className='mt-1 flex flex-wrap gap-1'>
            {props.log.matched.slice(0, 3).map((match) => (
              <Badge key={match.name} variant='secondary'>
                {match.name}
              </Badge>
            ))}
          </div>
        ) : null}
      </TableCell>
    </TableRow>
  )
}

type PromptFilterRulesPanelProps = {
  rules: PromptFilterRule[]
  disabledRules: string[]
  disabled: boolean
  onSaveOption: (key: string, value: string) => Promise<unknown>
}

function PromptFilterRulesPanel(props: PromptFilterRulesPanelProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [category, setCategory] = useState('all')
  const [selected, setSelected] = useState<string[]>([])
  const categories = useMemo(() => uniqueCategories(props.rules), [props.rules])
  const filteredRules = useMemo(
    () =>
      props.rules
        .map((rule, index) => ({ rule, index }))
        .filter(
          ({ rule }) => category === 'all' || rule.category === category
        )
        .sort((left, right) => {
          if (left.rule.enabled !== right.rule.enabled) {
            return left.rule.enabled ? -1 : 1
          }
          return left.index - right.index
        })
        .map(({ rule }) => rule),
    [category, props.rules]
  )
  const filteredRuleNames = useMemo(
    () => new Set(filteredRules.map((rule) => rule.name)),
    [filteredRules]
  )
  const visibleSelected = selected.filter((name) => filteredRuleNames.has(name))

  const saveDisabledRules = async (nextDisabled: string[]) => {
    await props.onSaveOption(
      'prompt_filter_setting.disabled_patterns',
      JSON.stringify([...new Set(nextDisabled)])
    )
    await queryClient.invalidateQueries({ queryKey: ['prompt-filter'] })
  }

  const toggleRule = async (rule: PromptFilterRule) => {
    const disabledSet = new Set(props.disabledRules)
    if (disabledSet.has(rule.name)) {
      disabledSet.delete(rule.name)
    } else {
      disabledSet.add(rule.name)
    }
    await saveDisabledRules([...disabledSet])
  }

  const batchSetEnabled = async (enabled: boolean) => {
    const disabledSet = new Set(props.disabledRules)
    visibleSelected.forEach((name) => {
      if (enabled) disabledSet.delete(name)
      else disabledSet.add(name)
    })
    await saveDisabledRules([...disabledSet])
    setSelected((current) =>
      current.filter((name) => !filteredRuleNames.has(name))
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Built-in Rules')}</CardTitle>
        <CardDescription>
          {t(
            'These rules come from the local prompt policy bundle and can be toggled individually.'
          )}
        </CardDescription>
        <CardAction>
          <Button variant='outline' size='sm' onClick={() => setSelected([])}>
            <X data-icon='inline-start' />
            <span>{t('Clear Selection')}</span>
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className='space-y-4'>
        <div className='flex flex-wrap items-center gap-2'>
          <Select
            value={category}
            onValueChange={(value) => setCategory(value ?? 'all')}
          >
            <SelectTrigger className='w-56'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectItem value='all'>{t('All categories')}</SelectItem>
              {categories.map((item) => (
                <SelectItem key={item} value={item}>
                  {item}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            variant='outline'
            size='sm'
            disabled={visibleSelected.length === 0 || props.disabled}
            onClick={() => batchSetEnabled(true)}
          >
            <ShieldCheck data-icon='inline-start' />
            <span>{t('Batch Enable')}</span>
          </Button>
          <Button
            variant='outline'
            size='sm'
            disabled={visibleSelected.length === 0 || props.disabled}
            onClick={() => batchSetEnabled(false)}
          >
            <AlertCircle data-icon='inline-start' />
            <span>{t('Batch Disable')}</span>
          </Button>
          <span className='text-muted-foreground text-sm'>
            {t('Selected')}: {visibleSelected.length}
          </span>
        </div>

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className='w-9'>
                <Checkbox
                  checked={
                    filteredRules.length > 0 &&
                    visibleSelected.length === filteredRules.length
                  }
                  onCheckedChange={(checked) => {
                    setSelected(checked ? filteredRules.map((r) => r.name) : [])
                  }}
                />
              </TableHead>
              <TableHead>{t('Rule Name')}</TableHead>
              <TableHead>{t('Category')}</TableHead>
              <TableHead>{t('Weight')}</TableHead>
              <TableHead>{t('Regex')}</TableHead>
              <TableHead>{t('Operation')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filteredRules.map((rule) => (
              <TableRow key={rule.name}>
                <TableCell>
                  <Checkbox
                    checked={selected.includes(rule.name)}
                    onCheckedChange={(checked) => {
                      setSelected((current) =>
                        checked
                          ? [...new Set([...current, rule.name])]
                          : current.filter((name) => name !== rule.name)
                      )
                    }}
                  />
                </TableCell>
                <TableCell>
                  <div className='font-mono font-medium'>{rule.name}</div>
                  <div className='mt-1 flex gap-1'>
                    <Badge variant='secondary'>{t('Built-in')}</Badge>
                    {rule.strict ? (
                      <Badge variant='destructive'>{t('Strict Rule')}</Badge>
                    ) : null}
                    <Badge variant={rule.enabled ? 'default' : 'outline'}>
                      {rule.enabled ? t('Enabled') : t('Disabled')}
                    </Badge>
                  </div>
                </TableCell>
                <TableCell>{rule.category || '-'}</TableCell>
                <TableCell>{rule.weight}</TableCell>
                <TableCell className='max-w-[42rem] whitespace-normal'>
                  <div className='flex min-w-0 items-start gap-2'>
                    <code className='bg-muted/60 line-clamp-2 min-w-0 flex-1 rounded px-1.5 py-1 text-xs'>
                      {rule.pattern}
                    </code>
                    <Dialog
                      title='正则完整预览'
                      description='查看该 Prompt 检查规则的完整正则表达式。'
                      trigger={
                        <Button
                          type='button'
                          variant='ghost'
                          size='sm'
                          className='shrink-0'
                          aria-label={`查看 ${rule.name} 完整正则`}
                        >
                          <Eye data-icon='inline-start' />
                          <span>查看</span>
                        </Button>
                      }
                      contentClassName='sm:max-w-3xl'
                      bodyClassName='space-y-4'
                    >
                      <div className='flex flex-wrap items-center gap-2'>
                        <Badge variant='secondary'>{t('Built-in')}</Badge>
                        {rule.strict ? (
                          <Badge variant='destructive'>{t('Strict Rule')}</Badge>
                        ) : null}
                        <Badge variant={rule.enabled ? 'default' : 'outline'}>
                          {rule.enabled ? t('Enabled') : t('Disabled')}
                        </Badge>
                      </div>
                      <div className='grid gap-3 sm:grid-cols-3'>
                        <div>
                          <div className='text-muted-foreground text-xs'>
                            {t('Rule Name')}
                          </div>
                          <div className='font-mono text-sm font-medium break-all'>
                            {rule.name}
                          </div>
                        </div>
                        <div>
                          <div className='text-muted-foreground text-xs'>
                            {t('Category')}
                          </div>
                          <div className='text-sm'>{rule.category || '-'}</div>
                        </div>
                        <div>
                          <div className='text-muted-foreground text-xs'>
                            {t('Weight')}
                          </div>
                          <div className='text-sm'>{rule.weight}</div>
                        </div>
                      </div>
                      <Textarea
                        readOnly
                        value={rule.pattern}
                        className='min-h-52 resize-y font-mono text-xs'
                      />
                    </Dialog>
                  </div>
                </TableCell>
                <TableCell>
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={props.disabled}
                    onClick={() => toggleRule(rule)}
                  >
                    {rule.enabled ? t('Disable') : t('Enable')}
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>

      </CardContent>
    </Card>
  )
}

type PromptFilterCustomRulesPanelProps = {
  customRules: PromptFilterCustomRule[]
  disabled: boolean
  onSaveOption: (key: string, value: string) => Promise<unknown>
}

function PromptFilterCustomRulesPanel(props: PromptFilterCustomRulesPanelProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [customDraft, setCustomDraft] = useState<PromptFilterCustomRule>({
    name: '',
    pattern: '',
    weight: 50,
    category: 'custom',
    strict: false,
    enabled: true,
  })

  const saveCustomRules = async (nextRules: PromptFilterCustomRule[]) => {
    await props.onSaveOption(
      'prompt_filter_setting.custom_patterns',
      JSON.stringify(nextRules)
    )
    await queryClient.invalidateQueries({ queryKey: ['prompt-filter'] })
  }

  const addCustomRule = async () => {
    const nextRule = {
      ...customDraft,
      name: customDraft.name.trim(),
      pattern: customDraft.pattern.trim(),
      category: customDraft.category?.trim() || 'custom',
    }
    if (!nextRule.name || !nextRule.pattern || nextRule.weight <= 0) {
      toast.error(t('Custom rule name, pattern, and weight are required'))
      return
    }
    const nextRules = [
      ...props.customRules.filter((rule) => rule.name !== nextRule.name),
      nextRule,
    ]
    await saveCustomRules(nextRules)
    setCustomDraft({
      name: '',
      pattern: '',
      weight: 50,
      category: 'custom',
      strict: false,
      enabled: true,
    })
  }

  const deleteCustomRule = async (name: string) => {
    await saveCustomRules(
      props.customRules.filter((rule) => rule.name !== name)
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Custom Rules')}</CardTitle>
        <CardDescription>
          维护少量自定义正则规则，用于本地 Prompt 评分。
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className='grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(360px,0.8fr)]'>
          <div className='space-y-3'>
            {props.customRules.length === 0 ? (
              <div className='text-muted-foreground rounded-lg border border-dashed p-4 text-sm'>
                {t('No custom rules yet.')}
              </div>
            ) : (
              <div className='space-y-2'>
                {props.customRules.map((rule) => (
                  <div
                    key={rule.name}
                    className='flex items-start justify-between gap-3 rounded-lg border p-3'
                  >
                    <div className='min-w-0 space-y-1'>
                      <div className='font-mono text-sm font-medium'>
                        {rule.name}
                      </div>
                      <div className='text-muted-foreground text-xs'>
                        {rule.category || 'custom'} · {rule.weight}
                      </div>
                      <code className='bg-muted/60 line-clamp-2 rounded px-1.5 py-1 text-xs'>
                        {rule.pattern}
                      </code>
                    </div>
                    <Button
                      variant='ghost'
                      size='icon-sm'
                      onClick={() => deleteCustomRule(rule.name)}
                    >
                      <Trash2 />
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className='space-y-3 rounded-lg border p-3'>
            <h3 className='text-sm font-semibold'>{t('Add Custom Rule')}</h3>
            <Input
              value={customDraft.name}
              placeholder={t('Rule Name')}
              onChange={(event) =>
                setCustomDraft((current) => ({
                  ...current,
                  name: event.target.value,
                }))
              }
            />
            <div className='grid gap-2 sm:grid-cols-2'>
              <Input
                value={customDraft.category ?? ''}
                placeholder={t('Category')}
                onChange={(event) =>
                  setCustomDraft((current) => ({
                    ...current,
                    category: event.target.value,
                  }))
                }
              />
              <Input
                type='number'
                min={1}
                value={customDraft.weight}
                onChange={(event) =>
                  setCustomDraft((current) => ({
                    ...current,
                    weight: Number(event.target.value),
                  }))
                }
              />
            </div>
            <Textarea
              rows={4}
              value={customDraft.pattern}
              placeholder='(?i)forbidden phrase'
              onChange={(event) =>
                setCustomDraft((current) => ({
                  ...current,
                  pattern: event.target.value,
                }))
              }
            />
            <div className='flex items-center justify-between gap-3'>
              <label className='flex items-center gap-2 text-sm'>
                <Checkbox
                  checked={customDraft.strict}
                  onCheckedChange={(checked) =>
                    setCustomDraft((current) => ({
                      ...current,
                      strict: Boolean(checked),
                    }))
                  }
                />
                {t('Strict Rule')}
              </label>
              <label className='flex items-center gap-2 text-sm'>
                <Switch
                  checked={customDraft.enabled !== false}
                  onCheckedChange={(checked) =>
                    setCustomDraft((current) => ({
                      ...current,
                      enabled: checked,
                    }))
                  }
                />
                {t('Enabled')}
              </label>
            </div>
            <Button
              type='button'
              onClick={addCustomRule}
              disabled={props.disabled}
            >
              <Plus data-icon='inline-start' />
              <span>{t('Add Rule')}</span>
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function PromptFilterLexiconsPanel(props: { disabled: boolean }) {
  const queryClient = useQueryClient()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [selectedFiles, setSelectedFiles] = useState<File[]>([])
  const [category, setCategory] = useState('uploaded')
  const [weight, setWeight] = useState(100)
  const [strict, setStrict] = useState(true)
  const [enabled, setEnabled] = useState(true)
  const [name, setName] = useState('')

  const lexiconsQuery = useQuery({
    queryKey: ['prompt-filter', 'lexicons'],
    queryFn: getPromptFilterLexicons,
  })

  const refreshPromptFilterQueries = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['prompt-filter'] }),
      queryClient.invalidateQueries({ queryKey: ['system-options'] }),
    ])
  }

  const uploadMutation = useMutation({
    mutationFn: async () => {
      if (selectedFiles.length === 0) {
        throw new Error('请选择词库文件')
      }
      for (const file of selectedFiles) {
        const formData = new FormData()
        formData.append('file', file)
        if (selectedFiles.length === 1 && name.trim()) {
          formData.append('name', name.trim())
        }
        formData.append('category', category.trim())
        formData.append('weight', String(weight))
        formData.append('strict', String(strict))
        formData.append('enabled', String(enabled))
        const response = await uploadPromptFilterLexicon(formData)
        if (!response.success) {
          throw new Error(response.message || '词库上传失败')
        }
      }
    },
    onSuccess: async () => {
      await refreshPromptFilterQueries()
      setSelectedFiles([])
      setName('')
      if (fileInputRef.current) {
        fileInputRef.current.value = ''
      }
      toast.success('词库文件已上传')
    },
    onError: (error: Error) => {
      toast.error(error.message || '词库上传失败')
    },
  })

  const toggleMutation = useMutation({
    mutationFn: updatePromptFilterLexicon,
    onSuccess: async (data) => {
      if (!data.success) {
        toast.error(data.message || '词库状态更新失败')
        return
      }
      await refreshPromptFilterQueries()
      toast.success('词库状态已更新')
    },
    onError: (error: Error) => {
      toast.error(error.message || '词库状态更新失败')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: deletePromptFilterLexicon,
    onSuccess: async (data) => {
      if (!data.success) {
        toast.error(data.message || '词库删除失败')
        return
      }
      await refreshPromptFilterQueries()
      toast.success('词库文件已删除')
    },
    onError: (error: Error) => {
      toast.error(error.message || '词库删除失败')
    },
  })

  const [preview, setPreview] =
    useState<PromptFilterLexiconPreviewData | null>(null)
  const [previewMode, setPreviewMode] = useState<'preview' | 'edit'>('preview')
  const [editorText, setEditorText] = useState('')

  const previewMutation = useMutation({
    mutationFn: async (request: {
      file: PromptFilterLexiconFile
      mode: 'preview' | 'edit'
    }) => {
      const response = await previewPromptFilterLexicon({
        id: request.file.id,
        limit:
          request.mode === 'edit' ? lexiconEditLimit : lexiconPreviewLimit,
      })
      if (!response.success) {
        throw new Error(response.message || '词库预览失败')
      }
      return { data: response.data, mode: request.mode }
    },
    onSuccess: (result) => {
      setPreview(result.data)
      setPreviewMode(result.mode)
      setEditorText(result.data.words.join('\n'))
    },
    onError: (error: Error) => {
      toast.error(error.message || '词库预览失败')
    },
  })

  const saveWordsMutation = useMutation({
    mutationFn: async () => {
      if (!preview) {
        throw new Error('请先选择词库')
      }
      const words = editorText
        .split(/\r?\n/)
        .map((word) => word.trim())
        .filter(Boolean)
      const response = await savePromptFilterLexiconWords({
        id: preview.file.id,
        words,
      })
      if (!response.success) {
        throw new Error(response.message || '词库保存失败')
      }
      return response.data.file
    },
    onSuccess: async (file) => {
      await refreshPromptFilterQueries()
      const response = await previewPromptFilterLexicon({
        id: file.id,
        limit: previewMode === 'edit' ? lexiconEditLimit : lexiconPreviewLimit,
      })
      if (response.success) {
        setPreview(response.data)
        setEditorText(response.data.words.join('\n'))
      }
      toast.success('词库词条已保存')
    },
    onError: (error: Error) => {
      toast.error(error.message || '词库保存失败')
    },
  })

  const rows = lexiconsQuery.data?.data.items ?? []
  const busy =
    props.disabled ||
    uploadMutation.isPending ||
    toggleMutation.isPending ||
    deleteMutation.isPending ||
    previewMutation.isPending ||
    saveWordsMutation.isPending

  return (
    <Card>
      <CardHeader>
        <CardTitle>词库文件</CardTitle>
        <CardDescription>
          上传 txt 或 JSON 词库文件；txt 按一行一个词解析，JSON 兼容 words 数组。
        </CardDescription>
        <CardAction>
          <Button
            variant='outline'
            size='sm'
            onClick={() => lexiconsQuery.refetch()}
            disabled={lexiconsQuery.isFetching}
          >
            <RefreshCcw data-icon='inline-start' />
            <span>刷新</span>
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className='space-y-4'>
        <div className='grid gap-3 lg:grid-cols-[minmax(0,1fr)_12rem_8rem_9rem_9rem_auto]'>
          <div className='space-y-2'>
            <Input
              ref={fileInputRef}
              type='file'
              multiple
              accept='.txt,.json,text/plain,application/json'
              disabled={busy}
              onChange={(event) =>
                setSelectedFiles([...(event.target.files ?? [])])
              }
            />
            <div className='text-muted-foreground text-xs'>
              {selectedLexiconFileLabel(selectedFiles)}
            </div>
          </div>
          <Input
            value={category}
            placeholder='分类'
            disabled={busy}
            onChange={(event) => setCategory(event.target.value)}
          />
          <Input
            type='number'
            min={1}
            step={1}
            value={weight}
            disabled={busy}
            onChange={(event) => setWeight(Number(event.target.value))}
          />
          <label className='flex items-center gap-2 rounded-md border px-3 py-2 text-sm'>
            <Switch checked={strict} disabled={busy} onCheckedChange={setStrict} />
            强规则
          </label>
          <label className='flex items-center gap-2 rounded-md border px-3 py-2 text-sm'>
            <Switch
              checked={enabled}
              disabled={busy}
              onCheckedChange={setEnabled}
            />
            上传后启用
          </label>
          <Button
            type='button'
            disabled={busy || selectedFiles.length === 0 || weight <= 0}
            onClick={() => uploadMutation.mutate()}
          >
            <Upload data-icon='inline-start' />
            <span>上传</span>
          </Button>
        </div>

        {selectedFiles.length === 1 ? (
          <Input
            value={name}
            placeholder='可选：自定义词库名称'
            disabled={busy}
            onChange={(event) => setName(event.target.value)}
          />
        ) : null}

        {previewMutation.isPending && previewMutation.variables ? (
          <div className='text-muted-foreground rounded-lg border p-4 text-sm'>
            正在加载「{previewMutation.variables.file.name}」词库内容...
          </div>
        ) : null}

        {preview ? (
          <div className='space-y-3 rounded-lg border p-4'>
            <div className='flex flex-wrap items-start justify-between gap-3'>
              <div>
                <div className='flex flex-wrap items-center gap-2'>
                  <h4 className='font-medium'>{preview.file.name}</h4>
                  <Badge variant='outline'>
                    {promptFilterLexiconSourceLabel(preview.file.source)}
                  </Badge>
                  <Badge variant={preview.file.enabled ? 'default' : 'outline'}>
                    {preview.file.enabled ? '已启用' : '已禁用'}
                  </Badge>
                </div>
                <p className='text-muted-foreground mt-1 text-sm'>
                  {preview.truncated
                    ? `当前显示前 ${preview.words.length} 条，共 ${preview.total} 条。`
                    : `共 ${preview.total} 条词。`}
                </p>
              </div>
              <div className='flex flex-wrap gap-2'>
                {previewMode === 'preview' ? (
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    disabled={busy}
                    onClick={() =>
                      previewMutation.mutate({
                        file: preview.file,
                        mode: 'edit',
                      })
                    }
                  >
                    <Pencil data-icon='inline-start' />
                    <span>编辑全部</span>
                  </Button>
                ) : (
                  <Button
                    type='button'
                    size='sm'
                    disabled={busy}
                    onClick={() => saveWordsMutation.mutate()}
                  >
                    <Save data-icon='inline-start' />
                    <span>保存词条</span>
                  </Button>
                )}
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  disabled={busy}
                  onClick={() => {
                    setPreview(null)
                    setEditorText('')
                  }}
                >
                  关闭
                </Button>
              </div>
            </div>
            <Textarea
              className='min-h-56 font-mono text-xs'
              value={editorText}
              readOnly={previewMode !== 'edit'}
              onChange={(event) => setEditorText(event.target.value)}
            />
          </div>
        ) : null}

        {rows.length === 0 ? (
          <div className='grid min-h-32 place-items-center rounded-lg border border-dashed'>
            <div className='text-center'>
              <FileText className='text-muted-foreground mx-auto mb-2 size-6' />
              <div className='font-medium'>暂无词库文件</div>
              <div className='text-muted-foreground text-sm'>
                上传后会参与 Prompt 检查，不需要每次请求读取文件。
              </div>
            </div>
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>词库</TableHead>
                <TableHead>分类</TableHead>
                <TableHead>词条</TableHead>
                <TableHead>大小</TableHead>
                <TableHead>权重</TableHead>
                <TableHead>更新时间</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((file) => (
                <PromptFilterLexiconRow
                  key={file.id}
                  file={file}
                  disabled={busy}
                  onToggle={(nextEnabled) =>
                    toggleMutation.mutate({
                      id: file.id,
                      enabled: nextEnabled,
                    })
                  }
                  onPreview={() =>
                    previewMutation.mutate({ file, mode: 'preview' })
                  }
                  onEdit={() => previewMutation.mutate({ file, mode: 'edit' })}
                  onDelete={() => {
                    const actionLabel =
                      file.source === 'preset' ? '重置预设词库' : '删除词库'
                    if (window.confirm(`确认${actionLabel}「${file.name}」吗？`)) {
                      deleteMutation.mutate(file.id)
                    }
                  }}
                />
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function PromptFilterLexiconRow(props: {
  file: PromptFilterLexiconFile
  disabled: boolean
  onToggle: (enabled: boolean) => void
  onPreview: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const canResetOrDelete =
    props.file.source !== 'preset' || props.file.uploaded_at > 0
  return (
    <TableRow>
      <TableCell>
        <div className='flex flex-wrap items-center gap-2'>
          <span className='font-medium'>{props.file.name}</span>
          <Badge variant='outline'>
            {promptFilterLexiconSourceLabel(props.file.source)}
          </Badge>
        </div>
        <div className='text-muted-foreground max-w-64 truncate text-xs'>
          {props.file.original_name}
        </div>
        <div className='text-muted-foreground mt-1 max-w-64 truncate font-mono text-xs'>
          {props.file.sha256.slice(0, 16)}
        </div>
      </TableCell>
      <TableCell>{props.file.category || '-'}</TableCell>
      <TableCell>{props.file.word_count}</TableCell>
      <TableCell>{formatFileSize(props.file.size)}</TableCell>
      <TableCell>
        <div>{props.file.weight}</div>
        {props.file.strict ? (
          <Badge variant='destructive'>强规则</Badge>
        ) : (
          <Badge variant='outline'>普通</Badge>
        )}
      </TableCell>
      <TableCell className='whitespace-nowrap'>
        {formatUploadedAt(props.file.uploaded_at)}
      </TableCell>
      <TableCell>
        <Badge variant={props.file.enabled ? 'default' : 'outline'}>
          {props.file.enabled ? '开启' : '关闭'}
        </Badge>
      </TableCell>
      <TableCell>
        <div className='flex flex-wrap gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={props.disabled}
            aria-label={`预览 ${props.file.name}`}
            onClick={props.onPreview}
          >
            <Eye data-icon='inline-start' />
            <span>查看</span>
          </Button>
          <Button
            variant='outline'
            size='sm'
            disabled={props.disabled}
            aria-label={`编辑 ${props.file.name}`}
            onClick={props.onEdit}
          >
            <Pencil data-icon='inline-start' />
            <span>修改</span>
          </Button>
          <Button
            variant='outline'
            size='sm'
            disabled={props.disabled}
            onClick={() => props.onToggle(!props.file.enabled)}
          >
            {props.file.enabled ? '关闭' : '开启'}
          </Button>
          {canResetOrDelete ? (
            <Button
              variant='ghost'
              size='icon-sm'
              disabled={props.disabled}
              aria-label={
                props.file.source === 'preset'
                  ? `重置 ${props.file.name}`
                  : `删除 ${props.file.name}`
              }
              onClick={props.onDelete}
            >
              {props.file.source === 'preset' ? <RefreshCcw /> : <Trash2 />}
            </Button>
          ) : null}
        </div>
      </TableCell>
    </TableRow>
  )
}

const modeLabel = (mode: string) => {
  switch (mode) {
    case 'warn':
      return 'Warn'
    case 'monitor':
      return 'Monitor'
    default:
      return 'Block'
  }
}

const actionLabel = (action: string) => {
  switch (action) {
    case 'warn':
      return 'Warn'
    case 'monitor':
      return 'Monitor'
    case 'allow':
      return 'Allow'
    default:
      return 'Block'
  }
}

const sourceLabel = (source: string) => {
  switch (source) {
    case 'upstream_cyber_policy':
      return 'Upstream Policy'
    case 'local_filter':
      return 'Local Filter'
    default:
      return source || '-'
  }
}
