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

export const ADMIN_SIDEBAR_BADGE_ACK_EVENT = 'admin-sidebar-badge-ack'

const ADMIN_BADGE_ACK_STORAGE_KEY_PREFIX = 'admin-sidebar-alert-badge-ack-v3'

function badgeAckStorageKey(userId?: number | string | null): string {
  const scope =
    userId === null || typeof userId === 'undefined' ? 'anonymous' : String(userId)
  return `${ADMIN_BADGE_ACK_STORAGE_KEY_PREFIX}:${scope}`
}

function readBadgeAckState(userId?: number | string | null): Record<string, number> {
  if (typeof window === 'undefined') return {}
  try {
    const raw = window.localStorage.getItem(badgeAckStorageKey(userId))
    return raw ? (JSON.parse(raw) as Record<string, number>) : {}
  } catch {
    return {}
  }
}

function writeBadgeAckState(
  userId: number | string | null | undefined,
  state: Record<string, number>
) {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(badgeAckStorageKey(userId), JSON.stringify(state))
}

export function normalizeSidebarBadgeCount(value: number | undefined): number {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) {
    return 0
  }
  return Math.floor(value)
}

export function readAdminSidebarBadgeAck(
  key: string,
  userId?: number | string | null
): number {
  const value = readBadgeAckState(userId)[key]
  return typeof value === 'number' && Number.isFinite(value) ? value : 0
}

export function unreadAdminSidebarBadgeCount(
  key: string,
  value: number,
  userId?: number | string | null
): number {
  const normalized = normalizeSidebarBadgeCount(value)
  const acknowledged = readAdminSidebarBadgeAck(key, userId)
  return Math.max(0, normalized - acknowledged)
}

export function acknowledgeAdminSidebarBadge(
  key: string | undefined,
  value: number | undefined,
  userId?: number | string | null
) {
  const normalized = normalizeSidebarBadgeCount(value)
  if (!key || normalized <= 0) return
  try {
    const state = readBadgeAckState(userId)
    state[key] = normalized
    writeBadgeAckState(userId, state)
    window.dispatchEvent(new Event(ADMIN_SIDEBAR_BADGE_ACK_EVENT))
  } catch {
    // Ignore localStorage failures; the badge will simply remain visible.
  }
}

export function lowerAdminSidebarBadgeAckBaselines(
  counts: Record<string, number>,
  userId?: number | string | null,
  loaded = true
): boolean {
  if (!loaded || typeof window === 'undefined') return false
  try {
    const state = readBadgeAckState(userId)
    let changed = false

    Object.entries(counts).forEach(([key, value]) => {
      const acknowledged = state[key]
      const normalized = normalizeSidebarBadgeCount(value)
      if (
        typeof acknowledged === 'number' &&
        Number.isFinite(acknowledged) &&
        acknowledged > normalized
      ) {
        state[key] = normalized
        changed = true
      }
    })

    if (changed) {
      writeBadgeAckState(userId, state)
    }
    return changed
  } catch {
    return false
  }
}
