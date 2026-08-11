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
import { DEFAULT_LOGO } from './constants'

function normalizeBaseUrl(value?: string | null): string {
  return (value || '').trim().replace(/\/+$/, '')
}

function isSameOrigin(value: string): boolean {
  if (typeof window === 'undefined') return false
  try {
    return new URL(value).origin === window.location.origin
  } catch {
    return false
  }
}

export function resolveAssetUrl(
  url?: string | null,
  fallback = DEFAULT_LOGO,
  serverAddress?: string | null
): string {
  const raw = (url || '').trim()
  const fallbackUrl = fallback || DEFAULT_LOGO
  if (!raw) return fallbackUrl

  if (/^https?:\/\//i.test(raw) || raw.startsWith('data:')) return raw

  const base = normalizeBaseUrl(serverAddress)
  const isBundledAsset =
    raw === fallbackUrl || raw === DEFAULT_LOGO || raw === '/favicon.ico'
  if (
    base &&
    raw.startsWith('/') &&
    !isBundledAsset &&
    (typeof window === 'undefined' || isSameOrigin(base))
  ) {
    return `${base}${raw}`
  }

  if (typeof window !== 'undefined') {
    try {
      return new URL(raw, window.location.origin).href
    } catch {
      return fallbackUrl
    }
  }

  return raw.startsWith('/') ? raw : fallbackUrl
}
