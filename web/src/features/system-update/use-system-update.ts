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
import {
  focusManager,
  onlineManager,
  queryOptions,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { useEffect, useState, useSyncExternalStore } from 'react'
import { useTranslation } from 'react-i18next'

import { useStatus } from '@/hooks/use-status'
import { handleServerError } from '@/lib/handle-server-error'
import { ROLE } from '@/lib/roles'
import { statusQueryOptions } from '@/lib/status-query'
import { useAuthStore } from '@/stores/auth-store'

import {
  fetchLatestSystemRelease,
  UpdateCheckError,
  type UpdateCheckErrorCode,
} from './api'
import { compareRunningToRelease } from './releases'
import {
  subscribeSystemUpdatePreferences,
  useSystemUpdatePreferencesStore,
  useSystemUpdateStore,
  type SystemUpdateSnapshot,
} from './store'

export const SYSTEM_UPDATE_INTERVAL = 60 * 60 * 1000
export const SYSTEM_UPDATE_QUERY_KEY = ['system-update'] as const

export function getUpdateErrorMessage(
  code: UpdateCheckErrorCode,
  t: TFunction
): string {
  switch (code) {
    case 'rate-limit':
      return t('GitHub rate limit reached. Try again later.')
    case 'timeout':
      return t('Update check timed out. Try again.')
    case 'payload':
      return t('Unexpected release payload')
    default:
      return t('Failed to check for updates')
  }
}

function normalizeRunningVersion(version: unknown): string | undefined {
  if (typeof version !== 'string') return undefined
  const trimmed = version.trim()
  if (!trimmed || trimmed === 'v0.0.0' || trimmed === '0.0.0') return undefined
  return trimmed
}

function snapshotMatchesVersion(
  snapshot: SystemUpdateSnapshot | null | undefined,
  currentVersion: string | undefined
): snapshot is SystemUpdateSnapshot {
  if (!snapshot) return false
  return (
    snapshot.checkedVersion === undefined ||
    snapshot.checkedVersion === currentVersion
  )
}

export const systemUpdateQueryOptions = queryOptions({
  queryKey: SYSTEM_UPDATE_QUERY_KEY,
  queryFn: async ({ signal, client }): Promise<SystemUpdateSnapshot> => {
    // The release channel depends on the running version, so wait for it.
    let currentVersion: string | undefined
    try {
      const status = await client.fetchQuery({
        ...statusQueryOptions,
        meta: { errorToast: false },
      })
      signal.throwIfAborted()
      currentVersion = normalizeRunningVersion(
        status && typeof status === 'object' ? status.version : undefined
      )
    } catch (error) {
      if (signal.aborted) throw error
      currentVersion = undefined
    }
    let snapshot: SystemUpdateSnapshot
    try {
      const checked = await fetchLatestSystemRelease(signal, currentVersion)
      signal.throwIfAborted()
      const now = Date.now()
      snapshot = {
        release: checked.release,
        commitShas: checked.commitShas,
        checkedVersion: currentVersion,
        lastCheckedAt: now,
        lastAttemptAt: now,
        error: null,
      }
    } catch (error) {
      if (signal.aborted) throw error
      const previous = useSystemUpdateStore.getState().snapshot
      const sameVersion = snapshotMatchesVersion(previous, currentVersion)
      // A failed attempt is also cached, so focus changes and remounts cannot
      // hammer GitHub while it is unavailable or rate limiting this browser.
      snapshot = {
        release: sameVersion ? (previous?.release ?? null) : null,
        commitShas: sameVersion ? previous?.commitShas : undefined,
        checkedVersion: currentVersion,
        lastCheckedAt: sameVersion ? (previous?.lastCheckedAt ?? 0) : 0,
        lastAttemptAt: Date.now(),
        error: error instanceof UpdateCheckError ? error.code : 'network',
      }
    }
    useSystemUpdateStore.getState().setSnapshot(snapshot)
    return snapshot
  },
  staleTime: SYSTEM_UPDATE_INTERVAL,
  gcTime: 24 * SYSTEM_UPDATE_INTERVAL,
  retry: false,
  meta: { errorToast: false },
  refetchOnWindowFocus: true,
  refetchOnReconnect: true,
  refetchIntervalInBackground: false,
  refetchInterval: (query) => {
    if (query.state.fetchStatus === 'fetching') return false
    return Math.max(
      1_000,
      SYSTEM_UPDATE_INTERVAL -
        (Date.now() - (query.state.data?.lastAttemptAt ?? 0))
    )
  },
})

function isPageVisible(): boolean {
  return focusManager.isFocused()
}

function isBrowserOnline(): boolean {
  return (
    onlineManager.isOnline() &&
    (typeof navigator === 'undefined' || navigator.onLine)
  )
}

export function useSystemUpdate() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const isAdmin = (user?.role ?? 0) >= ROLE.ADMIN
  const visible = useSyncExternalStore(focusManager.subscribe, isPageVisible)
  const online = useSyncExternalStore(onlineManager.subscribe, isBrowserOnline)
  const { status } = useStatus()
  const currentVersion = normalizeRunningVersion(status?.version)
  const [seeded] = useState(() => useSystemUpdateStore.getState().snapshot)
  const seededMatches = snapshotMatchesVersion(seeded, currentVersion)
  const queryClient = useQueryClient()
  const query = useQuery({
    ...systemUpdateQueryOptions,
    queryKey: [...SYSTEM_UPDATE_QUERY_KEY, currentVersion ?? ''],
    enabled: isAdmin && visible && online,
    ...(seededMatches
      ? {
          initialData: seeded,
          initialDataUpdatedAt:
            seeded.checkedVersion === undefined ? 0 : seeded.lastAttemptAt,
        }
      : {}),
  })

  useEffect(() => {
    if (!isAdmin) {
      void queryClient.cancelQueries({ queryKey: SYSTEM_UPDATE_QUERY_KEY })
    }
  }, [isAdmin, queryClient])

  const snapshotMatches = snapshotMatchesVersion(query.data, currentVersion)
  const release = snapshotMatches ? (query.data?.release ?? null) : null
  const comparison = compareRunningToRelease(
    currentVersion,
    release?.tag_name,
    snapshotMatches ? query.data?.commitShas : undefined
  )
  const hasUpdate = comparison === -1
  const isIgnored = useSyncExternalStore(subscribeSystemUpdatePreferences, () =>
    Boolean(
      user &&
      release &&
      useSystemUpdatePreferencesStore
        .getState()
        .ignoredVersionsByUserId[user.id]?.includes(release.tag_name)
    )
  )

  const setIgnored = (ignored: boolean) => {
    if (!isAdmin || !user || !release) return
    useSystemUpdatePreferencesStore
      .getState()
      .setVersionIgnored(user.id, release.tag_name, ignored)
  }

  const checkNow = async () => {
    if (!isAdmin || !online) return
    const result = await query.refetch({ cancelRefetch: false })
    if (result.data?.error) {
      handleServerError(new Error(getUpdateErrorMessage(result.data.error, t)))
    }
  }

  return {
    currentVersion,
    release,
    comparison,
    hasUpdate,
    isIgnored,
    shouldNotify: isAdmin && hasUpdate && !isIgnored,
    setIgnored,
    checking: query.isFetching,
    online,
    snapshot: query.data,
    checkNow,
  }
}
