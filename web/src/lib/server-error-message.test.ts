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
import { beforeEach, describe, test } from 'vitest'

import {
  getServerErrorMessageKey,
  localizeConsoleErrorText,
} from './server-error-message'

describe('server error message mapping', () => {
  test('maps the active-session limit to recovery instructions', () => {
    const message = getServerErrorMessageKey({ code: 'AUTH_SESSION_LIMIT' })

    assert.match(message ?? '', /Sign out other sessions/)
    assert.match(message ?? '', /reset your password/)
  })

  test('maps an Axios-shaped issuance limit to rolling-window guidance', () => {
    const message = getServerErrorMessageKey({
      response: { data: { code: 'AUTH_SESSION_ISSUANCE_LIMIT' } },
    })

    assert.match(message ?? '', /rolling window/)
    assert.equal(getServerErrorMessageKey({ code: 'UNKNOWN_CODE' }), null)
  })

  test('maps stable Telegram bind errors without exposing server text', () => {
    const expected = {
      TELEGRAM_BIND_DISABLED: 'Telegram binding is disabled.',
      TELEGRAM_BIND_INVALID_REQUEST:
        'The Telegram authorization request is invalid or expired.',
      TELEGRAM_BIND_FLOW_INVALID:
        'This Telegram binding request has expired or has already been used.',
      TELEGRAM_BIND_SESSION_INVALID:
        'The login session that started this Telegram binding is no longer valid.',
      TELEGRAM_BIND_ALREADY_BOUND: 'This Telegram account is already bound.',
      TELEGRAM_BIND_USER_DELETED: 'This user account no longer exists.',
      TELEGRAM_BIND_USER_DISABLED: 'This user account is disabled.',
      TELEGRAM_BIND_INTERNAL_ERROR:
        'Telegram binding failed. Please try again.',
    }

    for (const [code, message] of Object.entries(expected)) {
      assert.equal(getServerErrorMessageKey({ code }), message)
    }

    assert.equal(
      getServerErrorMessageKey({
        response: {
          data: { code: 'TELEGRAM_BIND_INTERNAL_ERROR', message: 'raw detail' },
        },
      }),
      expected.TELEGRAM_BIND_INTERNAL_ERROR
    )
  })

  test('maps remaining auth session codes to catalog keys', () => {
    assert.equal(
      getServerErrorMessageKey({ code: 'AUTH_UNAUTHORIZED' }),
      'Session expired!'
    )
    assert.equal(
      getServerErrorMessageKey({ code: 'AUTH_SESSION_REVOKED' }),
      'This login session has been revoked.'
    )
    assert.equal(
      getServerErrorMessageKey({ code: 'AUTH_SESSION_MISMATCH' }),
      'Login session mismatch. Please sign in again.'
    )
  })
})

describe('localizeConsoleErrorText', () => {
  beforeEach(async () => {
    i18n.addResourceBundle(
      'zhCN',
      'translation',
      {
        'Unable to connect to the server': '无法连接服务器',
        'Request timed out': '请求超时',
        'Request failed': '请求失败',
        Unauthorized: '未授权',
        'Internal Server Error': '服务器内部错误',
      },
      true,
      true
    )
    await i18n.changeLanguage('zhCN')
  })

  test('maps axios transport English to Chinese chrome', () => {
    assert.equal(
      localizeConsoleErrorText('Network Error'),
      '无法连接服务器'
    )
    assert.equal(
      localizeConsoleErrorText('timeout of 10000ms exceeded'),
      '请求超时'
    )
    assert.equal(
      localizeConsoleErrorText('Request failed with status code 403'),
      '请求失败'
    )
    assert.equal(
      localizeConsoleErrorText('Failed to fetch'),
      '无法连接服务器'
    )
    assert.equal(localizeConsoleErrorText('Unauthorized'), '未授权')
    assert.equal(
      localizeConsoleErrorText('Internal Server Error'),
      '服务器内部错误'
    )
  })

  test('keeps already-Chinese backend copy', () => {
    assert.equal(
      localizeConsoleErrorText('用户名或密码错误'),
      '用户名或密码错误'
    )
  })

  test('hides unknown English on Chinese chrome and keeps it in English', async () => {
    assert.equal(
      localizeConsoleErrorText('upstream error: invalid api key'),
      '请求失败'
    )
    assert.equal(
      localizeConsoleErrorText('连接失败: upstream timeout', '请求失败'),
      '连接失败'
    )

    await i18n.changeLanguage('en')
    assert.equal(
      localizeConsoleErrorText('upstream error: invalid api key', 'Request failed'),
      'upstream error: invalid api key'
    )
  })
})
