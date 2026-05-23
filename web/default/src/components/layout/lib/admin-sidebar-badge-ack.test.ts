import assert from 'node:assert/strict'
import { beforeEach, describe, test } from 'node:test'
import {
  acknowledgeAdminSidebarBadge,
  lowerAdminSidebarBadgeAckBaselines,
  unreadAdminSidebarBadgeCount,
} from './admin-sidebar-badge-ack'

const storage = new Map<string, string>()
let eventCount = 0

beforeEach(() => {
  storage.clear()
  eventCount = 0

  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    value: {
      localStorage: {
        getItem(key: string) {
          return storage.get(key) ?? null
        },
        setItem(key: string, value: string) {
          storage.set(key, value)
        },
      },
      dispatchEvent() {
        eventCount += 1
        return true
      },
    },
  })
})

describe('admin sidebar badge acknowledgement', () => {
  test('clears the viewed badge for the current user and keeps future increments unread', () => {
    assert.equal(unreadAdminSidebarBadgeCount('risk-center', 21, 1), 21)

    acknowledgeAdminSidebarBadge('risk-center', 21, 1)

    assert.equal(eventCount, 1)
    assert.equal(unreadAdminSidebarBadgeCount('risk-center', 21, 1), 0)
    assert.equal(unreadAdminSidebarBadgeCount('risk-center', 23, 1), 2)
  })

  test('keeps acknowledgements scoped by user', () => {
    acknowledgeAdminSidebarBadge('users', 99, 1)

    assert.equal(unreadAdminSidebarBadgeCount('users', 99, 1), 0)
    assert.equal(unreadAdminSidebarBadgeCount('users', 99, 2), 99)
  })

  test('lowers stale baselines when backend counts shrink', () => {
    acknowledgeAdminSidebarBadge('recharge-audit', 6, 1)

    assert.equal(
      lowerAdminSidebarBadgeAckBaselines({ 'recharge-audit': 2 }, 1),
      true
    )
    assert.equal(unreadAdminSidebarBadgeCount('recharge-audit', 2, 1), 0)
    assert.equal(unreadAdminSidebarBadgeCount('recharge-audit', 3, 1), 1)
  })

  test('does not lower baselines while badge counts are still loading', () => {
    acknowledgeAdminSidebarBadge('users', 99, 1)

    assert.equal(lowerAdminSidebarBadgeAckBaselines({ users: 0 }, 1, false), false)
    assert.equal(unreadAdminSidebarBadgeCount('users', 99, 1), 0)
  })
})
