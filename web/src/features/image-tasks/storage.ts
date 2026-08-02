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
import type { StoredImageTask } from './types'

const IMAGE_TASK_STORAGE_KEY = 'newapi:image-tasks:v1'

function isStoredImageTask(value: unknown): value is StoredImageTask {
  if (!value || typeof value !== 'object') return false
  const task = value as Partial<StoredImageTask>
  return (
    typeof task.taskId === 'string' &&
    task.taskId.trim().length > 0 &&
    typeof task.tokenId === 'number' &&
    Number.isInteger(task.tokenId) &&
    task.tokenId > 0 &&
    typeof task.createdAt === 'number' &&
    Number.isFinite(task.createdAt) &&
    task.createdAt >= 0
  )
}

export function loadStoredImageTasks(): StoredImageTask[] {
  try {
    const raw = window.localStorage.getItem(IMAGE_TASK_STORAGE_KEY)
    if (!raw) return []
    const value: unknown = JSON.parse(raw)
    if (!Array.isArray(value)) return []
    return value.filter(isStoredImageTask).slice(0, 100)
  } catch {
    return []
  }
}

export function saveStoredImageTasks(tasks: StoredImageTask[]): void {
  const normalized = tasks.filter(isStoredImageTask).slice(0, 100)
  window.localStorage.setItem(
    IMAGE_TASK_STORAGE_KEY,
    JSON.stringify(normalized)
  )
}
