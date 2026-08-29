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
import { useRef, useState, useCallback, useMemo, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import type {
  SystemTask,
  SystemTaskResponse,
  SystemTaskStatus,
} from '@/features/system-settings/types'
import { api, type ApiRequestConfig } from '@/lib/api'

import { normalizeModelList } from '../lib/upstream-update-utils'

const upstreamUpdateRequestConfig = {
  skipBusinessError: true,
  skipErrorHandler: true,
} satisfies ApiRequestConfig

const modelUpdateTaskPollIntervalMs = 2000
const modelUpdateTaskSlowPollAfterPolls = 900
const modelUpdateTaskSlowPollIntervalMs = 10000
const modelUpdateTaskHiddenPollIntervalMs = 30000
const modelUpdateTaskDiscoveryIntervalMs = 5000
const modelUpdateTaskStorageKey = 'newapi.channel.upstream_update.task_id'
const unknownModelUpdateTaskStatusMessage =
  'Unknown upstream model update task status'
const modelUpdateManualTaskType = 'model_update_manual'
const modelUpdateApplyAllTaskType = 'model_update_apply_all'

type ModelUpdateTaskStorage = Pick<
  Storage,
  'getItem' | 'setItem' | 'removeItem'
>

function getModelUpdateTaskStorage(): ModelUpdateTaskStorage | null {
  try {
    if (typeof window === 'undefined') return null
    return window.sessionStorage
  } catch {
    return null
  }
}

export function getPersistedModelUpdateTaskId(
  storage: ModelUpdateTaskStorage | null = getModelUpdateTaskStorage()
): string {
  try {
    return storage?.getItem(modelUpdateTaskStorageKey)?.trim() || ''
  } catch {
    return ''
  }
}

export function setPersistedModelUpdateTaskId(
  taskId: string,
  storage: ModelUpdateTaskStorage | null = getModelUpdateTaskStorage()
) {
  const normalizedTaskId = taskId.trim()
  if (!normalizedTaskId) {
    clearPersistedModelUpdateTaskId(storage)
    return
  }
  try {
    storage?.setItem(modelUpdateTaskStorageKey, normalizedTaskId)
  } catch {
    // Ignore storage failures; polling can still continue in-memory.
  }
}

export function clearPersistedModelUpdateTaskId(
  storage: ModelUpdateTaskStorage | null = getModelUpdateTaskStorage()
) {
  try {
    storage?.removeItem(modelUpdateTaskStorageKey)
  } catch {
    // Ignore storage failures; task terminal state is already known.
  }
}

type ModelUpdateTaskPayload = {
  manual?: boolean
}

type ModelUpdateTaskState = {
  progress?: number
}

type ModelUpdateTaskResult = {
  processed_channels?: number
  added_models?: number
  removed_models?: number
  remaining_remove_models_count?: number
  failed_channel_ids?: number[]
  results?: Array<Record<string, unknown>>
  runtime_cache_refresh_error?: string
  checked_channels?: number
  changed_channels?: number
  detected_add_models?: number
  detected_remove_models?: number
  failed_channels?: number
  auto_added_models?: number
}

type ModelUpdateTask = SystemTask<
  ModelUpdateTaskPayload,
  ModelUpdateTaskState,
  ModelUpdateTaskResult
>

type ModelUpdateTaskStartInfo = {
  task_id: string
  status?: SystemTaskStatus
  type?: string
}

type ResolvedModelUpdateTaskStartInfo = ModelUpdateTaskStartInfo & {
  type: string
}

type CurrentModelUpdateTask = ModelUpdateTask | ModelUpdateTaskStartInfo | null

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object'
}

function asSystemTaskStatus(value: unknown): SystemTaskStatus | undefined {
  // cancelled/canceled are forward-compatible terminal statuses; current Go
  // model-update runners still finish cancellation/lease loss as failed.
  if (
    value === 'pending' ||
    value === 'running' ||
    value === 'succeeded' ||
    value === 'failed' ||
    value === 'cancelled' ||
    value === 'canceled'
  ) {
    return value
  }
  return undefined
}

function getResponseMessage(payload: unknown): string | undefined {
  if (!isRecord(payload) || typeof payload.message !== 'string') return
  return payload.message
}

function getErrorPayload(error: unknown): unknown {
  if (!isRecord(error)) return undefined
  const response = error.response
  if (!isRecord(response)) return undefined
  return response.data
}

function getErrorMessage(error: unknown): string | undefined {
  const payloadMessage = getResponseMessage(getErrorPayload(error))
  if (payloadMessage) return payloadMessage
  if (isRecord(error) && typeof error.message === 'string') {
    return error.message
  }
  return undefined
}

function isRequestCancelled(error: unknown): boolean {
  return (
    isRecord(error) &&
    (error.name === 'CanceledError' || error.code === 'ERR_CANCELED')
  )
}

function getErrorStatus(error: unknown): number | undefined {
  if (!isRecord(error)) return undefined
  const response = error.response
  if (!isRecord(response) || typeof response.status !== 'number') {
    return undefined
  }
  return response.status
}

export function shouldClearPersistedModelUpdateTaskIdAfterPollingError(
  error: unknown
): boolean {
  const status = getErrorStatus(error)
  if (status === 404 || status === 410) return true

  const message =
    getResponseMessage(getErrorPayload(error)) ||
    (isRecord(error) && typeof error.message === 'string' ? error.message : '')
  return /task.*not found|not found|不存在|missing/i.test(message)
}

