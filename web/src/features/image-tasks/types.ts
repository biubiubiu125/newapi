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
export type PublicImageTaskError = {
  code: string
  message: string
}

export type PublicImageTask = {
  task_id: string
  client_task_id?: string
  status: string
  progress?: string
  created_at: number
  updated_at: number
  started_at?: number
  completed_at?: number
  result_available: boolean
  result_expires_at?: number
  result_acknowledged_at?: number
  error?: PublicImageTaskError
}

export type PublicImageTaskList = {
  data: PublicImageTask[]
  not_found_ids?: string[]
}

export type ImageGenerationTaskInput = {
  model: string
  prompt: string
  n?: number
  size?: string
  quality?: string
  client_task_id: string
}

export type ImageEditTaskInput = ImageGenerationTaskInput & {
  images: File[]
  mask?: File
}

export type ImageTaskMode = 'generation' | 'edit'

export type StoredImageTask = {
  taskId: string
  tokenId: number
  createdAt: number
  prompt?: string
  model?: string
  size?: string
  quality?: string
  mode?: ImageTaskMode
}

export type ImageTaskResult = {
  created?: number
  data?: Array<{
    url?: string
    b64_json?: string
    revised_prompt?: string
  }>
}

export type ImageTaskDownload = {
  blob: Blob
  filename: string
}
