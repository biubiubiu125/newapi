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
import { afterEach, describe, expect, it, vi } from 'vitest'

import { createImageGenerationTask, listImageTasks } from '../api'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('image task API client', () => {
  it('sends a generation request with the selected API key and client task id', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          task_id: 'task_generation_1',
          client_task_id: 'client_generation_1',
          status: 'queued',
          created_at: 100,
          updated_at: 100,
          result_available: false,
        }),
        { status: 202, headers: { 'Content-Type': 'application/json' } }
      )
    )
    vi.stubGlobal('fetch', fetchMock)

    const task = await createImageGenerationTask('sk-selected-key', {
      model: 'gpt-image-1',
      prompt: 'A lighthouse on a cliff',
      n: 1,
      client_task_id: 'client_generation_1',
    })

    expect(task).toMatchObject({
      task_id: 'task_generation_1',
      client_task_id: 'client_generation_1',
      status: 'queued',
    })
    const call = fetchMock.mock.calls[0]
    const init = call?.[1] as RequestInit | undefined
    expect(call?.[0]).toBe('/v1/image-tasks/generations')
    expect(init).toEqual(
      expect.objectContaining({
        method: 'POST',
        credentials: 'include',
        headers: {
          Accept: 'application/json',
          Authorization: 'Bearer sk-selected-key',
          'Content-Type': 'application/json',
        },
      })
    )
    expect(JSON.parse(String(init?.body))).toEqual({
      model: 'gpt-image-1',
      prompt: 'A lighthouse on a cliff',
      n: 1,
      client_task_id: 'client_generation_1',
    })
  })

  it('sends only task identifiers in a batched status lookup', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          data: [],
          not_found_ids: ['task_missing'],
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      )
    )
    vi.stubGlobal('fetch', fetchMock)

    const response = await listImageTasks('sk-selected-key', [
      'task_first',
      'task_missing',
    ])

    expect(response.not_found_ids).toEqual(['task_missing'])
    expect(fetchMock).toHaveBeenCalledWith(
      '/v1/image-tasks?ids=task_first%2Ctask_missing',
      expect.objectContaining({
        method: 'GET',
        headers: {
          Accept: 'application/json',
          Authorization: 'Bearer sk-selected-key',
        },
      })
    )
  })

  it('omits empty optional generation fields before sending JSON', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          task_id: 'task_generation_2',
          status: 'queued',
          created_at: 100,
          updated_at: 100,
          result_available: false,
        }),
        { status: 202, headers: { 'Content-Type': 'application/json' } }
      )
    )
    vi.stubGlobal('fetch', fetchMock)

    await createImageGenerationTask('sk-selected-key', {
      model: 'gpt-image-1',
      prompt: 'A lighthouse on a cliff',
      n: 1,
      size: '',
      quality: '',
      response_format: '',
      client_task_id: 'client_generation_2',
    })

    const call = fetchMock.mock.calls[0]
    const init = call?.[1] as RequestInit | undefined
    expect(call?.[0]).toBe('/v1/image-tasks/generations')
    expect(JSON.parse(String(init?.body))).toEqual({
      model: 'gpt-image-1',
      prompt: 'A lighthouse on a cliff',
      n: 1,
      client_task_id: 'client_generation_2',
    })
  })

  it('exposes the public image task error code when the server rejects a request', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            error: {
              code: 'image_task_unavailable',
              message: 'image task system is disabled',
              type: 'image_task_error',
            },
          }),
          { status: 503, headers: { 'Content-Type': 'application/json' } }
        )
      )
    )

    await expect(
      createImageGenerationTask('sk-selected-key', {
        model: 'gpt-image-1',
        prompt: 'A lighthouse on a cliff',
        client_task_id: 'client_generation_2',
      })
    ).rejects.toEqual(
      expect.objectContaining({
        code: 'image_task_unavailable',
        message: 'image task system is disabled',
        name: 'ImageTaskRequestError',
        status: 503,
      })
    )
  })
})
