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
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import {
  Check,
  Download,
  Image as ImageIcon,
  RefreshCw,
  Square,
} from 'lucide-react'
import {
  type ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Separator } from '@/components/ui/separator'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { fetchTokenKey, getApiKeys } from '@/features/keys/api'
import { API_KEY_STATUS } from '@/features/keys/constants'

import {
  cancelImageTask,
  createImageEditTask,
  createImageGenerationTask,
  downloadImageTaskResult,
  getImageTaskResult,
  ImageTaskRequestError,
  listImageTasks,
} from '../api'
import { imageTaskResultRenderKey } from '../lib/render'
import {
  CUSTOM_IMAGE_TASK_SIZE_VALUE,
  IMAGE_TASK_SIZE_OPTIONS,
  MAX_IMAGE_TASK_COUNT,
  MAX_IMAGE_TASK_REFERENCE_IMAGES,
  getImageTaskRequestSize,
  imageTaskFormSchema,
  isLargeImageTaskSize,
  type ImageTaskFormValues,
} from '../lib/validation'
import { loadStoredImageTasks, saveStoredImageTasks } from '../storage'
import type {
  ImageTaskResult,
  PublicImageTask,
  StoredImageTask,
} from '../types'

type TaskRecord = StoredImageTask & {
  task: PublicImageTask | null
  resultUrls: string[]
  resultLoading: boolean
  resultError?: string
}

const DEFAULT_FORM_VALUES: ImageTaskFormValues = {
  tokenId: 0,
  mode: 'generation',
  model: 'gpt-image-2',
  prompt: '',
  n: 1,
  size: '1024x1024',
  quality: 'high',
  images: [],
  mask: null,
  customWidth: '',
  customHeight: '',
}

const AUTO_RESULT_CONCURRENCY = 3
const AUTO_RESULT_RETRY_BASE_MS = 5000
const AUTO_RESULT_RETRY_MAX_MS = 60000
const SAFE_DATA_IMAGE_MEDIA_TYPES = new Set([
  'image/png',
  'image/jpeg',
  'image/jpg',
  'image/webp',
  'image/gif',
  'image/bmp',
  'image/avif',
])

function summarizePrompt(prompt: string | undefined): string {
  const normalized = prompt?.trim().replaceAll(/\s+/g, ' ') ?? ''
  if (normalized.length <= 80) return normalized
  return `${normalized.slice(0, 80)}…`
}

function createClientTaskId(): string {
  if (
    typeof crypto !== 'undefined' &&
    typeof crypto.randomUUID === 'function'
  ) {
    return crypto.randomUUID()
  }

  if (
    typeof crypto !== 'undefined' &&
    typeof crypto.getRandomValues === 'function'
  ) {
    const bytes = new Uint8Array(16)
    crypto.getRandomValues(bytes)
    bytes[6] = (bytes[6] & 0x0f) | 0x40
    bytes[8] = (bytes[8] & 0x3f) | 0x80
    const hex = [...bytes].map((byte) => byte.toString(16).padStart(2, '0'))
    return [
      hex.slice(0, 4).join(''),
      hex.slice(4, 6).join(''),
      hex.slice(6, 8).join(''),
      hex.slice(8, 10).join(''),
      hex.slice(10, 16).join(''),
    ].join('-')
  }

  return `client_${Date.now().toString(36)}_${Math.random()
    .toString(36)
    .slice(2, 12)}`
}

function formatRequestSize(
  size: string | undefined,
  t: (key: string) => string
): string {
  const normalized = size?.trim()
  if (!normalized) return '-'
  const option = IMAGE_TASK_SIZE_OPTIONS.find(
    (item) => item.value === normalized
  )
  if (!option) return normalized
  return `${option.value} (${t(option.labelKey)})`
}

function formatMode(
  mode: StoredImageTask['mode'] | undefined,
  t: (key: string) => string
): string {
  if (mode === 'generation') return t('Text-to-image')
  if (mode === 'edit') return t('Image-to-image')
  return '-'
}

function formatQuality(
  quality: string | undefined,
  t: (key: string) => string
): string {
  switch (quality?.trim().toLowerCase()) {
    case 'auto':
      return t('Auto')
    case 'low':
      return t('Low')
    case 'medium':
      return t('Medium')
    case 'high':
      return t('High')
    default:
      return quality?.trim() || t('Model default')
  }
}

function getStatusVariant(
  status: string
): 'success' | 'warning' | 'danger' | 'info' | 'neutral' {
  switch (status) {
    case 'completed':
      return 'success'
    case 'queued':
    case 'running':
    case 'finalizing':
    case 'cancelling':
      return 'warning'
    case 'failed':
      return 'danger'
    case 'cancelled':
      return 'neutral'
    default:
      return 'info'
  }
}

function getStatusLabel(status: string, t: (key: string) => string): string {
  switch (status) {
    case 'completed':
      return t('Completed')
    case 'queued':
      return t('Queued')
    case 'running':
      return t('Running')
    case 'finalizing':
      return t('Finalizing')
    case 'cancelling':
      return t('Cancelling')
    case 'failed':
      return t('Failed')
    case 'cancelled':
      return t('Cancelled')
    default:
      return status
  }
}

function formatTaskTime(timestamp: number): string {
  if (!timestamp) return '-'
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'short',
    timeStyle: 'medium',
  }).format(timestamp * 1000)
}

function formatWeakTaskId(taskId: string): string {
  if (taskId.length <= 8) return taskId
  return `…${taskId.slice(-8)}`
}

