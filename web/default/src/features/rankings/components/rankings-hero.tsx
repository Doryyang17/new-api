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
import { CheckCircle2, Gauge, Loader2, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

import { formatTokens } from '../lib/format'
import type { DailyUsageStatus, RankingPeriod } from '../types'

const PERIODS: { id: RankingPeriod; labelKey: string }[] = [
  { id: 'today', labelKey: 'Today' },
  { id: 'week', labelKey: 'Week' },
  { id: 'month', labelKey: 'Month' },
  { id: 'year', labelKey: 'Year' },
]

type RankingsHeroProps = {
  period: RankingPeriod
  onPeriodChange: (period: RankingPeriod) => void
  dailyUsage?: DailyUsageStatus
  dailyUsageLoading?: boolean
}

/**
 * Hero strip for the rankings page. Intentionally minimal — title +
 * subtitle + period tabs only.
 */
export function RankingsHero(props: RankingsHeroProps) {
  const { t } = useTranslation()

  return (
    <section className='space-y-5'>
      <div className='space-y-2'>
        <h1 className='text-[clamp(1.75rem,4vw,2.5rem)] leading-[1.15] font-bold tracking-tight'>
          {t('Rankings')}
        </h1>
        <p className='text-muted-foreground/80 max-w-2xl text-sm'>
          {t(
            'Discover the most-used models and rising vendors on the platform, updated from live usage data.'
          )}
        </p>
      </div>

      {/* Underline tabs for period — clean and unobtrusive. */}
      <div
        role='tablist'
        aria-label={t('Period')}
        className='border-border/60 flex items-center border-b'
      >
        {PERIODS.map((p) => {
          const isActive = props.period === p.id
          return (
            <button
              key={p.id}
              role='tab'
              type='button'
              aria-selected={isActive}
              onClick={() => props.onPeriodChange(p.id)}
              className={cn(
                'focus-visible:ring-ring/40 relative -mb-px rounded-sm px-3 py-2 text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:outline-none',
                isActive
                  ? 'text-foreground'
                  : 'text-muted-foreground hover:text-foreground'
              )}
            >
              {t(p.labelKey)}
              <span
                aria-hidden
                className={cn(
                  'bg-foreground absolute inset-x-3 -bottom-px h-[2px] rounded-full transition-opacity',
                  isActive ? 'opacity-100' : 'opacity-0'
                )}
              />
            </button>
          )
        })}
      </div>

      <DailyUsageStrip
        dailyUsage={props.dailyUsage}
        loading={props.dailyUsageLoading}
      />
    </section>
  )
}

type DailyUsageStripProps = {
  dailyUsage?: DailyUsageStatus
  loading?: boolean
}

function DailyUsageStrip(props: DailyUsageStripProps) {
  const { t } = useTranslation()
  const status = props.dailyUsage

  if (props.loading) {
    return (
      <div
        aria-live='polite'
        className='bg-card/80 border-border/70 flex min-h-20 items-center gap-3 rounded-lg border px-4 py-3 text-sm shadow-sm'
      >
        <Loader2 className='text-muted-foreground size-4 animate-spin' />
        <span className='text-muted-foreground'>
          {t('Daily limit status is loading')}
        </span>
      </div>
    )
  }

  if (!status) {
    return (
      <div className='bg-card/80 border-border/70 text-muted-foreground rounded-lg border px-4 py-3 text-sm shadow-sm'>
        {t('Usage snapshot unavailable')}
      </div>
    )
  }

  const progress =
    status.enabled && status.limit_tokens > 0
      ? Math.min(100, (status.used_tokens / status.limit_tokens) * 100)
      : 0
  const enabledModelLimits = (status.model_limits ?? []).filter(
    (limit) => limit.enabled
  )
  const hasEnabledModelLimits = enabledModelLimits.length > 0
  const hasExceededModelLimit = enabledModelLimits.some(
    (limit) => limit.exceeded
  )
  const hasEnabledLimit = status.enabled || hasEnabledModelLimits
  const hasExceededLimit = status.exceeded || hasExceededModelLimit

  let badgeLabel = t('Limit off')
  if (status.exceeded) {
    badgeLabel = t('Limit reached')
  } else if (hasExceededModelLimit) {
    badgeLabel = '模型已限制'
  } else if (status.enabled) {
    badgeLabel = t('Limit enabled')
  } else if (hasEnabledModelLimits) {
    badgeLabel = '模型限制已启用'
  }

  let badgeClassName = 'border-border bg-muted/50 text-muted-foreground'
  if (hasExceededLimit) {
    badgeClassName = 'border-destructive/40 bg-destructive/10 text-destructive'
  } else if (hasEnabledLimit) {
    badgeClassName = 'border-primary/30 bg-primary/10 text-primary'
  }
  const Icon = hasExceededLimit ? TriangleAlert : CheckCircle2

  let statusDescription = t(
    'Tracking refreshes every 5 minutes. No daily cap is enforced until the switch is enabled.'
  )
  if (status.enabled && hasEnabledModelLimits) {
    statusDescription =
      '全站限制与模型独立限制均已启用；全站超限会停止所有模型，模型超限只影响对应模型。'
  } else if (status.enabled) {
    statusDescription = t(
      'Updated every 5 minutes. Requests are blocked before channel dispatch when the limit is reached.'
    )
  } else if (hasEnabledModelLimits) {
    statusDescription =
      '当前仅启用模型独立限制；达到额度时只拒绝对应模型，其他模型继续可用。'
  }

  return (
    <div
      aria-live='polite'
      className='bg-card/80 border-border/70 rounded-lg border px-4 py-3 shadow-sm'
    >
      <div className='flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between'>
        <div className='min-w-0 space-y-2'>
          <div className='flex flex-wrap items-center gap-2'>
            <span className='bg-muted text-muted-foreground inline-flex size-8 items-center justify-center rounded-md'>
              <Gauge className='size-4' />
            </span>
            <h2 className='text-sm font-semibold'>今日全站使用量</h2>
            <span
              className={cn(
                'inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
                badgeClassName
              )}
            >
              <Icon className='size-3.5' />
              {badgeLabel}
            </span>
          </div>
          <p className='text-muted-foreground max-w-3xl text-xs leading-5'>
            {statusDescription}
          </p>
        </div>

        <div className='grid min-w-0 gap-3 sm:grid-cols-3 lg:min-w-[420px]'>
          <DailyUsageMetric
            label={t('Used today')}
            value={`${formatTokens(status.used_tokens)} ${t('tokens')}`}
          />
          <DailyUsageMetric
            label={t('Daily cap')}
            value={
              status.limit_tokens > 0
                ? `${formatTokens(status.limit_tokens)} ${t('tokens')}`
                : t('Not set')
            }
          />
          <DailyUsageMetric
            label={t('Remaining')}
            value={
              status.enabled
                ? `${formatTokens(status.remaining_tokens)} ${t('tokens')}`
                : t('Not enforced')
            }
          />
        </div>
      </div>

      {status.enabled && status.limit_tokens > 0 ? (
        <div className='mt-4 space-y-2'>
          <div className='bg-muted h-2 overflow-hidden rounded-full'>
            <div
              className={cn(
                'h-full rounded-full transition-[width]',
                status.exceeded ? 'bg-destructive' : 'bg-primary'
              )}
              style={{ width: `${progress}%` }}
            />
          </div>
        </div>
      ) : null}

      {enabledModelLimits.length > 0 ? (
        <div className='border-border/60 mt-4 border-t pt-4'>
          <div className='mb-3 flex flex-wrap items-center justify-between gap-2'>
            <h3 className='text-xs font-semibold'>模型独立限制</h3>
            <span className='text-muted-foreground text-xs'>
              仅影响对应模型
            </span>
          </div>
          <div className='grid gap-2 md:grid-cols-2 xl:grid-cols-3'>
            {enabledModelLimits.map((limit) => (
              <div
                key={limit.model_name}
                className='border-border/60 flex min-w-0 items-center justify-between gap-3 rounded-md border px-3 py-2'
              >
                <div className='min-w-0'>
                  <div className='truncate text-xs font-medium'>
                    {limit.model_name}
                  </div>
                  <div className='text-muted-foreground mt-0.5 text-xs'>
                    {formatTokens(limit.model_current_usage)} /{' '}
                    {formatTokens(limit.model_max_usage)} tokens
                  </div>
                </div>
                <Badge
                  variant={limit.exceeded ? 'destructive' : 'outline'}
                  className={cn(
                    !limit.exceeded &&
                      'border-emerald-500/40 text-emerald-700 dark:text-emerald-300'
                  )}
                >
                  {limit.exceeded ? '已限制' : '正常'}
                </Badge>
              </div>
            ))}
          </div>
        </div>
      ) : null}

      <div className='text-muted-foreground mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs'>
        <span>
          {t('Accounting timezone: {{timezone}}', {
            timezone: status.timezone || '-',
          })}
        </span>
        <span>
          {t('Last refresh: {{time}}', {
            time: formatUsageTime(status.refreshed_at),
          })}
        </span>
        <span>
          {t('Next refresh: {{time}}', {
            time: formatUsageTime(status.next_refresh_at),
          })}
        </span>
      </div>
    </div>
  )
}

function DailyUsageMetric(props: { label: string; value: string }) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground truncate text-xs'>
        {props.label}
      </div>
      <div className='truncate text-sm font-semibold'>{props.value}</div>
    </div>
  )
}

function formatUsageTime(timestamp: number) {
  if (!Number.isFinite(timestamp) || timestamp <= 0) return '-'
  return new Date(timestamp * 1000).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}
