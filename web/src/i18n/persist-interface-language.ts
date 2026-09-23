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
import { AxiosError } from 'axios'
import i18n from 'i18next'

import { getSavedLanguage } from '@/features/auth/lib/auth-redirect'
import { normalizeInterfaceLanguage } from '@/i18n/languages'
import { getServerErrorDisplayMessage } from '@/lib/handle-server-error'
import { localizeConsoleErrorText } from '@/lib/server-error-message'
import { api } from '@/lib/http-client'
import { useAuthStore, type AuthUser } from '@/stores/auth-store'

function readUserSettingRecord(
  setting: AuthUser['setting']
): Record<string, unknown> {
  if (!setting) return {}
  if (typeof setting === 'object') {
    return { ...setting }
  }
  try {
    const parsed = JSON.parse(setting) as unknown
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return { ...(parsed as Record<string, unknown>) }
    }
  } catch {
    return {}
  }
  return {}
}

function persistFailureMessage(message: unknown): string {
  return typeof message === 'string' && message.trim()
    ? localizeConsoleErrorText(message.trim())
    : i18n.t('Failed to update settings')
}

export function persistInterfaceLanguageErrorMessage(
  error: unknown,
  fallback: string
): string {
  if (error instanceof AxiosError) {
    return getServerErrorDisplayMessage(error)
  }
  if (error instanceof Error && error.message.trim()) {
    return localizeConsoleErrorText(error.message)
  }
  return fallback
}

export async function persistSignedInInterfaceLanguage(
  next: string
): Promise<void> {
  const response = await api.put<{ success?: boolean; message?: string }>(
    '/api/user/self',
    { language: next },
    { skipBusinessError: true, skipErrorHandler: true }
  )
  if (!response.data?.success) {
    throw new Error(persistFailureMessage(response.data?.message))
  }

  const auth = useAuthStore.getState().auth
  if (!auth.user) return

  auth.setUser({
    ...auth.user,
    language: next,
    setting: JSON.stringify({
      ...readUserSettingRecord(auth.user.setting),
      language: next,
    }),
  })
}

export function displayedInterfaceLanguage(input: {
  user?: AuthUser | null
  profileLanguage?: string | null
  fallback?: string | null
}): string {
  const fromAuth = input.user ? getSavedLanguage(input.user) : undefined
  const fromProfile =
    typeof input.profileLanguage === 'string' && input.profileLanguage.trim()
      ? input.profileLanguage
      : undefined
  return normalizeInterfaceLanguage(fromAuth || fromProfile || input.fallback)
}

const inflightLanguageSync = new Map<number, Promise<void>>()

export async function syncSignedInInterfaceLanguage(
  user?: AuthUser | null
): Promise<void> {
  if (!user) return

  const inflight = inflightLanguageSync.get(user.id)
  if (inflight) {
    await inflight
    return
  }

  const run = (async () => {
    const latest = useAuthStore.getState().auth.user ?? user
    const saved = getSavedLanguage(latest) ?? getSavedLanguage(user)
    if (saved) {
      if (saved !== i18n.language) {
        await i18n.changeLanguage(saved)
      }
      return
    }
    try {
      await persistSignedInInterfaceLanguage(
        normalizeInterfaceLanguage(i18n.language)
      )
    } catch {
      // Existing accounts may have no saved language. A failed persist
      // must not block login or session restore; the header switcher retries.
    }
  })().finally(() => {
    inflightLanguageSync.delete(user.id)
  })

  inflightLanguageSync.set(user.id, run)
  await run
}