async function resultToUrls(
  result: ImageTaskResult,
  loadURLResultPreview?: (imageIndex: number) => Promise<string | null>
): Promise<string[] | null> {
  const items = result.data ?? []
  if (items.length === 0) return null

  const urls: Array<string | null> = []
  for (let imageIndex = 0; imageIndex < items.length; imageIndex += 1) {
    const item = items[imageIndex]
    if (item.b64_json) {
      urls.push(imageDataUrlFromB64JSON(item.b64_json))
      continue
    }
    if (!item.url) return null

    const dataImageUrl = normalizeSafeDataImageUrl(item.url)
    if (dataImageUrl) {
      urls.push(dataImageUrl)
      continue
    }

    if (!loadURLResultPreview) return null
    urls.push(await loadURLResultPreview(imageIndex))
    continue
  }

  return urls.every((url): url is string => Boolean(url)) ? urls : null
}

function isImageTaskPreviewObjectUrl(url: string): boolean {
  return url.startsWith('blob:')
}

function revokeImageTaskPreviewObjectUrls(urls: string[] | undefined): void {
  if (typeof URL.revokeObjectURL !== 'function') return
  for (const url of urls ?? []) {
    if (isImageTaskPreviewObjectUrl(url)) {
      URL.revokeObjectURL(url)
    }
  }
}

function imageDataUrlFromB64JSON(value: string): string | null {
  const cleanBase64 = value.replaceAll(/\s+/g, '')
  if (!cleanBase64) return null
  const mediaType = sniffImageMediaTypeFromBase64(cleanBase64) ?? 'image/png'
  return `data:${mediaType};base64,${cleanBase64}`
}

function sniffImageMediaTypeFromBase64(value: string): string | null {
  const bytes = decodeBase64Prefix(value, 32)
  if (bytes.length === 0) return null

  if (
    bytes.length >= 8 &&
    bytes[0] === 0x89 &&
    bytes[1] === 0x50 &&
    bytes[2] === 0x4e &&
    bytes[3] === 0x47 &&
    bytes[4] === 0x0d &&
    bytes[5] === 0x0a &&
    bytes[6] === 0x1a &&
    bytes[7] === 0x0a
  ) {
    return 'image/png'
  }

  if (
    bytes.length >= 3 &&
    bytes[0] === 0xff &&
    bytes[1] === 0xd8 &&
    bytes[2] === 0xff
  ) {
    return 'image/jpeg'
  }

  const header = String.fromCharCode(...bytes)
  if (header.startsWith('GIF87a') || header.startsWith('GIF89a')) {
    return 'image/gif'
  }
  if (header.startsWith('RIFF') && header.slice(8, 12) === 'WEBP') {
    return 'image/webp'
  }
  if (header.startsWith('BM')) {
    return 'image/bmp'
  }
  if (header.slice(4, 8) === 'ftyp' && /avif|avis/.test(header.slice(8))) {
    return 'image/avif'
  }

  return null
}

function decodeBase64Prefix(value: string, maxBytes: number): number[] {
  if (typeof atob !== 'function') return []
  const chunkLength = Math.ceil(maxBytes / 3) * 4
  const chunk = value.slice(0, chunkLength)
  if (!chunk) return []
  const paddedChunk = chunk.padEnd(Math.ceil(chunk.length / 4) * 4, '=')
  try {
    return [...atob(paddedChunk)]
      .slice(0, maxBytes)
      .map((char) => char.charCodeAt(0))
  } catch {
    return []
  }
}

function normalizeSafeDataImageUrl(value: string): string | null {
  const trimmed = value.trim()
  const match = /^data:([^;,]+);base64,([A-Za-z0-9+/]+={0,2})$/i.exec(trimmed)
  if (!match) return null
  const mediaType = match[1]?.toLowerCase()
  if (!mediaType || !SAFE_DATA_IMAGE_MEDIA_TYPES.has(mediaType)) return null
  return trimmed
}

function triggerImageTaskDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.style.display = 'none'
  document.body.appendChild(link)
  link.click()
  link.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 0)
}

class ImageTaskPermanentResultError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'ImageTaskPermanentResultError'
  }
}

function taskErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof ImageTaskRequestError) {
    return `${error.code}: ${error.message}`
  }
  if (error instanceof ImageTaskPermanentResultError) {
    return error.message
  }
  if (error instanceof Error) return error.message
  return fallback
}

function getImageTaskRequestErrorDetails(error: unknown): {
  status?: number
  code?: string
  message?: string
} | null {
  if (!error || typeof error !== 'object') return null
  const candidate = error as {
    status?: unknown
    code?: unknown
    message?: unknown
  }
  const status =
    typeof candidate.status === 'number' ? candidate.status : undefined
  const code = typeof candidate.code === 'string' ? candidate.code : undefined
  const message =
    typeof candidate.message === 'string' ? candidate.message : undefined
  if (status === undefined && code === undefined) {
    return null
  }
  return { status, code, message }
}

function isRetryableImageTaskResultError(error: unknown): boolean {
  if (error instanceof ImageTaskPermanentResultError) {
    return false
  }
  const details = getImageTaskRequestErrorDetails(error)
  if (!details) {
    return true
  }
  return (
    details.status === 429 ||
    details.status === 503 ||
    details.code === 'rate_limit_exceeded' ||
    details.code === 'result_temporarily_unavailable'
  )
}

function imageTaskResultErrorMessage(
  error: unknown,
  t: (key: string) => string
): string {
  const details = getImageTaskRequestErrorDetails(error)
  if (details) {
    if (details.code === 'result_expired' || details.status === 410) {
      return t('Image generation result is no longer available')
    }
    if (details.code === 'task_not_found' || details.status === 404) {
      return t('Image generation task is no longer available')
    }
  }
  return taskErrorMessage(error, t('Request failed'))
}

