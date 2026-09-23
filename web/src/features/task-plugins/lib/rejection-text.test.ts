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
import { beforeEach, describe, test } from 'vitest'

import i18n, { whenInterfaceLanguageReady } from '@/i18n/config'
import zh from '@/i18n/locales/zh.json'

import { pluginRejectionText, pluginVisibleError } from './rejection-text'

describe('plugin rejection text', () => {
  beforeEach(async () => {
    await whenInterfaceLanguageReady
    i18n.addResourceBundle('zhCN', 'translation', zh.translation, true, true)
    await i18n.changeLanguage('zhCN')
  })

  test('keeps a Chinese conflict that names both plugins', () => {
    const message =
      '插件 alpha-plugin 的模型 "model-a" 与插件 beta-plugin 的模型 "model-a" 冲突'
    assert.equal(pluginRejectionText(message), message)
  })

  test('hides transport English on Chinese chrome', () => {
    assert.equal(pluginRejectionText('Network Error'), '无法连接服务器')
    assert.equal(
      pluginRejectionText('plugin alpha model "a" conflicts with plugin beta model "a"'),
      '网关拒绝了该插件'
    )
  })

  test('uses the caller fallback for internal English', () => {
    assert.equal(
      pluginRejectionText('source not fetched', 'Failed to load'),
      '加载失败'
    )
    assert.equal(
      pluginRejectionText('missing marketplace entry', 'Failed to load'),
      '加载失败'
    )
  })

  test('prefers a Chinese response body over the axios English message', () => {
    const message =
      '插件 alpha-plugin 的模型 "model-a" 与插件 beta-plugin 的模型 "model-a" 冲突'
    const error = Object.assign(new Error('Request failed with status code 400'), {
      response: { data: { message } },
    })
    assert.equal(pluginVisibleError(error, 'Failed to load'), message)
    assert.equal(
      pluginVisibleError(new Error('Network Error'), 'Failed to load'),
      '无法连接服务器'
    )
    assert.equal(
      pluginVisibleError(new Error('source not fetched'), 'Failed to load'),
      '加载失败'
    )
  })
})
