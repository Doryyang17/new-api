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
import { Copy, Loader2, Plus, RefreshCw } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { CopyButton } from '@/components/copy-button'
import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Button } from '@/components/ui/button'
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
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { TooltipProvider } from '@/components/ui/tooltip'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { createRegistrationCodes, getRegistrationCodes } from '../api'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import type { RegistrationCode } from '../types'
import { safeNumberFieldProps } from '../utils/numeric-field'

type RegistrationCodeStatusTab = 'unused' | 'used'

type RegistrationCodeSettingsValues = {
  RegistrationCodeRegisterEnabled: boolean
}

const registrationCodeSettingsSchema = z.object({
  RegistrationCodeRegisterEnabled: z.boolean(),
})

const createRegistrationCodeGenerateSchema = (t: (key: string) => string) =>
  z.object({
    count: z
      .number()
      .int()
      .min(1, t('Count must be between 1 and 500'))
      .max(500, t('Count must be between 1 and 500')),
    note: z.string().max(255).optional(),
  })

type RegistrationCodeGenerateValues = z.infer<
  ReturnType<typeof createRegistrationCodeGenerateSchema>
>

type RegistrationCodesSectionProps = {
  defaultValues: RegistrationCodeSettingsValues
}

const REGISTRATION_CODE_PAGE_SIZE = 10
const REGISTRATION_CODE_EXPORT_PAGE_SIZE = 100

function CodeCell({ code }: { code: string }) {
  return (
    <div className='flex min-w-0 items-center gap-1.5'>
      <span className='min-w-0 truncate font-mono text-xs tracking-normal'>
        {code}
      </span>
      <CopyButton
        value={code}
        size='icon'
        className='h-7 w-7'
        iconClassName='h-3.5 w-3.5'
      />
    </div>
  )
}

function MutedCell({ children }: { children: React.ReactNode }) {
  return <span className='text-muted-foreground text-sm'>{children}</span>
}

