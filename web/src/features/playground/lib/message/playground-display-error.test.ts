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
import { beforeEach, describe, expect, it } from 'vitest'

import { ERROR_MESSAGES } from '../../constants'
import { getPlaygroundDisplayError } from './playground-display-error'

describe('getPlaygroundDisplayError', () => {
  beforeEach(async () => {
    i18n.addResourceBundle(
      'zhCN',
      'translation',
      {
        [ERROR_MESSAGES.NETWORK_ERROR]: '网络连接失败或服务器无响应',
        [ERROR_MESSAGES.CONNECTION_CLOSED]: '连接已关闭',
        'Invalid token': '无效令牌',
        'Request failed': '请求失败',
      },
      true,
      true
    )
    await i18n.changeLanguage('zhCN')
  })

  it('translates known playground chrome errors', () => {
    expect(getPlaygroundDisplayError(ERROR_MESSAGES.NETWORK_ERROR)).toBe(
      '网络连接失败或服务器无响应'
    )
  })

  it('translates protocol Invalid token when painted in the playground', () => {
    expect(getPlaygroundDisplayError('Invalid token')).toBe('无效令牌')
  })

  it('keeps already-Chinese copy', () => {
    expect(getPlaygroundDisplayError('渠道不可用')).toBe('渠道不可用')
  })
})
