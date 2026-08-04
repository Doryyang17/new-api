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
import { Add01Icon, InformationCircleIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useEffect, useState } from 'react'
import {
  type Resolver,
  useFieldArray,
  useForm,
  useWatch,
} from 'react-hook-form'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormLabel,
} from '@/components/ui/form'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import {
  useUpdateUserLevelAdminConfig,
  useUserLevelAdminConfig,
  type UserLevelAdminData,
  type UserLevelConfig,
} from '@/features/user-levels'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import { parseQuotaFromDollars } from '@/lib/format'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { UserLevelEditorRow } from './user-level-editor-row'
import {
  DEFAULT_USER_LEVEL_CONFIG,
  USER_LEVEL_BADGE_COLORS,
  getBillingImpactMessages,
  normalizeUserLevelConfig,
  userLevelSettingsSchema,
  type UserLevelSettingsValues,
} from './user-level-settings-schema'

function cloneConfig(config: UserLevelConfig): UserLevelSettingsValues {
  return {
    ...config,
    levels: config.levels.map((level) => ({ ...level })),
  }
}

function getErrorMessage(error: unknown): string {
  if (error instanceof Error && error.message) return error.message
  return '用户等级设置保存失败'
}

export function UserLevelSettingsSection() {
  const query = useUserLevelAdminConfig()
  const updateMutation = useUpdateUserLevelAdminConfig()
  const [pendingConfig, setPendingConfig] = useState<UserLevelConfig | null>(
    null
  )
  const [impactMessages, setImpactMessages] = useState<string[]>([])
  const [baseline, setBaseline] = useState<UserLevelAdminData | null>(
    query.data ?? null
  )
  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const quotaStep = currencyMeta.kind === 'tokens' ? 1 : 0.000001

  const form = useForm<UserLevelSettingsValues>({
    resolver: zodResolver(
      userLevelSettingsSchema
    ) as Resolver<UserLevelSettingsValues>,
    defaultValues: cloneConfig(query.data?.config ?? DEFAULT_USER_LEVEL_CONFIG),
  })
  const levelFields = useFieldArray({
    control: form.control,
    name: 'levels',
    keyName: 'formKey',
  })
  const levels =
    useWatch({
      control: form.control,
      name: 'levels',
    }) ?? []
  const enabled = useWatch({ control: form.control, name: 'enabled' })
  const { isSubmitting } = form.formState
  const currentConfig = normalizeUserLevelConfig({
    schema_version: 1,
    enabled: enabled ?? false,
    levels,
  })
  const hasChanges =
    baseline !== null &&
    JSON.stringify(currentConfig) !== JSON.stringify(baseline.config)

  useEffect(() => {
    if (!query.data) return
    if (JSON.stringify(query.data) === JSON.stringify(baseline)) return
    if (baseline && hasChanges) return
    setBaseline(query.data)
    form.reset(cloneConfig(query.data.config))
  }, [baseline, form, hasChanges, query.data])

  const memberCounts = query.data?.member_counts ?? {}
  const totalMembers = Object.values(memberCounts).reduce(
    (total, count) => total + count,
    0
  )
  const saving = updateMutation.isPending || isSubmitting

  function addLevel() {
    const currentLevels = form.getValues('levels')
    if (currentLevels.length >= 20) {
      toast.error('最多配置 20 个等级')
      return
    }

    const usedIds = new Set(currentLevels.map((level) => level.id))
    let suffix = currentLevels.length
    while (usedIds.has(`level_${suffix}`)) suffix += 1

    const previous = currentLevels.at(-1) ?? DEFAULT_USER_LEVEL_CONFIG.levels[0]
    const quotaIncrement = Math.max(
      1,
      parseQuotaFromDollars(currencyMeta.kind === 'tokens' ? 100_000 : 100)
    )
    const nextThreshold = previous.threshold_quota + quotaIncrement
    if (!Number.isSafeInteger(nextThreshold)) {
      toast.error('累计消耗门槛已超出安全范围')
      return
    }

    const nextRatio = Math.max(
      0.01,
      Math.round((previous.ratio - 0.1) * 100) / 100
    )
    const color =
      USER_LEVEL_BADGE_COLORS[
        currentLevels.length % USER_LEVEL_BADGE_COLORS.length
      ].value
    levelFields.append({
      id: `level_${suffix}`,
      name: `等级 ${currentLevels.length}`,
      description: '',
      threshold_quota: nextThreshold,
      ratio: nextRatio,
      badge_color: color,
      archived: false,
    })
  }

  async function persistConfig(config: UserLevelConfig) {
    if (!baseline) {
      toast.error('尚未加载服务器上的用户等级设置')
      return
    }
    try {
      const result = await updateMutation.mutateAsync({
        config,
        revision: baseline.revision,
      })
      setBaseline(result)
      form.reset(cloneConfig(result.config))
      setPendingConfig(null)
      setImpactMessages([])
      toast.success(
        result.reconciled
          ? '已重新读取并确认用户等级设置生效'
          : '用户等级设置已保存'
      )
    } catch (error) {
      toast.error(getErrorMessage(error))
    }
  }

  function prepareSave(values: UserLevelSettingsValues) {
    const next = normalizeUserLevelConfig(values)
    if (!baseline) {
      toast.error('尚未加载服务器上的用户等级设置')
      return
    }
    const messages = getBillingImpactMessages(
      baseline.config,
      next,
      baseline.member_counts
    )
    if (messages.length > 0) {
      setImpactMessages(messages)
      setPendingConfig(next)
      return
    }
    void persistConfig(next)
  }

  function resetForm() {
    if (baseline) form.reset(cloneConfig(baseline.config))
  }

  if (query.isLoading && !query.data) {
    return (
      <SettingsSection title='用户等级'>
        <div className='space-y-4'>
          <Skeleton className='h-20 w-full' />
          <Skeleton className='h-52 w-full' />
        </div>
      </SettingsSection>
    )
  }

  if (query.isError && !query.data) {
    return (
      <SettingsSection title='用户等级'>
        <Alert variant='destructive'>
          <AlertTitle>用户等级设置加载失败</AlertTitle>
          <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
            <span>{getErrorMessage(query.error)}</span>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => void query.refetch()}
            >
              重新加载
            </Button>
          </AlertDescription>
        </Alert>
      </SettingsSection>
    )
  }

  return (
    <SettingsSection title='用户等级'>
      <Form {...form}>
        <SettingsForm
          onSubmit={form.handleSubmit(prepareSave)}
          autoComplete='off'
        >
          <SettingsPageFormActions
            onSave={form.handleSubmit(prepareSave)}
            onReset={resetForm}
            isSaving={saving}
            isSaveDisabled={!hasChanges}
            isResetDisabled={!hasChanges}
            saveLabel='保存用户等级设置'
          />

          <Alert>
            <HugeiconsIcon
              icon={InformationCircleIcon}
              strokeWidth={2}
              aria-hidden='true'
            />
            <AlertTitle>扣费规则</AlertTitle>
            <AlertDescription>
              普通请求、按次任务与动态计费统一按“Key 分组倍率 ×
              用户等级倍率”结算；用户无需修改
              Key。违规附加费不享受等级优惠、不计入等级累计消耗，并会在消费日志中单独标记。
            </AlertDescription>
          </Alert>

          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <div className='flex flex-wrap items-center gap-2'>
                    <FormLabel>开启用户等级</FormLabel>
                    <Badge variant={enabled ? 'default' : 'outline'}>
                      {enabled ? '已开启' : '未开启'}
                    </Badge>
                    <Badge variant='secondary'>{totalMembers} 名用户</Badge>
                  </div>
                  <FormDescription>
                    开启后，用户可按等级进度中记录的已结算消耗领取等级，新请求立即使用对应等级倍率。
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={saving}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <div data-settings-form-span='full' className='space-y-4'>
            <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
              <div className='space-y-1'>
                <h2 className='text-base font-semibold'>等级规则</h2>
                <p className='text-muted-foreground text-xs leading-5'>
                  等级按页面顺序递增。消耗门槛必须逐级提高，等级倍率不能高于前一级。
                </p>
              </div>
              <Button
                type='button'
                variant='outline'
                onClick={addLevel}
                disabled={saving || levels.length >= 20}
              >
                <HugeiconsIcon
                  icon={Add01Icon}
                  strokeWidth={2}
                  data-icon='inline-start'
                />
                添加等级
              </Button>
            </div>

            <div className='space-y-3'>
              {levelFields.fields.map((field, index) => {
                const level = levels[index] ?? field
                const persistedLevel = baseline?.config.levels[index]
                return (
                  <UserLevelEditorRow
                    key={field.formKey}
                    control={form.control}
                    index={index}
                    level={level}
                    persisted={persistedLevel !== undefined}
                    memberCount={
                      persistedLevel
                        ? (memberCounts[persistedLevel.id] ?? 0)
                        : 0
                    }
                    currencyLabel={currencyLabel}
                    quotaStep={quotaStep}
                    disabled={saving}
                    onRemove={() => levelFields.remove(index)}
                  />
                )
              })}
            </div>

            <p className='text-muted-foreground text-xs leading-5'>
              等级进度只统计系统开始记录后的已结算实际使用消耗，不含违规附加费，也不受充值或签到额度来源影响；关闭功能只停止等级倍率参与新请求计费，不会删除用户已领取等级与领取记录。
            </p>
          </div>
        </SettingsForm>
      </Form>

      <AlertDialog
        open={pendingConfig !== null}
        onOpenChange={(open) => {
          if (!open && !saving) {
            setPendingConfig(null)
            setImpactMessages([])
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认变更用户计费倍率</AlertDialogTitle>
            <AlertDialogDescription>
              这些设置会影响已有用户之后发起的新请求：
            </AlertDialogDescription>
          </AlertDialogHeader>
          <ul className='text-muted-foreground list-disc space-y-2 pl-5 text-sm'>
            {impactMessages.map((message) => (
              <li key={message}>{message}</li>
            ))}
          </ul>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={saving}>取消</AlertDialogCancel>
            <AlertDialogAction
              type='button'
              disabled={saving || pendingConfig === null}
              onClick={() => {
                if (pendingConfig) void persistConfig(pendingConfig)
              }}
            >
              确认并保存
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
