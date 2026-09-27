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

import i18n from 'i18next'
import { describe, test, expect } from 'vitest'

import type { AuthUser } from '@/stores/auth-store'

import {
  applySavedLanguage,
  getSavedLanguage,
  sanitizeAuthRedirect,
} from './auth-redirect'

const origin = 'https://dashboard.example.com'

describe('authentication redirect validation', () => {
  test('preserves safe internal paths, search parameters, and fragments', () => {
    expect(sanitizeAuthRedirect('/console?tab=usage#recent', origin)).toBe(
      '/console?tab=usage#recent'
    )
    expect(
      sanitizeAuthRedirect(
        'https://dashboard.example.com/dashboard?tab=quota#daily',
        origin
      )
    ).toBe('/dashboard?tab=quota#daily')
  })

  test('rejects external and ambiguously parsed redirect targets', () => {
    const unsafeTargets: unknown[] = [
      undefined,
      '',
      'dashboard',
      '//attacker.example/path',
      'https://attacker.example/path',
      'javascript:alert(1)',
      '/\\attacker.example/path',
      'https:\\attacker.example/path',
    ]

    for (const target of unsafeTargets) {
      expect(sanitizeAuthRedirect(target, origin)).toBe(null)
    }
  })

  test('rejects invalid or non-HTTP application origins', () => {
    expect(sanitizeAuthRedirect('/dashboard', 'not-an-origin')).toBe(null)
    expect(sanitizeAuthRedirect('/dashboard', 'file:///tmp/app')).toBe(null)
  })
})

describe('saved authentication language', () => {
  const user: AuthUser = { id: 1, username: 'user', role: 1 }

  test('prefers the explicit user language', () => {
    expect(
      getSavedLanguage({
        ...user,
        language: 'ja',
        setting: { language: 'fr' },
      })
    ).toBe('ja')
  })

  test('reads object and JSON string settings', () => {
    expect(getSavedLanguage({ ...user, setting: { language: 'fr' } })).toBe(
      'fr'
    )
    expect(getSavedLanguage({ ...user, setting: '{"language":"ru"}' })).toBe(
      'ru'
    )
  })

  test('ignores malformed and non-string setting languages', () => {
    expect(getSavedLanguage({ ...user, setting: '{' })).toBe(undefined)
    expect(getSavedLanguage({ ...user, setting: { language: 123 } })).toBe(
      undefined
    )
    assert.equal(getSavedLanguage({ ...user, language: '   ' }), undefined)
  })

  test('normalizes backend and browser language tags onto interface codes', () => {
    assert.equal(getSavedLanguage({ ...user, language: 'zh-CN' }), 'zhCN')
    assert.equal(
      getSavedLanguage({ ...user, setting: { language: 'zh_CN' } }),
      'zhCN'
    )
    assert.equal(
      getSavedLanguage({ ...user, setting: '{"language":"zh-tw"}' }),
      'zhTW'
    )
    assert.equal(getSavedLanguage({ ...user, language: 'fr-FR' }), 'fr')
  })
})

describe('applySavedLanguage', () => {
  const user: AuthUser = { id: 1, username: 'user', role: 1 }

  test('switches the interface language from the saved user setting', async () => {
    await i18n.changeLanguage('en')
    await applySavedLanguage({ ...user, language: 'zh-CN' })
    assert.equal(i18n.language, 'zhCN')
  })

  test('keeps the current language when the user has no saved language', async () => {
    await i18n.changeLanguage('en')
    await applySavedLanguage(user)
    assert.equal(i18n.language, 'en')
  })
})
