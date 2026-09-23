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

import { getJsonError } from '@/features/system-settings/integrations/utils'
import { validateJsonString } from '@/features/system-settings/models/utils'
import i18n, { whenInterfaceLanguageReady } from '@/i18n/config'
import zh from '@/i18n/locales/zh.json'

const STATIC_FORM_KEYS = [
  'JSON structure is invalid for this setting',
  'Invalid JSON data',
  'Invalid JSON array',
  'Expected a JSON array of model identifiers',
  'Must be at least 0.002',
  'Must be at least 0.1',
  'Must be 1 or less',
  'Group name is required',
  'Must be ≥ 0',
  'Must be ≥ 1',
  'Must be ≤ 2,147,483,647',
  'Question is required',
  'Question must be less than 200 characters',
  'Answer is required',
  'Answer must be less than 1000 characters',
  'Product name is required',
  'Price must be greater than 0',
  'Quota must be at least 1',
  'Provide a valid callback URL starting with http:// or https://',
  'Wallet notice must be 1000 characters or fewer',
] as const

describe('settings JSON errors', () => {
  beforeEach(async () => {
    await whenInterfaceLanguageReady
    i18n.addResourceBundle('zhCN', 'translation', zh.translation, true, true)
    await i18n.changeLanguage('zhCN')
  })

  test('does not show the browser parser sentence on Chinese chrome', () => {
    const broken = '{\n  "a": 1\n  "b": 2\n}'
    const validated = validateJsonString(broken)
    const payment = getJsonError(broken)

    assert.equal(validated.valid, false)
    for (const message of [validated.message, payment]) {
      assert.equal(typeof message, 'string')
      assert.match(message ?? '', /第 3 行、第 3 列/)
      assert.match(message ?? '', /第 2 行/)
      assert.doesNotMatch(message ?? '', /Unexpected|Expected|Error at line|position/i)
    }
  })

  test('settings form strings exist in the simplified Chinese catalog', () => {
    const catalog = zh.translation as Record<string, string>
    for (const key of STATIC_FORM_KEYS) {
      const value = catalog[key]
      assert.equal(typeof value, 'string', key)
      assert.notEqual(value, key, key)
      assert.match(value, /\p{Script=Han}/u, key)
      assert.equal(i18n.t(key, { keySeparator: false }), value, key)
    }
  })
})
