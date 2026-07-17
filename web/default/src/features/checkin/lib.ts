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
import dayjs from '@/lib/dayjs'

import type { CheckinBonus } from './types'

export function getCurrentMonthString(): string {
  const now = new Date()
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`
}

export function formatBonusWindow(bonus: CheckinBonus): string {
  return `今天 ${dayjs.unix(bonus.created_at).format('HH:mm')} - 24:00`
}

export function formatBonusRemaining(
  expireAt: number,
  nowUnix: number
): string {
  const remainingSeconds = Math.max(0, expireAt - nowUnix)
  if (remainingSeconds === 0) return '已失效'

  const hours = Math.floor(remainingSeconds / 3600)
  const minutes = Math.ceil((remainingSeconds % 3600) / 60)
  if (hours === 0) return `${minutes} 分钟`
  if (minutes === 0 || minutes === 60) {
    return `${hours + (minutes === 60 ? 1 : 0)} 小时`
  }
  return `${hours} 小时 ${minutes} 分钟`
}

export function isCheckinBonusActive(
  bonus: CheckinBonus | null | undefined,
  nowUnix: number
): bonus is CheckinBonus {
  return (
    bonus?.status === 'active' &&
    bonus.remaining_amount > 0 &&
    bonus.expire_at > nowUnix
  )
}