export function RegistrationCodesSection({
  defaultValues,
}: RegistrationCodesSectionProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const updateOption = useUpdateOption()
  const { copyToClipboard } = useCopyToClipboard({ notify: false })
  const [activeTab, setActiveTab] =
    useState<RegistrationCodeStatusTab>('unused')
  const [page, setPage] = useState(1)
  const [generatedCodes, setGeneratedCodes] = useState<string[]>([])
  const [isCopyingAllCodes, setIsCopyingAllCodes] = useState(false)

  const settingsForm = useForm<RegistrationCodeSettingsValues>({
    resolver: zodResolver(registrationCodeSettingsSchema),
    defaultValues,
  })
  useResetForm(settingsForm, defaultValues)

  const generateSchema = useMemo(
    () => createRegistrationCodeGenerateSchema(t),
    [t]
  )
  const generateForm = useForm<RegistrationCodeGenerateValues>({
    resolver: zodResolver(generateSchema),
    defaultValues: {
      count: 20,
      note: '',
    },
  })

  const codeQuery = useQuery({
    queryKey: [
      'registration-codes',
      activeTab,
      page,
      REGISTRATION_CODE_PAGE_SIZE,
    ],
    queryFn: async () => {
      const response = await getRegistrationCodes(
        activeTab,
        page,
        REGISTRATION_CODE_PAGE_SIZE
      )
      if (!response.success) {
        throw new Error(
          response.message || t('Failed to load registration codes')
        )
      }
      return (
        response.data ?? {
          page,
          page_size: REGISTRATION_CODE_PAGE_SIZE,
          total: 0,
          items: [],
        }
      )
    },
  })

  const codeSummaryQuery = useQuery({
    queryKey: ['registration-codes', 'summary'],
    queryFn: async () => {
      const [unusedResponse, usedResponse] = await Promise.all([
        getRegistrationCodes('unused', 1, 1),
        getRegistrationCodes('used', 1, 1),
      ])
      if (!unusedResponse.success) {
        throw new Error(
          unusedResponse.message || t('Failed to load registration codes')
        )
      }
      if (!usedResponse.success) {
        throw new Error(
          usedResponse.message || t('Failed to load registration codes')
        )
      }
      return {
        unused: unusedResponse.data?.total ?? 0,
        used: usedResponse.data?.total ?? 0,
      }
    },
  })

  const generateMutation = useMutation({
    mutationFn: (values: RegistrationCodeGenerateValues) =>
      createRegistrationCodes({
        count: values.count,
        note: values.note?.trim() || undefined,
      }),
    onSuccess: (response) => {
      if (!response.success) {
        toast.error(
          response.message || t('Failed to generate registration codes')
        )
        return
      }
      const codes = response.data ?? []
      setGeneratedCodes(codes)
      setActiveTab('unused')
      setPage(1)
      queryClient.invalidateQueries({ queryKey: ['registration-codes'] })
      toast.success(t('Registration codes generated'))
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to generate registration codes'))
    },
  })

  const onSettingsSubmit = async (values: RegistrationCodeSettingsValues) => {
    if (
      values.RegistrationCodeRegisterEnabled ===
      defaultValues.RegistrationCodeRegisterEnabled
    ) {
      toast.info(t('No changes to save'))
      return
    }
    await updateOption.mutateAsync({
      key: 'RegistrationCodeRegisterEnabled',
      value: values.RegistrationCodeRegisterEnabled,
    })
  }

  const onGenerateSubmit = (values: RegistrationCodeGenerateValues) => {
    generateMutation.mutate(values)
  }

  const handleCopyAllCodes = async () => {
    setIsCopyingAllCodes(true)
    try {
      const firstResponse = await getRegistrationCodes(
        activeTab,
        1,
        REGISTRATION_CODE_EXPORT_PAGE_SIZE
      )
      if (!firstResponse.success) {
        throw new Error(
          firstResponse.message || t('Failed to copy registration codes')
        )
      }

      const firstPage = firstResponse.data ?? {
        page: 1,
        page_size: REGISTRATION_CODE_EXPORT_PAGE_SIZE,
        total: 0,
        items: [],
      }
      const codes = firstPage.items.map((item) => item.code)
      const totalPages = Math.ceil(
        firstPage.total / REGISTRATION_CODE_EXPORT_PAGE_SIZE
      )

      for (let nextPage = 2; nextPage <= totalPages; nextPage += 1) {
        const response = await getRegistrationCodes(
          activeTab,
          nextPage,
          REGISTRATION_CODE_EXPORT_PAGE_SIZE
        )
        if (!response.success) {
          throw new Error(
            response.message || t('Failed to copy registration codes')
          )
        }
        codes.push(...(response.data?.items ?? []).map((item) => item.code))
      }

      if (codes.length === 0) {
        toast.info(t('No registration codes to copy'))
        return
      }

      const copied = await copyToClipboard(codes.join('\n'))
      if (copied) {
        toast.success(t('All registration codes copied'))
      }
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to copy registration codes')
      )
    } finally {
      setIsCopyingAllCodes(false)
    }
  }

  const handleRefreshCodes = () => {
    void codeQuery.refetch()
    void codeSummaryQuery.refetch()
  }

  const tableData = codeQuery.data
  const totalPages = Math.max(
    1,
    Math.ceil((tableData?.total ?? 0) / REGISTRATION_CODE_PAGE_SIZE)
  )
  const summaryCounts = codeSummaryQuery.data
  const isRefreshingCodes = codeQuery.isFetching || codeSummaryQuery.isFetching
  const activeTotal =
    tableData?.total ??
    (activeTab === 'unused' ? summaryCounts?.unused : summaryCounts?.used) ??
    0
  const copyAllLabel =
    activeTab === 'used' ? t('Copy all used codes') : t('Copy all unused codes')
  const formatSummaryCount = (count: number | undefined) => {
    if (typeof count !== 'number') {
      return '-'
    }
    return count.toLocaleString()
  }

  const columns = useMemo<StaticDataTableColumn<RegistrationCode>[]>(() => {
    const baseColumns: StaticDataTableColumn<RegistrationCode>[] = [
      {
        id: 'code',
        header: t('Registration code'),
        className: 'min-w-[15rem]',
        cell: (row) => <CodeCell code={row.code} />,
      },
      {
        id: 'batch',
        header: t('Batch'),
        className: 'min-w-[9rem]',
        cell: (row) => <MutedCell>{row.batch_id || '-'}</MutedCell>,
      },
      {
        id: 'created_at',
        header: t('Created at'),
        className: 'min-w-[10rem]',
        cell: (row) => (
          <MutedCell>{formatTimestampToDate(row.created_at)}</MutedCell>
        ),
      },
      {
        id: 'note',
        header: t('Internal note'),
        className: 'min-w-[10rem]',
        cell: (row) => <MutedCell>{row.note || '-'}</MutedCell>,
      },
    ]

    if (activeTab !== 'used') {
      return baseColumns
    }

    return [
      baseColumns[0],
      {
        id: 'used_by',
        header: t('Used by'),
        className: 'min-w-[12rem]',
        cell: (row) => (
          <div className='flex min-w-0 flex-col gap-0.5'>
            <span className='text-sm font-medium'>
              #{row.used_user_id || '-'}
            </span>
            <span className='text-muted-foreground truncate text-xs'>
              {row.used_username || '-'}
            </span>
          </div>
        ),
      },
      {
        id: 'used_at',
        header: t('Used at'),
        className: 'min-w-[10rem]',
        cell: (row) => (
          <MutedCell>{formatTimestampToDate(row.used_at)}</MutedCell>
        ),
      },
      ...baseColumns.slice(1),
    ]
  }, [activeTab, t])

  return (
    <TooltipProvider>
      <SettingsSection title={t('Registration Codes')}>
        <Form {...settingsForm}>
          <SettingsForm
            onSubmit={settingsForm.handleSubmit(onSettingsSubmit)}
            className='gap-y-4'
          >
            <SettingsPageFormActions
              onSave={settingsForm.handleSubmit(onSettingsSubmit)}
              isSaving={updateOption.isPending}
              isSaveDisabled={!settingsForm.formState.isDirty}
            />
            <FormField
              control={settingsForm.control}
              name='RegistrationCodeRegisterEnabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Require registration code')}</FormLabel>
                    <FormDescription>
                      {t(
                        'When enabled, new users must enter an unused registration code before creating an account.'
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
          </SettingsForm>
        </Form>

        <div className='border-border/70 grid gap-4 border-t pt-4'>
          <Form {...generateForm}>
            <form
              onSubmit={generateForm.handleSubmit(onGenerateSubmit)}
              className='grid gap-4 lg:grid-cols-[10rem_minmax(0,1fr)_auto]'
            >
              <FormField
                control={generateForm.control}
                name='count'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Batch size')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        max={500}
                        step={1}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={generateForm.control}
                name='note'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Internal note')}</FormLabel>
                    <FormControl>
                      <Input
                        maxLength={255}
                        placeholder={t('Optional')}
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <div className='flex items-end'>
                <Button
                  type='submit'
                  disabled={generateMutation.isPending}
                  className='w-full gap-2 lg:w-auto'
                >
                  {generateMutation.isPending ? (
                    <Loader2
                      data-icon='inline-start'
                      className='animate-spin'
                    />
                  ) : (
                    <Plus data-icon='inline-start' />
                  )}
                  {t('Generate codes')}
                </Button>
              </div>
            </form>
          </Form>

          {generatedCodes.length > 0 && (
            <div className='grid gap-2'>
              <div className='flex items-center justify-between gap-2'>
                <div className='text-sm font-medium'>
                  {t('Generated codes')}
                </div>
                <CopyButton
                  value={generatedCodes.join('\n')}
                  variant='outline'
                  size='sm'
                  tooltip={t('Copy generated codes')}
                  successTooltip={t('Codes copied')}
                  className='gap-2'
                >
                  <span>{t('Copy')}</span>
                </CopyButton>
              </div>
              <Textarea
                readOnly
                value={generatedCodes.join('\n')}
                className='h-36 resize-y font-mono text-xs'
              />
            </div>
          )}
        </div>

        <div className='border-border/70 grid gap-3 border-t pt-4'>
          <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
            <Tabs
              value={activeTab}
              onValueChange={(value) => {
                setActiveTab(value as RegistrationCodeStatusTab)
                setPage(1)
              }}
            >
              <TabsList className='grid w-full grid-cols-2 sm:w-fit'>
                <TabsTrigger value='unused' className='gap-2'>
                  <span>{t('Unused codes')}</span>
                  <span className='bg-background/80 text-muted-foreground rounded px-1.5 py-0.5 text-[11px] leading-none font-medium tabular-nums'>
                    {formatSummaryCount(summaryCounts?.unused)}
                  </span>
                </TabsTrigger>
                <TabsTrigger value='used' className='gap-2'>
                  <span>{t('Used codes')}</span>
                  <span className='bg-background/80 text-muted-foreground rounded px-1.5 py-0.5 text-[11px] leading-none font-medium tabular-nums'>
                    {formatSummaryCount(summaryCounts?.used)}
                  </span>
                </TabsTrigger>
              </TabsList>
            </Tabs>
            <div className='flex flex-col gap-2 sm:flex-row sm:items-center'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={handleCopyAllCodes}
                disabled={
                  codeQuery.isLoading || isCopyingAllCodes || activeTotal === 0
                }
                className='w-full gap-2 sm:w-auto'
              >
                {isCopyingAllCodes ? (
                  <Loader2 data-icon='inline-start' className='animate-spin' />
                ) : (
                  <Copy data-icon='inline-start' />
                )}
                {isCopyingAllCodes ? t('Copying...') : copyAllLabel}
              </Button>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={handleRefreshCodes}
                disabled={isRefreshingCodes}
                className='w-full gap-2 sm:w-auto'
              >
                <RefreshCw
                  data-icon='inline-start'
                  className={cn(isRefreshingCodes && 'animate-spin')}
                />
                {t('Refresh')}
              </Button>
            </div>
          </div>

          {codeQuery.isError ? (
            <div className='text-destructive rounded-md border px-3 py-2 text-sm'>
              {codeQuery.error instanceof Error
                ? codeQuery.error.message
                : t('Failed to load registration codes')}
            </div>
          ) : (
            <StaticDataTable
              columns={columns}
              data={tableData?.items ?? []}
              emptyContent={
                activeTab === 'used'
                  ? t('No used registration codes.')
                  : t('No unused registration codes.')
              }
              tableClassName='min-w-[46rem]'
            />
          )}

          <div className='flex flex-col gap-2 text-sm sm:flex-row sm:items-center sm:justify-between'>
            <span className='text-muted-foreground'>
              {codeQuery.isLoading
                ? t('Loading registration codes...')
                : t(
                    'Page {{page}} of {{totalPages}} - {{count}} total - {{pageSize}} per page',
                    {
                      count: activeTotal,
                      page,
                      pageSize: REGISTRATION_CODE_PAGE_SIZE,
                      totalPages,
                    }
                  )}
            </span>
            <div className='flex items-center gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => setPage((current) => Math.max(1, current - 1))}
                disabled={page <= 1 || codeQuery.isFetching}
              >
                {t('Previous page')}
              </Button>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() =>
                  setPage((current) => Math.min(totalPages, current + 1))
                }
                disabled={page >= totalPages || codeQuery.isFetching}
              >
                {t('Next page')}
              </Button>
            </div>
          </div>
        </div>
      </SettingsSection>
    </TooltipProvider>
  )
}
