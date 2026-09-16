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
import { afterEach, describe, test } from 'vitest'

import { STORAGE_KEYS } from './constants'
import { applySystemNameToDom } from './dom-utils'

afterEach(() => {
  window.localStorage.clear()
  document.title = ''
})

describe('applySystemNameToDom', () => {
  test('rewrites leftover New API titles to RK API', () => {
    document.title = 'placeholder'
    const meta = document.createElement('meta')
    meta.setAttribute('name', 'title')
    meta.setAttribute('content', 'placeholder')
    document.head.appendChild(meta)

    applySystemNameToDom('New API')

    assert.equal(document.title, 'RK API')
    assert.equal(meta.getAttribute('content'), 'RK API')
    assert.equal(window.localStorage.getItem(STORAGE_KEYS.SYSTEM_NAME), 'RK API')
  })

  test('keeps a custom system name', () => {
    applySystemNameToDom('My Site')
    assert.equal(document.title, 'My Site')
    assert.equal(window.localStorage.getItem(STORAGE_KEYS.SYSTEM_NAME), 'My Site')
  })
})
