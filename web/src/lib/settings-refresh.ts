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
const SETTINGS_REFRESH_EVENT = 'newapi:settings-refresh'
const SETTINGS_REFRESH_STORAGE_KEY = 'newapi:settings-refresh'

export type SettingsRefreshPayload = {
  keys: string[]
  timestamp: number
}

function createPayload(keys: string[] = []): SettingsRefreshPayload {
  return {
    keys: keys.filter(Boolean),
    timestamp: Date.now(),
  }
}

export function emitSettingsRefresh(keys: string[] = []) {
  if (typeof window === 'undefined') return

  const payload = createPayload(keys)
  window.dispatchEvent(
    new CustomEvent(SETTINGS_REFRESH_EVENT, { detail: payload })
  )

  try {
    window.localStorage.setItem(
      SETTINGS_REFRESH_STORAGE_KEY,
      JSON.stringify(payload)
    )
  } catch {
    /* empty */
  }

  if ('BroadcastChannel' in window) {
    try {
      const channel = new BroadcastChannel(SETTINGS_REFRESH_EVENT)
      channel.postMessage(payload)
      channel.close()
    } catch {
      /* empty */
    }
  }
}

export function subscribeSettingsRefresh(
  handler: (payload: SettingsRefreshPayload) => void
) {
  if (typeof window === 'undefined') return () => {}
  let lastTimestamp = 0

  const notify = (payload: SettingsRefreshPayload) => {
    if (!payload || payload.timestamp <= lastTimestamp) return
    lastTimestamp = payload.timestamp
    handler(payload)
  }

  const handleCustomEvent = (event: Event) => {
    const detail = (event as CustomEvent<SettingsRefreshPayload>).detail
    if (detail) notify(detail)
  }

  const handleStorageEvent = (event: StorageEvent) => {
    if (event.key !== SETTINGS_REFRESH_STORAGE_KEY || !event.newValue) return
    try {
      notify(JSON.parse(event.newValue) as SettingsRefreshPayload)
    } catch {
      /* empty */
    }
  }

  let channel: BroadcastChannel | null = null
  if ('BroadcastChannel' in window) {
    try {
      channel = new BroadcastChannel(SETTINGS_REFRESH_EVENT)
      channel.onmessage = (event) => {
        if (event.data) notify(event.data as SettingsRefreshPayload)
      }
    } catch {
      channel = null
    }
  }

  window.addEventListener(SETTINGS_REFRESH_EVENT, handleCustomEvent)
  window.addEventListener('storage', handleStorageEvent)

  return () => {
    window.removeEventListener(SETTINGS_REFRESH_EVENT, handleCustomEvent)
    window.removeEventListener('storage', handleStorageEvent)
    channel?.close()
  }
}
