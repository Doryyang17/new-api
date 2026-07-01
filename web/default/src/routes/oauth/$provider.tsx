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
import { useCallback, useEffect, useRef, useState } from 'react'
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
  OAUTH_BIND_STORAGE_KEY,
  REGISTRATION_CODE_LENGTH,
  REGISTRATION_CODE_REGEX,
} from '@/features/auth/constants'
import { useTurnstile } from '@/features/auth/hooks/use-turnstile'
import { getRegistrationFingerprint } from '@/features/auth/lib/registration-fingerprint'
import { api, getSelf } from '@/lib/api'
import { useAuthStore, type AuthUser } from '@/stores/auth-store'

type OAuthRequestConfig = AxiosRequestConfig & {
  skipBusinessError?: boolean
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
    /* empty */
  }
}

function clearStoredPendingOAuthRegistration(provider: string) {
  if (typeof window === 'undefined') return
  try {
    window.sessionStorage.removeItem(
      getOAuthPendingRegistrationStorageKey(provider)
    )
  } catch {
    /* empty */
  }
}

function replaceOAuthCallbackUrl(redirect?: string) {
  if (typeof window === 'undefined') return
  const url = new URL(window.location.href)
  url.searchParams.delete('code')
  url.searchParams.delete('state')
  if (redirect) {
    url.searchParams.set('redirect', redirect)
  }
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
    redirect?: string
  }
  const [mode, setMode] = useState<'login' | 'bind'>(() => {
    if (typeof window === 'undefined') return 'login'
    return window.opener ? 'bind' : 'login'
  })
  const [pendingRegistration, setPendingRegistration] =
    useState<PendingOAuthRegistration | null>(() =>
      readStoredPendingOAuthRegistration(provider)
    )
  const [registrationCode, setRegistrationCode] = useState('')
  const [registrationCodeError, setRegistrationCodeError] = useState('')
  const [isCompletingRegistration, setIsCompletingRegistration] =
    useState(false)
  const hasProcessedOAuthRef = useRef(false)
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

  const safeNavigate = useCallback(
    (target: string) => {
      navigate({ to: target as never, replace: true })
      if (typeof window !== 'undefined') {
        setTimeout(() => {
          const normalizedTarget = target.startsWith('/')
            ? target
            : `/${target}`
          const currentPath = window.location.pathname + window.location.search
          if (
            currentPath !== normalizedTarget &&
            currentPath !== `${normalizedTarget}/`
          ) {
            window.location.replace(target)
          }
        }, 100)
      }
    },
    [navigate]
  )

  const storeLoginUser = useCallback((user: AuthUser) => {
    useAuthStore.getState().auth.setUser(user)
    try {
      if (typeof window !== 'undefined' && user.id != null) {
        window.localStorage.setItem('uid', String(user.id))
      }
    } catch (_error) {
      void _error
    }
  }, [])

  const finalizeLogin = useCallback(async (): Promise<boolean> => {
    try {
      const selfResponse = (await getSelf()) as {
        success?: boolean
        data?: AuthUser | null
      }
      if (selfResponse?.success && selfResponse.data) {
        storeLoginUser(selfResponse.data)
        return true
      }
    } catch (_error) {
      void _error
    }
    return false
  }, [storeLoginUser])

  const redirectAfterLogin = useCallback(
    (target?: string) => {
      const to =
        target ||
        pendingRegistration?.redirect ||
        search?.redirect ||
        '/dashboard'
      safeNavigate(to)
      toast.success(i18next.t('Signed in successfully!'))
    },
    [pendingRegistration?.redirect, safeNavigate, search?.redirect]
  )

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
      const res = await completeOAuthRegistration(provider, {
        ticket: pendingRegistration.ticket,
        registrationCode: normalizedCode,
        turnstile: turnstileToken,
      })
      if (res?.success) {
        clearStoredPendingOAuthRegistration(provider)
        const loginUser = (res.data ?? null) as AuthUser | null
        if (loginUser) {
          storeLoginUser(loginUser)
        } else if (!(await finalizeLogin())) {
          toast.error(t('OAuth registration failed'))
          return
        }
        redirectAfterLogin()
        return
      }
      resetTurnstile()
      toast.error(res?.message || t('OAuth registration failed'))
    } catch (error) {
      resetTurnstile()
      const message = ((error &&
        typeof error === 'object' &&
        'response' in error &&
        (error as { response?: { data?: { message?: string } } }).response?.data
          ?.message) ??
        (error instanceof Error ? error.message : undefined) ??
        t('OAuth registration failed')) as string
      toast.error(message)
    } finally {
      setIsCompletingRegistration(false)
    }
  }

  useEffect(() => {
    if (typeof window === 'undefined') return
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setMode(window.opener ? 'bind' : 'login')
  }, [])

  useEffect(() => {
    if (pendingRegistration) {
      hasProcessedOAuthRef.current = true
      return
    }
    if (hasProcessedOAuthRef.current) return
    hasProcessedOAuthRef.current = true

    ;(async () => {
      if (!search?.code) {
        toast.error(i18next.t('Missing code'))
        safeNavigate('/sign-in')
        return
      }
      const isBindingFlow =
        typeof window !== 'undefined' ? Boolean(window.opener) : mode === 'bind'
      if (isBindingFlow && mode !== 'bind') {
        setMode('bind')
      } else if (!isBindingFlow && mode !== 'login') {
        setMode('login')
      }
      const notifyBindingResult = (status: 'success' | 'error') => {
        if (typeof window === 'undefined') return
        try {
          window.localStorage.setItem(
            OAUTH_BIND_STORAGE_KEY,
            JSON.stringify({
              provider,
              status,
              timestamp: Date.now(),
            })
          )
        } catch (_error) {
          // ignore storage write failures
          void _error
        }
      }

      const closeBindingWindow = () => {
        if (typeof window === 'undefined') return
        window.close()
        setTimeout(() => {
          if (!window.closed) {
            window.location.replace('/_authenticated/profile/')
          }
        }, 200)
      }

      const handleBindingFailure = (message: string) => {
        notifyBindingResult('error')
        toast.error(message)
      }

      const handleLoginFailure = async (message: string) => {
        if (await finalizeLogin()) {
          redirectAfterLogin()
          return
        }
        toast.error(message)
        safeNavigate('/sign-in')
      }

      try {
        const config: OAuthRequestConfig = {
          params: { code: search.code, state: search.state },
          headers: {
            'X-Registration-Fingerprint': getRegistrationFingerprint(),
          },
          skipBusinessError: true,
        }
        const res = await api.get(`/api/oauth/${provider}`, config)
        if (res?.data?.success) {
          const { message } = res.data
          const pending = parsePendingOAuthRegistration(res.data?.data)
          if (pending) {
            const pendingWithRedirect = {
              ...pending,
              redirect: search?.redirect,
            }
            storePendingOAuthRegistration(provider, pendingWithRedirect)
            setPendingRegistration(pendingWithRedirect)
            replaceOAuthCallbackUrl(search?.redirect)
            return
          }
          const loginUser = (res.data?.data ?? null) as AuthUser | null
          // Check if this is a bind operation
          if (message === 'bind') {
            toast.success(i18next.t('Binding successful!'))
            notifyBindingResult('success')
            if (isBindingFlow) {
              // Close the callback window if we opened a new tab for binding
              closeBindingWindow()
            } else {
              safeNavigate('/_authenticated/profile/')
            }
            return
          }
          // Otherwise it's a login, use payload user if available
          if (loginUser) {
            storeLoginUser(loginUser)
            redirectAfterLogin()
            return
          }
          if (await finalizeLogin()) {
            redirectAfterLogin()
            return
          }
          toast.error(res?.data?.message || i18next.t('OAuth failed'))
          safeNavigate('/sign-in')
          return
        }
        const message = res?.data?.message || 'OAuth failed'
        if (!res?.data?.success && !isBindingFlow) {
          // When logging in with an already bound GitHub account, backend may return this message
          if (message === '该 GitHub 账户已被绑定') {
            if (await finalizeLogin()) {
              redirectAfterLogin()
              return
            }
          }
        }
        if (isBindingFlow) {
          handleBindingFailure(message)
        } else {
          await handleLoginFailure(message)
        }
        return
      } catch (error) {
        const message = ((error &&
          typeof error === 'object' &&
          'response' in error &&
          (error as { response?: { data?: { message?: string } } }).response
            ?.data?.message) ??
          (error instanceof Error ? error.message : undefined) ??
          'OAuth failed') as string

        if (isBindingFlow) {
          handleBindingFailure(message)
          return
        }
        await handleLoginFailure(message)
        return
      }
    })()
  }, [
    finalizeLogin,
    mode,
    pendingRegistration,
    provider,
    redirectAfterLogin,
    safeNavigate,
    search,
    storeLoginUser,
  ])

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
                safeNavigate('/sign-in')
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
