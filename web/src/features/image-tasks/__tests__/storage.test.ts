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
import { afterEach, describe, expect, it } from 'vitest'

import { loadStoredImageTasks, saveStoredImageTasks } from '../storage'

afterEach(() => {
  window.localStorage.clear()
})

describe('image task storage', () => {
  it('persists only task metadata and never an API key', () => {
    saveStoredImageTasks([
      {
        taskId: 'task_1',
        tokenId: 24,
        createdAt: 100,
      },
    ])

    expect(loadStoredImageTasks()).toEqual([
      {
        taskId: 'task_1',
        tokenId: 24,
        createdAt: 100,
      },
    ])
    expect(window.localStorage.getItem('newapi:image-tasks:v1')).toBe(
      '[{"taskId":"task_1","tokenId":24,"createdAt":100}]'
    )
  })

  it('ignores malformed task metadata restored from local storage', () => {
    window.localStorage.setItem(
      'newapi:image-tasks:v1',
      JSON.stringify([
        { taskId: 'task_valid', tokenId: 24, createdAt: 100 },
        { taskId: '', tokenId: 24, createdAt: 100 },
        { taskId: 'task_invalid_token', tokenId: 0, createdAt: 100 },
      ])
    )

    expect(loadStoredImageTasks()).toEqual([
      {
        taskId: 'task_valid',
        tokenId: 24,
        createdAt: 100,
      },
    ])
  })
})
