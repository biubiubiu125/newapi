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
import { t } from 'i18next'

import { currentConsoleFailureText } from './console-failure-text'

const TRANSPORT_ERROR_KEYS = [
  {
    test: (value: string) => value === 'Network Error',
    key: 'Unable to connect to the server',
  },
  {
    test: (value: string) => /^timeout of \d+ms exceeded$/i.test(value),
    key: 'Request timed out',
  },
  {
    test: (value: string) =>
      /^Request failed with status code \d+$/i.test(value),
    key: 'Request failed',
  },
  {
    test: (value: string) =>
      value === 'Failed to fetch' ||
      value === 'Load failed' ||
      value === 'NetworkError when attempting to fetch resource.',
    key: 'Unable to connect to the server',
  },
] as const

const serverErrorMessageKeys = {
  AUTH_SESSION_LIMIT:
    'Too many active login sessions. On a device where you are already signed in, open Login sessions and use “Sign out other sessions” to revoke them. If you cannot access a signed-in device, reset your password to sign out all sessions.',
  AUTH_SESSION_ISSUANCE_LIMIT:
    'Too many login sessions were created recently. Please wait for the rolling window to pass, then try again.',
  AUTH_SESSION_MISMATCH: 'Login session mismatch. Please sign in again.',
  AUTH_REFRESH_RACE: 'Login session is being refreshed. Please retry.',
  AUTH_TOKEN_EXPIRED: 'Login flow expired. Please sign in again.',
  AUTH_SESSION_REVOKED: 'This login session has been revoked.',
  AUTH_UNAUTHORIZED: 'Session expired!',
  AUTH_INTERNAL_ERROR: 'Something went wrong!',
  TELEGRAM_BIND_DISABLED: 'Telegram binding is disabled.',
  TELEGRAM_BIND_INVALID_REQUEST:
    'The Telegram authorization request is invalid or expired.',
  TELEGRAM_BIND_FLOW_INVALID:
    'This Telegram binding request has expired or has already been used.',
  TELEGRAM_BIND_SESSION_INVALID:
    'The login session that started this Telegram binding is no longer valid.',
  TELEGRAM_BIND_ALREADY_BOUND: 'This Telegram account is already bound.',
  TELEGRAM_BIND_USER_DELETED: 'This user account no longer exists.',
  TELEGRAM_BIND_USER_DISABLED: 'This user account is disabled.',
  TELEGRAM_BIND_INTERNAL_ERROR: 'Telegram binding failed. Please try again.',
} as const

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object'
}

export function serverErrorPayload(
  value: unknown
): Record<string, unknown> | null {
  if (!isRecord(value)) return null

  const response = value.response
  if (isRecord(response) && isRecord(response.data)) {
    return response.data
  }
  return value
}

export function getServerErrorMessageKey(value: unknown): string | null {
  const payload = serverErrorPayload(value)
  if (!payload || typeof payload.code !== 'string') return null

  return (
    serverErrorMessageKeys[
      payload.code as keyof typeof serverErrorMessageKeys
    ] ?? null
  )
}

export function localizeConsoleErrorText(
  message: string | null | undefined,
  fallbackKey = 'Request failed'
): string {
  const trimmed = message?.trim() ?? ''
  if (!trimmed) return t(fallbackKey)

  for (const rule of TRANSPORT_ERROR_KEYS) {
    if (rule.test(trimmed)) return t(rule.key)
  }

  const translated = t(trimmed)
  if (translated !== trimmed) return translated
  return currentConsoleFailureText(trimmed, fallbackKey)
}

export function requireServerSuccess<T>(response: {
  success?: boolean
  message?: string
  data?: T
}): T {
  if (response == null || response.success === false) {
    throw createServerError(response, response?.message)
  }
  if (response.data === undefined) {
    throw createServerError(response, response.message)
  }
  return response.data
}

export function createServerError(value: unknown, fallback?: string): Error {
  const payload = serverErrorPayload(value)
  const key = getServerErrorMessageKey(value)
  const message =
    (typeof payload?.message === 'string' && payload.message.trim()
      ? payload.message
      : undefined) ||
    fallback ||
    t('Something went wrong!')
  return new Error(key || message, { cause: value })
}
