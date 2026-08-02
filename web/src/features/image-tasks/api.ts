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
import type {
  ImageEditTaskInput,
  ImageGenerationTaskInput,
  ImageTaskResult,
  PublicImageTask,
  PublicImageTaskList,
} from './types'

type PublicImageTaskErrorResponse = {
  error?: {
    code?: string
    message?: string
  }
}

function generationPayload(
  input: ImageGenerationTaskInput
): Record<string, unknown> {
  const payload: Record<string, unknown> = {
    model: input.model,
    prompt: input.prompt,
    client_task_id: input.client_task_id,
  }
  if (input.n) payload.n = input.n
  if (input.size?.trim()) payload.size = input.size.trim()
  if (input.quality?.trim()) payload.quality = input.quality.trim()
  if (input.response_format?.trim()) {
    payload.response_format = input.response_format.trim()
  }
  return payload
}

export class ImageTaskRequestError extends Error {
  status: number
  code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ImageTaskRequestError'
    this.status = status
    this.code = code
  }
}

async function imageTaskRequest<T>(
  apiKey: string,
  path: string,
  init: RequestInit
): Promise<T> {
  const response = await fetch(path, {
    ...init,
    credentials: 'include',
    headers: {
      Accept: 'application/json',
      Authorization: `Bearer ${apiKey}`,
      ...init.headers,
    },
  })
  const payload = (await response.json().catch(() => null)) as
    | T
    | PublicImageTaskErrorResponse
    | null

  if (response.ok) {
    return payload as T
  }

  const error = (payload as PublicImageTaskErrorResponse | null)?.error
  throw new ImageTaskRequestError(
    response.status,
    error?.code || 'image_task_request_failed',
    error?.message || 'Image task request failed'
  )
}

export function createImageGenerationTask(
  apiKey: string,
  input: ImageGenerationTaskInput
): Promise<PublicImageTask> {
  return imageTaskRequest(apiKey, '/v1/image-tasks/generations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(generationPayload(input)),
  })
}

export function createImageEditTask(
  apiKey: string,
  input: ImageEditTaskInput
): Promise<PublicImageTask> {
  const form = new FormData()
  form.set('image', input.image)
  form.set('model', input.model)
  form.set('prompt', input.prompt)
  form.set('client_task_id', input.client_task_id)
  if (input.mask) form.set('mask', input.mask)
  if (input.n) form.set('n', String(input.n))
  if (input.size) form.set('size', input.size)
  if (input.quality) form.set('quality', input.quality)
  if (input.response_format) form.set('response_format', input.response_format)

  return imageTaskRequest(apiKey, '/v1/image-tasks/edits', {
    method: 'POST',
    body: form,
  })
}

export function listImageTasks(
  apiKey: string,
  taskIds: string[]
): Promise<PublicImageTaskList> {
  const query = new URLSearchParams({ ids: taskIds.join(',') })
  return imageTaskRequest(apiKey, `/v1/image-tasks?${query.toString()}`, {
    method: 'GET',
  })
}

export function getImageTask(
  apiKey: string,
  taskId: string
): Promise<PublicImageTask> {
  return imageTaskRequest(
    apiKey,
    `/v1/image-tasks/${encodeURIComponent(taskId)}`,
    { method: 'GET' }
  )
}

export function getImageTaskResult(
  apiKey: string,
  taskId: string
): Promise<ImageTaskResult> {
  return imageTaskRequest(
    apiKey,
    `/v1/image-tasks/${encodeURIComponent(taskId)}/result`,
    { method: 'GET' }
  )
}

export function acknowledgeImageTaskResult(
  apiKey: string,
  taskId: string
): Promise<PublicImageTask> {
  return imageTaskRequest(
    apiKey,
    `/v1/image-tasks/${encodeURIComponent(taskId)}/ack`,
    { method: 'POST' }
  )
}

export function cancelImageTask(
  apiKey: string,
  taskId: string
): Promise<PublicImageTask> {
  return imageTaskRequest(
    apiKey,
    `/v1/image-tasks/${encodeURIComponent(taskId)}/cancel`,
    { method: 'POST' }
  )
}
