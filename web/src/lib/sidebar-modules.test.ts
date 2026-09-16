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

import {
  GPT_IMAGE_WORKBENCH_URL,
  SIDEBAR_MODULES_DEFAULT,
  SIDEBAR_MODULES_META,
  mergeWithDefaultSidebarModules,
} from './sidebar-modules'

describe('sidebar modules gpt image workbench', () => {
  test('defaults include a visible gpt_image console module', () => {
    assert.equal(SIDEBAR_MODULES_DEFAULT.console.gpt_image, true)
    assert.equal(
      SIDEBAR_MODULES_META.console.modules.gpt_image.title,
      'GPT生图工作台'
    )
    assert.equal(GPT_IMAGE_WORKBENCH_URL, 'https://gptimage.rkai6.com/')
    const keys = Object.keys(SIDEBAR_MODULES_DEFAULT.console)
    assert.ok(keys.indexOf('image_tasks') < keys.indexOf('gpt_image'))
    assert.ok(keys.indexOf('gpt_image') < keys.indexOf('task'))
  })

  test('merges gpt_image into saved admin configs that lack it', () => {
    const merged = mergeWithDefaultSidebarModules({
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
    assert.equal(merged.console.gpt_image, true)
  })

  test('keeps an explicit gpt_image hide setting', () => {
    const merged = mergeWithDefaultSidebarModules({
      console: {
        enabled: true,
        gpt_image: false,
      },
    })
    assert.equal(merged.console.gpt_image, false)
  })
})
