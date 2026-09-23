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
import { localizeConsoleErrorText } from '@/lib/server-error-message'

function hasHan(text: string) {
  for (const char of text) {
    const code = char.codePointAt(0) ?? 0
    if (
      (code >= 0x3400 && code <= 0x9fff) ||
      (code >= 0xf900 && code <= 0xfaff)
    ) {
      return true
    }
  }
  return false
}

// pluginRejectionText keeps a translated conflict, including plugin names.
// Unknown English, such as a transport error, uses the dialog fallback.
export function pluginRejectionText(
  message: string | null | undefined,
  fallbackKey = 'The gateway rejected this plugin'
) {
  const trimmed = message?.trim() ?? ''
  if (!trimmed || !hasHan(trimmed)) {
    return localizeConsoleErrorText(trimmed, fallbackKey)
  }
  return trimmed
}

function responseMessage(error: unknown): string {
  if (!error || typeof error !== 'object' || !('response' in error)) return ''
  const response = (error as { response?: { data?: unknown } }).response
  const data = response?.data
  if (!data || typeof data !== 'object') return ''
  const message = (data as { message?: unknown }).message
  return typeof message === 'string' ? message.trim() : ''
}

// Prefer the server sentence. Axios puts English like "Request failed with
// status code 400" on Error.message even when the body is already translated.
export function pluginVisibleError(
  error: unknown,
  fallbackKey = 'The gateway rejected this plugin'
) {
  const fromBody = responseMessage(error)
  if (fromBody) return pluginRejectionText(fromBody, fallbackKey)
  if (error instanceof Error) {
    return pluginRejectionText(error.message, fallbackKey)
  }
  return pluginRejectionText('', fallbackKey)
}
