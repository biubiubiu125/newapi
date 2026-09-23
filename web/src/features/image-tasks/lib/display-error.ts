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

import { localizeConsoleErrorText } from '@/lib/server-error-message'

import { ImageTaskRequestError } from '../api'

export function getImageTaskDisplayError(
  error: unknown,
  fallbackKey = 'Request failed'
): string {
  if (error instanceof ImageTaskRequestError) {
    return localizeConsoleErrorText(error.message, fallbackKey)
  }
  if (error instanceof Error && error.message.trim()) {
    return localizeConsoleErrorText(error.message, fallbackKey)
  }
  return t(fallbackKey)
}

export function getImageTaskStoredErrorLabel(
  error?: { code?: string; message?: string } | null,
  fallbackKey = 'Request failed'
): string {
  if (!error) return t(fallbackKey)
  const message = error.message?.trim() ?? ''
  if (message) return localizeConsoleErrorText(message, fallbackKey)
  const code = error.code?.trim() ?? ''
  if (code) return localizeConsoleErrorText(code, fallbackKey)
  return t(fallbackKey)
}
