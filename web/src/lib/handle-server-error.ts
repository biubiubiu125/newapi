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
import i18next from 'i18next'
import { toast } from 'sonner'

import {
  getServerErrorMessageKey,
  localizeConsoleErrorText,
  serverErrorPayload,
} from '@/lib/server-error-message'

function readErrorText(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  return trimmed || undefined
}

export function getServerErrorDisplayMessage(error: unknown): string {
  const messageKey = getServerErrorMessageKey(error)
  if (messageKey) return i18next.t(messageKey)

  if (
    error &&
    typeof error === 'object' &&
    'status' in error &&
    Number(error.status) === 204
  ) {
    return i18next.t('Content not found.')
  }

  if (error instanceof AxiosError && error.response?.status === 401) {
    return i18next.t('Session expired!')
  }

  const payload = serverErrorPayload(error)
  const message =
    readErrorText(payload?.message) || readErrorText(payload?.title)
  if (message) return localizeConsoleErrorText(message)

  if (error instanceof AxiosError) {
    if (!error.response) {
      return localizeConsoleErrorText(
        error.message,
        'Unable to connect to the server'
      )
    }
    return i18next.t('Something went wrong!')
  }

  if (error instanceof Error && error.message.trim()) {
    return localizeConsoleErrorText(error.message)
  }

  return i18next.t('Something went wrong!')
}

export function getUnhandledConsoleErrorMessage(error: unknown): string | null {
  if (error instanceof AxiosError && error.config?.skipErrorHandler !== true) {
    return null
  }
  return getServerErrorDisplayMessage(error)
}

export function toastUnhandledConsoleError(error: unknown): void {
  const message = getUnhandledConsoleErrorMessage(error)
  if (message) toast.error(message)
}

export function handleServerError(error: unknown) {
  toast.error(getServerErrorDisplayMessage(error))
}

export function notifyQueryCacheError(
  error: unknown,
  notify: (message: string) => void,
  goToServerError: () => void
): void {
  if (error instanceof AxiosError && error.response?.status === 500) {
    notify(getServerErrorDisplayMessage(error))
    goToServerError()
  }
}
