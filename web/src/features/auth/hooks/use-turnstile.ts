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
import i18next from 'i18next'
import { useCallback, useState } from 'react'
import { toast } from 'sonner'

import { useStatus } from '@/hooks/use-status'

/**
 * Hook for managing Turnstile verification
 */
export function useTurnstile() {
  const { status } = useStatus()
  const [turnstileToken, setTurnstileToken] = useState('')
  const [turnstileError, setTurnstileError] = useState('')
  const [turnstileResetKey, setTurnstileResetKey] = useState(0)

  const isTurnstileEnabled = !!(
    status?.turnstile_check && status?.turnstile_site_key
  )
  const turnstileSiteKey = status?.turnstile_site_key || ''

  /**
   * Validate if turnstile is ready when required
   */
  const validateTurnstile = (): boolean => {
    if (isTurnstileEnabled && turnstileError) {
      toast.error(turnstileError)
      return false
    }

    if (isTurnstileEnabled && !turnstileToken) {
      toast.info(
        i18next.t('Please wait a moment, human check is initializing...')
      )
      return false
    }
    return true
  }

  const handleTurnstileVerify = useCallback((token: string) => {
    setTurnstileToken(token)
    setTurnstileError('')
  }, [])

  const handleTurnstileError = useCallback((message: string) => {
    setTurnstileToken('')
    setTurnstileError(message)
  }, [])

  const resetTurnstile = useCallback(() => {
    setTurnstileToken('')
    setTurnstileError('')
    setTurnstileResetKey((key) => key + 1)
  }, [])

  return {
    isTurnstileEnabled,
    turnstileSiteKey,
    turnstileToken,
    turnstileError,
    turnstileResetKey,
    setTurnstileToken,
    handleTurnstileVerify,
    handleTurnstileError,
    resetTurnstile,
    validateTurnstile,
  }
}
