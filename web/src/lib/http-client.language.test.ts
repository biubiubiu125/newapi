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
import i18n from 'i18next'
import { afterEach, describe, expect, it } from 'vitest'

import { authClient } from './auth-session'
import { api } from './http-client'

describe('api Accept-Language', () => {
  const originalAdapter = api.defaults.adapter

  afterEach(() => {
    api.defaults.adapter = originalAdapter
  })

  it('sends a BCP-47 locale instead of the frontend language code', async () => {
    await i18n.changeLanguage('zhCN')
    let header: unknown
    api.defaults.adapter = async (config) => {
      header =
        config.headers?.get?.('Accept-Language') ??
        config.headers?.['Accept-Language']
      return {
        data: { success: true },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }

    await api.get('/__language_probe__', {
      skipErrorHandler: true,
      skipBusinessError: true,
      disableDuplicate: true,
    })

    expect(header).toBe('zh-CN')
  })
})

describe('authClient Accept-Language', () => {
  const originalAdapter = authClient.defaults.adapter

  afterEach(() => {
    authClient.defaults.adapter = originalAdapter
  })

  it('sends a BCP-47 locale on session refresh requests', async () => {
    await i18n.changeLanguage('zhCN')
    let header: unknown
    authClient.defaults.adapter = async (config) => {
      header =
        config.headers?.get?.('Accept-Language') ??
        config.headers?.['Accept-Language']
      return {
        data: { success: true },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }

    await authClient.get('/api/user/session/refresh')
    expect(header).toBe('zh-CN')
  })
})
