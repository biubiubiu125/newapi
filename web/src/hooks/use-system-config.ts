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
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useCallback, useMemo } from 'react'

import { resolveAssetUrl } from '@/lib/asset-url'
import { applyFaviconToDom } from '@/lib/dom-utils'
import { ensureStatus, mapStatusDataToConfig } from '@/lib/status-query'
import { useSystemConfigStore } from '@/stores/system-config-store'

export { mapStatusDataToConfig }

interface UseSystemConfigOptions {
  /** Automatically fetch config from backend (use only in root component) */
  autoLoad?: boolean
}

// Preload image and return cleanup function
function preloadImage(
  src: string,
  onLoad: () => void,
  onError: () => void
): () => void {
  const img = new Image()
  img.onload = onLoad
  img.onerror = onError
  img.src = src

  return () => {
    img.onload = null
    img.onerror = null
  }
}

function applySystemNameToDom(name: string) {
  if (typeof document === 'undefined' || !name) return
  document.title = name
  const titleMeta =
    document.querySelector<HTMLMetaElement>('meta[name="title"]')
  if (titleMeta) {
    titleMeta.content = name
  }
}

/**
 * System configuration hook with auto-loading and logo preloading
 *
 * @example
 * // Root component - auto-load from backend
 * useSystemConfig({ autoLoad: true })
 *
 * @example
 * // Other components - use cached config
 * const { systemName, logo, loading } = useSystemConfig()
 */
export function useSystemConfig(options: UseSystemConfigOptions = {}) {
  const { autoLoad = false } = options
  const queryClient = useQueryClient()
  const { config, loading, loadedLogoUrl, setLoadedLogoUrl, setLoading } =
    useSystemConfigStore()

  // Load config from backend via the shared `/api/status` cache.
  const loadConfig = useCallback(async () => {
    try {
      setLoading(true)
      await ensureStatus(queryClient)
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to load system config:', error)
    } finally {
      setLoading(false)
    }
  }, [queryClient, setLoading])

  useEffect(() => {
    if (autoLoad) loadConfig()
  }, [autoLoad, loadConfig])

  useEffect(() => {
    applySystemNameToDom(config.systemName)
  }, [config.systemName])

  useEffect(() => {
    if (config.logo) {
      applyFaviconToDom(config.logo, config.serverAddress)
    }
  }, [config.logo, config.serverAddress])

  const resolvedLogo = useMemo(
    () => resolveAssetUrl(config.logo, DEFAULT_LOGO, config.serverAddress),
    [config.logo, config.serverAddress]
  )

  // Preload logo image when URL changes
  useEffect(() => {
    const { logo } = config
    if (!logo) return

    // Preload new logo
    return preloadImage(
      resolvedLogo,
      () => {
        setLoadedLogoUrl(resolvedLogo)
        applyFaviconToDom(resolvedLogo, config.serverAddress)
      },
      () => {
        if (logo !== DEFAULT_LOGO) {
          // eslint-disable-next-line no-console
          console.error('Failed to load logo:', resolvedLogo)
        }
        setLoadedLogoUrl(DEFAULT_LOGO)
        applyFaviconToDom(DEFAULT_LOGO)
      }
    )
  }, [config.logo, config.serverAddress, resolvedLogo, setLoadedLogoUrl])

  const displayLogo =
    loadedLogoUrl === resolvedLogo || resolvedLogo === DEFAULT_LOGO
      ? resolvedLogo
      : DEFAULT_LOGO

  return {
    ...config,
    logo: displayLogo,
    loading,
    logoLoaded: displayLogo === loadedLogoUrl && !!loadedLogoUrl,
  }
}
