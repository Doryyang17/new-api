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
  createFileRoute,
  useNavigate,
  useParams,
  useSearch,
} from '@tanstack/react-router'
import type { AxiosRequestConfig } from 'axios'
import i18next from 'i18next'
import { KeyRound, Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Turnstile } from '@/components/turnstile'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { completeOAuthRegistration } from '@/features/auth/api'
import { AuthLayout } from '@/features/auth/auth-layout'
import { OAuthCallbackScreen } from '@/features/auth/components/oauth-callback-screen'
import {
  OAUTH_BIND_CALLBACK_MESSAGE,
  OAUTH_BIND_RESULT_MESSAGE,
  REGISTRATION_CODE_LENGTH,
  REGISTRATION_CODE_REGEX,
} from '@/features/auth/constants'
import { useTurnstile } from '@/features/auth/hooks/use-turnstile'
import { sanitizeAuthRedirect } from '@/features/auth/lib/auth-redirect'
import {
  parseTelegramBindCallback,
  postTelegramBindResult,
  startOAuthBindResponseDeadline,
} from '@/features/auth/lib/oauth-bind-window'
import { getRegistrationFingerprint } from '@/features/auth/lib/registration-fingerprint'
import { api, applyAuthBundle, isAuthBundle } from '@/lib/api'
import { getServerErrorMessageKey } from '@/lib/server-error-message'

type OAuthRequestConfig = AxiosRequestConfig & {
  skipBusinessError?: boolean
}

interface OAuthBindingResult {
  type: typeof OAUTH_BIND_RESULT_MESSAGE
  provider: string
  state: string
  success: boolean
  message?: string
}

type PendingOAuthRegistration = {
  action: 'registration_code_required'
  ticket: string
  provider?: string
  provider_name?: string
  display_name?: string
  username?: string
  expires_at?: number
  redirect?: string
}

const OAUTH_PENDING_REGISTRATION_STORAGE_PREFIX =
  'auth:oauth-pending-registration:'

function getOAuthPendingRegistrationStorageKey(provider: string) {
  return `${OAUTH_PENDING_REGISTRATION_STORAGE_PREFIX}${provider}`
}

function parsePendingOAuthRegistration(
  data: unknown
): PendingOAuthRegistration | null {
  if (!data || typeof data !== 'object') return null
  const payload = data as Record<string, unknown>
  if (
    payload.action !== 'registration_code_required' ||
    typeof payload.ticket !== 'string' ||
    payload.ticket.trim() === ''
  ) {
    return null
  }
  return {
    action: 'registration_code_required',
    ticket: payload.ticket,
    provider:
      typeof payload.provider === 'string' ? payload.provider : undefined,
    provider_name:
      typeof payload.provider_name === 'string'
        ? payload.provider_name
        : undefined,
    display_name:
      typeof payload.display_name === 'string'
        ? payload.display_name
        : undefined,
    username:
      typeof payload.username === 'string' ? payload.username : undefined,
    expires_at:
      typeof payload.expires_at === 'number' ? payload.expires_at : undefined,
    redirect:
      typeof payload.redirect === 'string' ? payload.redirect : undefined,
  }
}

function readStoredPendingOAuthRegistration(
  provider: string
): PendingOAuthRegistration | null {
  if (typeof window === 'undefined') return null
  try {
    const raw = window.sessionStorage.getItem(
      getOAuthPendingRegistrationStorageKey(provider)
    )
    const pending = parsePendingOAuthRegistration(raw ? JSON.parse(raw) : null)
    if (!pending) return null
    if (
      pending.expires_at &&
      pending.expires_at <= Math.floor(Date.now() / 1000)
    ) {
      window.sessionStorage.removeItem(
        getOAuthPendingRegistrationStorageKey(provider)
      )
      return null
    }
    return pending
  } catch {
    return null
  }
}

