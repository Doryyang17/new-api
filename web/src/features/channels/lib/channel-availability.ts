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
import { z } from 'zod'

import type {
  ChannelAvailabilitySchedule,
  ChannelOtherSettings,
} from '../types'

const clockPattern = /^([01]\d|2[0-3]):[0-5]\d$/
const HOUR_MS = 60 * 60 * 1000
const DAY_MS = 24 * HOUR_MS
const CHANNEL_AVAILABILITY_BOUNDARY_CACHE_LIMIT = 4096
const channelAvailabilityFormatterCache = new Map<string, Intl.DateTimeFormat>()
const channelAvailabilityBoundaryCache = new Map<string, number>()

export const CHANNEL_AVAILABILITY_REFRESH_INTERVAL_MS = 15_000

export const DEFAULT_CHANNEL_AVAILABILITY_SCHEDULE: ChannelAvailabilitySchedule =
  {
    enabled: false,
    start: '08:00',
    end: '12:00',
    timezone: 'Asia/Shanghai',
  }

export const CHANNEL_AVAILABILITY_TIMEZONES = [
  { value: 'Asia/Shanghai', label: '上海 (Asia/Shanghai)' },
  { value: 'UTC', label: '协调世界时 (UTC)' },
  { value: 'Asia/Hong_Kong', label: '香港 (Asia/Hong_Kong)' },
  { value: 'Asia/Singapore', label: '新加坡 (Asia/Singapore)' },
  { value: 'Asia/Tokyo', label: '东京 (Asia/Tokyo)' },
  { value: 'Asia/Seoul', label: '首尔 (Asia/Seoul)' },
  { value: 'Europe/London', label: '伦敦 (Europe/London)' },
  { value: 'Europe/Berlin', label: '柏林 (Europe/Berlin)' },
  { value: 'America/New_York', label: '纽约 (America/New_York)' },
  { value: 'America/Chicago', label: '芝加哥 (America/Chicago)' },
  { value: 'America/Los_Angeles', label: '洛杉矶 (America/Los_Angeles)' },
] as const

type ChannelAvailabilityLocalDate = {
  year: number
  month: number
  day: number
}

type ChannelAvailabilityLocalParts = ChannelAvailabilityLocalDate & {
  hour: number
  minute: number
  second: number
}

function getChannelAvailabilityFormatter(
  timezone: string
): Intl.DateTimeFormat {
  const cached = channelAvailabilityFormatterCache.get(timezone)
  if (cached) return cached

  const formatter = new Intl.DateTimeFormat('en-US', {
    timeZone: timezone,
    calendar: 'gregory',
    numberingSystem: 'latn',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  })
  channelAvailabilityFormatterCache.set(timezone, formatter)
  return formatter
}

function isValidTimezone(timezone: string): boolean {
  try {
    getChannelAvailabilityFormatter(timezone).format()
    return true
  } catch {
    return false
  }
}

export const channelAvailabilitySchema = z
  .object({
    enabled: z.boolean(),
    start: z.string().regex(clockPattern, '请输入有效的开始时间'),
    end: z.string().regex(clockPattern, '请输入有效的结束时间'),
    timezone: z
      .string()
      .trim()
      .min(1, '请选择时区')
      .refine(isValidTimezone, '请选择有效的时区'),
  })
  .refine((schedule) => schedule.start !== schedule.end, {
    message: '开始时间和结束时间不能相同',
    path: ['end'],
  })

export type ChannelAvailabilityFormValues = z.infer<
  typeof channelAvailabilitySchema
>

export type ChannelAvailabilityEvaluation = {
  inWindow: boolean
  nextAction: 'enable' | 'disable'
  nextTransitionAtMs: number
  nextTime: string
}

type ChannelAvailabilityItem = {
  settings?: string | null
  children?: ChannelAvailabilityItem[]
}

function parseClockSeconds(value: string): number | null {
  const match = clockPattern.exec(value)
  if (!match) return null
  return Number(match[1]) * 3600 + Number(value.slice(3, 5)) * 60
}

function getLocalParts(
  value: Date,
  timezone: string
): ChannelAvailabilityLocalParts | null {
  try {
    const parts = getChannelAvailabilityFormatter(timezone).formatToParts(value)
    const values = Object.fromEntries(
      parts.map((part) => [part.type, part.value])
    )
    const result = {
      year: Number(values.year),
      month: Number(values.month),
      day: Number(values.day),
      hour: Number(values.hour) % 24,
      minute: Number(values.minute),
      second: Number(values.second),
    }
    return Object.values(result).every(Number.isFinite) ? result : null
  } catch {
    return null
  }
}

