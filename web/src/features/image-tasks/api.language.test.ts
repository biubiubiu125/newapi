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
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  downloadImageTaskResult,
  ImageTaskRequestError,
  listImageTasks,
} from './api'

describe('image workbench request language', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('sends Accept-Language as a BCP-47 tag', async () => {
    await i18n.changeLanguage('zhCN')
    const fetchMock = vi.fn(
      async () =>
        new Response(JSON.stringify({ data: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
    )
    vi.stubGlobal('fetch', fetchMock)

    await listImageTasks('sk-test', ['task-1'])

    const init = fetchMock.mock.calls[0]?.[1] as RequestInit
    const headers = new Headers(init.headers)
    expect(headers.get('Accept-Language')).toBe('zh-CN')
  })

  it('localizes the fallback workbench error', async () => {
    i18n.addResourceBundle(
      'zhCN',
      'translation',
      { 'Image workbench request failed': '图片工作台请求失败' },
      true,
      true
    )
    await i18n.changeLanguage('zhCN')
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async () =>
          new Response('{}', {
            status: 500,
            headers: { 'Content-Type': 'application/json' },
          })
      )
    )

    await expect(listImageTasks('sk-test', ['task-1'])).rejects.toEqual(
      expect.objectContaining({
        name: 'ImageTaskRequestError',
        message: '图片工作台请求失败',
      })
    )
    await expect(listImageTasks('sk-test', ['task-1'])).rejects.toBeInstanceOf(
      ImageTaskRequestError
    )
  })

  it('localizes the download fallback workbench error', async () => {
    i18n.addResourceBundle(
      'zhCN',
      'translation',
      { 'Image workbench request failed': '图片工作台请求失败' },
      true,
      true
    )
    await i18n.changeLanguage('zhCN')
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async () =>
          new Response('{}', {
            status: 500,
            headers: { 'Content-Type': 'application/json' },
          })
      )
    )

    await expect(
      downloadImageTaskResult('sk-test', 'task-1', 0)
    ).rejects.toEqual(
      expect.objectContaining({
        name: 'ImageTaskRequestError',
        message: '图片工作台请求失败',
      })
    )
  })
})
