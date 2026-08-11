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
type BadgeAckValue = number | string
type BadgeAckState = Record<string, BadgeAckValue>
type BadgeAckMode = 'count' | 'cursor'
type BadgeAckOptions = {
  mode?: BadgeAckMode
  cursor?: BadgeAckValue
}

function badgeAckStorageKey(userId?: number | string | null): string {
  const scope =
    userId === null || typeof userId === 'undefined'
      ? 'anonymous'
      : String(userId)
  return `${ADMIN_BADGE_ACK_STORAGE_KEY_PREFIX}:${scope}`
}

function readBadgeAckState(userId?: number | string | null): BadgeAckState {
  if (typeof window === 'undefined') return {}
  try {
    const raw = window.localStorage.getItem(badgeAckStorageKey(userId))
    return raw ? (JSON.parse(raw) as BadgeAckState) : {}
  } catch {
    return {}
  }
}

function writeBadgeAckState(
  userId: number | string | null | undefined,
  state: BadgeAckState
) {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(badgeAckStorageKey(userId), JSON.stringify(state))
}

function badgeAckEntryKey(key: string, mode: BadgeAckMode = 'count'): string {
  return mode === 'cursor' ? `${key}:cursor` : key
}

export function normalizeSidebarBadgeCount(
  value: number | undefined | null
): number {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) {
    return 0
  }
  return Math.floor(value)
}

function normalizeBadgeAckCursor(
  value: BadgeAckValue | undefined | null
): string | undefined {
  if (typeof value === 'number') {
    if (!Number.isFinite(value) || value < 0) return undefined
    return String(Math.floor(value))
  }
  const normalized = String(value ?? '').trim()
  return normalized ? normalized : undefined
}

function parseCursorParts(
  value: BadgeAckValue | undefined
): number[] | undefined {
  const normalized = normalizeBadgeAckCursor(value)
  if (!normalized) return undefined
  const parts = normalized.split(':')
  const numericParts = parts.map((part) => Number(part))
  if (numericParts.some((part) => !Number.isFinite(part))) return undefined
  return numericParts
}

function compareBadgeAckCursor(
  left: BadgeAckValue | undefined,
  right: BadgeAckValue | undefined
): number {
  const leftParts = parseCursorParts(left)
  const rightParts = parseCursorParts(right)
  if (!leftParts || !rightParts) {
    const normalizedLeft = normalizeBadgeAckCursor(left) ?? ''
    const normalizedRight = normalizeBadgeAckCursor(right) ?? ''
    return normalizedLeft.localeCompare(normalizedRight)
  }
  const length = Math.max(leftParts.length, rightParts.length)
  for (let index = 0; index < length; index += 1) {
    const leftPart = leftParts[index] ?? 0
    const rightPart = rightParts[index] ?? 0
    if (leftPart !== rightPart) return leftPart - rightPart
  }
  return 0
}

export function readAdminSidebarBadgeAck(
  key: string,
  userId?: number | string | null,
  options: Pick<BadgeAckOptions, 'mode'> = {}
): BadgeAckValue {
  const value = readBadgeAckState(userId)[badgeAckEntryKey(key, options.mode)]
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string') return value
  return 0
}

export function hasAdminSidebarBadgeAck(
  key: string,
  userId?: number | string | null,
  options: Pick<BadgeAckOptions, 'mode'> = {}
): boolean {
  const state = readBadgeAckState(userId)
  return Object.hasOwn(state, badgeAckEntryKey(key, options.mode))
}

export function unreadAdminSidebarBadgeCount(
  key: string,
  value: number,
  userId?: number | string | null,
  options: BadgeAckOptions = {}
): number {
  const normalized = normalizeSidebarBadgeCount(value)
  if (options.mode === 'cursor') {
    const cursor = normalizeBadgeAckCursor(options.cursor)
    if (!cursor) return normalized
    const acknowledgedCursor = readAdminSidebarBadgeAck(key, userId, {
      mode: 'cursor',
    })
    return compareBadgeAckCursor(acknowledgedCursor, cursor) >= 0
      ? 0
      : normalized
  }
  const acknowledged = readAdminSidebarBadgeAck(key, userId)
  if (typeof acknowledged !== 'number' || !Number.isFinite(acknowledged)) {
    return normalized
  }
  return Math.max(0, normalized - acknowledged)
}

export function acknowledgeAdminSidebarBadge(
  key: string | undefined,
  value: number | undefined,
  userId?: number | string | null,
  options: BadgeAckOptions = {}
) {
  const normalized = normalizeSidebarBadgeCount(value)
  if (!key) return
  try {
    const state = readBadgeAckState(userId)
    if (options.mode === 'cursor') {
      const cursor = normalizeBadgeAckCursor(options.cursor)
      if (!cursor) return
      state[badgeAckEntryKey(key, 'cursor')] = cursor
      writeBadgeAckState(userId, state)
      window.dispatchEvent(new Event(ADMIN_SIDEBAR_BADGE_ACK_EVENT))
      return
    }
    if (normalized <= 0) return
    state[key] = normalized
    writeBadgeAckState(userId, state)
    window.dispatchEvent(new Event(ADMIN_SIDEBAR_BADGE_ACK_EVENT))
  } catch {
    // Ignore localStorage failures; the badge will simply remain visible.
  }
}

export function initializeAdminSidebarBadgeCursors(
  cursors: Record<string, BadgeAckValue | undefined | null>,
  userId?: number | string | null,
  loaded = true
): boolean {
  if (!loaded || typeof window === 'undefined') return false
  try {
    const state = readBadgeAckState(userId)
    let changed = false

    Object.entries(cursors).forEach(([key, value]) => {
      const cursor = normalizeBadgeAckCursor(value)
      if (!cursor) return
      const entryKey = badgeAckEntryKey(key, 'cursor')
      if (normalizeBadgeAckCursor(state[entryKey])) return
      state[entryKey] = cursor
      changed = true
    })

    if (changed) {
      writeBadgeAckState(userId, state)
      window.dispatchEvent(new Event(ADMIN_SIDEBAR_BADGE_ACK_EVENT))
    }
    return changed
  } catch {
    return false
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