function localPartsTimestamp(parts: ChannelAvailabilityLocalParts): number {
  return Date.UTC(
    parts.year,
    parts.month - 1,
    parts.day,
    parts.hour,
    parts.minute,
    parts.second
  )
}

function addLocalDays(
  date: ChannelAvailabilityLocalDate,
  days: number
): ChannelAvailabilityLocalDate {
  const shifted = new Date(Date.UTC(date.year, date.month - 1, date.day + days))
  return {
    year: shifted.getUTCFullYear(),
    month: shifted.getUTCMonth() + 1,
    day: shifted.getUTCDate(),
  }
}

function resolveChannelAvailabilityBoundary(
  date: ChannelAvailabilityLocalDate,
  clockSeconds: number,
  timezone: string,
  preferLatest: boolean
): number | null {
  const cacheKey = `${timezone}|${date.year}-${date.month}-${date.day}|${clockSeconds}|${preferLatest ? 'end' : 'start'}`
  const cached = channelAvailabilityBoundaryCache.get(cacheKey)
  if (cached !== undefined) return cached

  const targetWall = Date.UTC(
    date.year,
    date.month - 1,
    date.day,
    Math.floor(clockSeconds / 3600),
    Math.floor((clockSeconds % 3600) / 60),
    clockSeconds % 60
  )
  const offsets = new Set<number>()
  for (const delta of [
    -7 * DAY_MS,
    -48 * HOUR_MS,
    -DAY_MS,
    0,
    DAY_MS,
    48 * HOUR_MS,
    7 * DAY_MS,
  ]) {
    const sample = targetWall + delta
    const parts = getLocalParts(new Date(sample), timezone)
    if (!parts) return null
    offsets.add(localPartsTimestamp(parts) - sample)
  }

  const exact: number[] = []
  let beforeGap: number | null = null
  let beforeGapWall = Number.NEGATIVE_INFINITY
  let afterGap: number | null = null
  let afterGapWall = Number.POSITIVE_INFINITY
  for (const offset of offsets) {
    const candidate = targetWall - offset
    const parts = getLocalParts(new Date(candidate), timezone)
    if (!parts) return null
    const candidateWall = localPartsTimestamp(parts)
    if (candidateWall === targetWall) {
      exact.push(candidate)
    } else if (candidateWall < targetWall) {
      if (candidateWall > beforeGapWall) {
        beforeGap = candidate
        beforeGapWall = candidateWall
      }
    } else if (candidateWall < afterGapWall) {
      afterGap = candidate
      afterGapWall = candidateWall
    }
  }

  let resolved: number | null = null
  if (exact.length > 0) {
    resolved = preferLatest ? Math.max(...exact) : Math.min(...exact)
  } else if (beforeGap !== null && afterGap !== null && beforeGap < afterGap) {
    let low = Math.floor(beforeGap / 1000)
    let high = Math.floor(afterGap / 1000)
    while (low < high) {
      const middle = low + Math.floor((high - low) / 2)
      const parts = getLocalParts(new Date(middle * 1000), timezone)
      if (!parts) return null
      if (localPartsTimestamp(parts) < targetWall) {
        low = middle + 1
      } else {
        high = middle
      }
    }
    resolved = low * 1000
  }

  if (resolved === null) return null
  if (
    channelAvailabilityBoundaryCache.size >=
    CHANNEL_AVAILABILITY_BOUNDARY_CACHE_LIMIT
  ) {
    channelAvailabilityBoundaryCache.clear()
  }
  channelAvailabilityBoundaryCache.set(cacheKey, resolved)
  return resolved
}

function formatChannelAvailabilityBoundary(
  timestamp: number,
  timezone: string
): string | null {
  const parts = getLocalParts(new Date(timestamp), timezone)
  if (!parts) return null
  return `${String(parts.hour).padStart(2, '0')}:${String(parts.minute).padStart(2, '0')}`
}

