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
  acknowledgeImageTaskResult,
  cancelImageTask,
  createImageEditTask,
  createImageGenerationTask,
  getImageTaskResult,
  ImageTaskRequestError,
  listImageTasks,
} from '../api'
import {
  imageTaskFormSchema,
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
}

const DEFAULT_FORM_VALUES: ImageTaskFormValues = {
  tokenId: 0,
  mode: 'generation',
  model: 'gpt-image-1',
  prompt: '',
  n: 1,
  size: '',
  quality: '',
  responseFormat: '',
  image: null,
  mask: null,
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

function resultToUrls(result: ImageTaskResult): string[] | null {
  const items = result.data ?? []
  if (items.length === 0) return null

  const urls = items.map((item) => {
    if (item.b64_json) return `data:image/png;base64,${item.b64_json}`
    if (!item.url) return null

    try {
      const url = new URL(item.url, window.location.origin)
      if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
      return url.toString()
    } catch {
      return null
    }
  })

  return urls.every((url): url is string => Boolean(url)) ? urls : null
}

function taskErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof ImageTaskRequestError) {
    return `${error.code}: ${error.message}`
  }
  if (error instanceof Error) return error.message
  return fallback
}

export function ImageTaskPage() {
  const { t } = useTranslation()
  const [storedTasks, setStoredTasks] = useState<StoredImageTask[]>(() =>
    loadStoredImageTasks()
  )
  const storedTasksRef = useRef(storedTasks)
  const [taskRecords, setTaskRecords] = useState<Record<string, TaskRecord>>({})
  const resolvedKeysRef = useRef<Record<number, string>>({})
  const pendingKeyRequests = useRef<
    Record<number, Promise<string | null> | undefined>
  >({})
  const [refreshing, setRefreshing] = useState(false)
  const refreshingRef = useRef(false)

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

  const setTask = useCallback((task: PublicImageTask, tokenId: number) => {
    setStoredTasks((previous) => {
      const next = [
        { taskId: task.task_id, tokenId, createdAt: task.created_at },
        ...previous.filter((item) => item.taskId !== task.task_id),
      ].slice(0, 100)
      saveStoredImageTasks(next)
      return next
    })
    setTaskRecords((previous) => ({
      ...previous,
      [task.task_id]: {
        taskId: task.task_id,
        tokenId,
        createdAt: task.created_at,
        task,
        resultUrls: previous[task.task_id]?.resultUrls ?? [],
        resultLoading: false,
      },
    }))
  }, [])

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
              setStoredTasks((previous) => {
                const next = previous.filter(
                  (item) => !notFoundIDs.has(item.taskId)
                )
                saveStoredImageTasks(next)
                return next
              })
              setTaskRecords((previous) => {
                const next = { ...previous }
                for (const taskId of notFoundIDs) {
                  delete next[taskId]
                }
                return next
              })
            }
            setTaskRecords((previous) => {
              const next = { ...previous }
              for (const task of response.data ?? []) {
                const stored = next[task.task_id] ?? {
                  taskId: task.task_id,
                  tokenId,
                  createdAt: task.created_at,
                  task: null,
                  resultUrls: [],
                  resultLoading: false,
                }
                next[task.task_id] = { ...stored, task }
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
    [resolveKey, t]
  )

  useEffect(() => {
    void refreshTasks(true)
    const interval = window.setInterval(() => {
      void refreshTasks(true)
    }, 3000)
    return () => window.clearInterval(interval)
  }, [refreshTasks])

  const taskList = useMemo(
    () =>
      storedTasks
        .map((item) => ({
          ...item,
          task: taskRecords[item.taskId]?.task ?? null,
          resultUrls: taskRecords[item.taskId]?.resultUrls ?? [],
          resultLoading: taskRecords[item.taskId]?.resultLoading ?? false,
        }))
        .sort((left, right) => right.createdAt - left.createdAt),
    [storedTasks, taskRecords]
  )

  const handleResult = useCallback(
    async (record: TaskRecord) => {
      if (
        !record.task?.result_available ||
        record.task.status !== 'completed'
      ) {
        return
      }
      setTaskRecords((previous) => ({
        ...previous,
        [record.taskId]: { ...previous[record.taskId], resultLoading: true },
      }))
      try {
        const apiKey = await resolveKey(record.tokenId)
        if (!apiKey) return
        const result = await getImageTaskResult(apiKey, record.taskId)
        const resultUrls = resultToUrls(result)
        if (!resultUrls) throw new Error(t('Image result is empty'))
        setTaskRecords((previous) => ({
          ...previous,
          [record.taskId]: {
            ...previous[record.taskId],
            resultUrls,
            resultLoading: false,
          },
        }))
        try {
          const acknowledged = await acknowledgeImageTaskResult(
            apiKey,
            record.taskId
          )
          setTaskRecords((previous) => ({
            ...previous,
            [record.taskId]: {
              ...previous[record.taskId],
              task: acknowledged,
            },
          }))
        } catch (error) {
          toast.error(taskErrorMessage(error, t('Request failed')))
        }
      } catch (error) {
        setTaskRecords((previous) => ({
          ...previous,
          [record.taskId]: { ...previous[record.taskId], resultLoading: false },
        }))
        toast.error(taskErrorMessage(error, t('Request failed')))
      }
    },
    [resolveKey, t]
  )

  const handleCancel = useCallback(
    async (record: TaskRecord) => {
      if (record.task?.status !== 'queued') return
      try {
        const apiKey = await resolveKey(record.tokenId)
        if (!apiKey) return
        const task = await cancelImageTask(apiKey, record.taskId)
        setTaskRecords((previous) => ({
          ...previous,
          [record.taskId]: { ...previous[record.taskId], task },
        }))
      } catch (error) {
        toast.error(taskErrorMessage(error, t('Request failed')))
      }
    },
    [resolveKey, t]
  )

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Image Tasks')}</SectionPageLayout.Title>
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
              storedTasks.length > 0 && Object.keys(taskRecords).length === 0
            }
            onResult={handleResult}
            onCancel={handleCancel}
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
  onTaskCreated: (task: PublicImageTask, tokenId: number) => void
}) {
  const { t } = useTranslation()
  const [submitting, setSubmitting] = useState(false)
  const form = useForm<ImageTaskFormValues>({
    resolver: zodResolver(imageTaskFormSchema),
    defaultValues: DEFAULT_FORM_VALUES,
  })
  const mode = form.watch('mode')

  const submit = async (values: ImageTaskFormValues) => {
    setSubmitting(true)
    try {
      const tokenId = values.tokenId
      const apiKey = await resolveKey(tokenId)
      if (!apiKey) return
      const clientTaskId = crypto.randomUUID()
      const task =
        values.mode === 'edit' && values.image
          ? await createImageEditTask(apiKey, {
              model: values.model,
              prompt: values.prompt,
              n: values.n,
              size: values.size,
              quality: values.quality,
              response_format: values.responseFormat || undefined,
              client_task_id: clientTaskId,
              image: values.image,
              mask: values.mask ?? undefined,
            })
          : await createImageGenerationTask(apiKey, {
              model: values.model,
              prompt: values.prompt,
              n: values.n,
              size: values.size,
              quality: values.quality,
              response_format: values.responseFormat || undefined,
              client_task_id: clientTaskId,
            })
      onTaskCreated(task, tokenId)
      form.reset({
        ...DEFAULT_FORM_VALUES,
        tokenId,
        mode: values.mode,
      })
      toast.success(t('Image task created'))
    } catch (error) {
      toast.error(taskErrorMessage(error, t('Request failed')))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Card className='h-fit'>
      <CardHeader>
        <CardTitle>{t('Create Image Task')}</CardTitle>
      </CardHeader>
      <CardContent>
        <form className='grid gap-4' onSubmit={form.handleSubmit(submit)}>
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
                {t('Generation')}
              </NativeSelectOption>
              <NativeSelectOption value='edit'>{t('Edit')}</NativeSelectOption>
            </NativeSelect>
          </div>

          <div className='grid gap-2'>
            <Label htmlFor='image-task-model'>{t('Model')}</Label>
            <Input id='image-task-model' {...form.register('model')} />
            {form.formState.errors.model && (
              <p className='text-destructive text-xs'>
                {form.formState.errors.model.message}
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
                {form.formState.errors.prompt.message}
              </p>
            )}
          </div>

          {mode === 'edit' && (
            <>
              <div className='grid gap-2'>
                <Label htmlFor='image-task-image'>{t('Input image')}</Label>
                <Input
                  id='image-task-image'
                  type='file'
                  accept='image/*'
                  onChange={(event) => {
                    form.setValue('image', event.target.files?.[0] ?? null, {
                      shouldValidate: true,
                    })
                  }}
                />
                {form.formState.errors.image && (
                  <p className='text-destructive text-xs'>
                    {form.formState.errors.image.message}
                  </p>
                )}
              </div>
              <div className='grid gap-2'>
                <Label htmlFor='image-task-mask'>
                  {t('Mask')} ({t('Optional')})
                </Label>
                <Input
                  id='image-task-mask'
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
                max={128}
                {...form.register('n', { valueAsNumber: true })}
              />
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='image-task-format'>{t('Response format')}</Label>
              <NativeSelect
                id='image-task-format'
                {...form.register('responseFormat')}
              >
                <NativeSelectOption value=''>{t('Auto')}</NativeSelectOption>
                <NativeSelectOption value='url'>URL</NativeSelectOption>
                <NativeSelectOption value='b64_json'>Base64</NativeSelectOption>
              </NativeSelect>
            </div>
          </div>

          <div className='grid grid-cols-2 gap-3'>
            <div className='grid gap-2'>
              <Label htmlFor='image-task-size'>{t('Size')}</Label>
              <NativeSelect id='image-task-size' {...form.register('size')}>
                <NativeSelectOption value=''>{t('Auto')}</NativeSelectOption>
                <NativeSelectOption value='1024x1024'>
                  1024x1024
                </NativeSelectOption>
                <NativeSelectOption value='1024x1536'>
                  1024x1536
                </NativeSelectOption>
                <NativeSelectOption value='1536x1024'>
                  1536x1024
                </NativeSelectOption>
              </NativeSelect>
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='image-task-quality'>{t('Quality')}</Label>
              <NativeSelect
                id='image-task-quality'
                {...form.register('quality')}
              >
                <NativeSelectOption value=''>{t('Auto')}</NativeSelectOption>
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
  onResult,
  onCancel,
}: {
  records: TaskRecord[]
  isLoading: boolean
  onResult: (record: TaskRecord) => void
  onCancel: (record: TaskRecord) => void
}) {
  const { t } = useTranslation()
  let content: ReactNode = records.map((record) => (
    <TaskCard
      key={record.taskId}
      record={record}
      onResult={onResult}
      onCancel={onCancel}
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
          <EmptyTitle>{t('No image tasks')}</EmptyTitle>
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
        <h3 className='text-base font-semibold'>{t('Image Task History')}</h3>
        <span className='text-muted-foreground text-sm'>{records.length}</span>
      </div>
      {content}
    </div>
  )
}

function TaskCard({
  record,
  onResult,
  onCancel,
}: {
  record: TaskRecord
  onResult: (record: TaskRecord) => void
  onCancel: (record: TaskRecord) => void
}) {
  const { t } = useTranslation()
  const task = record.task
  const status = task?.status ?? 'queued'
  const canCancel = status === 'queued'
  const canLoadResult = status === 'completed' && task?.result_available
  const resultKeyCounts = new Map<string, number>()

  return (
    <Card size='sm'>
      <CardHeader className='gap-2'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <CardTitle className='font-mono text-sm'>{record.taskId}</CardTitle>
          <StatusBadge
            label={getStatusLabel(status, t)}
            variant={getStatusVariant(status)}
            copyable={false}
          />
        </div>
        <div className='text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-xs'>
          <span>
            {t('Created')}:{' '}
            {formatTaskTime(task?.created_at ?? record.createdAt)}
          </span>
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
        {record.resultUrls.length > 0 && (
          <div className='grid gap-2'>
            {record.resultUrls.map((resultUrl) => {
              const occurrence = resultKeyCounts.get(resultUrl) ?? 0
              resultKeyCounts.set(resultUrl, occurrence + 1)
              return (
                <div
                  key={`${record.taskId}-${resultUrl}-${occurrence}`}
                  className='grid gap-2'
                >
                  <img
                    src={resultUrl}
                    alt={t('Image task result')}
                    className='max-h-96 w-full rounded-lg border object-contain'
                  />
                  <a
                    className='text-primary inline-flex items-center gap-1 text-sm underline underline-offset-4'
                    href={resultUrl}
                    download={`${record.taskId}-${occurrence + 1}.png`}
                    target='_blank'
                    rel='noreferrer'
                  >
                    <Download className='size-4' />
                    {t('Download')}
                  </a>
                </div>
              )
            })}
          </div>
        )}
        <div className='flex flex-wrap justify-end gap-2'>
          {canLoadResult && record.resultUrls.length === 0 && (
            <Button
              variant='secondary'
              size='sm'
              onClick={() => onResult(record)}
              disabled={record.resultLoading}
            >
              {record.resultLoading ? <Spinner /> : <Check />}
              {record.resultLoading ? t('Loading...') : t('View result')}
            </Button>
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
