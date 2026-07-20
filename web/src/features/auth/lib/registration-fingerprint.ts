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

const REGISTRATION_FINGERPRINT_KEY = 'auth:registration-fingerprint'

function createRegistrationFingerprint(): string {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) {
    return crypto.randomUUID().replaceAll('-', '')
  }

  const randomPart = Math.random().toString(36).slice(2)
  const timePart = Date.now().toString(36)
  return `${timePart}${randomPart}`.padEnd(16, '0').slice(0, 32)
}

export function getRegistrationFingerprint(): string {
  if (typeof window === 'undefined') return ''

  try {
    const saved = window.localStorage.getItem(REGISTRATION_FINGERPRINT_KEY)
    if (saved && /^[A-Za-z0-9_-]{16,128}$/.test(saved)) {
      return saved
    }

    const next = createRegistrationFingerprint()
    window.localStorage.setItem(REGISTRATION_FINGERPRINT_KEY, next)
    return next
  } catch {
    return createRegistrationFingerprint()
  }
}
