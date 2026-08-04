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
import {
  Award01Icon,
  InformationCircleIcon,
  Tick02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { IconBadge } from '@/components/ui/icon-badge'
import {
  Progress,
  ProgressLabel,
  ProgressValue,
} from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { formatQuotaWithCurrency, getCurrencyDisplay } from '@/lib/currency'

import { useClaimUserLevel } from '../hooks'
import type { UserLevel, UserLevelClaimResult, UserLevelStatus } from '../types'
import { LevelBadge } from './level-badge'

type UserLevelCardProps = {
  status?: UserLevelStatus
  isLoading: boolean
  isError: boolean
  onRetry: () => void
}

const LEVEL_AMOUNT_OPTIONS = {
  abbreviate: false,
  digitsLarge: 2,
  digitsSmall: 2,
} as const

const LEVEL_AMOUNT_EXACT_OPTIONS = {
  abbreviate: false,
  digitsLarge: 4,
  digitsSmall: 4,
} as const

type FormattedLevelAmount = {
  display: string
  exact: string
}

function minimumVisibleQuota(): number {
  const { config } = getCurrencyDisplay()
  const minimumDisplayAmount = 0.01

  switch (config.quotaDisplayType) {
    case 'CNY':
      return (
        (minimumDisplayAmount / config.usdExchangeRate) * config.quotaPerUnit
      )
    case 'CUSTOM':
      return (
        (minimumDisplayAmount / config.customCurrencyExchangeRate) *
        config.quotaPerUnit
      )
    case 'TOKENS':
      return minimumDisplayAmount
    case 'USD':
    default:
      return minimumDisplayAmount * config.quotaPerUnit
  }
}

function formatLevelAmount(quota: number): FormattedLevelAmount {
  const display = formatQuotaWithCurrency(quota, LEVEL_AMOUNT_OPTIONS)
  const exact = formatQuotaWithCurrency(quota, LEVEL_AMOUNT_EXACT_OPTIONS)
  const zero = formatQuotaWithCurrency(0, LEVEL_AMOUNT_OPTIONS)

  if (quota > 0 && display === zero) {
    const minimum = formatQuotaWithCurrency(
      minimumVisibleQuota(),
      LEVEL_AMOUNT_OPTIONS
    )
    return { display: `< ${minimum}`, exact }
  }

  return { display, exact }
}

function formatProgressPercent(percent: number): string {
  if (percent > 0 && percent < 0.01) return '< 0.01%'

  return `${new Intl.NumberFormat('zh-CN', {
    maximumFractionDigits: 2,
  }).format(percent)}%`
}

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) return error.message
  return '等级领取失败，请稍后重试'
}

function levelStateLabel(level: UserLevel): string {
  switch (level.state) {
    case 'current':
      return '当前等级'
    case 'claimable':
      return '可领取'
    case 'passed':
      return '已达成'
    case 'archived':
      return '停止发放'
    default:
      return '未达成'
  }
}

