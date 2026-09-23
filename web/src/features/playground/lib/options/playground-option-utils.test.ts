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
import { beforeEach, describe, expect, it } from 'vitest'

import { getOptionLoadErrorMessage } from './playground-option-utils'

describe('getOptionLoadErrorMessage', () => {
  beforeEach(async () => {
    i18n.addResourceBundle(
      'zhCN',
      'translation',
      {
        'Unable to connect to the server': '无法连接服务器',
        'Failed to load playground models': '加载 Playground 模型失败',
      },
      true,
      true
    )
    await i18n.changeLanguage('zhCN')
  })

  it('localizes axios Network Error instead of leaking English', () => {
    expect(
      getOptionLoadErrorMessage(
        new AxiosError('Network Error'),
        'Failed to load playground models'
      )
    ).toBe('无法连接服务器')
  })

  it('localizes the fallback chrome key', () => {
    expect(
      getOptionLoadErrorMessage(null, 'Failed to load playground models')
    ).toBe('加载 Playground 模型失败')
  })
})
