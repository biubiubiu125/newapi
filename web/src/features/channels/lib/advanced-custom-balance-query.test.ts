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

import {
  ADVANCED_CUSTOM_BALANCE_PATH,
  createAdvancedCustomManagementRoute,
  validateAdvancedCustomConfig,
} from './advanced-custom'

const BALANCE_QUERY_KEYS = [
  'Only one Balance Query route is allowed',
  'Balance Query route does not support client model rules',
  'Balance Query route must use native forwarding',
  'Balance Query upstream path must not contain {model}',
] as const

describe('advanced custom balance query errors', () => {
  beforeEach(async () => {
    await whenInterfaceLanguageReady
    i18n.addResourceBundle('zhCN', 'translation', zh.translation, true, true)
    await i18n.changeLanguage('zhCN')
  })

  test('balance query validation text is simplified Chinese', () => {
    const balance = createAdvancedCustomManagementRoute(
      ADVANCED_CUSTOM_BALANCE_PATH
    )
    const messages = [
      validateAdvancedCustomConfig({
        advanced_routes: [
          balance,
          createAdvancedCustomManagementRoute(ADVANCED_CUSTOM_BALANCE_PATH),
        ],
      })?.message,
      validateAdvancedCustomConfig({
        advanced_routes: [{ ...balance, models: ['gpt-4'] }],
      })?.message,
      validateAdvancedCustomConfig({
        advanced_routes: [
          {
            ...balance,
            converter: 'anthropic_messages_to_openai_chat_completions',
          },
        ],
      })?.message,
      validateAdvancedCustomConfig({
        advanced_routes: [{ ...balance, upstream_path: '/v1/{model}' }],
      })?.message,
    ]

    assert.deepEqual(messages, [...BALANCE_QUERY_KEYS])
    for (const key of BALANCE_QUERY_KEYS) {
      const translated = i18n.t(key, { keySeparator: false })
      assert.notEqual(translated, key)
      assert.match(translated, /余额查询/)
      assert.doesNotMatch(translated, /Balance Query/)
    }
  })
})
