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
import axios from 'axios'
import i18n from 'i18next'

import { api, refreshAuthentication, type RefreshOutcome } from '@/lib/api'
import { buildGitHubOAuthUrl } from '@/lib/oauth'
import { createServerError } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { sanitizeAuthRedirect } from './lib/auth-redirect'
import {
  clearPasswordEncryptionCache,
  encryptPassword,
  encryptPasswordFields,
} from './lib/password-encryption'
import { getAffiliateCode } from './lib/storage'
import type { TelegramAuthorization } from './lib/telegram-login'
import type {
  LoginPayload,
  LoginResponse,
  Login2FAResponse,
  TwoFAPayload,
  RegisterPayload,
  ApiResponse,
} from './types'

// ============================================================================
// Authentication APIs
// ============================================================================

// ----------------------------------------------------------------------------
// Login & Logout
// ----------------------------------------------------------------------------

// User login with username and password
export async function login(payload: LoginPayload): Promise<LoginResponse> {
  const turnstile = payload.turnstile ?? ''
  try {
    let passwordFields:
      | { password: string }
      | { password_encrypted: string; encryption_key_id: string }
    if (payload.passwordEncryptionEnabled) {
      const encryptedPassword = await encryptPassword(payload.password)
      passwordFields = {
        password_encrypted: encryptedPassword.password_encrypted,
        encryption_key_id: encryptedPassword.encryption_key_id,
      }
    } else {
      passwordFields = { password: payload.password }
    }
    const res = await api.post<LoginResponse>(
      `/api/user/login?turnstile=${turnstile}`,
      {
        username: payload.username,
        ...passwordFields,
      },
      { skipAuthRefresh: true }
    )
    if (payload.passwordEncryptionEnabled && !res.data?.success) {
      clearPasswordEncryptionCache()
    }
    return res.data
  } catch (error: unknown) {
    if (payload.passwordEncryptionEnabled) {
      clearPasswordEncryptionCache()
    }
    throw error
  }
}

// Two-factor authentication login
export async function login2fa(payload: TwoFAPayload) {
  const res = await api.post<Login2FAResponse>('/api/user/login/2fa', payload, {
    skipAuthRefresh: true,
    skipBusinessError: true,
    skipErrorHandler: true,
  })
  return res.data
}

interface LogoutRuntime {
  getExpectedSID: () => string | undefined
  request: (expectedSID?: string) => Promise<ApiResponse>
  refresh: () => Promise<RefreshOutcome>
}

export async function executeLogout(
  runtime: LogoutRuntime,
  allowMismatchRecovery = true
): Promise<ApiResponse> {
  try {
    return await runtime.request(runtime.getExpectedSID())
  } catch (error: unknown) {
    const code = axios.isAxiosError(error)
      ? error.response?.data?.code
      : undefined
    if (
      allowMismatchRecovery &&
      axios.isAxiosError(error) &&
      error.response?.status === 409 &&
      code === 'AUTH_SESSION_MISMATCH'
    ) {
      const outcome = await runtime.refresh()
      if (outcome.kind === 'authenticated') {
        return executeLogout(runtime, false)
      }
      if (outcome.kind === 'anonymous') {
        return { success: true, message: '' }
      }
    }
    throw error
  }
}

// User logout
export async function logout(): Promise<ApiResponse> {
  return executeLogout({
    getExpectedSID: () => useAuthStore.getState().auth.session?.sid,
    request: async (sid) => {
      const res = await api.post('/api/user/auth/logout', undefined, {
        headers: sid ? { 'X-Auth-Session': sid } : undefined,
        skipAuthRefresh: true,
        skipErrorHandler: true,
      })
      return res.data
    },
    refresh: refreshAuthentication,
  })
}

// ----------------------------------------------------------------------------
// Password Management
// ----------------------------------------------------------------------------

// Send password reset email
export async function sendPasswordResetEmail(
  email: string,
  turnstile?: string
): Promise<ApiResponse> {
  const res = await api.get('/api/reset_password', {
    params: { email, turnstile },
  })
  return res.data
}

// ----------------------------------------------------------------------------
// OAuth
// ----------------------------------------------------------------------------

// Start GitHub OAuth flow
export async function githubOAuthStart(
  clientId: string,
  state: string,
  serverAddress?: string
) {
  const url = buildGitHubOAuthUrl(clientId, state, serverAddress)
  window.open(url)
}

// Get OAuth state for CSRF protection
export async function getOAuthState(affiliateCode?: string): Promise<string> {
  const aff = affiliateCode?.trim() || getAffiliateCode()
  const res = await api.get('/api/oauth/state', { params: { aff } })
  if (res.data?.success) return res.data.data
  return ''
}

export async function createOAuthFlow(
  provider: string,
  intent: 'login' | 'bind',
  affiliateCode?: string,
  redirectTo?: string
): Promise<string> {
  const aff =
    intent === 'login' ? affiliateCode?.trim() || getAffiliateCode() : ''
  const redirect =
    intent === 'login' && typeof window !== 'undefined'
      ? (sanitizeAuthRedirect(redirectTo, window.location.origin) ?? undefined)
      : undefined
  const res = await api.post(
    '/api/oauth/state',
    {
      provider,
      intent,
      aff: aff || undefined,
      redirect,
      language: i18n.language,
    },
    {
      skipAuthRefresh: intent === 'login',
      skipBusinessError: true,
      skipErrorHandler: true,
    }
  )
  if (res.data?.success) {
    if (typeof res.data.data === 'string') return res.data.data
    if (typeof res.data.data?.flow_token === 'string') {
      return res.data.data.flow_token
    }
  }
  throw createServerError(res.data, i18n.t('Failed to initialize OAuth'))
}

// WeChat login by authorization code
export async function wechatLoginByCode(
  code: string,
  aff?: string
): Promise<ApiResponse> {
  const state = await createOAuthFlow('wechat', 'login', aff)
  const res = await api.get('/api/oauth/wechat', {
    params: { code, aff: aff || undefined, state },
    skipAuthRefresh: true,
    skipBusinessError: true,
    skipErrorHandler: true,
  })
  return res.data
}

export async function telegramLogin(
  authorization: TelegramAuthorization
): Promise<ApiResponse> {
  const res = await api.get('/api/oauth/telegram/login', {
    params: authorization,
    disableDuplicate: true,
    skipAuthRefresh: true,
    skipBusinessError: true,
    skipErrorHandler: true,
  })
  return res.data
}

// ----------------------------------------------------------------------------
// Registration
// ----------------------------------------------------------------------------

// User registration
export async function register(payload: RegisterPayload): Promise<ApiResponse> {
  const body = await encryptPasswordFields(payload, ['password'])
  const res = await api.post(`/api/user/register`, body, {
    params: { turnstile: payload.turnstile ?? '' },
  })
  return res.data
}

// Send email verification code
export async function sendEmailVerification(
  email: string,
  turnstile?: string
): Promise<ApiResponse> {
  const res = await api.get('/api/verification', {
    params: { email, turnstile },
  })
  return res.data
}

// Bind email to OAuth account
export async function bindEmail(
  email: string,
  code: string
): Promise<ApiResponse> {
  const res = await api.post('/api/oauth/email/bind', {
    email,
    code,
  })
  return res.data
}