export function UserLevelCard(props: UserLevelCardProps) {
  const claimMutation = useClaimUserLevel()
  const [rulesOpen, setRulesOpen] = useState(false)
  const [claimResult, setClaimResult] = useState<UserLevelClaimResult | null>(
    null
  )

  if (props.isLoading) {
    return (
      <Card data-card-hover='false'>
        <CardHeader>
          <Skeleton className='h-5 w-32' />
          <Skeleton className='h-4 w-56' />
        </CardHeader>
        <CardContent className='flex flex-col gap-4'>
          <Skeleton className='h-16 w-full' />
          <Skeleton className='h-4 w-full' />
        </CardContent>
      </Card>
    )
  }

  if (props.isError) {
    return (
      <Card data-card-hover='false'>
        <CardHeader>
          <CardTitle>用户等级</CardTitle>
          <CardDescription role='alert'>等级信息加载失败</CardDescription>
          <CardAction>
            <Button variant='outline' size='sm' onClick={props.onRetry}>
              重新加载
            </Button>
          </CardAction>
        </CardHeader>
      </Card>
    )
  }

  if (!props.status?.enabled) return null

  const status = props.status
  const currentLevel = status.current_level
  const claimableLevel = status.claimable_level
  const consumedAmount = formatLevelAmount(status.total_consumed_quota)
  const remainingAmount = formatLevelAmount(status.progress.remaining_quota)
  const progressPercent = formatProgressPercent(status.progress.percent)

  async function handleClaim() {
    try {
      const result = await claimMutation.mutateAsync(currentLevel.id)
      if (result.changed) {
        setClaimResult(result)
        toast.success(
          result.reconciled ? '等级状态已确认并更新' : '等级领取成功'
        )
      } else {
        toast.info('当前没有可领取的新等级')
      }
    } catch (error) {
      toast.error(errorMessage(error))
    }
  }

  return (
    <>
      <Dialog open={rulesOpen} onOpenChange={setRulesOpen}>
        <DialogContent className='sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>用户等级规则</DialogTitle>
            <DialogDescription>
              等级进度只统计系统开始记录后的已结算消耗，不含违规附加费；达到条件后领取，所有
              Key 自动使用等级倍率。
            </DialogDescription>
          </DialogHeader>
          <div className='flex max-h-[55vh] flex-col gap-2 overflow-y-auto'>
            {status.levels.map((level) => (
              <div
                key={level.id}
                className='flex items-center justify-between gap-3 rounded-lg border p-3'
              >
                <div className='flex min-w-0 flex-col gap-1'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <LevelBadge name={level.name} color={level.badge_color} />
                    <span className='text-muted-foreground text-xs'>
                      {levelStateLabel(level)}
                    </span>
                  </div>
                  <span className='text-muted-foreground text-xs'>
                    累计消耗 {formatQuotaWithCurrency(level.threshold_quota)}
                  </span>
                </div>
                <span className='font-mono text-sm font-medium tabular-nums'>
                  ×{level.ratio.toFixed(2)}
                </span>
              </div>
            ))}
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setRulesOpen(false)}>
              关闭
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={claimResult !== null}
        onOpenChange={(open) => {
          if (!open) setClaimResult(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <div className='flex items-center gap-3'>
              <IconBadge tone='success' size='lg'>
                <HugeiconsIcon icon={Tick02Icon} strokeWidth={2} />
              </IconBadge>
              <div className='flex flex-col gap-1'>
                <DialogTitle>等级升级成功</DialogTitle>
                <DialogDescription>
                  新等级已自动应用到你的所有 Key。
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>
          {claimResult && (
            <div className='bg-muted/50 flex items-center justify-between gap-4 rounded-lg p-4'>
              <div className='flex min-w-0 flex-col gap-2'>
                <LevelBadge
                  name={claimResult.status.current_level.name}
                  color={claimResult.status.current_level.badge_color}
                />
                <span className='text-muted-foreground text-xs'>
                  {claimResult.previous_level.name} ×
                  {claimResult.previous_level.ratio.toFixed(2)} → 当前 ×
                  {claimResult.status.current_level.ratio.toFixed(2)}
                </span>
              </div>
              <span className='font-mono text-2xl font-semibold tabular-nums'>
                ×{claimResult.status.current_level.ratio.toFixed(2)}
              </span>
            </div>
          )}
          <DialogFooter>
            <Button onClick={() => setClaimResult(null)}>知道了</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Card data-card-hover='false'>
        <CardHeader>
          <div className='flex items-start gap-3'>
            <IconBadge tone='neutral' size='lg'>
              <HugeiconsIcon icon={Award01Icon} strokeWidth={2} />
            </IconBadge>
            <div className='flex min-w-0 flex-col gap-1'>
              <CardTitle>用户等级</CardTitle>
              <CardDescription>等级越高，实际使用倍率越优惠</CardDescription>
            </div>
          </div>
          <CardAction>
            <LevelBadge
              name={`当前：${currentLevel.name}`}
              color={currentLevel.badge_color}
            />
          </CardAction>
        </CardHeader>

        <CardContent className='flex flex-col gap-5'>
          <div className='grid grid-cols-2 gap-3'>
            <div className='bg-muted/50 flex min-w-0 flex-col gap-1 rounded-lg p-3'>
              <span className='text-muted-foreground text-xs'>等级倍率</span>
              <span className='font-mono text-xl font-semibold tabular-nums'>
                ×{currentLevel.ratio.toFixed(2)}
              </span>
            </div>
            <div className='bg-muted/50 flex min-w-0 flex-col gap-1 rounded-lg p-3'>
              <span className='text-muted-foreground text-xs'>累计消耗</span>
              <span
                className='truncate font-mono text-xl font-semibold tabular-nums'
                title={`精确值：${consumedAmount.exact}`}
              >
                {consumedAmount.display}
              </span>
            </div>
          </div>

          {status.next_level ? (
            <Progress
              value={status.progress.percent}
              className={
                status.progress.percent > 0
                  ? '[&_[data-slot=progress-indicator]]:min-w-0.5'
                  : undefined
              }
            >
              <ProgressLabel>距离 {status.next_level.name}</ProgressLabel>
              <ProgressValue
                title={
                  status.progress.remaining_quota > 0
                    ? `精确剩余：${remainingAmount.exact}`
                    : undefined
                }
              >
                {() =>
                  status.progress.remaining_quota > 0
                    ? `${progressPercent} · 还差 ${remainingAmount.display}`
                    : `${progressPercent} · 已达成`
                }
              </ProgressValue>
            </Progress>
          ) : (
            <p className='text-muted-foreground text-sm'>已达到最高等级</p>
          )}

          <div className='text-muted-foreground flex items-start gap-2 text-xs'>
            <HugeiconsIcon
              icon={InformationCircleIcon}
              className='mt-0.5 size-4 shrink-0'
              strokeWidth={2}
              aria-hidden='true'
            />
            <span>实际倍率 = Key 分组倍率 × 等级倍率；无需修改任何 Key。</span>
          </div>
        </CardContent>

        <CardFooter className='justify-between gap-3'>
          <Button variant='ghost' size='sm' onClick={() => setRulesOpen(true)}>
            查看等级规则
          </Button>
          {claimableLevel && (
            <Button
              size='sm'
              disabled={claimMutation.isPending}
              onClick={() => void handleClaim()}
            >
              {claimMutation.isPending && <Spinner data-icon='inline-start' />}
              领取 {claimableLevel.name}
            </Button>
          )}
        </CardFooter>
      </Card>
    </>
  )
}
