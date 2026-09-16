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

import { parseSidebarModulesAdmin } from './config'

describe('parseSidebarModulesAdmin', () => {
  test('inserts gpt_image in default console order instead of appending it', () => {
    const parsed = parseSidebarModulesAdmin(
      JSON.stringify({
        console: {
          enabled: true,
          detail: true,
          token: true,
          model_check: true,
          log: true,
          midjourney: true,
          image_tasks: true,
          task: true,
        },
      })
    )
    const keys = Object.keys(parsed.console)
    assert.equal(parsed.console.gpt_image, true)
    assert.ok(keys.indexOf('image_tasks') < keys.indexOf('gpt_image'))
    assert.ok(keys.indexOf('gpt_image') < keys.indexOf('task'))
  })
})