function storePendingOAuthRegistration(
  provider: string,
  pending: PendingOAuthRegistration
) {
  if (typeof window === 'undefined') return
  try {
    window.sessionStorage.setItem(
      getOAuthPendingRegistrationStorageKey(provider),
      JSON.stringify(pending)
    )
  } catch {
    // Storage may be unavailable in privacy-restricted browsers.
  }
}

function clearStoredPendingOAuthRegistration(provider: string) {
  if (typeof window === 'undefined') return
  try {
    window.sessionStorage.removeItem(
      getOAuthPendingRegistrationStorageKey(provider)
    )
  } catch {
    // Storage may be unavailable in privacy-restricted browsers.
  }
}

function replaceOAuthCallbackUrl(redirect?: string) {
  if (typeof window === 'undefined') return
  const url = new URL(window.location.href)
  url.searchParams.delete('code')
  url.searchParams.delete('state')
  if (redirect) url.searchParams.set('redirect', redirect)
  window.history.replaceState(
    window.history.state,
    '',
    `${url.pathname}${url.search}${url.hash}`
  )
}

function OAuthCallback() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { provider } = useParams({ from: '/oauth/$provider' }) as {
    provider: string
  }
  const search = useSearch({ from: '/oauth/$provider' }) as {
    code?: string
    state?: string
    error?: string
    error_description?: string
    redirect?: string
    telegram_bind?: string
    flow_token?: string
    error_code?: string
  }
  const mode: 'login' | 'bind' =
    typeof window !== 'undefined' && window.opener ? 'bind' : 'login'
  const [pendingRegistration, setPendingRegistration] =
    useState<PendingOAuthRegistration | null>(() =>
      readStoredPendingOAuthRegistration(provider)
    )
  const [registrationCode, setRegistrationCode] = useState('')
  const [registrationCodeError, setRegistrationCodeError] = useState('')
  const [isCompletingRegistration, setIsCompletingRegistration] =
    useState(false)
  const {
    isTurnstileEnabled,
    turnstileSiteKey,
    turnstileToken,
    turnstileResetKey,
    handleTurnstileVerify,
    handleTurnstileError,
    resetTurnstile,
    validateTurnstile,
  } = useTurnstile()
  const turnstileReady = !isTurnstileEnabled || Boolean(turnstileToken)

  useEffect(() => {
    if (typeof window === 'undefined') return

    const code = search.code ?? ''
    const state = search.state ?? ''
    const telegramCallback =
      provider === 'telegram'
        ? parseTelegramBindCallback({
            telegram_bind: search.telegram_bind,
            flow_token: search.flow_token,
            error_code: search.error_code,
          })
        : null
    if (telegramCallback) {
      const opener = window.opener
      if (
        !postTelegramBindResult(
          telegramCallback,
          opener,
          window.location.origin
        )
      ) {
        toast.error(i18next.t('Telegram binding failed. Please try again.'))
        const closeTimeout = window.setTimeout(() => window.close(), 1500)
        return () => window.clearTimeout(closeTimeout)
      }
      window.close()
      return
    }

    if (mode === 'bind') {
      const opener = window.opener
      if (!opener || opener.closed) {
        toast.error(i18next.t('OAuth binding window is no longer available'))
        return
      }

      let cancelResultTimeout: () => void = () => undefined
      let delayedClose: number | undefined
      const handleBindingResult = (event: MessageEvent<unknown>) => {
        if (
          event.origin !== window.location.origin ||
          event.source !== opener
        ) {
          return
        }
        const result = event.data as Partial<OAuthBindingResult> | null
        if (
          !result ||
          result.type !== OAUTH_BIND_RESULT_MESSAGE ||
          result.provider !== provider ||
          result.state !== state
        ) {
          return
        }
        cancelResultTimeout()
        if (result.success) {
          toast.success(i18next.t('Binding successful!'))
          window.close()
          return
        }
        toast.error(result.message || i18next.t('OAuth failed'))
        delayedClose = window.setTimeout(() => window.close(), 1500)
      }

      window.addEventListener('message', handleBindingResult)
      cancelResultTimeout = startOAuthBindResponseDeadline(() => {
        toast.error(i18next.t('OAuth binding timed out. Please try again.'))
        delayedClose = window.setTimeout(() => window.close(), 1500)
      })
      opener.postMessage(
        {
          type: OAUTH_BIND_CALLBACK_MESSAGE,
          provider,
          code,
          state,
          error: search.error,
          errorDescription: search.error_description,
        },
        window.location.origin
      )
      return () => {
        window.removeEventListener('message', handleBindingResult)
        cancelResultTimeout()
        if (delayedClose !== undefined) window.clearTimeout(delayedClose)
      }
    }

    const safeNavigate = (target: unknown, fallback = '/dashboard') => {
      const href =
        sanitizeAuthRedirect(target, window.location.origin) ?? fallback
      void navigate({ href, replace: true })
    }

    if (pendingRegistration) return

    if (!code && !search.error) {
      toast.error(i18next.t('Missing code'))
      safeNavigate('/sign-in', '/sign-in')
      return
    }

    void (async () => {
      try {
        const config: OAuthRequestConfig = {
          params: {
            code: code || undefined,
            state,
            error: search.error,
            error_description: search.error_description,
          },
          headers: {
            'X-Registration-Fingerprint': getRegistrationFingerprint(),
          },
          skipBusinessError: true,
        }
        const response = await api.get(`/api/oauth/${provider}`, config)
        const pending = parsePendingOAuthRegistration(response.data?.data)
        if (response.data?.success && pending) {
          const pendingWithRedirect = { ...pending, redirect: search.redirect }
          storePendingOAuthRegistration(provider, pendingWithRedirect)
          setPendingRegistration(pendingWithRedirect)
          replaceOAuthCallbackUrl(search.redirect)
          return
        }
        if (response.data?.success && isAuthBundle(response.data?.data)) {
          applyAuthBundle(response.data.data)
          safeNavigate(search.redirect)
          toast.success(i18next.t('Signed in successfully!'))
          return
        }
        const messageKey = getServerErrorMessageKey(response.data)
        toast.error(
          messageKey
            ? i18next.t(messageKey)
            : response.data?.message || i18next.t('OAuth failed')
        )
      } catch (error: unknown) {
        const messageKey = getServerErrorMessageKey(error)
        const responseMessage = (
          error as { response?: { data?: { message?: string } } }
        ).response?.data?.message
        if (!messageKey) {
          toast.error(
            responseMessage ||
              (error instanceof Error
                ? error.message
                : i18next.t('OAuth failed'))
          )
        }
      }
      safeNavigate('/sign-in', '/sign-in')
    })()
  }, [
    mode,
    pendingRegistration,
    navigate,
    provider,
    search.code,
    search.error,
    search.error_code,
    search.error_description,
    search.flow_token,
    search.redirect,
    search.state,
    search.telegram_bind,
  ])

  const handleCompleteRegistration = async (
    event: React.FormEvent<HTMLFormElement>
  ) => {
    event.preventDefault()
    if (!pendingRegistration) return
    const normalizedCode = registrationCode.trim().toUpperCase()
    setRegistrationCode(normalizedCode)
    if (!REGISTRATION_CODE_REGEX.test(normalizedCode)) {
      const message = t('Enter a 20-character registration code')
      setRegistrationCodeError(message)
      toast.error(message)
      return
    }
    setRegistrationCodeError('')
    if (!validateTurnstile()) return
    setIsCompletingRegistration(true)
    try {
      const response = await completeOAuthRegistration(provider, {
        ticket: pendingRegistration.ticket,
        registrationCode: normalizedCode,
        turnstile: turnstileToken,
      })
      if (response.success && isAuthBundle(response.data)) {
        applyAuthBundle(response.data)
        clearStoredPendingOAuthRegistration(provider)
        const redirect = pendingRegistration.redirect ?? search.redirect
        const href =
          sanitizeAuthRedirect(redirect, window.location.origin) ?? '/dashboard'
        void navigate({ href, replace: true })
        toast.success(i18next.t('Signed in successfully!'))
        return
      }
      resetTurnstile()
      toast.error(response.message || t('OAuth registration failed'))
    } catch (error: unknown) {
      resetTurnstile()
      const responseMessage = (
        error as { response?: { data?: { message?: string } } }
      ).response?.data?.message
      toast.error(
        responseMessage ||
          (error instanceof Error
            ? error.message
            : t('OAuth registration failed'))
      )
    } finally {
      setIsCompletingRegistration(false)
    }
  }

  if (pendingRegistration) {
    const providerLabel = pendingRegistration.provider_name || provider
    const accountLabel =
      pendingRegistration.display_name || pendingRegistration.username
    return (
      <AuthLayout>
        <div className='w-full space-y-6'>
          <div className='flex flex-col items-center space-y-4 text-center'>
            <div className='bg-muted flex h-16 w-16 items-center justify-center rounded-2xl'>
              <KeyRound className='h-8 w-8' />
            </div>
            <div className='space-y-2'>
              <h2 className='text-2xl font-semibold tracking-tight'>
                {t('Complete account creation')}
              </h2>
              <p className='text-muted-foreground text-sm sm:text-base'>
                {accountLabel
                  ? t(
                      '{{provider}} verified {{account}}. Enter your registration code to create your account.',
                      { provider: providerLabel, account: accountLabel }
                    )
                  : t(
                      '{{provider}} verified. Enter your registration code to create your account.',
                      { provider: providerLabel }
                    )}
              </p>
            </div>
          </div>
          <form className='space-y-4' onSubmit={handleCompleteRegistration}>
            <div className='space-y-2'>
              <Label htmlFor='oauth-registration-code'>
                {t('Registration code')}
              </Label>
              <Input
                id='oauth-registration-code'
                autoFocus
                autoComplete='one-time-code'
                inputMode='text'
                maxLength={REGISTRATION_CODE_LENGTH}
                placeholder={t('Please enter your registration code')}
                value={registrationCode}
                onChange={(event) => {
                  setRegistrationCode(event.target.value.toUpperCase())
                  setRegistrationCodeError('')
                }}
              />
              {registrationCodeError ? (
                <p className='text-destructive text-xs' role='alert'>
                  {registrationCodeError}
                </p>
              ) : (
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'The registration code is 20 uppercase letters or numbers.'
                  )}
                </p>
              )}
            </div>
            {isTurnstileEnabled ? (
              <Turnstile
                siteKey={turnstileSiteKey}
                onVerify={handleTurnstileVerify}
                onExpire={resetTurnstile}
                onError={handleTurnstileError}
                resetKey={turnstileResetKey}
              />
            ) : null}
            <Button
              type='submit'
              className='w-full justify-center gap-2'
              disabled={
                isCompletingRegistration ||
                !REGISTRATION_CODE_REGEX.test(
                  registrationCode.trim().toUpperCase()
                ) ||
                !turnstileReady
              }
            >
              {isCompletingRegistration ? (
                <Loader2 className='h-4 w-4 animate-spin' />
              ) : null}
              {t('Create account')}
            </Button>
            <Button
              type='button'
              variant='ghost'
              className='w-full'
              onClick={() => {
                clearStoredPendingOAuthRegistration(provider)
                void navigate({ to: '/sign-in', replace: true })
              }}
            >
              {t('Back to sign in')}
            </Button>
          </form>
        </div>
      </AuthLayout>
    )
  }

  return <OAuthCallbackScreen provider={provider} mode={mode} />
}

export const Route = createFileRoute('/oauth/$provider')({
  component: OAuthCallback,
})
