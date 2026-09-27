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
import { useState, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { clearAuthentication } from '@/lib/api'
import { createOAuthFlow, logout, createOAuthAuthorization } from '../api'
import { buildGitHubOAuthUrl, buildDiscordOAuthUrl, buildOIDCOAuthUrl, buildLinuxDOOAuthUrl } from '../lib/oauth'
import { getOAuthSessionStorage, rememberOAuthLoginRedirect } from '../lib/oauth-callback-mode'
import type { SystemStatus, CustomOAuthProviderInfo } from '../types'
import { createServerError } from '@/lib/server-error-message'
import { handleServerError } from '@/lib/handle-server-error'
import { AuthOperationError } from '@/lib/secure-verification'
type OAuthLoginOptions = {
  redirectTo?: string
  affiliateCode?: string
}

export function useOAuthLogin(
  status: SystemStatus | null,
  options: OAuthLoginOptions = {}
) {
  const { t } = useTranslation()
  const { redirectTo, affiliateCode } = options
  const [isLoading, setIsLoading] = useState(false)
  const [githubButtonText, setGithubButtonText] = useState('')
  const [githubButtonDisabled, setGithubButtonDisabled] = useState(false)
  const githubTimeoutRef = useRef<NodeJS.Timeout | null>(null)

  useEffect(() => {
    setGithubButtonText(t('Continue with GitHub'))

    return () => {
      if (githubTimeoutRef.current) {
        clearTimeout(githubTimeoutRef.current)
      }
    }
  }, [t])

  const resetSession = async () => {
    const response = await logout()
    if (!response.success) {
      throw createServerError(response, t('Failed to sign out session'))
    }
    clearAuthentication()
  }

  const rememberLoginRedirect = (provider: string, state: string) => {
    if (typeof window === 'undefined') return
    rememberOAuthLoginRedirect(
      getOAuthSessionStorage(window),
      provider,
      state,
      redirectTo,
      window.location.origin
    )
  }

  const handleGitHubLogin = async () => {
    if (!status?.github_client_id) return
    if (githubButtonDisabled) return

    setIsLoading(true)
    setGithubButtonDisabled(true)
    setGithubButtonText(t('Redirecting to GitHub...'))

    if (githubTimeoutRef.current) {
      clearTimeout(githubTimeoutRef.current)
    }

    githubTimeoutRef.current = setTimeout(() => {
      setIsLoading(false)
      setGithubButtonText(
        t('Request timed out, please refresh and restart GitHub login')
      )
      setGithubButtonDisabled(true)
    }, 20000)

    try {
      await resetSession()

      const state = await createOAuthFlow(
        'github',
        'login',
        affiliateCode,
        redirectTo
      )
      rememberLoginRedirect('github', state)

      const url = buildGitHubOAuthUrl(
        status.github_client_id,
        state,
        status.server_address
      )
      window.open(url, '_self')

    } catch (error) {
      handleServerError(
        AuthOperationError.from(error, t('Failed to start GitHub login'))
      )
      if (githubTimeoutRef.current) {
        clearTimeout(githubTimeoutRef.current)
      }
      setIsLoading(false)
      setGithubButtonText(t('Continue with GitHub'))
      setGithubButtonDisabled(false)
    }
  }

  const handleDiscordLogin = async () => {
    if (!status?.discord_client_id) return

    setIsLoading(true)
    try {
      await resetSession()

      const state = await createOAuthFlow(
        'discord',
        'login',
        affiliateCode,
        redirectTo
      )
      rememberLoginRedirect('discord', state)

      const url = buildDiscordOAuthUrl(
        status.discord_client_id,
        state,
        status.server_address
      )
      window.open(url, '_self')

    } catch (error) {
      handleServerError(
        AuthOperationError.from(error, t('Failed to start Discord login'))
      )
    } finally {
      setIsLoading(false)
    }
  }

  const handleOIDCLogin = async () => {
    if (!status?.oidc_authorization_endpoint || !status?.oidc_client_id) return

    setIsLoading(true)
    try {
      await resetSession()

      const state = await createOAuthFlow(
        'oidc',
        'login',
        affiliateCode,
        redirectTo
      )
      rememberLoginRedirect('oidc', state)

      const url = buildOIDCOAuthUrl(
        status.oidc_authorization_endpoint,
        status.oidc_client_id,
        state,
        status.server_address
      )
      window.open(url, '_self')

    } catch (error) {
      handleServerError(
        AuthOperationError.from(error, t('Failed to start OIDC login'))
      )
    } finally {
      setIsLoading(false)
    }
  }

  const handleLinuxDOLogin = async () => {
    if (!status?.linuxdo_client_id) return

    setIsLoading(true)
    try {
      await resetSession()

      const state = await createOAuthFlow(
        'linuxdo',
        'login',
        affiliateCode,
        redirectTo
      )
      rememberLoginRedirect('linuxdo', state)

      const url = buildLinuxDOOAuthUrl(
        status.linuxdo_client_id,
        state,
        status.server_address
      )
      window.open(url, '_self')

    } catch (error) {
      handleServerError(
        AuthOperationError.from(error, t('Failed to start LinuxDO login'))
      )
    } finally {
      setIsLoading(false)
    }
  }

  const handleTelegramLogin = async () => {
    if (!status?.telegram_oauth_configured) {
      toast.error(
        t(
          'Telegram OAuth is not configured or enabled. Please contact your administrator.'
        )
      )
      return
    }
    setIsLoading(true)
    try {

      const authorization = await createOAuthAuthorization(
        'telegram',
        'login',
        undefined,
        undefined,
        undefined,
        affiliateCode,
        redirectTo
      )
      if (!authorization.authorizationUrl) {
        throw new AuthOperationError('Failed to initialize OAuth')
      }

      await resetSession()
      rememberLoginRedirect('telegram', authorization.state)
      window.open(authorization.authorizationUrl, '_self')
    } catch (error) {
      handleServerError(AuthOperationError.from(error))
    } finally {
      setIsLoading(false)
    }
  }

  const handleCustomOAuthLogin = async (provider: CustomOAuthProviderInfo) => {
    if (!provider.authorization_endpoint || !provider.client_id) return

    setIsLoading(true)
    try {
      await resetSession()

      const state = await createOAuthFlow(
        provider.slug,
        'login',
        affiliateCode,
        redirectTo
      )
      rememberLoginRedirect(provider.slug, state)

      const redirectUri = `${window.location.origin}/oauth/${provider.slug}`
      const url = new URL(provider.authorization_endpoint)
      url.searchParams.set('client_id', provider.client_id)
      url.searchParams.set('redirect_uri', redirectUri)
      url.searchParams.set('response_type', 'code')
      url.searchParams.set('state', state)
      if (provider.scopes) {
        url.searchParams.set('scope', provider.scopes)
      }


      window.open(url.toString(), '_self')
    } catch (error) {
      handleServerError(
        AuthOperationError.from(
          error,
          t('Failed to start {{provider}} login', { provider: provider.name })
        )
      )
    } finally {
      setIsLoading(false)
    }
  }

  return {
    isLoading,
    githubButtonText,
    githubButtonDisabled,
    handleGitHubLogin,
    handleDiscordLogin,
    handleOIDCLogin,
    handleLinuxDOLogin,
    handleTelegramLogin,
    handleCustomOAuthLogin,
  }
}
