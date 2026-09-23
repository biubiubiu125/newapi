import assert from 'node:assert/strict'
import i18n from 'i18next'
import { beforeEach, describe, test } from 'vitest'

import { getPasskeyDisplayError, isPasskeyCancelledError } from './display-error'

describe('getPasskeyDisplayError', () => {
  beforeEach(async () => {
    i18n.addResourceBundle(
      'zhCN',
      'translation',
      {
        'Passkey login failed': 'Passkey 登录失败',
        'Failed to complete Passkey login': '完成 Passkey 登录失败',
        'Failed to register Passkey': '注册 Passkey 失败',
        'Passkey login was cancelled or timed out':
          'Passkey 登录已取消或超时',
        'This login session has been revoked.': '该登录会话已被撤销',
        'Unable to connect to the server': '无法连接服务器',
      },
      true,
      true
    )
    await i18n.changeLanguage('zhCN')
  })

  test('maps NotAllowedError to the cancelled copy', () => {
    const error = new DOMException('The operation was aborted.', 'NotAllowedError')
    assert.equal(isPasskeyCancelledError(error), true)
    assert.equal(
      getPasskeyDisplayError(error),
      'Passkey 登录已取消或超时'
    )
  })

  test('maps AUTH session codes instead of silent English', () => {
    assert.equal(
      getPasskeyDisplayError({
        success: false,
        code: 'AUTH_SESSION_REVOKED',
        message: 'This login session has been revoked.',
      }),
      '该登录会话已被撤销'
    )
  })

  test('keeps already-Chinese backend copy', () => {
    assert.equal(
      getPasskeyDisplayError(new Error('当前账号未设置密码，请使用密码重置或联系管理员重置密码')),
      '当前账号未设置密码，请使用密码重置或联系管理员重置密码'
    )
  })

  test('hides unknown WebAuthn English behind the Passkey fallback', () => {
    assert.equal(
      getPasskeyDisplayError(new Error('The operation is insecure.')),
      'Passkey 登录失败'
    )
    assert.equal(
      getPasskeyDisplayError(
        new Error('The operation is insecure.'),
        'Failed to register Passkey'
      ),
      '注册 Passkey 失败'
    )
  })
})
