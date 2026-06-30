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

export const SYSTEM_CURFEW_CODE = 'system_curfew'
export const SYSTEM_AVAILABILITY_EVENT = 'new-api-system-availability-change'
const AVAILABILITY_SECONDS_PER_DAY = 24 * 60 * 60
const DEFAULT_AVAILABILITY_REFRESH_MS = 60 * 1000

export type SystemAvailabilityStatus = {
  enabled?: boolean
  unavailable?: boolean
  message?: string
  code?: string
  timezone?: string
  unavailable_start?: string
  unavailable_end?: string
  retry_after_seconds?: number
}

declare global {
  interface Window {
    __NEW_API_AVAILABILITY__?: SystemAvailabilityStatus
  }
}

export function getSystemAvailabilityStatus(): SystemAvailabilityStatus | null {
  if (typeof window === 'undefined') return null
  return window.__NEW_API_AVAILABILITY__ ?? null
}

export function setSystemAvailabilityStatus(status: SystemAvailabilityStatus) {
  if (typeof window === 'undefined') return
  window.__NEW_API_AVAILABILITY__ = {
    ...window.__NEW_API_AVAILABILITY__,
    ...status,
  }
  window.dispatchEvent(new Event(SYSTEM_AVAILABILITY_EVENT))
}

export function syncSystemAvailabilityFromStatus(status: unknown) {
  if (!status || typeof status !== 'object') return
  const statusBody = status as Record<string, unknown>
  const availability =
    statusBody.system_availability ?? statusBody.availability ?? null
  if (!availability || typeof availability !== 'object') return
  setSystemAvailabilityStatus(availability as SystemAvailabilityStatus)
}

export function isSystemCurfewActive(): boolean {
  const status = getSystemAvailabilityStatus()
  return Boolean(status?.enabled && status?.unavailable)
}

function getResponseData(error: unknown): Record<string, unknown> | null {
  if (!error || typeof error !== 'object') return null
  const response = (error as { response?: { data?: unknown } }).response
  const data = response?.data
  if (!data || typeof data !== 'object') return null
  return data as Record<string, unknown>
}

export function isSystemCurfewError(error: unknown): boolean {
  const data = getResponseData(error)
  if (!data) return false
  if (data.code === SYSTEM_CURFEW_CODE) return true
  const errorBody = data.error
  if (!errorBody || typeof errorBody !== 'object') return false
  const typedError = errorBody as Record<string, unknown>
  return (
    typedError.code === SYSTEM_CURFEW_CODE ||
    typedError.type === SYSTEM_CURFEW_CODE
  )
}

function getSystemCurfewMessageFromError(error: unknown): string | undefined {
  const data = getResponseData(error)
  if (!data) return undefined
  if (typeof data.message === 'string') return data.message
  if (typeof data.description === 'string') return data.description
  const errorBody = data.error
  if (!errorBody || typeof errorBody !== 'object') return undefined
  const typedError = errorBody as Record<string, unknown>
  return typeof typedError.message === 'string' ? typedError.message : undefined
}

export function markSystemCurfewFromError(error: unknown) {
  if (!isSystemCurfewError(error)) return
  setSystemAvailabilityStatus({
    enabled: true,
    unavailable: true,
    code: SYSTEM_CURFEW_CODE,
    message: getSystemCurfewMessageFromError(error),
  })
}

export function subscribeSystemAvailability(listener: () => void) {
  if (typeof window === 'undefined') {
    return () => {}
  }
  window.addEventListener(SYSTEM_AVAILABILITY_EVENT, listener)
  return () => window.removeEventListener(SYSTEM_AVAILABILITY_EVENT, listener)
}

export function getSystemCurfewMessage(): string {
  return (
    getSystemAvailabilityStatus()?.message ||
    '当前处于宵禁状态，22:00-8:00期间服务不可用，敬请谅解~'
  )
}

function parseAvailabilityClock(value: unknown): number | null {
  if (typeof value !== 'string') return null
  const match = /^([01]\d|2[0-3]):([0-5]\d)$/.exec(value.trim())
  if (!match) return null
  return Number(match[1]) * 3600 + Number(match[2]) * 60
}

function getCurrentSecondsInTimezone(timezone: unknown): number | null {
  if (typeof timezone !== 'string' || timezone.trim() === '') return null
  try {
    const parts = new Intl.DateTimeFormat('en-US', {
      timeZone: timezone,
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hourCycle: 'h23',
    }).formatToParts(new Date())
    const values = Object.fromEntries(parts.map((part) => [part.type, part.value]))
    return (
      Number(values.hour) * 3600 +
      Number(values.minute) * 60 +
      Number(values.second)
    )
  } catch {
    return null
  }
}

function normalizePositiveSeconds(value: unknown): number | null {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) {
    return null
  }
  return value
}

export function getSystemAvailabilityRefreshDelayMs(): number {
  const status = getSystemAvailabilityStatus()
  if (!status?.enabled) return DEFAULT_AVAILABILITY_REFRESH_MS

  const retryAfterSeconds = normalizePositiveSeconds(
    status.retry_after_seconds
  )
  if (status.unavailable && retryAfterSeconds) {
    return Math.max(1000, (retryAfterSeconds + 1) * 1000)
  }

  const startSeconds = parseAvailabilityClock(status.unavailable_start)
  const currentSeconds = getCurrentSecondsInTimezone(status.timezone)
  if (startSeconds === null || currentSeconds === null) {
    return DEFAULT_AVAILABILITY_REFRESH_MS
  }

  let secondsUntilStart = startSeconds - currentSeconds
  if (secondsUntilStart <= 0) {
    secondsUntilStart += AVAILABILITY_SECONDS_PER_DAY
  }
  return Math.max(1000, (secondsUntilStart + 1) * 1000)
}
