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
import { buildOAuthCallbackUrl } from '@/features/system-settings/auth/oauth-callback-url'

// ============================================================================
// OAuth URL Builders
// ============================================================================

export interface CustomOAuthBinding {
  provider_id: number
  provider_name: string
  provider_slug: string
  provider_icon: string
  provider_user_id: string
}

export function indexCustomOAuthBindings(
  bindings: CustomOAuthBinding[]
): Map<number, CustomOAuthBinding> {
  return new Map(bindings.map((binding) => [binding.provider_id, binding]))
}

/**
 * Build GitHub OAuth URL
 */
export function buildGitHubOAuthUrl(
  clientId: string,
  state: string,
  serverAddress?: string
): string {
  const url = new URL('https://github.com/login/oauth/authorize')
  url.searchParams.set('client_id', clientId)
  url.searchParams.set('state', state)
  url.searchParams.set('scope', 'user:email')
  url.searchParams.set(
    'redirect_uri',
    buildOAuthCallbackUrl(serverAddress ?? '', 'github', '')
  )
  return url.toString()
}

/**
 * Build Discord OAuth URL
 */
export function buildDiscordOAuthUrl(
  clientId: string,
  state: string,
  serverAddress?: string
): string {
  const url = new URL('https://discord.com/oauth2/authorize')
  url.searchParams.set('client_id', clientId)
  url.searchParams.set(
    'redirect_uri',
    buildOAuthCallbackUrl(serverAddress ?? '', 'discord', '')
  )
  url.searchParams.set('response_type', 'code')
  url.searchParams.set('scope', 'identify+openid')
  url.searchParams.set('state', state)
  return url.toString()
}

/**
 * Build OIDC OAuth URL
 */
export function buildOIDCOAuthUrl(
  authUrl: string,
  clientId: string,
  state: string,
  serverAddress?: string
): string {
  const url = new URL(authUrl)
  url.searchParams.set('client_id', clientId)
  url.searchParams.set(
    'redirect_uri',
    buildOAuthCallbackUrl(serverAddress ?? '', 'oidc', '')
  )
  url.searchParams.set('response_type', 'code')
  url.searchParams.set('scope', 'openid profile email')
  url.searchParams.set('state', state)
  return url.toString()
}

/**
 * Build LinuxDO OAuth URL
 */
export function buildLinuxDOOAuthUrl(
  clientId: string,
  state: string,
  serverAddress?: string
): string {
  const url = new URL('https://connect.linux.do/oauth2/authorize')
  url.searchParams.set('response_type', 'code')
  url.searchParams.set('client_id', clientId)
  url.searchParams.set(
    'redirect_uri',
    buildOAuthCallbackUrl(serverAddress ?? '', 'linuxdo', '')
  )
  url.searchParams.set('state', state)
  return url.toString()
}

export function buildCustomOAuthUrl(options: {
  authorizationEndpoint: string
  clientId: string
  slug: string
  state: string
  scopes?: string
  serverAddress?: string
  fallbackOrigin?: string
}): string {
  const url = new URL(options.authorizationEndpoint)
  url.searchParams.set('client_id', options.clientId)
  url.searchParams.set(
    'redirect_uri',
    buildOAuthCallbackUrl(options.serverAddress ?? '', options.slug, '')
  )
  url.searchParams.set('response_type', 'code')
  url.searchParams.set('state', options.state)
  if (options.scopes) {
    url.searchParams.set('scope', options.scopes)
  }
  return url.toString()
}