export function ImageTaskPage() {
  const { t } = useTranslation()
  const [storedTasks, setStoredTasks] = useState<StoredImageTask[]>(() =>
    loadStoredImageTasks()
  )
  const storedTasksRef = useRef(storedTasks)
  const [taskRecords, setTaskRecords] = useState<Record<string, TaskRecord>>({})
  const taskRecordsRef = useRef(taskRecords)
  const autoResultTaskIdsRef = useRef<Set<string>>(new Set())
  const autoResultRetryTimersRef = useRef<Record<string, number | undefined>>(
    {}
  )
  const autoResultRetryAttemptsRef = useRef<Record<string, number | undefined>>(
    {}
  )
  const autoResultInFlightRef = useRef(0)
  const autoResultErrorNotifiedRef = useRef<Set<string>>(new Set())
  const resultPreviewObjectUrlsRef = useRef<Record<string, string[] | undefined>>(
    {}
  )
  const [autoResultRetryTick, setAutoResultRetryTick] = useState(0)
  const resolvedKeysRef = useRef<Record<number, string>>({})
  const pendingKeyRequests = useRef<
    Record<number, Promise<string | null> | undefined>
  >({})
  const [refreshing, setRefreshing] = useState(false)
  const refreshingRef = useRef(false)

  const updateStoredTasks = useCallback(
    (updater: (previous: StoredImageTask[]) => StoredImageTask[]) => {
      const next = updater(storedTasksRef.current)
      storedTasksRef.current = next
      setStoredTasks(next)
    },
    []
  )

  const updateTaskRecords = useCallback(
    (
      updater: (previous: Record<string, TaskRecord>) => Record<string, TaskRecord>
    ) => {
      const next = updater(taskRecordsRef.current)
      taskRecordsRef.current = next
      setTaskRecords(next)
    },
    []
  )

  const keyQuery = useQuery({
    queryKey: ['image-task-api-keys'],
    queryFn: async () => {
      const response = await getApiKeys({ p: 1, size: 100 })
      if (!response.success) {
        throw new Error(response.message || t('Request failed'))
      }
      return response.data?.items ?? []
    },
    staleTime: 60 * 1000,
  })

  const enabledKeys = useMemo(
    () =>
      (keyQuery.data ?? []).filter(
        (apiKey) => apiKey.status === API_KEY_STATUS.ENABLED
      ),
    [keyQuery.data]
  )

  const resolveKey = useCallback(
    async (tokenId: number): Promise<string | null> => {
      if (resolvedKeysRef.current[tokenId]) {
        return resolvedKeysRef.current[tokenId]
      }
      if (pendingKeyRequests.current[tokenId]) {
        return pendingKeyRequests.current[tokenId]
      }

      const request = (async () => {
        try {
          const response = await fetchTokenKey(tokenId)
          if (!response.success || !response.data?.key) {
            throw new Error(response.message || t('Failed to load API keys'))
          }
          const fullKey = response.data.key.startsWith('sk-')
            ? response.data.key
            : `sk-${response.data.key}`
          resolvedKeysRef.current[tokenId] = fullKey
          return fullKey
        } catch (error) {
          throw new Error(taskErrorMessage(error, t('Request failed')))
        } finally {
          delete pendingKeyRequests.current[tokenId]
        }
      })()

      pendingKeyRequests.current[tokenId] = request
      return request
    },
    [t]
  )

  useEffect(() => {
    storedTasksRef.current = storedTasks
  }, [storedTasks])

  useEffect(() => {
    taskRecordsRef.current = taskRecords
  }, [taskRecords])

  useEffect(() => {
    return () => {
      for (const urls of Object.values(resultPreviewObjectUrlsRef.current)) {
        revokeImageTaskPreviewObjectUrls(urls)
      }
      resultPreviewObjectUrlsRef.current = {}
    }
  }, [])

  const setTask = useCallback(
    (
      task: PublicImageTask,
      tokenId: number,
      metadata: Pick<
        StoredImageTask,
        'prompt' | 'model' | 'size' | 'quality' | 'mode'
      >
    ) => {
      const storedTask: StoredImageTask = {
        taskId: task.task_id,
        tokenId,
        createdAt: task.created_at,
        ...metadata,
      }
      updateStoredTasks((previous) => {
        const next = [
          storedTask,
          ...previous.filter((item) => item.taskId !== task.task_id),
        ].slice(0, 100)
        saveStoredImageTasks(next)
        return next
      })
      updateTaskRecords((previous) => ({
        ...previous,
        [task.task_id]: {
          taskId: task.task_id,
          tokenId,
          createdAt: task.created_at,
          ...metadata,
          task,
          resultUrls: previous[task.task_id]?.resultUrls ?? [],
          resultLoading: false,
          resultError: undefined,
        },
      }))
    },
    [updateStoredTasks, updateTaskRecords]
  )

  const setTaskResultPreviewObjectUrls = useCallback(
    (taskId: string, urls: string[]) => {
      revokeImageTaskPreviewObjectUrls(
        resultPreviewObjectUrlsRef.current[taskId]
      )
      const objectUrls = urls.filter(isImageTaskPreviewObjectUrl)
      if (objectUrls.length === 0) {
        delete resultPreviewObjectUrlsRef.current[taskId]
        return
      }
      resultPreviewObjectUrlsRef.current[taskId] = objectUrls
    },
    []
  )

  const refreshTasks = useCallback(
    async (silent = false) => {
      if (refreshingRef.current || storedTasksRef.current.length === 0) return
      refreshingRef.current = true
      setRefreshing(true)
      try {
        const grouped = new Map<number, string[]>()
        for (const item of storedTasksRef.current) {
          const ids = grouped.get(item.tokenId) ?? []
          ids.push(item.taskId)
          grouped.set(item.tokenId, ids)
        }

        for (const [tokenId, taskIds] of grouped) {
          try {
            const apiKey = await resolveKey(tokenId)
            if (!apiKey) continue
            const response = await listImageTasks(apiKey, taskIds)
            const notFoundIDs = new Set(response.not_found_ids ?? [])
            if (notFoundIDs.size > 0) {
              updateStoredTasks((previous) => {
                const next = previous.filter(
                  (item) => !notFoundIDs.has(item.taskId)
                )
                saveStoredImageTasks(next)
                return next
              })
              updateTaskRecords((previous) => {
                const next = { ...previous }
                for (const taskId of notFoundIDs) {
                  revokeImageTaskPreviewObjectUrls(
                    resultPreviewObjectUrlsRef.current[taskId]
                  )
                  delete resultPreviewObjectUrlsRef.current[taskId]
                  delete next[taskId]
                }
                return next
              })
            }
            updateTaskRecords((previous) => {
              const next = { ...previous }
              const storedByTaskId = new Map(
                storedTasksRef.current.map((item) => [item.taskId, item])
              )
              for (const task of response.data ?? []) {
                const previousRecord = next[task.task_id]
                const storedTask = storedByTaskId.get(task.task_id)
                const resultExpired =
                  task.status === 'completed' && !task.result_available
                const shouldClearResultPreview =
                  resultExpired ||
                  task.status !== 'completed' ||
                  (previousRecord?.resultUrls.length ?? 0) === 0
                if (resultExpired) {
                  revokeImageTaskPreviewObjectUrls(
                    resultPreviewObjectUrlsRef.current[task.task_id]
                  )
                  delete resultPreviewObjectUrlsRef.current[task.task_id]
                }
                const stored = previousRecord ?? {
                  taskId: task.task_id,
                  tokenId,
                  createdAt: task.created_at,
                  prompt: storedTask?.prompt,
                  model: storedTask?.model,
                  size: storedTask?.size,
                  quality: storedTask?.quality,
                  mode: storedTask?.mode,
                  task: null,
                  resultUrls: [],
                  resultLoading: false,
                  resultError: undefined,
                }
                next[task.task_id] = { ...stored, task }
                next[task.task_id].resultUrls = shouldClearResultPreview
                  ? []
                  : stored.resultUrls
                next[task.task_id].resultLoading = resultExpired
                  ? false
                  : stored.resultLoading
                next[task.task_id].resultError = resultExpired
                  ? t('Image generation result is no longer available')
                  : task.result_available
                    ? undefined
                    : stored.resultError
              }
              return next
            })
          } catch (error) {
            if (!silent) {
              toast.error(taskErrorMessage(error, t('Request failed')))
            }
          }
        }
      } finally {
        refreshingRef.current = false
        setRefreshing(false)
      }
    },
    [resolveKey, t, updateStoredTasks, updateTaskRecords]
  )

  useEffect(() => {
    void refreshTasks(true)
    const interval = window.setInterval(() => {
      void refreshTasks(true)
    }, 3000)
    return () => window.clearInterval(interval)
  }, [refreshTasks])

  const finishAutoResultLoad = useCallback(
    (taskId: string, success: boolean) => {
      autoResultInFlightRef.current = Math.max(
        0,
        autoResultInFlightRef.current - 1
      )

      const existingTimer = autoResultRetryTimersRef.current[taskId]
      if (existingTimer) {
        window.clearTimeout(existingTimer)
        delete autoResultRetryTimersRef.current[taskId]
      }

      if (success) {
        autoResultTaskIdsRef.current.delete(taskId)
        delete autoResultRetryAttemptsRef.current[taskId]
        setAutoResultRetryTick((value) => value + 1)
        return
      }

      const attempt = (autoResultRetryAttemptsRef.current[taskId] ?? 0) + 1
      autoResultRetryAttemptsRef.current[taskId] = attempt
      const delay = Math.min(
        AUTO_RESULT_RETRY_MAX_MS,
        AUTO_RESULT_RETRY_BASE_MS * 2 ** Math.min(attempt - 1, 4)
      )
      autoResultRetryTimersRef.current[taskId] = window.setTimeout(() => {
        autoResultTaskIdsRef.current.delete(taskId)
        delete autoResultRetryTimersRef.current[taskId]
        setAutoResultRetryTick((value) => value + 1)
      }, delay)
      setAutoResultRetryTick((value) => value + 1)
    },
    []
  )

  useEffect(
    () => () => {
      for (const timer of Object.values(autoResultRetryTimersRef.current)) {
        if (timer) {
          window.clearTimeout(timer)
        }
      }
      autoResultRetryTimersRef.current = {}
    },
    []
  )

  const taskList = useMemo(
    () =>
      storedTasks
        .map((item) => ({
          ...item,
          task: taskRecords[item.taskId]?.task ?? null,
          resultUrls: taskRecords[item.taskId]?.resultUrls ?? [],
          resultLoading: taskRecords[item.taskId]?.resultLoading ?? false,
          resultError: taskRecords[item.taskId]?.resultError,
        }))
        .sort((left, right) => right.createdAt - left.createdAt),
    [storedTasks, taskRecords]
  )

  const handleResult = useCallback(
    async (record: TaskRecord, notifyError = true): Promise<boolean> => {
      if (
        !record.task?.result_available ||
        record.task.status !== 'completed'
      ) {
        return true
      }
      updateTaskRecords((previous) => ({
        ...previous,
        [record.taskId]: {
          ...previous[record.taskId],
          resultLoading: true,
          resultError: undefined,
        },
      }))
      let loadedPreviewObjectUrls: string[] = []
      try {
        const apiKey = await resolveKey(record.tokenId)
        if (!apiKey) throw new Error(t('API Key is required'))
        const result = await getImageTaskResult(apiKey, record.taskId)
        const resultUrls = await resultToUrls(result, async (imageIndex) => {
          const download = await downloadImageTaskResult(
            apiKey,
            record.taskId,
            imageIndex
          )
          const objectUrl = URL.createObjectURL(download.blob)
          loadedPreviewObjectUrls.push(objectUrl)
          return objectUrl
        })
        const latestRecord = taskRecordsRef.current[record.taskId]
        const latestTask = latestRecord?.task
        if (
          latestTask?.status !== 'completed' ||
          !latestTask?.result_available
        ) {
          revokeImageTaskPreviewObjectUrls(loadedPreviewObjectUrls)
          loadedPreviewObjectUrls = []
          return true
        }
        if (!resultUrls) {
          revokeImageTaskPreviewObjectUrls(loadedPreviewObjectUrls)
          loadedPreviewObjectUrls = []
          throw new ImageTaskPermanentResultError(t('Image result is empty'))
        }
        setTaskResultPreviewObjectUrls(record.taskId, resultUrls)
        loadedPreviewObjectUrls = []
        updateTaskRecords((previous) => ({
          ...previous,
          [record.taskId]: {
            ...previous[record.taskId],
            resultUrls,
            resultLoading: false,
            resultError: undefined,
          },
        }))
        autoResultErrorNotifiedRef.current.delete(record.taskId)
        return true
      } catch (error) {
        revokeImageTaskPreviewObjectUrls(loadedPreviewObjectUrls)
        loadedPreviewObjectUrls = []
        const latestRecord = taskRecordsRef.current[record.taskId]
        const latestTask = latestRecord?.task
        if (
          latestTask?.status !== 'completed' ||
          !latestTask?.result_available
        ) {
          return true
        }
        const retryable = isRetryableImageTaskResultError(error)
        const resultError = retryable
          ? undefined
          : imageTaskResultErrorMessage(error, t)
        updateTaskRecords((previous) => ({
          ...previous,
          [record.taskId]: {
            ...previous[record.taskId],
            resultLoading: false,
            resultError,
          },
        }))
        if (notifyError && retryable) {
          toast.error(taskErrorMessage(error, t('Request failed')))
        }
        return !retryable
      }
    },
    [resolveKey, setTaskResultPreviewObjectUrls, t, updateTaskRecords]
  )

  useEffect(() => {
    let availableSlots = AUTO_RESULT_CONCURRENCY - autoResultInFlightRef.current
    if (availableSlots <= 0) return

    for (const record of taskList) {
      if (availableSlots <= 0) break
      const shouldLoadResult =
        record.task?.status === 'completed' &&
        record.task.result_available &&
        record.resultUrls.length === 0 &&
        !record.resultLoading &&
        !record.resultError
      if (
        !shouldLoadResult ||
        autoResultTaskIdsRef.current.has(record.taskId)
      ) {
        continue
      }
      autoResultTaskIdsRef.current.add(record.taskId)
      autoResultInFlightRef.current += 1
      availableSlots -= 1
      const notifyError = !autoResultErrorNotifiedRef.current.has(record.taskId)
      if (notifyError) {
        autoResultErrorNotifiedRef.current.add(record.taskId)
      }
      void handleResult(record, notifyError)
        .then((success) => {
          if (success) {
            autoResultErrorNotifiedRef.current.delete(record.taskId)
          }
          return success
        })
        .then((success) => finishAutoResultLoad(record.taskId, success))
    }
  }, [autoResultRetryTick, finishAutoResultLoad, handleResult, taskList])

  const handleCancel = useCallback(
    async (record: TaskRecord) => {
      if (record.task?.status !== 'queued') return
      try {
        const apiKey = await resolveKey(record.tokenId)
        if (!apiKey) return
        const task = await cancelImageTask(apiKey, record.taskId)
        updateTaskRecords((previous) => ({
          ...previous,
          [record.taskId]: { ...previous[record.taskId], task },
        }))
      } catch (error) {
        toast.error(taskErrorMessage(error, t('Request failed')))
      }
    },
    [resolveKey, t, updateTaskRecords]
  )

  const handleDownload = useCallback(
    async (record: TaskRecord, imageIndex: number) => {
      try {
        const apiKey = await resolveKey(record.tokenId)
        if (!apiKey) throw new Error(t('API Key is required'))
        const download = await downloadImageTaskResult(
          apiKey,
          record.taskId,
          imageIndex
        )
        triggerImageTaskDownload(download.blob, download.filename)
      } catch (error) {
        toast.error(taskErrorMessage(error, t('Request failed')))
      }
    },
    [resolveKey, t]
  )

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Image Workbench')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          size='sm'
          onClick={() => void refreshTasks()}
          disabled={refreshing}
        >
          {refreshing ? <Spinner /> : <RefreshCw />}
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='grid min-h-0 gap-4 lg:grid-cols-[minmax(280px,360px)_minmax(0,1fr)]'>
          <ImageTaskForm
            enabledKeys={enabledKeys}
            isLoadingKeys={keyQuery.isLoading}
            resolveKey={resolveKey}
            onTaskCreated={setTask}
          />
          <TaskList
            records={taskList}
            isLoading={
              refreshing &&
              storedTasks.length > 0 &&
              Object.keys(taskRecords).length === 0
            }
            onCancel={handleCancel}
            onDownload={handleDownload}
          />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function ImageTaskForm({
  enabledKeys,
  isLoadingKeys,
  resolveKey,
  onTaskCreated,
}: {
  enabledKeys: Array<{ id: number; name: string; key: string }>
  isLoadingKeys: boolean
  resolveKey: (tokenId: number) => Promise<string | null>
  onTaskCreated: (
    task: PublicImageTask,
    tokenId: number,
    metadata: Pick<
      StoredImageTask,
      'prompt' | 'model' | 'size' | 'quality' | 'mode'
    >
  ) => void
}) {
  const { t } = useTranslation()
  const [submitting, setSubmitting] = useState(false)
  const imageInputRef = useRef<HTMLInputElement | null>(null)
  const maskInputRef = useRef<HTMLInputElement | null>(null)
  const form = useForm<ImageTaskFormValues>({
    resolver: zodResolver(imageTaskFormSchema),
    defaultValues: DEFAULT_FORM_VALUES,
  })
  const mode = form.watch('mode')
  const selectedSize = form.watch('size')
  const customWidth = form.watch('customWidth')
  const customHeight = form.watch('customHeight')
  const requestSizePreview = getImageTaskRequestSize({
    size: selectedSize,
    customWidth,
    customHeight,
  })
  const showLargeSizeHint = isLargeImageTaskSize(requestSizePreview)

  useEffect(() => {
    if (mode === 'edit') {
      return
    }
    form.setValue('images', [], {
      shouldDirty: false,
      shouldTouch: false,
      shouldValidate: true,
    })
    form.setValue('mask', null, {
      shouldDirty: false,
      shouldTouch: false,
      shouldValidate: true,
    })
    form.clearErrors('images')
    form.clearErrors('mask')
    if (imageInputRef.current) {
      imageInputRef.current.value = ''
    }
    if (maskInputRef.current) {
      maskInputRef.current.value = ''
    }
  }, [form, mode])

  const submit = async (values: ImageTaskFormValues) => {
    setSubmitting(true)
    try {
      const tokenId = values.tokenId
      const apiKey = await resolveKey(tokenId)
      if (!apiKey) return
      const requestSize = getImageTaskRequestSize(values)
      const modelName = values.model.trim()
      const qualityValue = values.quality.trim()
      const submitQuality = qualityValue || undefined
      const metadata: Pick<
        StoredImageTask,
        'prompt' | 'model' | 'size' | 'quality' | 'mode'
      > = {
        prompt: values.prompt.trim(),
        model: modelName,
        size: requestSize,
        mode: values.mode,
        ...(submitQuality ? { quality: submitQuality } : {}),
      }
      let createdCount = 0
      let failedCount = 0
      let lastError: unknown = null

      for (let index = 0; index < values.n; index += 1) {
        try {
          const clientTaskId = createClientTaskId()
          const baseInput = {
            model: modelName,
            prompt: values.prompt,
            n: 1,
            size: requestSize,
            client_task_id: clientTaskId,
            ...(submitQuality ? { quality: submitQuality } : {}),
          }
          let task: PublicImageTask
          if (values.mode === 'edit') {
            task = await createImageEditTask(apiKey, {
              ...baseInput,
              images: values.images,
              mask: values.mask ?? undefined,
            })
          } else {
            task = await createImageGenerationTask(apiKey, {
              ...baseInput,
            })
          }
          onTaskCreated(task, tokenId, metadata)
          createdCount += 1
        } catch (error) {
          failedCount += 1
          lastError = error
        }
      }

      if (createdCount > 0) {
        form.reset({
          ...DEFAULT_FORM_VALUES,
          tokenId,
          mode: values.mode,
        })
        if (imageInputRef.current) {
          imageInputRef.current.value = ''
        }
        if (maskInputRef.current) {
          maskInputRef.current.value = ''
        }
        if (failedCount > 0) {
          toast.error(
            t(
              'Created {{created}} / {{total}} image generation tasks, {{failed}} failed',
              {
                created: createdCount,
                total: values.n,
                failed: failedCount,
              }
            )
          )
        } else if (createdCount > 1) {
          toast.success(
            t('Created {{count}} image generation tasks', { count: createdCount })
          )
        } else {
          toast.success(t('Image generation task created'))
        }
      } else {
        toast.error(taskErrorMessage(lastError, t('Request failed')))
      }
    } catch (error) {
      toast.error(taskErrorMessage(error, t('Request failed')))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Card className='h-fit'>
      <CardHeader>
        <CardTitle>{t('Create Image Generation Task')}</CardTitle>
      </CardHeader>
      <CardContent>
        <form
          className='grid gap-4'
          noValidate
          onSubmit={form.handleSubmit(submit)}
        >
          <div className='grid gap-2'>
            <Label htmlFor='image-task-key'>{t('API Keys')}</Label>
            <NativeSelect
              id='image-task-key'
              disabled={isLoadingKeys || enabledKeys.length === 0}
              {...form.register('tokenId', {
                setValueAs: (value) => Number.parseInt(String(value || 0), 10),
              })}
            >
              <NativeSelectOption value=''>
                {isLoadingKeys
                  ? t('Loading...')
                  : t('Select an enabled API key')}
              </NativeSelectOption>
              {enabledKeys.map((apiKey) => (
                <NativeSelectOption key={apiKey.id} value={String(apiKey.id)}>
                  {apiKey.name || `#${apiKey.id}`}
                </NativeSelectOption>
              ))}
            </NativeSelect>
            {enabledKeys.length === 0 && !isLoadingKeys && (
              <p className='text-destructive text-xs'>
                {t('No enabled API keys found. Create or enable one first.')}
              </p>
            )}
          </div>

          <div className='grid gap-2'>
            <Label htmlFor='image-task-mode'>{t('Mode')}</Label>
            <NativeSelect id='image-task-mode' {...form.register('mode')}>
              <NativeSelectOption value='generation'>
                {t('Text-to-image')}
              </NativeSelectOption>
              <NativeSelectOption value='edit'>
                {t('Image-to-image')}
              </NativeSelectOption>
            </NativeSelect>
          </div>

          <div className='grid gap-2'>
            <Label htmlFor='image-task-model'>{t('Model')}</Label>
            <Input id='image-task-model' {...form.register('model')} />
            {form.formState.errors.model && (
              <p className='text-destructive text-xs'>
                {t(form.formState.errors.model.message || '')}
              </p>
            )}
          </div>

          <div className='grid gap-2'>
            <Label htmlFor='image-task-prompt'>{t('Prompt')}</Label>
            <Textarea
              id='image-task-prompt'
              rows={4}
              {...form.register('prompt')}
            />
            {form.formState.errors.prompt && (
              <p className='text-destructive text-xs'>
                {t(form.formState.errors.prompt.message || '')}
              </p>
            )}
          </div>

          {mode === 'edit' && (
            <>
              <div className='grid gap-2'>
                <Label htmlFor='image-task-images'>
                  {t('Reference images')}
                </Label>
                <Input
                  id='image-task-images'
                  ref={imageInputRef}
                  type='file'
                  accept='image/*'
                  multiple
                  onChange={(event) => {
                    const files = [...(event.target.files ?? [])]
                    if (files.length > MAX_IMAGE_TASK_REFERENCE_IMAGES) {
                      form.setValue('images', [], {
                        shouldValidate: true,
                      })
                      form.setError('images', {
                        type: 'manual',
                        message: t('You can upload up to 6 images'),
                      })
                      event.target.value = ''
                      return
                    }
                    form.clearErrors('images')
                    form.setValue('images', files, {
                      shouldValidate: true,
                    })
                  }}
                />
                <p className='text-muted-foreground text-xs'>
                  {t('Upload 1 to 6 reference images.')}
                </p>
                {form.formState.errors.images && (
                  <p className='text-destructive text-xs'>
                    {t(form.formState.errors.images.message || '')}
                  </p>
                )}
              </div>
              <div className='grid gap-2'>
                <Label htmlFor='image-task-mask'>
                  {t('Mask')} ({t('Optional')})
                </Label>
                <Input
                  id='image-task-mask'
                  ref={maskInputRef}
                  type='file'
                  accept='image/*'
                  onChange={(event) =>
                    form.setValue('mask', event.target.files?.[0] ?? null, {
                      shouldValidate: true,
                    })
                  }
                />
              </div>
            </>
          )}

          <div className='grid grid-cols-2 gap-3'>
            <div className='grid gap-2'>
              <Label htmlFor='image-task-n'>{t('Count')}</Label>
              <Input
                id='image-task-n'
                type='number'
                min={1}
                max={MAX_IMAGE_TASK_COUNT}
                {...form.register('n', { valueAsNumber: true })}
              />
              {form.formState.errors.n && (
                <p className='text-destructive text-xs'>
                  {t(form.formState.errors.n.message || '')}
                </p>
              )}
            </div>
          </div>

          <div className='grid grid-cols-2 gap-3'>
            <div className='grid gap-2'>
              <Label htmlFor='image-task-size'>{t('Size')}</Label>
              <NativeSelect id='image-task-size' {...form.register('size')}>
                {IMAGE_TASK_SIZE_OPTIONS.map((option) => (
                  <NativeSelectOption key={option.value} value={option.value}>
                    {option.value} ({t(option.labelKey)})
                  </NativeSelectOption>
                ))}
                <NativeSelectOption value={CUSTOM_IMAGE_TASK_SIZE_VALUE}>
                  {t('Custom size')}
                </NativeSelectOption>
              </NativeSelect>
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='image-task-quality'>{t('Quality')}</Label>
              <NativeSelect
                id='image-task-quality'
                {...form.register('quality')}
              >
                <NativeSelectOption value='auto'>
                  {t('Auto')}
                </NativeSelectOption>
                <NativeSelectOption value='low'>{t('Low')}</NativeSelectOption>
                <NativeSelectOption value='medium'>
                  {t('Medium')}
                </NativeSelectOption>
                <NativeSelectOption value='high'>
                  {t('High')}
                </NativeSelectOption>
              </NativeSelect>
            </div>
          </div>

          {selectedSize === CUSTOM_IMAGE_TASK_SIZE_VALUE && (
            <div className='grid grid-cols-2 gap-3'>
              <div className='grid gap-2'>
                <Label htmlFor='image-task-custom-width'>{t('Width')}</Label>
                <Input
                  id='image-task-custom-width'
                  type='number'
                  min={16}
                  step={16}
                  inputMode='numeric'
                  {...form.register('customWidth')}
                />
                {form.formState.errors.customWidth && (
                  <p className='text-destructive text-xs'>
                    {t(form.formState.errors.customWidth.message || '')}
                  </p>
                )}
              </div>
              <div className='grid gap-2'>
                <Label htmlFor='image-task-custom-height'>{t('Height')}</Label>
                <Input
                  id='image-task-custom-height'
                  type='number'
                  min={16}
                  step={16}
                  inputMode='numeric'
                  {...form.register('customHeight')}
                />
                {form.formState.errors.customHeight && (
                  <p className='text-destructive text-xs'>
                    {t(form.formState.errors.customHeight.message || '')}
                  </p>
                )}
              </div>
              {form.formState.errors.size && (
                <p className='text-destructive col-span-2 text-xs'>
                  {t(form.formState.errors.size.message || '')}
                </p>
              )}
            </div>
          )}

          {showLargeSizeHint && (
            <p className='text-muted-foreground text-xs'>
              {t('Large sizes may generate slower or be less stable.')}
            </p>
          )}

          <p className='text-muted-foreground text-xs'>
            {t(
              'Generated images are kept for up to three days. Please download them in time.'
            )}
          </p>

          <Button
            type='submit'
            disabled={submitting || enabledKeys.length === 0}
          >
            {submitting ? <Spinner /> : <ImageIcon />}
            {t('Create')}
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}

function TaskList({
  records,
  isLoading,
  onCancel,
  onDownload,
}: {
  records: TaskRecord[]
  isLoading: boolean
  onCancel: (record: TaskRecord) => void
  onDownload: (record: TaskRecord, imageIndex: number) => Promise<void>
}) {
  const { t } = useTranslation()
  let content: ReactNode = records.map((record) => (
    <TaskCard
      key={record.taskId}
      record={record}
      onCancel={onCancel}
      onDownload={onDownload}
    />
  ))

  if (isLoading) {
    content = (
      <div className='flex items-center justify-center py-12'>
        <Spinner className='size-6' />
      </div>
    )
  } else if (records.length === 0) {
    content = (
      <Empty className='min-h-64'>
        <EmptyHeader>
          <EmptyTitle>{t('No image generation history')}</EmptyTitle>
          <EmptyDescription>
            {t('Created tasks will appear here with live status updates.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className='min-h-0 space-y-3 overflow-auto'>
      <div className='flex items-center justify-between gap-3'>
        <h3 className='text-base font-semibold'>
          {t('Image Generation History')}
        </h3>
        <span className='text-muted-foreground text-sm'>{records.length}</span>
      </div>
      {content}
    </div>
  )
}

function TaskCard({
  record,
  onCancel,
  onDownload,
}: {
  record: TaskRecord
  onCancel: (record: TaskRecord) => void
  onDownload: (record: TaskRecord, imageIndex: number) => Promise<void>
}) {
  const { t } = useTranslation()
  const [imageDimensions, setImageDimensions] = useState<
    Record<string, string>
  >({})
  const [downloadingIndexes, setDownloadingIndexes] = useState<
    Record<number, boolean>
  >({})
  const task = record.task
  const status = task?.status ?? 'queued'
  const canCancel = status === 'queued'
  const title = summarizePrompt(record.prompt) || t('Untitled image generation')

  const handleDownloadClick = async (imageIndex: number) => {
    if (downloadingIndexes[imageIndex]) return
    setDownloadingIndexes((previous) => ({ ...previous, [imageIndex]: true }))
    try {
      await onDownload(record, imageIndex)
    } finally {
      setDownloadingIndexes((previous) => {
        const next = { ...previous }
        delete next[imageIndex]
        return next
      })
    }
  }

  return (
    <Card size='sm'>
      <CardHeader className='gap-2'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <CardTitle className='text-sm'>{title}</CardTitle>
          <StatusBadge
            label={getStatusLabel(status, t)}
            variant={getStatusVariant(status)}
            copyable={false}
          />
        </div>
        <div className='text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-xs'>
          <span
            className='font-mono opacity-60'
            data-testid='image-task-id'
            title={record.taskId}
          >
            {t('Task ID')}: {formatWeakTaskId(record.taskId)}
          </span>
          <span>
            {t('Created')}:{' '}
            {formatTaskTime(task?.created_at ?? record.createdAt)}
          </span>
          <span>
            {t('Mode')}: {formatMode(record.mode, t)}
          </span>
          <span>
            {t('Model')}: {record.model || '-'}
          </span>
          <span>
            {t('Request size')}: {formatRequestSize(record.size, t)}
          </span>
          <span>
            {t('Quality')}: {formatQuality(record.quality, t)}
          </span>
          {task?.result_expires_at && (
            <span>
              {t('Expires at')}: {formatTaskTime(task.result_expires_at)}
            </span>
          )}
          {task?.progress && <span>{task.progress}</span>}
        </div>
      </CardHeader>
      <Separator />
      <CardContent className='grid gap-3'>
        {task?.error && (
          <div className='text-destructive border-destructive/30 bg-destructive/5 rounded-md border px-3 py-2 text-sm'>
            {task.error.code}: {task.error.message}
          </div>
        )}
        {record.resultError && (
          <div className='text-muted-foreground rounded-md border px-3 py-2 text-sm'>
            {record.resultError}
          </div>
        )}
        {record.resultUrls.length > 0 && (
          <div className='grid gap-2'>
            {record.resultUrls.map((resultUrl, imageIndex) => {
              const dimensionKey = imageTaskResultRenderKey(
                record.taskId,
                resultUrl,
                imageIndex,
                0
              )
              return (
                <div key={dimensionKey} className='grid gap-2'>
                  <img
                    src={resultUrl}
                    alt={t('Image generation result')}
                    className='max-h-96 w-full rounded-lg border object-contain'
                    onLoad={(event) => {
                      const image = event.currentTarget
                      const dimensions = `${image.naturalWidth} x ${image.naturalHeight}`
                      setImageDimensions((previous) => {
                        if (previous[dimensionKey] === dimensions) {
                          return previous
                        }
                        return { ...previous, [dimensionKey]: dimensions }
                      })
                    }}
                  />
                  <div className='text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-xs'>
                    <span>
                      {t('Actual size')}: {imageDimensions[dimensionKey] ?? '-'}
                    </span>
                    <span>
                      {t('Request size')}: {formatRequestSize(record.size, t)}
                    </span>
                    <span>
                      {t('Model')}: {record.model || '-'}
                    </span>
                    <span>
                      {t('Quality')}: {formatQuality(record.quality, t)}
                    </span>
                    {task?.result_expires_at && (
                      <span>
                        {t('Expires at')}:{' '}
                        {formatTaskTime(task.result_expires_at)}
                      </span>
                    )}
                  </div>
                  <Button
                    type='button'
                    variant='link'
                    size='sm'
                    className='h-auto w-fit px-0'
                    disabled={Boolean(downloadingIndexes[imageIndex])}
                    onClick={() => void handleDownloadClick(imageIndex)}
                  >
                    {downloadingIndexes[imageIndex] ? (
                      <Spinner className='size-4' />
                    ) : (
                      <Download className='size-4' />
                    )}
                    {t('Download')}
                  </Button>
                </div>
              )
            })}
          </div>
        )}
        <div className='flex flex-wrap justify-end gap-2'>
          {record.resultLoading && (
            <span className='text-muted-foreground inline-flex items-center gap-1 text-xs'>
              <Spinner className='size-3.5' />
              {t('Loading...')}
            </span>
          )}
          {canCancel && (
            <Button
              variant='destructive'
              size='sm'
              onClick={() => onCancel(record)}
            >
              <Square />
              {t('Cancel')}
            </Button>
          )}
          {task?.result_acknowledged_at ? (
            <span className='text-muted-foreground inline-flex items-center gap-1 text-xs'>
              <Check className='size-3.5' />
              {t('Acknowledged')}
            </span>
          ) : null}
        </div>
      </CardContent>
    </Card>
  )
}