export function parseChannelAvailabilitySchedule(
  settings: string | null | undefined
): ChannelAvailabilitySchedule | null {
  if (!settings) return null

  try {
    const parsed = JSON.parse(settings) as ChannelOtherSettings
    const result = channelAvailabilitySchema.safeParse(
      parsed.availability_schedule
    )
    return result.success ? result.data : null
  } catch {
    return null
  }
}

export function shouldRefreshChannelAvailability(
  items: ChannelAvailabilityItem[],
  statusFilter: string[]
): boolean {
  if (statusFilter.includes('enabled') || statusFilter.includes('disabled')) {
    return true
  }

  return hasEnabledChannelAvailability(items)
}

export function hasEnabledChannelAvailability(
  items: ChannelAvailabilityItem[]
): boolean {
  return items.some((item) => {
    if (parseChannelAvailabilitySchedule(item.settings)?.enabled) return true
    return (
      item.children?.some(
        (child) =>
          parseChannelAvailabilitySchedule(child.settings)?.enabled === true
      ) ?? false
    )
  })
}

export function mergeChannelAvailabilitySchedule(
  settings: string | null | undefined,
  schedule: ChannelAvailabilitySchedule
): string {
  let parsed: ChannelOtherSettings = {}
  try {
    parsed = settings ? (JSON.parse(settings) as ChannelOtherSettings) : {}
  } catch {
    parsed = {}
  }
  return JSON.stringify({ ...parsed, availability_schedule: schedule })
}

export function evaluateChannelAvailability(
  schedule: ChannelAvailabilitySchedule,
  now: Date = new Date()
): ChannelAvailabilityEvaluation | null {
  const parsed = channelAvailabilitySchema.safeParse(schedule)
  if (!parsed.success) return null

  const startSeconds = parseClockSeconds(parsed.data.start)
  const endSeconds = parseClockSeconds(parsed.data.end)
  const localNow = getLocalParts(now, parsed.data.timezone)
  if (startSeconds === null || endSeconds === null || localNow === null) {
    return null
  }

  const localDate: ChannelAvailabilityLocalDate = {
    year: localNow.year,
    month: localNow.month,
    day: localNow.day,
  }
  const startToday = resolveChannelAvailabilityBoundary(
    localDate,
    startSeconds,
    parsed.data.timezone,
    false
  )
  const endToday = resolveChannelAvailabilityBoundary(
    localDate,
    endSeconds,
    parsed.data.timezone,
    true
  )
  if (startToday === null || endToday === null) return null

  const nowMs = now.getTime()
  let inWindow: boolean
  let nextTransitionAtMs: number
  if (startSeconds < endSeconds) {
    if (startToday >= endToday) {
      const startTomorrow = resolveChannelAvailabilityBoundary(
        addLocalDays(localDate, 1),
        startSeconds,
        parsed.data.timezone,
        false
      )
      if (startTomorrow === null) return null
      inWindow = false
      nextTransitionAtMs = startTomorrow
    } else if (nowMs < startToday) {
      inWindow = false
      nextTransitionAtMs = startToday
    } else if (nowMs < endToday) {
      inWindow = true
      nextTransitionAtMs = endToday
    } else {
      const startTomorrow = resolveChannelAvailabilityBoundary(
        addLocalDays(localDate, 1),
        startSeconds,
        parsed.data.timezone,
        false
      )
      if (startTomorrow === null) return null
      inWindow = false
      nextTransitionAtMs = startTomorrow
    }
  } else {
    if (nowMs >= startToday) {
      const endTomorrow = resolveChannelAvailabilityBoundary(
        addLocalDays(localDate, 1),
        endSeconds,
        parsed.data.timezone,
        true
      )
      if (endTomorrow === null) return null
      inWindow = nowMs < endTomorrow
      nextTransitionAtMs = endTomorrow
    } else if (nowMs < endToday) {
      const startYesterday = resolveChannelAvailabilityBoundary(
        addLocalDays(localDate, -1),
        startSeconds,
        parsed.data.timezone,
        false
      )
      if (startYesterday === null) return null
      inWindow = nowMs >= startYesterday
      nextTransitionAtMs = endToday
    } else {
      inWindow = false
      nextTransitionAtMs = startToday
    }
  }

  const nextTime = formatChannelAvailabilityBoundary(
    nextTransitionAtMs,
    parsed.data.timezone
  )
  if (!nextTime) return null

  return {
    inWindow,
    nextAction: inWindow ? 'disable' : 'enable',
    nextTransitionAtMs,
    nextTime,
  }
}
