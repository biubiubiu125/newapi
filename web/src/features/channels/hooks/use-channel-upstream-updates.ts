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
const modelUpdateTaskMaxPolls = 900
const modelUpdateTaskStorageKey = 'newapi.channel.upstream_update.task_id'

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

function countRemainingRemoveModels(results: unknown): number {
  if (!Array.isArray(results)) return 0
  return results.reduce((total, item) => {
    if (!isRecord(item)) return total
    return (
      total +
      normalizeModelList(
        (item.remaining_remove_models as unknown[] | undefined) || []
      ).length
    )
  }, 0)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object'
}

function asSystemTaskStatus(value: unknown): SystemTaskStatus | undefined {
  if (
    value === 'pending' ||
    value === 'running' ||
    value === 'succeeded' ||
    value === 'failed'
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
  if (!isRecord(response) || typeof response.status !== 'number')
    return undefined
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
  if (typeof taskId !== 'string' || taskId.length === 0) return null
  return {
    task_id: taskId,
    status: asSystemTaskStatus(payload.data.status),
    type: typeof payload.data.type === 'string' ? payload.data.type : undefined,
  }
}

function isSuccessPayload(payload: unknown): boolean {
  return isRecord(payload) && payload.success === true
}

function isTerminalTaskStatus(status: SystemTaskStatus): boolean {
  return status === 'succeeded' || status === 'failed'
}

function sleep(ms: number, signal?: AbortSignal) {
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

export async function waitForModelUpdateTask(
  taskId: string,
  {
    signal,
    maxPolls = modelUpdateTaskMaxPolls,
    pollIntervalMs = modelUpdateTaskPollIntervalMs,
  }: {
    signal?: AbortSignal
    maxPolls?: number
    pollIntervalMs?: number
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
    if (isTerminalTaskStatus(res.data.status)) return res.data
    if (i + 1 < maxPolls) {
      await sleep(pollIntervalMs, signal)
    }
  }
  return null
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

  const applyRef = useRef(false)
  const detectRef = useRef(false)
  const detectAllRef = useRef(false)
  const applyAllRef = useRef(false)
  const mountedRef = useRef(true)
  const modelUpdateTaskGenerationRef = useRef(0)
  const modelUpdateTaskAbortControllerRef = useRef<AbortController | null>(null)

  const isCurrentModelUpdateTaskRun = useCallback((generation: number) => {
    return (
      mountedRef.current && modelUpdateTaskGenerationRef.current === generation
    )
  }, [])

  const beginModelUpdateTaskRun = useCallback((taskId: string) => {
    modelUpdateTaskAbortControllerRef.current?.abort()
    const controller = new AbortController()
    modelUpdateTaskAbortControllerRef.current = controller
    const generation = modelUpdateTaskGenerationRef.current + 1
    modelUpdateTaskGenerationRef.current = generation
    setPersistedModelUpdateTaskId(taskId)
    detectAllRef.current = true
    setDetectAllLoading(true)
    return { controller, generation }
  }, [])

  const finishModelUpdateTaskRun = useCallback(
    (generation: number, clearTaskId: boolean) => {
      if (!isCurrentModelUpdateTaskRun(generation)) return
      if (clearTaskId) {
        clearPersistedModelUpdateTaskId()
      }
      modelUpdateTaskAbortControllerRef.current = null
      detectAllRef.current = false
      setDetectAllLoading(false)
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
        await refresh()
      } catch (e: unknown) {
        const err = e as {
          response?: { data?: { message?: string } }
          message?: string
        }
        toast.error(
          err?.response?.data?.message || err?.message || t('Operation failed')
        )
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
      const { success, message, data } = res.data || {}
      if (!success) {
        toast.error(message || t('Batch processing failed'))
        return
      }

      const keptRemoveModels =
        typeof data?.remaining_remove_models_count === 'number'
          ? data.remaining_remove_models_count
          : countRemainingRemoveModels(data?.results)
      toast.success(
        t(
          'Batch upstream model additions applied: {{channels}} channels, {{added}} added, {{kept}} pending removals kept for manual review, {{fails}} failed',
          {
            channels: data?.processed_channels || 0,
            added: data?.added_models || 0,
            kept: keptRemoveModels,
            fails: (data?.failed_channel_ids || []).length,
          }
        )
      )
      await refresh()
    } catch (e: unknown) {
      const err = e as {
        response?: { data?: { message?: string } }
        message?: string
      }
      toast.error(
        err?.response?.data?.message ||
          err?.message ||
          t('Batch processing failed')
      )
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
          return
        }

        toast.success(
          t('Detection complete: {{add}} to add, {{remove}} to remove', {
            add: data?.add_models?.length || 0,
            remove: data?.remove_models?.length || 0,
          })
        )
        await refresh()
      } catch (e: unknown) {
        const err = e as {
          response?: { data?: { message?: string } }
          message?: string
        }
        toast.error(
          err?.response?.data?.message || err?.message || t('Detection failed')
        )
      } finally {
        detectRef.current = false
        setDetectChannelLoadingId(null)
      }
    },
    [canDetectUpstreamUpdates, refresh, t]
  )

  const pollAndReportModelUpdateTask = useCallback(
    async (taskInfo: ModelUpdateTaskStartInfo, existingTask: boolean) => {
      const { controller, generation } = beginModelUpdateTaskRun(
        taskInfo.task_id
      )
      if (existingTask) {
        toast.info(
          t('Batch detection task is already running. Waiting for completion')
        )
      } else {
        toast.success(t('Batch detection task started'))
      }

      let clearTaskId = false
      try {
        const task = await waitForModelUpdateTask(taskInfo.task_id, {
          signal: controller.signal,
        })
        if (!isCurrentModelUpdateTaskRun(generation)) return
        if (!task) {
          toast.info(
            t('Batch detection is still running. Please refresh later')
          )
          return
        }

        clearTaskId = true
        if (task.status === 'failed') {
          toast.error(task.error || t('Batch detection failed'))
          return
        }

        const result = task.result || {}
        toast.success(
          t(
            'Batch detection complete: {{channels}} channels, {{add}} to add, {{remove}} to remove, {{fails}} failed',
            {
              channels: result.checked_channels || 0,
              add: result.detected_add_models || 0,
              remove: result.detected_remove_models || 0,
              fails: result.failed_channels || 0,
            }
          )
        )
        await refresh()
      } catch (pollError: unknown) {
        if (!isCurrentModelUpdateTaskRun(generation)) return
        clearTaskId =
          shouldClearPersistedModelUpdateTaskIdAfterPollingError(pollError)
        toast.error(getErrorMessage(pollError) || t('Batch detection failed'))
      } finally {
        finishModelUpdateTaskRun(generation, clearTaskId)
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
    const taskId = getPersistedModelUpdateTaskId()
    if (
      detectAllRef.current ||
      !shouldResumePersistedModelUpdateTask(canDetectUpstreamUpdates, taskId)
    ) {
      return
    }
    void pollAndReportModelUpdateTask({ task_id: taskId }, true)
  }, [canDetectUpstreamUpdates, pollAndReportModelUpdateTask])

  const detectAllUpdates = useCallback(async () => {
    if (!canDetectUpstreamUpdates) {
      toast.error(t('No permission to perform this action'))
      return
    }

    if (detectAllRef.current) return

    const persistedTaskId = getPersistedModelUpdateTaskId()
    if (
      shouldResumePersistedModelUpdateTask(
        canDetectUpstreamUpdates,
        persistedTaskId
      )
    ) {
      await pollAndReportModelUpdateTask({ task_id: persistedTaskId }, true)
      return
    }

    detectAllRef.current = true
    setDetectAllLoading(true)
    let handedOffToTaskPolling = false
    try {
      const res = await api.post(
        '/api/channel/upstream_updates/detect_all',
        {},
        upstreamUpdateRequestConfig
      )
      const taskInfo = getModelUpdateTaskStartInfo(res.data)
      if (!isSuccessPayload(res.data) || !taskInfo) {
        toast.error(getResponseMessage(res.data) || t('Batch detection failed'))
        return
      }

      handedOffToTaskPolling = true
      await pollAndReportModelUpdateTask(taskInfo, false)
    } catch (e: unknown) {
      const taskInfo = getModelUpdateTaskStartInfo(getErrorPayload(e))
      if (taskInfo) {
        handedOffToTaskPolling = true
        await pollAndReportModelUpdateTask(taskInfo, true)
        return
      }
      toast.error(getErrorMessage(e) || t('Batch detection failed'))
    } finally {
      if (!handedOffToTaskPolling && mountedRef.current) {
        detectAllRef.current = false
        setDetectAllLoading(false)
      }
    }
  }, [canDetectUpstreamUpdates, pollAndReportModelUpdateTask, t])

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
      applyAllLoading,
      openModal,
      closeModal,
      applyUpdates,
      applyAllUpdates,
      detectChannelUpdates,
      detectAllUpdates,
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
      applyAllLoading,
      openModal,
      closeModal,
      applyUpdates,
      applyAllUpdates,
      detectChannelUpdates,
      detectAllUpdates,
    ]
  )
}
