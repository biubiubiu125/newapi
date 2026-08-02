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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { API_KEY_STATUS } from '@/features/keys/constants'

import {
  acknowledgeImageTaskResult,
  getImageTaskResult,
  listImageTasks,
} from '../api'
import { ImageTaskPage } from '../components/image-task-page'
import type { PublicImageTask, PublicImageTaskList } from '../types'

vi.mock('@/features/keys/api', () => ({
  fetchTokenKey: vi.fn(async () => ({
    success: true,
    data: { key: 'browser-visible-key' },
  })),
  getApiKeys: vi.fn(async () => ({
    success: true,
    data: {
      items: [
        {
          id: 24,
          name: 'Browser key',
          key: 'masked',
          status: API_KEY_STATUS.ENABLED,
          remain_quota: 100,
          used_quota: 0,
          unlimited_quota: false,
          expired_time: -1,
          created_time: 100,
          accessed_time: 0,
          group: 'default',
          auto_groups: null,
          cross_group_retry: false,
          model_limits_enabled: false,
          model_limits: '',
          allow_ips: '',
        },
      ],
      total: 1,
      page: 1,
      page_size: 100,
    },
  })),
}))

vi.mock('../api', () => ({
  ImageTaskRequestError: class ImageTaskRequestError extends Error {
    status: number
    code: string

    constructor(status: number, code: string, message: string) {
      super(message)
      this.name = 'ImageTaskRequestError'
      this.status = status
      this.code = code
    }
  },
  acknowledgeImageTaskResult: vi.fn(),
  cancelImageTask: vi.fn(),
  createImageEditTask: vi.fn(),
  createImageGenerationTask: vi.fn(),
  getImageTaskResult: vi.fn(),
  listImageTasks: vi.fn(),
}))

vi.mock('sonner', () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}))

const storageKey = 'newapi:image-tasks:v1'
const listImageTasksMock = vi.mocked(listImageTasks)
const getImageTaskResultMock = vi.mocked(getImageTaskResult)
const acknowledgeImageTaskResultMock = vi.mocked(acknowledgeImageTaskResult)

function task(overrides: Partial<PublicImageTask> = {}): PublicImageTask {
  return {
    task_id: 'task_visible',
    status: 'queued',
    created_at: 100,
    updated_at: 100,
    result_available: false,
    ...overrides,
  }
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const view = render(
    <QueryClientProvider client={queryClient}>
      <ImageTaskPage />
    </QueryClientProvider>
  )
  return { queryClient, ...view }
}

function saveTasks(taskIds: string[]) {
  window.localStorage.setItem(
    storageKey,
    JSON.stringify(
      taskIds.map((taskId, index) => ({
        taskId,
        tokenId: 24,
        createdAt: 100 + index,
      }))
    )
  )
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((innerResolve) => {
    resolve = innerResolve
  })
  return { promise, resolve }
}

afterEach(() => {
  window.localStorage.clear()
  vi.clearAllMocks()
})

beforeEach(() => {
  acknowledgeImageTaskResultMock.mockResolvedValue(
    task({
      status: 'completed',
      result_available: true,
      result_acknowledged_at: 500,
    })
  )
})

describe('image task page', () => {
  it('does not immediately poll again when a refresh finishes', async () => {
    saveTasks(['task_visible'])
    const firstRefresh = deferred<PublicImageTaskList>()
    listImageTasksMock.mockReturnValueOnce(firstRefresh.promise)

    renderPage()

    await waitFor(() => expect(listImageTasksMock).toHaveBeenCalledTimes(1))

    await act(async () => {
      firstRefresh.resolve({
        data: [task({ task_id: 'task_visible' })],
      })
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(listImageTasksMock).toHaveBeenCalledTimes(1)
  })

  it('renders every returned image before acknowledging the result', async () => {
    saveTasks(['task_completed'])
    listImageTasksMock.mockResolvedValue({
      data: [
        task({
          task_id: 'task_completed',
          status: 'completed',
          result_available: true,
        }),
      ],
    })
    getImageTaskResultMock.mockResolvedValue({
      data: [
        { b64_json: 'Zmlyc3Q=' },
        { b64_json: 'c2Vjb25k' },
      ],
    })

    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: /View result/i }))

    await waitFor(() => {
      expect(screen.getAllByAltText('Image task result')).toHaveLength(2)
    })
    expect(acknowledgeImageTaskResultMock).toHaveBeenCalledWith(
      'sk-browser-visible-key',
      'task_completed'
    )
  })

  it('removes tasks reported as not found from local history', async () => {
    saveTasks(['task_visible', 'task_missing'])
    listImageTasksMock.mockResolvedValue({
      data: [task({ task_id: 'task_visible' })],
      not_found_ids: ['task_missing'],
    })

    renderPage()

    await waitFor(() =>
      expect(window.localStorage.getItem(storageKey)).toBe(
        '[{"taskId":"task_visible","tokenId":24,"createdAt":100}]'
      )
    )
    expect(screen.queryByText('task_missing')).not.toBeInTheDocument()
  })
})
