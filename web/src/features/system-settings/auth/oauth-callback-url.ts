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
export function resolveOAuthSiteUrl(
  serverAddress: string,
  fallback: string
): string {
  const normalized = serverAddress.trim().replace(/\/+$/, '')
  if (!normalized) {
    return fallback
  }
  try {
    const parsed = new URL(normalized)
    if (!parsed.protocol || !parsed.host) {
      return fallback
    }
  } catch {
    return fallback
  }
  return normalized
}

export function buildOAuthCallbackUrl(
  serverAddress: string,
  callbackPath: string,
  fallback: string
): string {
  const siteUrl = resolveOAuthSiteUrl(serverAddress, fallback)
  if (!siteUrl) {
    throw new Error('OAuth server address is not configured')
  }
  return `${siteUrl}/oauth/${callbackPath.replace(/^\/+/, '')}`
}
