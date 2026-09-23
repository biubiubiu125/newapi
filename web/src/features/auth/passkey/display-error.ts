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
import i18n, { t } from 'i18next'

import {
  getServerErrorMessageKey,
  localizeConsoleErrorText,
  serverErrorPayload,
} from '@/lib/server-error-message'

const HAN = /[\u4e00-\u9fff]/

export function isPasskeyCancelledError(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'NotAllowedError'
}

export function getPasskeyDisplayError(
  error: unknown,
  fallbackKey = 'Passkey login failed'
): string {
  if (isPasskeyCancelledError(error)) {
    return t('Passkey login was cancelled or timed out')
  }

  const mapped = getServerErrorMessageKey(error)
  if (mapped) return t(mapped)

  const payload = serverErrorPayload(error)
  const raw =
    (typeof payload?.message === 'string' && payload.message.trim()) ||
    (error instanceof Error ? error.message.trim() : '')

  if (!raw) return t(fallbackKey)

  const localized = localizeConsoleErrorText(raw, fallbackKey)
  if (localized !== raw) return localized
  if (i18n.exists(raw)) return t(raw)
  if (HAN.test(raw)) return raw
  return t(fallbackKey)
}
