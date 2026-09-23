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

import { ERROR_MESSAGES } from '../../constants'

const KNOWN_ERROR_MESSAGES = new Set<string>(Object.values(ERROR_MESSAGES))

export function getPlaygroundDisplayError(error: string): string {
  const trimmed = error.trim()
  if (!trimmed) return localizeConsoleErrorText('', 'Request failed')

  if (KNOWN_ERROR_MESSAGES.has(trimmed)) {
    return localizeConsoleErrorText(trimmed)
  }

  const connectionClosedSuffix = `: ${ERROR_MESSAGES.CONNECTION_CLOSED}`
  if (trimmed.endsWith(connectionClosedSuffix)) {
    const prefix = trimmed.slice(0, -ERROR_MESSAGES.CONNECTION_CLOSED.length)
    return `${prefix}${localizeConsoleErrorText(ERROR_MESSAGES.CONNECTION_CLOSED)}`
  }

  return localizeConsoleErrorText(trimmed)
}
