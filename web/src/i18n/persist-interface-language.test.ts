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
import { AxiosError } from 'axios'
import i18n from 'i18next'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { getSavedLanguage } from '@/features/auth/lib/auth-redirect'
import { changeInterfaceLanguage } from '@/i18n/languages'
import {
  displayedInterfaceLanguage,
  persistInterfaceLanguageErrorMessage,
  persistSignedInInterfaceLanguage,
  syncSignedInInterfaceLanguage,
} from '@/i18n/persist-interface-language'
import { applyAuthRotation } from '@/lib/auth-session'
import { api } from '@/lib/http-client'
import {
  useAuthStore,
  type AuthUser,
  type LoginSession,
} from '@/stores/auth-store'

const session: LoginSession = {
  sid: 'sid-1',
  current: true,
  login_method: 'password',
  ip: '127.0.0.1',
  user_agent: 'vitest',
  created_at: 1,
  last_active_at: 1,
  expires_at: 9_999_999_999,
}

function signedInUser(language: string): AuthUser {
  return {
    id: 1,
    username: 'user',
    role: 1,
    setting: JSON.stringify({ language }),
  }
}

function mockPutResponse(body: { success: boolean; message?: string }) {
  api.defaults.adapter = async (config) => ({
    data: body,
    status: 200,
    statusText: 'OK',
    headers: {},
    config,
  })
}

describe('persistSignedInInterfaceLanguage', () => {
  const originalAdapter = api.defaults.adapter

  beforeEach(async () => {
    await i18n.changeLanguage('zhCN')
    useAuthStore.getState().auth.reset()
    useAuthStore.getState().auth.setBundle({
      access_token: 'access-a',
      token_type: 'Bearer',
      access_expires_at: 9_999_999_999,
      user: signedInUser('zhCN'),
      session,
    })
  })

  afterEach(() => {
    api.defaults.adapter = originalAdapter
    useAuthStore.getState().auth.reset()
  })

  it('reverts the UI language when the dashboard returns success:false', async () => {
    mockPutResponse({ success: false, message: '更新失败' })

    await expect(
      changeInterfaceLanguage('en', persistSignedInInterfaceLanguage)
    ).rejects.toThrow('更新失败')
    expect(i18n.language).toBe('zhCN')
    expect(getSavedLanguage(useAuthStore.getState().auth.user!)).toBe('zhCN')
  })

  it('keeps the saved language through token rotation after a successful persist', async () => {
    mockPutResponse({ success: true, message: '' })

    await changeInterfaceLanguage('en', persistSignedInInterfaceLanguage)

    expect(i18n.language).toBe('en')
    expect(getSavedLanguage(useAuthStore.getState().auth.user!)).toBe('en')

    applyAuthRotation({
      access_token: 'access-b',
      token_type: 'Bearer',
      access_expires_at: 9_999_999_999,
      session,
    })

    expect(getSavedLanguage(useAuthStore.getState().auth.user!)).toBe('en')
    expect(i18n.language).toBe('en')
  })

  it('prefers the signed-in user language over a stale profile setting', () => {
    expect(
      displayedInterfaceLanguage({
        user: signedInUser('en'),
        profileLanguage: 'zhCN',
        fallback: 'zhCN',
      })
    ).toBe('en')
  })

  it('persists the current guest language when the signed-in user has none saved', async () => {
    mockPutResponse({ success: true, message: '' })
    await i18n.changeLanguage('en')
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'user',
      role: 1,
    })

    await syncSignedInInterfaceLanguage(useAuthStore.getState().auth.user)

    expect(i18n.language).toBe('en')
    expect(getSavedLanguage(useAuthStore.getState().auth.user!)).toBe('en')
  })

  it('does not fail session restore when persisting a missing language errors', async () => {
    mockPutResponse({ success: false, message: '更新失败' })
    await i18n.changeLanguage('en')
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'user',
      role: 1,
    })

    await expect(
      syncSignedInInterfaceLanguage(useAuthStore.getState().auth.user)
    ).resolves.toBeUndefined()
    expect(i18n.language).toBe('en')
    expect(getSavedLanguage(useAuthStore.getState().auth.user!)).toBeUndefined()
  })

  it('localizes axios Network Error when persisting language fails', async () => {
    i18n.addResourceBundle(
      'zhCN',
      'translation',
      { 'Unable to connect to the server': '无法连接服务器' },
      true,
      true
    )
    expect(
      persistInterfaceLanguageErrorMessage(
        new AxiosError('Network Error'),
        'fallback'
      )
    ).toBe('无法连接服务器')
  })

  it('does not persist when the signed-in user already has a language', async () => {
    let putCount = 0
    api.defaults.adapter = async (config) => {
      putCount += 1
      return {
        data: { success: true, message: '' },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }
    await i18n.changeLanguage('en')

    await syncSignedInInterfaceLanguage(useAuthStore.getState().auth.user)

    expect(putCount).toBe(0)
    expect(i18n.language).toBe('zhCN')
    expect(getSavedLanguage(useAuthStore.getState().auth.user!)).toBe('zhCN')
  })
})
