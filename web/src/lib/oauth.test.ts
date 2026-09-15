import assert from 'node:assert/strict'
import { describe, test } from 'vitest'

import { buildCustomOAuthUrl, buildGitHubOAuthUrl } from './oauth'

describe('custom oauth authorize url', () => {
  test('uses the configured server address for redirect_uri', () => {
    const url = new URL(
      buildCustomOAuthUrl({
        authorizationEndpoint: 'https://idp.example/authorize',
        clientId: 'cid-1',
        slug: 'acme',
        state: 'state-1',
        scopes: 'openid email',
        serverAddress: 'https://api.example.com',
        fallbackOrigin: 'https://app.example.com',
      })
    )

    assert.equal(url.origin + url.pathname, 'https://idp.example/authorize')
    assert.equal(url.searchParams.get('client_id'), 'cid-1')
    assert.equal(url.searchParams.get('state'), 'state-1')
    assert.equal(url.searchParams.get('scope'), 'openid email')
    assert.equal(
      url.searchParams.get('redirect_uri'),
      'https://api.example.com/oauth/acme'
    )
  })

  test('uses the configured server address even when it is localhost', () => {
    const url = new URL(
      buildCustomOAuthUrl({
        authorizationEndpoint: 'https://idp.example/authorize',
        clientId: 'cid-1',
        slug: 'acme',
        state: 'state-1',
        serverAddress: 'http://localhost:3000',
        fallbackOrigin: 'https://app.example.com',
      })
    )

    assert.equal(
      url.searchParams.get('redirect_uri'),
      'http://localhost:3000/oauth/acme'
    )
  })
})

describe('github oauth authorize url', () => {
  test('uses the configured server address for redirect_uri', () => {
    const url = new URL(
      buildGitHubOAuthUrl('cid-gh', 'state-gh', 'https://api.example.com')
    )

    assert.equal(url.origin + url.pathname, 'https://github.com/login/oauth/authorize')
    assert.equal(url.searchParams.get('client_id'), 'cid-gh')
    assert.equal(url.searchParams.get('state'), 'state-gh')
    assert.equal(url.searchParams.get('scope'), 'user:email')
    assert.equal(
      url.searchParams.get('redirect_uri'),
      'https://api.example.com/oauth/github'
    )
  })

  test('rejects an empty server address instead of falling back to the page origin', () => {
    assert.throws(
      () => buildGitHubOAuthUrl('cid-gh', 'state-gh', ''),
      /server address is not configured/
    )
    assert.throws(
      () =>
        buildCustomOAuthUrl({
          authorizationEndpoint: 'https://idp.example/authorize',
          clientId: 'cid-1',
          slug: 'acme',
          state: 'state-1',
          serverAddress: '',
          fallbackOrigin: 'https://app.example.com',
        }),
      /server address is not configured/
    )
  })
})
