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
import { describe, test } from 'vitest'

import { DEFAULT_SYSTEM_NAME, resolveSystemName } from './constants'

describe('system branding defaults', () => {
  test('uses RK API as the fallback system name', () => {
    assert.equal(DEFAULT_SYSTEM_NAME, 'RK API')
  })

  test('rewrites leftover New API and RKAPI names', () => {
    assert.equal(resolveSystemName('New API'), 'RK API')
    assert.equal(resolveSystemName('NEW API'), 'RK API')
    assert.equal(resolveSystemName(' NewAPI '), 'RK API')
    assert.equal(resolveSystemName('RKAPI'), 'RK API')
    assert.equal(resolveSystemName(''), 'RK API')
    assert.equal(resolveSystemName('My Site'), 'My Site')
  })
})