export function shouldResumePersistedModelUpdateTask(
  canDetectUpstreamUpdates: boolean,
  taskId: string
): boolean {
  return canDetectUpstreamUpdates === true && taskId.trim().length > 0
}

function getModelUpdateTaskStartInfo(
  payload: unknown
): ModelUpdateTaskStartInfo | null {
  if (!isRecord(payload) || !isRecord(payload.data)) return null
  const taskId = payload.data.task_id
  if (typeof taskId !== 'string' || taskId.trim().length === 0) return null
  const status = asSystemTaskStatus(payload.data.status)
  if (!status) return null
  return {
    task_id: taskId.trim(),
    status,
    type: typeof payload.data.type === 'string' ? payload.data.type : undefined,
  }
}

function isSuccessPayload(payload: unknown): boolean {
  return isRecord(payload) && payload.success === true
}

function isTerminalTaskStatus(status: SystemTaskStatus | undefined): boolean {
  return (
    status === 'succeeded' ||
    status === 'failed' ||
    status === 'cancelled' ||
    status === 'canceled'
  )
}

function isCancelledTaskStatus(status: SystemTaskStatus | undefined): boolean {
  return status === 'cancelled' || status === 'canceled'
}

function defaultModelUpdateTaskSleep(ms: number, signal?: AbortSignal) {
  return new Promise<void>((resolve) => {
    if (signal?.aborted) {
      resolve()
      return
    }

    const onAbort = () => {
      clearTimeout(timer)
      resolve()
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort)
      resolve()
    }, ms)
    signal?.addEventListener('abort', onAbort, { once: true })
  })
}

function isModelUpdateTaskDocumentHidden(): boolean {
  try {
    return (
      typeof document !== 'undefined' && document.visibilityState === 'hidden'
    )
  } catch {
    return false
  }
}

export function getModelUpdateTaskPollIntervalMs(
  pollIndex: number,
  basePollIntervalMs: number
): number {
  const normalizedBasePollIntervalMs =
    Number.isFinite(basePollIntervalMs) && basePollIntervalMs > 0
      ? basePollIntervalMs
      : 0
  let intervalMs = normalizedBasePollIntervalMs
  if (pollIndex >= modelUpdateTaskSlowPollAfterPolls) {
    intervalMs = Math.max(intervalMs, modelUpdateTaskSlowPollIntervalMs)
  }
  if (isModelUpdateTaskDocumentHidden()) {
    intervalMs = Math.max(intervalMs, modelUpdateTaskHiddenPollIntervalMs)
  }
  return intervalMs
}

async function getChannelModelUpdateTask(taskId: string, signal?: AbortSignal) {
  const res = await api.get<SystemTaskResponse<ModelUpdateTask>>(
    `/api/channel/upstream_updates/task/${encodeURIComponent(taskId)}`,
    {
      ...upstreamUpdateRequestConfig,
      disableDuplicate: true,
      signal,
    }
  )
  return res.data
}

async function resolveModelUpdateTaskStartInfo(
  taskInfo: ModelUpdateTaskStartInfo,
  signal?: AbortSignal,
  fallbackType?: string
): Promise<ResolvedModelUpdateTaskStartInfo> {
  const currentType = taskInfo.type?.trim()
  if (currentType) return { ...taskInfo, type: currentType }

  const payload = await getChannelModelUpdateTask(taskInfo.task_id, signal)
  if (!payload.success || !payload.data) {
    throw new Error(payload.message || '')
  }
  const status = asSystemTaskStatus(payload.data.status)
  if (!status) {
    throw new Error(unknownModelUpdateTaskStatusMessage)
  }
  const type =
    (typeof payload.data.type === 'string'
      ? payload.data.type.trim()
      : '') || fallbackType?.trim() || ''
  if (!type) {
    throw new Error('Upstream model update task type is missing')
  }
  return {
    task_id: taskInfo.task_id,
    status,
    type,
  }
}

async function getCurrentChannelModelUpdateTask(signal?: AbortSignal) {
  const res = await api.get<SystemTaskResponse<ModelUpdateTask | null>>(
    '/api/channel/upstream_updates/current',
    {
      ...upstreamUpdateRequestConfig,
      disableDuplicate: true,
      signal,
    }
  )
  const payload = res.data
  if (!payload.success) {
    throw new Error(payload.message || '')
  }
  if (!payload.data) return null
  const taskStatus = asSystemTaskStatus(payload.data.status)
  if (!taskStatus) {
    throw new Error(unknownModelUpdateTaskStatusMessage)
  }
  return { ...payload.data, status: taskStatus } as ModelUpdateTask
}

