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

import { getServerErrorDisplayMessage } from '@/lib/handle-server-error'

export function getOAuthLoginDisplayError(
  error: unknown,
  fallbackKey = 'Login failed',
  fallbackParams?: Record<string, unknown>
): string {
  const fallback = t(fallbackKey, fallbackParams)
  const message = getServerErrorDisplayMessage(error).trim()
  if (!message) {
    return fallback
  }
  const generic = t('Something went wrong!')
  if (message === generic || message === 'Something went wrong!') {
    return fallback
  }
  return message
}
