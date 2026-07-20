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
import { Activity, BarChart3, Sparkles, WalletCards } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { IconBadge, type IconBadgeTone } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { formatCompactNumber, formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  formatBonusRemaining,
  getCurrentMonthString,
  isCheckinBonusActive,
} from './lib'
import { useCheckinStatus } from './use-checkin-status'

export interface AccountBalanceSummaryProps {
  balance: number
  usedQuota: number
  requestCount: number
  loading: boolean
  checkinEnabled: boolean
  variant?: 'embedded' | 'standalone'
  compactRequestCount?: boolean
}

interface AccountMetric {
  label: string
  value: string
  description: string
  icon: typeof WalletCards
  tone: IconBadgeTone
}

export function AccountBalanceSummary(props: AccountBalanceSummaryProps) {
  const { t } = useTranslation()
  const [nowUnix, setNowUnix] = useState(() => Math.floor(Date.now() / 1000))
  const currentMonth = getCurrentMonthString()
  const { data: checkinData, isLoading: checkinLoading } = useCheckinStatus(
    currentMonth,
    true
  )
  const activeBonus = checkinData?.active_bonus
  const latestBonus = checkinData?.latest_bonus
  const bonusSettingEnabled = checkinData?.bonus_setting?.enabled === true
  const bonusIsActive = isCheckinBonusActive(activeBonus, nowUnix)
  const hasCurrentBonus =
    activeBonus != null || (latestBonus?.expire_at ?? 0) > nowUnix
  const showBonus =
    hasCurrentBonus ||
    (props.checkinEnabled && (checkinLoading || bonusSettingEnabled))
  const metricCount = showBonus ? 4 : 3
  const gridClass = showBonus
    ? 'grid-cols-2 sm:grid-cols-4'
    : 'grid-cols-1 sm:grid-cols-3'
  const containerClass =
    props.variant === 'embedded'
      ? 'border-t'
      : 'overflow-hidden rounded-lg border'

  useEffect(() => {
    if (activeBonus?.expire_at == null) return
    const timer = window.setInterval(() => {
      setNowUnix(Math.floor(Date.now() / 1000))
    }, 60000)
    return () => window.clearInterval(timer)
  }, [activeBonus?.expire_at])

  if (props.loading) {
    return (
      <div className={containerClass}>
        <div className={cn('bg-border/60 grid gap-px', gridClass)}>
          {Array.from({ length: metricCount }, (_, index) => (
            <div
              key={index}
              className='bg-card min-w-0 px-3 py-3 sm:px-5 sm:py-4'
            >
              <Skeleton className='h-3.5 w-20' />
              <Skeleton className='mt-2 h-7 w-28' />
              <Skeleton className='mt-2 hidden h-3.5 w-24 md:block' />
            </div>
          ))}
        </div>
      </div>
    )
  }

  let bonusDescription = '签到后发放 · 当日有效'
  if (checkinLoading) {
    bonusDescription = '正在读取赠金余额'
  } else if (bonusIsActive && activeBonus) {
    bonusDescription = `剩余 ${formatBonusRemaining(activeBonus.expire_at, nowUnix)} · 优先抵扣`
  } else if (latestBonus?.status === 'consumed' && hasCurrentBonus) {
    bonusDescription = '今日赠金已用完'
  } else if (latestBonus && hasCurrentBonus) {
    bonusDescription = '今日赠金已失效'
  }

  const requestCount = props.compactRequestCount
    ? formatCompactNumber(props.requestCount)
    : props.requestCount.toLocaleString()
  const metrics: AccountMetric[] = [
    {
      label: t('Current Balance'),
      value: formatQuota(props.balance),
      description: t('Remaining quota'),
      icon: WalletCards,
      tone: 'success',
    },
  ]

  if (showBonus) {
    metrics.push({
      label: '签到赠金',
      value: formatQuota(
        bonusIsActive && activeBonus ? activeBonus.remaining_amount : 0
      ),
      description: bonusDescription,
      icon: Sparkles,
      tone: 'warning',
    })
  }

  metrics.push(
    {
      label: t('Total Usage'),
      value: formatQuota(props.usedQuota),
      description: t('Total consumed quota'),
      icon: BarChart3,
      tone: 'info',
    },
    {
      label: t('API Requests'),
      value: requestCount,
      description: t('Total requests made'),
      icon: Activity,
      tone: 'chart-4',
    }
  )

  return (
    <div className={containerClass}>
      <div className={cn('bg-border/60 grid gap-px', gridClass)}>
        {metrics.map((item) => (
          <div
            key={item.label}
            className='bg-card min-w-0 px-3 py-3 sm:px-5 sm:py-4'
          >
            <div className='flex items-center gap-2'>
              <IconBadge tone={item.tone} size='stat'>
                <item.icon />
              </IconBadge>
              <div className='text-muted-foreground truncate text-xs font-medium'>
                {item.label}
              </div>
            </div>
            <div className='text-foreground mt-2 truncate font-mono text-lg font-bold tracking-tight tabular-nums sm:text-2xl'>
              {item.value}
            </div>
            <div className='text-muted-foreground mt-1 hidden text-xs md:block'>
              {item.description}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