export async function waitForModelUpdateTask(
  taskId: string,
  {
    signal,
    // The server processes enabled channels sequentially. Do not stop the
    // default foreground poll before a large installation can reach a
    // terminal task state; callers can still provide a finite cap when
    // bounded polling is required.
    maxPolls = Number.POSITIVE_INFINITY,
    pollIntervalMs = modelUpdateTaskPollIntervalMs,
    sleep = defaultModelUpdateTaskSleep,
  }: {
    signal?: AbortSignal
    maxPolls?: number
    pollIntervalMs?: number
    sleep?: (ms: number, signal?: AbortSignal) => Promise<void> | void
  } = {}
) {
  for (let i = 0; i < maxPolls; i++) {
    if (signal?.aborted) return null

    let res: SystemTaskResponse<ModelUpdateTask>
    try {
      res = await getChannelModelUpdateTask(taskId, signal)
    } catch (error) {
      if (signal?.aborted || isRequestCancelled(error)) return null
      throw error
    }
    if (signal?.aborted) return null
    if (!res.success || !res.data) {
      throw new Error(res.message || '')
    }
    const taskStatus = asSystemTaskStatus(res.data.status)
    if (!taskStatus) {
      throw new Error(unknownModelUpdateTaskStatusMessage)
    }
    const task = { ...res.data, status: taskStatus } as ModelUpdateTask
    if (isTerminalTaskStatus(task.status)) return task
    if (i + 1 < maxPolls) {
      await sleep(getModelUpdateTaskPollIntervalMs(i, pollIntervalMs), signal)
    }
  }
  return null
}

async function refreshChannelsBestEffort(refresh: () => Promise<void>) {
  try {
    await refresh()
  } catch {
    // Keep the original operation result visible; refresh is best-effort.
  }
}

function modelUpdateTaskResultNumber(
  result: ModelUpdateTaskResult | undefined,
  key: keyof ModelUpdateTaskResult
): number {
  const value = result?.[key]
  return typeof value === 'number' && Number.isFinite(value) && value > 0
    ? value
    : 0
}

function hasModelUpdateTaskPartialResult(task: ModelUpdateTask): boolean {
  const result = task.result
  if (!result) return false
  return (
    modelUpdateTaskResultNumber(result, 'checked_channels') > 0 ||
    modelUpdateTaskResultNumber(result, 'changed_channels') > 0 ||
    modelUpdateTaskResultNumber(result, 'detected_add_models') > 0 ||
    modelUpdateTaskResultNumber(result, 'detected_remove_models') > 0 ||
    modelUpdateTaskResultNumber(result, 'failed_channels') > 0
  )
}

function formatModelUpdateTaskFailureMessage(
  task: ModelUpdateTask,
  fallbackMessage: string,
  t: ReturnType<typeof useTranslation>['t']
): string {
  const errorMessage = typeof task.error === 'string' ? task.error.trim() : ''
  const cacheRefreshError =
    typeof task.result?.runtime_cache_refresh_error === 'string'
      ? task.result.runtime_cache_refresh_error.trim()
      : ''
  if (task.type === modelUpdateApplyAllTaskType && task.result) {
    const result = task.result
    const processed = modelUpdateTaskResultNumber(
      result,
      'processed_channels'
    )
    const added = modelUpdateTaskResultNumber(result, 'added_models')
    const kept = modelUpdateTaskResultNumber(
      result,
      'remaining_remove_models_count'
    )
    const failed = Array.isArray(result.failed_channel_ids)
      ? result.failed_channel_ids.length
      : 0
    if (processed > 0 || added > 0 || kept > 0 || failed > 0) {
      const partialMessage = t(
        'Batch upstream model additions partially completed: {{channels}} channels, {{added}} added, {{kept}} pending removals kept for manual review, {{fails}} failed.',
        { channels: processed, added, kept, fails: failed }
      )
      const suffix = [errorMessage, cacheRefreshError].filter(Boolean).join(' ')
      return suffix ? `${partialMessage} ${suffix}` : partialMessage
    }
  }
  if (!hasModelUpdateTaskPartialResult(task)) {
    const suffix = [errorMessage, cacheRefreshError].filter(Boolean).join(' ')
    return suffix || fallbackMessage
  }
  const result = task.result
  const partialMessage = t(
    'Batch detection partially completed: {{channels}} channels checked, {{changed}} changed, {{add}} to add, {{remove}} to remove, {{fails}} failed.',
    {
      channels: modelUpdateTaskResultNumber(result, 'checked_channels'),
      changed: modelUpdateTaskResultNumber(result, 'changed_channels'),
      add: modelUpdateTaskResultNumber(result, 'detected_add_models'),
      remove: modelUpdateTaskResultNumber(result, 'detected_remove_models'),
      fails: modelUpdateTaskResultNumber(result, 'failed_channels'),
    }
  )
  const suffix = [errorMessage, cacheRefreshError].filter(Boolean).join(' ')
  return suffix ? `${partialMessage} ${suffix}` : partialMessage
}

function formatModelUpdateTaskSuccessMessage(
  task: ModelUpdateTask,
  t: ReturnType<typeof useTranslation>['t']
): string {
  const result = task.result || {}
  if (task.type === modelUpdateApplyAllTaskType) {
    return t(
      'Batch upstream model additions applied: {{channels}} channels, {{added}} added, {{kept}} pending removals kept for manual review, {{fails}} failed',
      {
        channels: modelUpdateTaskResultNumber(result, 'processed_channels'),
        added: modelUpdateTaskResultNumber(result, 'added_models'),
        kept: modelUpdateTaskResultNumber(
          result,
          'remaining_remove_models_count'
        ),
        fails: Array.isArray(result.failed_channel_ids)
          ? result.failed_channel_ids.length
          : 0,
      }
    )
  }
  if (task.type === 'model_update') {
    return t(
      'Scheduled upstream model sync complete: {{channels}} channels checked, {{add}} to add, {{remove}} to remove, {{autoAdded}} auto added, {{fails}} failed',
      {
        channels: modelUpdateTaskResultNumber(result, 'checked_channels'),
        add: modelUpdateTaskResultNumber(result, 'detected_add_models'),
        remove: modelUpdateTaskResultNumber(result, 'detected_remove_models'),
        autoAdded: modelUpdateTaskResultNumber(result, 'auto_added_models'),
        fails: modelUpdateTaskResultNumber(result, 'failed_channels'),
      }
    )
  }
  return t(
    'Batch detection complete: {{channels}} channels, {{add}} to add, {{remove}} to remove, {{fails}} failed',
    {
      channels: modelUpdateTaskResultNumber(result, 'checked_channels'),
      add: modelUpdateTaskResultNumber(result, 'detected_add_models'),
      remove: modelUpdateTaskResultNumber(result, 'detected_remove_models'),
      fails: modelUpdateTaskResultNumber(result, 'failed_channels'),
    }
  )
}

