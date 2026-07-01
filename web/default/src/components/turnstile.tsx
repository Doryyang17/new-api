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
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

declare global {
  interface Window {
    turnstile?: {
      render: (element: HTMLElement, options: Record<string, unknown>) => string
      reset?: (widgetId?: string) => void
    }
  }
}

interface TurnstileProps {
  siteKey: string
  onVerify: (token: string) => void
  onExpire?: () => void
  onError?: (message: string) => void
  resetKey?: number
  className?: string
}

const TURNSTILE_SCRIPT_ID = 'cf-turnstile'
const TURNSTILE_LOAD_TIMEOUT_MS = 15_000
const TURNSTILE_SCRIPT_URL =
  'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'

export function Turnstile({
  siteKey,
  onVerify,
  onExpire,
  onError,
  resetKey,
  className,
}: TurnstileProps) {
  const { t } = useTranslation()
  const ref = useRef<HTMLDivElement | null>(null)
  const widgetIdRef = useRef<string | null>(null)
  const lastResetKeyRef = useRef(resetKey)
  const [message, setMessage] = useState('')
  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    let isMounted = true

    const fail = (nextMessage: string) => {
      if (!isMounted) return
      setIsLoading(false)
      setMessage(nextMessage)
      onError?.(nextMessage)
    }

    const render = () => {
      if (!ref.current || !window.turnstile) return
      try {
        if (widgetIdRef.current) return
        widgetIdRef.current = window.turnstile.render(ref.current, {
          sitekey: siteKey,
          callback: (token: string) => {
            setIsLoading(false)
            setMessage('')
            onVerify(token)
          },
          'error-callback': () => {
            const nextMessage = t(
              'Cloudflare human check reported an error. Please refresh the page or contact the administrator.'
            )
            onExpire?.()
            fail(nextMessage)
          },
          'expired-callback': () => {
            const nextMessage = t(
              'Cloudflare human check expired. Please complete it again.'
            )
            onExpire?.()
            fail(nextMessage)
          },
          'timeout-callback': () => {
            const nextMessage = t(
              'Cloudflare human check timed out. Please refresh the page or contact the administrator.'
            )
            onExpire?.()
            fail(nextMessage)
          },
        })
      } catch {
        fail(
          t(
            'Cloudflare human check could not initialize. Please refresh the page or contact the administrator.'
          )
        )
      }
    }

    if (window.turnstile) {
      render()
      return () => {
        isMounted = false
      }
    }

    const handleLoad = () => {
      if (!window.turnstile) {
        fail(
          t(
            'Cloudflare human check is not ready. Please refresh the page or contact the administrator.'
          )
        )
        return
      }
      render()
    }

    const handleError = () => {
      fail(
        t(
          'Cloudflare human check failed to load. Please refresh the page or contact the administrator.'
        )
      )
    }

    const script =
      document.querySelector<HTMLScriptElement>(`#${TURNSTILE_SCRIPT_ID}`) ??
      document.createElement('script')

    if (!script.id) {
      script.id = TURNSTILE_SCRIPT_ID
      script.src = TURNSTILE_SCRIPT_URL
      script.async = true
      script.defer = true
      document.head.appendChild(script)
    }

    script.addEventListener('load', handleLoad, { once: true })
    script.addEventListener('error', handleError, { once: true })
    const timeoutId = setTimeout(() => {
      if (!window.turnstile) {
        handleError()
      }
    }, TURNSTILE_LOAD_TIMEOUT_MS)

    return () => {
      isMounted = false
      clearTimeout(timeoutId)
      script.removeEventListener('load', handleLoad)
      script.removeEventListener('error', handleError)
    }
  }, [siteKey, onVerify, onExpire, onError, t])

  useEffect(() => {
    if (resetKey === undefined || resetKey === lastResetKeyRef.current) return
    lastResetKeyRef.current = resetKey
    if (!widgetIdRef.current || !window.turnstile?.reset) return

    try {
      window.turnstile.reset(widgetIdRef.current)
    } catch {
      /* empty */
    }
  }, [resetKey])

  return (
    <div className={className}>
      <div ref={ref} />
      {isLoading ? (
        <p className='text-muted-foreground mt-2 text-xs'>
          {t('Loading Cloudflare human check...')}
        </p>
      ) : null}
      {message ? (
        <p className='text-destructive mt-2 text-xs' role='alert'>
          {message}
        </p>
      ) : null}
    </div>
  )
}
