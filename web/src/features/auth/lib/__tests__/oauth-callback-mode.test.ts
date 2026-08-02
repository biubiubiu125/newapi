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
import assert from 'node:assert/strict'
import { describe, test } from 'vitest'

import {
  getOAuthSessionStorage,
  markOAuthBindPopup,
  rememberOAuthLoginRedirect,
  resolveOAuthLoginRedirectTarget,
  resolveOAuthCallbackMode,
  type OAuthModeStorage,
} from '../oauth-callback-mode'

function fakeStorage(initial: Record<string, string> = {}): OAuthModeStorage {
  const data = new Map(Object.entries(initial))
  return {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => void data.set(key, value),
    removeItem: (key: string) => void data.delete(key),
  }
}

const openOpener = { closed: false }
const bindState = 'bind-state'
const origin = 'https://newapi.example'

describe('resolveOAuthCallbackMode', () => {
  test('matching provider and state mark is treated as a bind flow', () => {
    const storage = fakeStorage()
    assert.equal(markOAuthBindPopup(storage, 'oidc', bindState), true)

    assert.equal(
      resolveOAuthCallbackMode('oidc', bindState, {
        opener: openOpener,
        storage,
      }),
      'bind'
    )
  })

  // Regression: a tab opened from an external link (Slack, e-mail, another
  // site) keeps a live window.opener across the cross-origin round trip to the
  // identity provider. Treating that opener as proof of a bind flow made every
  // such login hang on the binding screen until the 30s handshake deadline.
  test('login redirect in a tab with a foreign opener stays a login flow', () => {
    const storage = fakeStorage()

    assert.equal(
      resolveOAuthCallbackMode('oidc', bindState, {
        opener: openOpener,
        storage,
      }),
      'login'
    )
  })

  test('bind marker for another provider does not hijack this callback', () => {
    const storage = fakeStorage()
    markOAuthBindPopup(storage, 'github', bindState)

    assert.equal(
      resolveOAuthCallbackMode('oidc', bindState, {
        opener: openOpener,
        storage,
      }),
      'login'
    )
  })

  test('stale bind marker does not hijack a later callback', () => {
    const storage = fakeStorage()
    markOAuthBindPopup(storage, 'oidc', 'previous-state')

    assert.equal(
      resolveOAuthCallbackMode('oidc', bindState, {
        opener: openOpener,
        storage,
      }),
      'login'
    )
  })

  test('bind marker without an opener falls back to login', () => {
    const storage = fakeStorage()
    markOAuthBindPopup(storage, 'oidc', bindState)

    assert.equal(
      resolveOAuthCallbackMode('oidc', bindState, {
        opener: null,
        storage,
      }),
      'login'
    )
  })

  test('closed opener falls back to login', () => {
    const storage = fakeStorage()
    markOAuthBindPopup(storage, 'oidc', bindState)

    assert.equal(
      resolveOAuthCallbackMode('oidc', bindState, {
        opener: { closed: true },
        storage,
      }),
      'login'
    )
  })

  test('missing storage degrades to login instead of throwing', () => {
    assert.equal(
      resolveOAuthCallbackMode('oidc', bindState, {
        opener: openOpener,
        storage: null,
      }),
      'login'
    )
  })

  test('storage read failure degrades to login instead of throwing', () => {
    const storage: OAuthModeStorage = {
      getItem: () => {
        throw new Error('blocked')
      },
      setItem: () => undefined,
    }

    assert.equal(
      resolveOAuthCallbackMode('oidc', bindState, {
        opener: openOpener,
        storage,
      }),
      'login'
    )
  })
})

describe('OAuth login redirect storage', () => {
  test('stores and resolves the safe redirect for the matching provider and state', () => {
    const storage = fakeStorage()

    assert.equal(
      rememberOAuthLoginRedirect(
        storage,
        'github',
        'login-state',
        '/keys?tab=default#active',
        origin
      ),
      true
    )
    assert.equal(
      resolveOAuthLoginRedirectTarget(
        storage,
        'github',
        'login-state',
        undefined,
        origin
      ),
      '/keys?tab=default#active'
    )
    assert.equal(
      resolveOAuthLoginRedirectTarget(
        storage,
        'github',
        'login-state',
        undefined,
        origin
      ),
      null
    )
  })

  test('query redirect wins over stale stored redirect and unsafe values are rejected', () => {
    const storage = fakeStorage()
    assert.equal(
      rememberOAuthLoginRedirect(
        storage,
        'github',
        'login-state',
        'https://evil.example/path',
        origin
      ),
      false
    )
    assert.equal(
      rememberOAuthLoginRedirect(
        storage,
        'github',
        'login-state',
        '/keys',
        origin
      ),
      true
    )

    assert.equal(
      resolveOAuthLoginRedirectTarget(
        storage,
        'github',
        'login-state',
        '/dashboard',
        origin
      ),
      '/dashboard'
    )
  })
})

describe('OAuth bind popup storage', () => {
  test('blocked sessionStorage getter is contained', () => {
    const owner = {
      get sessionStorage(): OAuthModeStorage {
        throw new Error('blocked')
      },
    }

    assert.equal(getOAuthSessionStorage(owner), null)
  })

  test('marking reports unavailable or unwritable storage', () => {
    const storage: OAuthModeStorage = {
      getItem: () => null,
      setItem: () => {
        throw new Error('blocked')
      },
    }

    assert.equal(markOAuthBindPopup(null, 'oidc', bindState), false)
    assert.equal(markOAuthBindPopup(storage, 'oidc', bindState), false)
    assert.equal(
      markOAuthBindPopup(
        {
          getItem: () => null,
          setItem: () => undefined,
        },
        'oidc',
        bindState
      ),
      false
    )
  })
})