function getModelUpdateTaskPollErrorMessage(
  error: unknown,
  fallbackMessage: string,
  t: ReturnType<typeof useTranslation>['t']
): string {
  const message = getErrorMessage(error)
  if (message === unknownModelUpdateTaskStatusMessage) {
    return t(unknownModelUpdateTaskStatusMessage)
  }
  return message || fallbackMessage
}

function getManualIgnoredModelCount(settings: unknown): number {
  let parsed: Record<string, unknown> | null = null
  if (settings && typeof settings === 'object') {
    parsed = settings as Record<string, unknown>
  } else if (typeof settings === 'string') {
    try {
      parsed = JSON.parse(settings)
    } catch {
      parsed = null
    }
  }
  if (!parsed) return 0
  return normalizeModelList(
    (parsed.upstream_model_update_ignored_models as unknown[]) || []
  ).length
}

export function useChannelUpstreamUpdates(
  refresh: () => Promise<void>,
  {
    canDetectUpstreamUpdates = false,
    canApplyUpstreamUpdates = false,
  }: {
    canDetectUpstreamUpdates?: boolean
    canApplyUpstreamUpdates?: boolean
  } = {}
) {
  const { t } = useTranslation()
  const canAccessModelUpdateTasks =
    canDetectUpstreamUpdates || canApplyUpstreamUpdates

  const [showModal, setShowModal] = useState(false)
  const [channel, setChannel] = useState<{
    id: number
    [key: string]: unknown
  } | null>(null)
  const [addModels, setAddModels] = useState<string[]>([])
  const [removeModels, setRemoveModels] = useState<string[]>([])
  const [preferredTab, setPreferredTab] = useState<'add' | 'remove'>('add')
  const [applyLoading, setApplyLoading] = useState(false)
  const [detectChannelLoadingId, setDetectChannelLoadingId] = useState<
    number | null
  >(null)
  const [detectAllLoading, setDetectAllLoading] = useState(false)
  const [applyAllLoading, setApplyAllLoading] = useState(false)
  const [cancelTaskLoading, setCancelTaskLoading] = useState(false)
  const [currentModelUpdateTask, setCurrentModelUpdateTask] =
    useState<CurrentModelUpdateTask>(null)
  const [
    currentModelUpdateTaskLookupComplete,
    setCurrentModelUpdateTaskLookupComplete,
  ] = useState(false)

  const applyRef = useRef(false)
  const detectRef = useRef(false)
  const detectAllRef = useRef(false)
  const applyAllRef = useRef(false)
  const cancelTaskRef = useRef(false)
  const mountedRef = useRef(true)
  const modelUpdateTaskGenerationRef = useRef(0)
  const modelUpdateTaskAbortControllerRef = useRef<AbortController | null>(null)

  const isCurrentModelUpdateTaskRun = useCallback((generation: number) => {
    return (
      mountedRef.current && modelUpdateTaskGenerationRef.current === generation
    )
  }, [])

  const beginModelUpdateTaskRun = useCallback(
    (
      taskInfo: ModelUpdateTaskStartInfo,
      existingController?: AbortController
    ) => {
      if (
        existingController &&
        modelUpdateTaskAbortControllerRef.current !== existingController
      ) {
        modelUpdateTaskAbortControllerRef.current?.abort()
      } else if (!existingController) {
        modelUpdateTaskAbortControllerRef.current?.abort()
      }
      const controller = existingController || new AbortController()
      modelUpdateTaskAbortControllerRef.current = controller
      const generation = modelUpdateTaskGenerationRef.current + 1
      modelUpdateTaskGenerationRef.current = generation
      setPersistedModelUpdateTaskId(taskInfo.task_id)
      setCurrentModelUpdateTask(taskInfo)
      const isApplyAllTask = taskInfo.type === modelUpdateApplyAllTaskType
      if (isApplyAllTask) {
        applyAllRef.current = true
        setApplyAllLoading(true)
      } else {
        detectAllRef.current = true
        setDetectAllLoading(true)
      }
      return { controller, generation, isApplyAllTask }
    },
    []
  )

  const finishModelUpdateTaskRun = useCallback(
    (
      generation: number,
      clearTaskId: boolean,
      isApplyAllTask: boolean
    ) => {
      if (!isCurrentModelUpdateTaskRun(generation)) return
      if (clearTaskId) {
        clearPersistedModelUpdateTaskId()
        setCurrentModelUpdateTask(null)
      }
      modelUpdateTaskAbortControllerRef.current = null
      if (isApplyAllTask) {
        applyAllRef.current = false
        setApplyAllLoading(false)
        detectAllRef.current = false
        setDetectAllLoading(false)
      } else {
        detectAllRef.current = false
        setDetectAllLoading(false)
        applyAllRef.current = false
        setApplyAllLoading(false)
      }
    },
    [isCurrentModelUpdateTaskRun]
  )

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      modelUpdateTaskGenerationRef.current += 1
      modelUpdateTaskAbortControllerRef.current?.abort()
      modelUpdateTaskAbortControllerRef.current = null
    }
  }, [])

  useEffect(() => {
    if (!canAccessModelUpdateTasks) {
      setCurrentModelUpdateTaskLookupComplete(false)
    }
  }, [canAccessModelUpdateTasks])

  const openModal = useCallback(
    (
      record: { id: number; [key: string]: unknown } | null,
      pendingAdd: string[] = [],
      pendingRemove: string[] = [],
      tab: 'add' | 'remove' = 'add'
    ) => {
      if (!canApplyUpstreamUpdates) {
        toast.error(t('No permission to perform this action'))
        return
      }
      const normAdd = normalizeModelList(pendingAdd)
      const normRemove = normalizeModelList(pendingRemove)
      if (!record?.id || (normAdd.length === 0 && normRemove.length === 0)) {
        toast.info(t('No processable upstream model updates for this channel'))
        return
      }
      setChannel(record)
      setAddModels(normAdd)
      setRemoveModels(normRemove)
      setPreferredTab(tab)
      setShowModal(true)
    },
    [canApplyUpstreamUpdates, t]
  )

  const closeModal = useCallback(() => {
    setShowModal(false)
    setChannel(null)
    setAddModels([])
    setRemoveModels([])
    setPreferredTab('add')
  }, [])

  const applyUpdates = useCallback(
    async ({
      addModels: selectedAdd = [],
      removeModels: selectedRemove = [],
    }: {
      addModels?: string[]
      removeModels?: string[]
    } = {}) => {
      if (!canApplyUpstreamUpdates) {
        toast.error(t('No permission to perform this action'))
        return
      }
      if (applyRef.current) return
      if (!channel?.id) {
        closeModal()
        return
      }
      applyRef.current = true
      setApplyLoading(true)
      try {
        const normSelectedAdd = normalizeModelList(selectedAdd)
        const selectedAddSet = new Set(normSelectedAdd)
        const ignoreModels = addModels.filter((m) => !selectedAddSet.has(m))

        const res = await api.post(
          '/api/channel/upstream_updates/apply',
          {
            id: channel.id,
            add_models: normSelectedAdd,
            ignore_models: ignoreModels,
            remove_models: normalizeModelList(selectedRemove),
          },
          upstreamUpdateRequestConfig
        )
        const { success, message, data } = res.data || {}
        if (!success) {
          toast.error(message || t('Operation failed'))
          await refreshChannelsBestEffort(refresh)
          return
        }

        toast.success(
          t(
            'Upstream model updates applied: {{added}} added, {{removed}} removed, {{ignored}} ignored this time, {{totalIgnored}} total ignored models',
            {
              added: data?.added_models?.length || 0,
              removed: data?.removed_models?.length || 0,
              ignored: normalizeModelList(ignoreModels).length,
              totalIgnored: getManualIgnoredModelCount(data?.settings),
            }
          )
        )
        closeModal()
        await refreshChannelsBestEffort(refresh)
      } catch (e: unknown) {
        const err = e as {
          response?: { data?: { message?: string } }
          message?: string
        }
        toast.error(
          err?.response?.data?.message || err?.message || t('Operation failed')
        )
        await refreshChannelsBestEffort(refresh)
      } finally {
        applyRef.current = false
        setApplyLoading(false)
      }
    },
    [canApplyUpstreamUpdates, channel, addModels, closeModal, refresh, t]
  )

  const applyAllUpdates = useCallback(async () => {
    if (!canApplyUpstreamUpdates) {
      toast.error(t('No permission to perform this action'))
      return
    }
    if (applyAllRef.current) return
    applyAllRef.current = true
    setApplyAllLoading(true)
    try {
      const res = await api.post(
        '/api/channel/upstream_updates/apply_all',
        {},
        upstreamUpdateRequestConfig
      )
      const taskInfo = getModelUpdateTaskStartInfo(res.data)
      if (!isSuccessPayload(res.data) || !taskInfo) {
        toast.error(
          getResponseMessage(res.data) || t('Batch processing failed')
        )
        await refreshChannelsBestEffort(refresh)
        return
      }
      await pollAndReportModelUpdateTask(
        taskInfo,
        false,
        true,
        modelUpdateApplyAllTaskType
      )
    } catch (e: unknown) {
      const taskInfo = getModelUpdateTaskStartInfo(getErrorPayload(e))
      if (taskInfo) {
        await pollAndReportModelUpdateTask(taskInfo, true)
        return
      }
      const err = e as {
        response?: { data?: { message?: string } }
        message?: string
      }
      toast.error(
        err?.response?.data?.message ||
          err?.message ||
          t('Batch processing failed')
      )
      try {
        await refreshChannelsBestEffort(refresh)
      } catch {
        // Keep the original batch failure visible; a best-effort refresh must
        // not mask it.
      }
    } finally {
      applyAllRef.current = false
      setApplyAllLoading(false)
    }
  }, [canApplyUpstreamUpdates, refresh, t])

  const detectChannelUpdates = useCallback(
    async (ch: { id: number; [key: string]: unknown } | null) => {
      if (!canDetectUpstreamUpdates) {
        toast.error(t('No permission to perform this action'))
        return
      }
      if (detectRef.current) {
        toast.info(t('Please wait a moment before trying again.'))
        return
      }
      if (!ch?.id) return
      detectRef.current = true
      setDetectChannelLoadingId(ch.id)
      try {
        const res = await api.post(
          '/api/channel/upstream_updates/detect',
          { id: ch.id },
          upstreamUpdateRequestConfig
        )
        const { success, message, data } = res.data || {}
        if (!success) {
          toast.error(message || t('Detection failed'))
          await refreshChannelsBestEffort(refresh)
          return
        }

        toast.success(
          t('Detection complete: {{add}} to add, {{remove}} to remove', {
            add: data?.add_models?.length || 0,
            remove: data?.remove_models?.length || 0,
          })
        )
        await refreshChannelsBestEffort(refresh)
      } catch (e: unknown) {
        const err = e as {
          response?: { data?: { message?: string } }
          message?: string
        }
        toast.error(
          err?.response?.data?.message || err?.message || t('Detection failed')
        )
        await refreshChannelsBestEffort(refresh)
      } finally {
        detectRef.current = false
        setDetectChannelLoadingId(null)
      }
    },
    [canDetectUpstreamUpdates, refresh, t]
  )

  const pollAndReportModelUpdateTask = useCallback(
    async (
      taskInfo: ModelUpdateTaskStartInfo,
      existingTask: boolean,
      notify = true,
      fallbackType?: string
    ) => {
      if (
        !taskInfo.type?.trim() &&
        modelUpdateTaskAbortControllerRef.current
      ) {
        return
      }
      const resolvingController = taskInfo.type?.trim()
        ? undefined
        : new AbortController()
      if (resolvingController) {
        modelUpdateTaskAbortControllerRef.current = resolvingController
      }
      let taskRun:
        | ReturnType<typeof beginModelUpdateTaskRun>
        | undefined
      let clearTaskId = false
      try {
        const resolvedTaskInfo = await resolveModelUpdateTaskStartInfo(
          taskInfo,
          resolvingController?.signal,
          fallbackType
        )
        if (resolvingController?.signal.aborted) return
        taskRun = beginModelUpdateTaskRun(
          resolvedTaskInfo,
          resolvingController
        )
        const { controller, generation } = taskRun
        if (!notify) {
          // Mount recovery should keep the UI synchronized without showing a
          // duplicate "started/already running" toast.
        } else if (existingTask) {
          toast.info(
            t('Batch detection task is already running. Waiting for completion')
          )
        } else {
          toast.success(t('Batch detection task started'))
        }

        const polledTask = await waitForModelUpdateTask(resolvedTaskInfo.task_id, {
          signal: controller.signal,
        })
        const task =
          polledTask && !polledTask.type?.trim()
            ? { ...polledTask, type: resolvedTaskInfo.type }
            : polledTask
        if (!isCurrentModelUpdateTaskRun(generation)) return
        if (!task) {
          toast.info(
            t('Batch detection is still running. Please refresh later')
          )
          return
        }

        clearTaskId = true
        if (
          task.status === 'failed' ||
          task.status === 'cancelled' ||
          task.status === 'canceled'
        ) {
          toast.error(
            formatModelUpdateTaskFailureMessage(
              task,
              t('Batch detection failed'),
              t
            )
          )
          await refreshChannelsBestEffort(refresh)
          return
        }

        toast.success(formatModelUpdateTaskSuccessMessage(task, t))
        await refreshChannelsBestEffort(refresh)
      } catch (pollError: unknown) {
        if (!mountedRef.current || resolvingController?.signal.aborted) {
          return
        }
        if (
          taskRun &&
          !isCurrentModelUpdateTaskRun(taskRun.generation)
        ) {
          return
        }
        clearTaskId =
          shouldClearPersistedModelUpdateTaskIdAfterPollingError(pollError)
        toast.error(
          getModelUpdateTaskPollErrorMessage(
            pollError,
            t('Batch detection failed'),
            t
          )
        )
        await refreshChannelsBestEffort(refresh)
      } finally {
        if (taskRun) {
          finishModelUpdateTaskRun(
            taskRun.generation,
            clearTaskId,
            taskRun.isApplyAllTask
          )
        } else if (
          resolvingController &&
          modelUpdateTaskAbortControllerRef.current === resolvingController
        ) {
          if (
            clearTaskId &&
            getPersistedModelUpdateTaskId() === taskInfo.task_id
          ) {
            clearPersistedModelUpdateTaskId()
            setCurrentModelUpdateTask((currentTask) =>
              currentTask?.task_id === taskInfo.task_id ? null : currentTask
            )
          }
          modelUpdateTaskAbortControllerRef.current = null
        }
      }
    },
    [
      beginModelUpdateTaskRun,
      finishModelUpdateTaskRun,
      isCurrentModelUpdateTaskRun,
      refresh,
      t,
    ]
  )

  useEffect(() => {
    if (!canAccessModelUpdateTasks || !currentModelUpdateTaskLookupComplete) {
      return
    }
    const taskId = getPersistedModelUpdateTaskId()
    if (
      detectAllRef.current ||
      modelUpdateTaskAbortControllerRef.current ||
      !shouldResumePersistedModelUpdateTask(canAccessModelUpdateTasks, taskId)
    ) {
      return
    }
    void pollAndReportModelUpdateTask({ task_id: taskId }, true)
  }, [
    canAccessModelUpdateTasks,
    currentModelUpdateTaskLookupComplete,
    pollAndReportModelUpdateTask,
  ])

  useEffect(() => {
    if (!canAccessModelUpdateTasks) return

    let cancelled = false
    let lookupInFlight = false
    let lookupController: AbortController | null = null

    const lookupCurrentTask = async () => {
      if (cancelled || lookupInFlight) return
      lookupInFlight = true
      lookupController = new AbortController()
      try {
        const currentTask = await getCurrentChannelModelUpdateTask(
          lookupController.signal
        )
        if (cancelled || !mountedRef.current) return
        const persistedTaskId = getPersistedModelUpdateTaskId()
        if (currentTask) {
          if (isTerminalTaskStatus(currentTask.status)) {
            clearPersistedModelUpdateTaskId()
            setCurrentModelUpdateTask(null)
          } else {
            setCurrentModelUpdateTask(currentTask)
            if (persistedTaskId && persistedTaskId !== currentTask.task_id) {
              clearPersistedModelUpdateTaskId()
            }
            if (
              !detectAllRef.current &&
              !modelUpdateTaskAbortControllerRef.current
            ) {
              void pollAndReportModelUpdateTask(
                {
                  task_id: currentTask.task_id,
                  status: currentTask.status,
                  type: currentTask.type,
                },
                true,
                false
              )
            }
          }
        } else if (persistedTaskId) {
          // The task may still be running even when the current-task lookup
          // temporarily returns no row (for example during a transient
          // database/API failure on another node). Keep the persisted ID as
          // the recovery anchor and retry the task endpoint on the next
          // discovery pass after a polling error.
          if (
            !detectAllRef.current &&
            !modelUpdateTaskAbortControllerRef.current
          ) {
            void pollAndReportModelUpdateTask(
              { task_id: persistedTaskId },
              true,
              false
            )
          }
        } else {
          setCurrentModelUpdateTask(null)
        }
        if (!cancelled && mountedRef.current) {
          setCurrentModelUpdateTaskLookupComplete(true)
        }
      } catch (error) {
        if (
          !cancelled &&
          mountedRef.current &&
          !isRequestCancelled(error) &&
          !getPersistedModelUpdateTaskId()
        ) {
          setCurrentModelUpdateTask(null)
        }
        if (!cancelled && mountedRef.current) {
          setCurrentModelUpdateTaskLookupComplete(true)
        }
      } finally {
        lookupInFlight = false
        lookupController = null
      }
    }

    void lookupCurrentTask()
    const discoveryTimer = setInterval(() => {
      void lookupCurrentTask()
    }, modelUpdateTaskDiscoveryIntervalMs)

    return () => {
      cancelled = true
      lookupController?.abort()
      clearInterval(discoveryTimer)
    }
  }, [canAccessModelUpdateTasks, pollAndReportModelUpdateTask])

  const detectAllUpdates = useCallback(async () => {
    if (!canDetectUpstreamUpdates) {
      toast.error(t('No permission to perform this action'))
      return
    }

    if (detectAllRef.current) return

    const persistedTaskId = getPersistedModelUpdateTaskId()
    detectAllRef.current = true
    setDetectAllLoading(true)
    let handedOffToTaskPolling = false
    try {
      const currentTask = await getCurrentChannelModelUpdateTask()
      if (currentTask) {
        setCurrentModelUpdateTask(currentTask)
        if (persistedTaskId && persistedTaskId !== currentTask.task_id) {
          clearPersistedModelUpdateTaskId()
        }
        if (isTerminalTaskStatus(currentTask.status)) {
          clearPersistedModelUpdateTaskId()
          setCurrentModelUpdateTask(null)
          toast.info(
            t(
              'Upstream model update task already finished. Refreshing channel list.'
            )
          )
          await refreshChannelsBestEffort(refresh)
          return
        }
        handedOffToTaskPolling = true
        await pollAndReportModelUpdateTask(
          {
            task_id: currentTask.task_id,
            status: currentTask.status,
            type: currentTask.type,
          },
          true
        )
        return
      }
      if (
        shouldResumePersistedModelUpdateTask(
          canAccessModelUpdateTasks,
          persistedTaskId
        )
      ) {
        handedOffToTaskPolling = true
        await pollAndReportModelUpdateTask({ task_id: persistedTaskId }, true)
        return
      }
      setCurrentModelUpdateTask(null)

      const res = await api.post(
        '/api/channel/upstream_updates/detect_all',
        {},
        upstreamUpdateRequestConfig
      )
      const taskInfo = getModelUpdateTaskStartInfo(res.data)
      if (!isSuccessPayload(res.data) || !taskInfo) {
        toast.error(getResponseMessage(res.data) || t('Batch detection failed'))
        await refreshChannelsBestEffort(refresh)
        return
      }
      if (isTerminalTaskStatus(taskInfo.status)) {
        clearPersistedModelUpdateTaskId()
        setCurrentModelUpdateTask(null)
        toast.info(
          t(
            'Upstream model update task already finished. Refreshing channel list.'
          )
        )
        await refreshChannelsBestEffort(refresh)
        return
      }

      handedOffToTaskPolling = true
      await pollAndReportModelUpdateTask(
        taskInfo,
        false,
        true,
        modelUpdateManualTaskType
      )
    } catch (e: unknown) {
      const taskInfo = getModelUpdateTaskStartInfo(getErrorPayload(e))
      if (taskInfo) {
        setCurrentModelUpdateTask(taskInfo)
        if (persistedTaskId && persistedTaskId !== taskInfo.task_id) {
          clearPersistedModelUpdateTaskId()
        }
        if (isTerminalTaskStatus(taskInfo.status)) {
          clearPersistedModelUpdateTaskId()
          setCurrentModelUpdateTask(null)
          toast.info(
            t(
              'Upstream model update task already finished. Refreshing channel list.'
            )
          )
          await refreshChannelsBestEffort(refresh)
          return
        }
        handedOffToTaskPolling = true
        await pollAndReportModelUpdateTask(taskInfo, true)
        return
      }
      toast.error(
        getModelUpdateTaskPollErrorMessage(e, t('Batch detection failed'), t)
      )
      await refreshChannelsBestEffort(refresh)
    } finally {
      if (!handedOffToTaskPolling && mountedRef.current) {
        detectAllRef.current = false
        setDetectAllLoading(false)
      }
    }
  }, [
    canAccessModelUpdateTasks,
    canDetectUpstreamUpdates,
    pollAndReportModelUpdateTask,
    refresh,
    t,
  ])

  const cancelModelUpdateTask = useCallback(async () => {
    if (!canAccessModelUpdateTasks) {
      toast.error(t('No permission to perform this action'))
      return
    }
    if (cancelTaskRef.current) return

    const taskId =
      currentModelUpdateTask?.task_id?.trim() || getPersistedModelUpdateTaskId()
    if (!taskId) {
      toast.info(t('No running upstream model update task'))
      return
    }

    cancelTaskRef.current = true
    setCancelTaskLoading(true)
    const clearTrackedTaskIfMissing = (error: unknown) => {
      if (!shouldClearPersistedModelUpdateTaskIdAfterPollingError(error)) {
        return
      }
      const persistedTaskId = getPersistedModelUpdateTaskId()
      const currentTaskId = currentModelUpdateTask?.task_id?.trim() || ''
      const stillTrackingCancelledTask =
        persistedTaskId === taskId ||
        (!persistedTaskId && currentTaskId === taskId)
      if (!stillTrackingCancelledTask) return

      modelUpdateTaskGenerationRef.current += 1
      modelUpdateTaskAbortControllerRef.current?.abort()
      modelUpdateTaskAbortControllerRef.current = null
      clearPersistedModelUpdateTaskId()
      setCurrentModelUpdateTask(null)
      detectAllRef.current = false
      setDetectAllLoading(false)
      applyAllRef.current = false
      setApplyAllLoading(false)
    }
    try {
      const res = await api.post(
        '/api/channel/upstream_updates/cancel',
        { task_id: taskId },
        upstreamUpdateRequestConfig
      )
      const payload = res.data || {}
      if (!isSuccessPayload(payload)) {
        clearTrackedTaskIfMissing({ response: { data: payload } })
        toast.error(
          getResponseMessage(payload) ||
            t('Failed to cancel upstream model update task')
        )
        await refreshChannelsBestEffort(refresh)
        return
      }

      modelUpdateTaskGenerationRef.current += 1
      modelUpdateTaskAbortControllerRef.current?.abort()
      modelUpdateTaskAbortControllerRef.current = null
      clearPersistedModelUpdateTaskId()
      setCurrentModelUpdateTask(null)
      detectAllRef.current = false
      setDetectAllLoading(false)
      applyAllRef.current = false
      setApplyAllLoading(false)
      const cancelledStatus = isRecord(payload.data)
        ? asSystemTaskStatus(payload.data.status)
        : undefined
      if (isCancelledTaskStatus(cancelledStatus)) {
        toast.success(t('Upstream model update task cancelled'))
      } else {
        toast.info(t('No running upstream model update task'))
      }
      await refreshChannelsBestEffort(refresh)
    } catch (e: unknown) {
      clearTrackedTaskIfMissing(e)
      toast.error(
        getErrorMessage(e) || t('Failed to cancel upstream model update task')
      )
      await refreshChannelsBestEffort(refresh)
    } finally {
      cancelTaskRef.current = false
      setCancelTaskLoading(false)
    }
  }, [
    canAccessModelUpdateTasks,
    currentModelUpdateTask?.task_id,
    refresh,
    t,
  ])

  // Memoized so consumers (and the channels context value built from this) get
  // a stable reference unless an actual field changes. Callbacks above are all
  // useCallback-stable, so this only changes when relevant state changes.
  return useMemo(
    () => ({
      showModal,
      channel,
      addModels,
      removeModels,
      preferredTab,
      applyLoading,
      detectChannelLoadingId,
      detectAllLoading,
      cancelTaskLoading,
      currentModelUpdateTask,
      applyAllLoading,
      openModal,
      closeModal,
      applyUpdates,
      applyAllUpdates,
      detectChannelUpdates,
      detectAllUpdates,
      cancelModelUpdateTask,
    }),
    [
      showModal,
      channel,
      addModels,
      removeModels,
      preferredTab,
      applyLoading,
      detectChannelLoadingId,
      detectAllLoading,
      cancelTaskLoading,
      currentModelUpdateTask,
      applyAllLoading,
      openModal,
      closeModal,
      applyUpdates,
      applyAllUpdates,
      detectChannelUpdates,
      detectAllUpdates,
      cancelModelUpdateTask,
    ]
  )
}
