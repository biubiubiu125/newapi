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

import { getOAuthLoginDisplayError } from './login-display-error'

describe('getOAuthLoginDisplayError', () => {
  beforeEach(async () => {
    i18n.addResourceBundle(
      'zhCN',
      'translation',
      {
        'Login failed': '登录失败',
        'Failed to initialize OAuth': '初始化 OAuth 失败',
        'Failed to start GitHub login': '启动 GitHub 登录失败',
        'This login session has been revoked.': '该登录会话已被撤销',
        'Unable to connect to the server': '无法连接服务器',
      },
      true,
      true
    )
    await i18n.changeLanguage('zhCN')
  })

  test('shows already-Chinese backend WeChat copy instead of swallowing AUTH mapping', () => {
    assert.equal(
      getOAuthLoginDisplayError({
        success: false,
        message: '验证码错误或已过期',
      }),
      '验证码错误或已过期'
    )
  })

  test('maps AUTH session codes instead of returning without a toast', () => {
    assert.equal(
      getOAuthLoginDisplayError({
        success: false,
        code: 'AUTH_SESSION_REVOKED',
        message: 'Unauthorized',
      }),
      '该登录会话已被撤销'
    )
  })

  test('localizes createOAuthFlow English fallback', () => {
    assert.equal(
      getOAuthLoginDisplayError(new Error('Failed to initialize OAuth')),
      '初始化 OAuth 失败'
    )
  })

  test('uses the caller fallback when nothing else is available', () => {
    assert.equal(
      getOAuthLoginDisplayError({}, 'Failed to start GitHub login'),
      '启动 GitHub 登录失败'
    )
  })

  test('localizes catalog OAuth denial instead of a provider English sentence', () => {
    i18n.addResourceBundle(
      'zhCN',
      'translation',
      {
        'Authorization was cancelled or access was denied.':
          '授权已取消，或该登录方式拒绝了访问。',
      },
      true,
      true
    )
    assert.equal(
      getOAuthLoginDisplayError({
        success: false,
        message: 'Authorization was cancelled or access was denied.',
      }),
      '授权已取消，或该登录方式拒绝了访问。'
    )
  })
})
