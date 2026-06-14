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

import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Download, RefreshCw, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const exportFields = [
  ['time', '时间'],
  ['username', '用户名'],
  ['api_key', 'API Key'],
  ['model', '模型'],
  ['request_content', '请求内容'],
  ['response_content', '响应内容'],
  ['user_id', '用户 ID'],
  ['token_id', '密钥 ID'],
  ['token_name', '密钥名称'],
  ['group', '分组'],
  ['prompt_tokens', '输入 Tokens'],
  ['completion_tokens', '输出 Tokens'],
  ['total_tokens', '总 Tokens'],
  ['cache_tokens', '缓存 Tokens'],
  ['channel_id', '渠道 ID'],
  ['channel_name', '渠道名称'],
] as const

type ExportField = (typeof exportFields)[number][0]

type ExportTask = {
  id: number
  status: string
  mode: string
  file_name: string
  file_size: number
  failure_reason: string
  created_at: number
  finished_at: number
  expires_at: number
  total_rows: number
}

type ExportTokenOption = {
  id: number
  name: string
  masked_key: string
  display: string
}

type ConversationExportSectionProps = {
  retentionDays: number
}

function toTimestamp(value: string, endOfDay = false) {
  if (!value) return 0
  const date = new Date(`${value}T${endOfDay ? '23:59:59' : '00:00:00'}+08:00`)
  return Math.floor(date.getTime() / 1000)
}

function formatTime(ts?: number) {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleString('zh-CN', {
    timeZone: 'Asia/Shanghai',
    hour12: false,
  })
}

function formatTaskStatus(status: string) {
  const statusMap: Record<string, string> = {
    pending: '等待中',
    running: '导出中',
    succeeded: '已完成',
    failed: '失败',
    expired: '已过期',
  }
  return statusMap[status] ?? status
}

function formatExportMode(mode: string) {
  return mode === 'strict' ? '严格脱敏' : '明文'
}

async function downloadConversationExport(task: ExportTask) {
  const res = await api.get(`/api/conversation_export/${task.id}/download`, {
    responseType: 'blob',
    disableDuplicate: true,
  })
  const blob = res.data as Blob
  const contentType = String(res.headers?.['content-type'] ?? '')
  if (contentType.includes('application/json')) {
    const text = await blob.text()
    try {
      const data = JSON.parse(text)
      throw new Error(data?.message || '导出文件下载失败')
    } catch (error) {
      if (error instanceof Error) throw error
      throw new Error('导出文件下载失败')
    }
  }
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = task.file_name || `conversation-export-${task.id}.csv`
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

export function ConversationExportSection({
  retentionDays,
}: ConversationExportSectionProps) {
  const updateOption = useUpdateOption()
  const queryClient = useQueryClient()
  const [startDate, setStartDate] = useState('')
  const [endDate, setEndDate] = useState('')
  const [mode, setMode] = useState('plain')
  const [selectedFields, setSelectedFields] = useState<ExportField[]>([])
  const [userId, setUserId] = useState('')
  const [tokenId, setTokenId] = useState('')
  const [tokenKeyword, setTokenKeyword] = useState('')
  const [selectedTokenLabel, setSelectedTokenLabel] = useState('')
  const [model, setModel] = useState('')
  const [group, setGroup] = useState('')
  const [retention, setRetention] = useState(String(retentionDays ?? 30))
  const [pendingDownloadTaskId, setPendingDownloadTaskId] = useState<number | null>(null)
  const [showDeleteSnapshotsDialog, setShowDeleteSnapshotsDialog] = useState(false)

  const { data: tasks = [] } = useQuery({
    queryKey: ['conversation-export-tasks'],
    queryFn: async () => {
      const res = await api.get('/api/conversation_export/?p=1&page_size=20')
      return (res.data?.data?.items ?? []) as ExportTask[]
    },
    refetchInterval: 5000,
  })

  const { data: tokenOptions = [] } = useQuery({
    queryKey: ['conversation-export-tokens', tokenKeyword],
    enabled: tokenKeyword.trim().length > 0 && tokenKeyword !== selectedTokenLabel,
    queryFn: async () => {
      const res = await api.get('/api/conversation_export/tokens/search', {
        params: { keyword: tokenKeyword.trim() },
      })
      return (res.data?.data ?? []) as ExportTokenOption[]
    },
    staleTime: 30 * 1000,
  })

  const createExport = useMutation({
    mutationFn: async () => {
      const startTime = toTimestamp(startDate)
      const endTime = toTimestamp(endDate, true)
      if (!startTime || !endTime) throw new Error('请选择导出时间范围')
      if (selectedFields.length === 0) throw new Error('请至少选择一个字段')
      const res = await api.post('/api/conversation_export/', {
        mode,
        fields: selectedFields,
        filter: {
          start_time: startTime,
          end_time: endTime,
          user_id: userId ? Number(userId) : 0,
          token_id: tokenId ? Number(tokenId) : 0,
          model,
          group,
        },
      })
      if (!res.data.success) throw new Error(res.data.message)
      return res.data.data
    },
    onSuccess: (task: ExportTask) => {
      setPendingDownloadTaskId(task.id)
      toast.success('导出任务已创建，完成后将自动下载')
      queryClient.invalidateQueries({ queryKey: ['conversation-export-tasks'] })
    },
    onError: (error: Error) => toast.error(error.message),
  })

  const deleteSnapshots = useMutation({
    mutationFn: async () => {
      const startTime = toTimestamp(startDate)
      const endTime = toTimestamp(endDate, true)
      if (!startTime || !endTime) throw new Error('请选择删除时间范围')
      const res = await api.delete('/api/conversation_export/snapshots', {
        data: { start_time: startTime, end_time: endTime },
      })
      if (!res.data.success) throw new Error(res.data.message)
      return res.data.data
    },
    onSuccess: (data) => toast.success(`已删除 ${data.deleted} 条对话快照`),
    onError: (error: Error) => toast.error(error.message),
  })

  const fieldMap = useMemo(() => new Map(exportFields), [])

  useEffect(() => {
    if (!pendingDownloadTaskId) return
    const task = tasks.find((item) => item.id === pendingDownloadTaskId)
    if (!task) return
    if (task.status === 'succeeded') {
      setPendingDownloadTaskId(null)
      toast.success('导出完成，正在下载')
      void downloadConversationExport(task).catch((error: Error) => {
        toast.error(error.message || '导出文件下载失败')
      })
    }
    if (task.status === 'failed' || task.status === 'expired') {
      setPendingDownloadTaskId(null)
      toast.error(task.failure_reason || '导出任务失败')
    }
  }, [pendingDownloadTaskId, tasks])

  return (
    <SettingsSection
      title='对话导出'
      description='仅 root 可见。导出文件保留 24 小时，单次最多导出 90 天。'
    >
      <div className='grid gap-4 lg:grid-cols-2'>
        <div className='space-y-2'>
          <Label>开始日期</Label>
          <Input type='date' value={startDate} onChange={(e) => setStartDate(e.target.value)} />
        </div>
        <div className='space-y-2'>
          <Label>结束日期</Label>
          <Input type='date' value={endDate} onChange={(e) => setEndDate(e.target.value)} />
        </div>
        <div className='space-y-2'>
          <Label>用户 ID</Label>
          <Input value={userId} onChange={(e) => setUserId(e.target.value)} placeholder='不填代表不限用户' />
        </div>
        <div className='space-y-2'>
          <Label>API 密钥</Label>
          <Input
            value={tokenKeyword}
            onChange={(e) => {
              const next = e.target.value
              const trimmed = next.trim()
              setTokenKeyword(next)
              setSelectedTokenLabel('')
              setTokenId(/^\d+$/.test(trimmed) ? trimmed : '')
            }}
            placeholder='输入密钥 ID 或名称搜索；不填代表全站所有 Key'
          />
          {tokenId ? (
            <div className='text-muted-foreground flex items-center justify-between gap-2 text-xs'>
              <span className='truncate'>当前选择：{selectedTokenLabel || `#${tokenId}`}</span>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={() => {
                  setTokenId('')
                  setTokenKeyword('')
                  setSelectedTokenLabel('')
                }}
              >
                清除
              </Button>
            </div>
          ) : null}
          {tokenKeyword.trim() && tokenKeyword !== selectedTokenLabel && tokenOptions.length > 0 ? (
            <div className='max-h-40 overflow-y-auto rounded-md border p-1'>
              {tokenOptions.map((option) => (
                <button
                  key={option.id}
                  type='button'
                  className='hover:bg-muted flex w-full rounded-sm px-2 py-1.5 text-left text-xs'
                  onClick={() => {
                    setTokenId(String(option.id))
                    setTokenKeyword(option.display)
                    setSelectedTokenLabel(option.display)
                  }}
                >
                  <span className='truncate'>{option.display}</span>
                </button>
              ))}
            </div>
          ) : null}
        </div>
        <div className='space-y-2'>
          <Label>模型</Label>
          <Input value={model} onChange={(e) => setModel(e.target.value)} placeholder='不填代表不限模型' />
        </div>
        <div className='space-y-2'>
          <Label>分组</Label>
          <Input value={group} onChange={(e) => setGroup(e.target.value)} placeholder='不填代表不限分组' />
        </div>
        <div className='space-y-2'>
          <Label>导出模式</Label>
          <Select
            value={mode}
            onValueChange={(value) => {
              if (value) setMode(value)
            }}
          >
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value='plain'>明文导出</SelectItem>
              <SelectItem value='strict'>严格脱敏导出</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className='space-y-2'>
          <Label>快照自动删除</Label>
          <div className='flex gap-2'>
            <Input value={retention} onChange={(e) => setRetention(e.target.value)} />
            <Button
              type='button'
              onClick={() => updateOption.mutate({ key: 'ConversationSnapshotRetentionDays', value: retention })}
            >
              保存
            </Button>
          </div>
        </div>
      </div>
      <div className='space-y-2'>
        <Label>导出字段</Label>
        <div className='grid gap-2 sm:grid-cols-2 lg:grid-cols-3'>
          {exportFields.map(([field, label]) => (
            <label key={field} className='flex items-center gap-2 text-sm'>
              <Checkbox
                checked={selectedFields.includes(field)}
                onCheckedChange={(checked) =>
                  setSelectedFields((prev) =>
                    checked ? [...prev, field] : prev.filter((item) => item !== field)
                  )
                }
              />
              {label}
            </label>
          ))}
        </div>
      </div>
      <div className='flex flex-wrap gap-2'>
        <Button onClick={() => createExport.mutate()} disabled={createExport.isPending}>
          {createExport.isPending ? '正在创建' : '创建导出任务'}
        </Button>
        <Button variant='outline' onClick={() => queryClient.invalidateQueries({ queryKey: ['conversation-export-tasks'] })}>
          <RefreshCw className='mr-2 size-4' />刷新记录
        </Button>
        <Button
          variant='destructive'
          onClick={() => setShowDeleteSnapshotsDialog(true)}
          disabled={deleteSnapshots.isPending}
        >
          <Trash2 className='mr-2 size-4' />删除所选日期快照
        </Button>
      </div>
      <div className='overflow-x-auto rounded-md border'>
        <table className='w-full min-w-[760px] text-sm'>
          <thead className='bg-muted/50'>
            <tr>
              <th className='px-3 py-2 text-left'>任务</th>
              <th className='px-3 py-2 text-left'>状态</th>
              <th className='px-3 py-2 text-left'>模式</th>
              <th className='px-3 py-2 text-left'>行数</th>
              <th className='px-3 py-2 text-left'>完成时间</th>
              <th className='px-3 py-2 text-left'>失败原因</th>
              <th className='px-3 py-2 text-left'>操作</th>
            </tr>
          </thead>
          <tbody>
            {tasks.map((task) => (
              <tr key={task.id} className='border-t'>
                <td className='px-3 py-2'>#{task.id}</td>
                <td className='px-3 py-2'>{formatTaskStatus(task.status)}</td>
                <td className='px-3 py-2'>{formatExportMode(task.mode)}</td>
                <td className='px-3 py-2'>{task.total_rows}</td>
                <td className='px-3 py-2'>{formatTime(task.finished_at)}</td>
                <td className='max-w-[240px] truncate px-3 py-2'>{task.failure_reason || '-'}</td>
                <td className='px-3 py-2'>
                  {task.status === 'succeeded' ? (
                    <Button
                      size='sm'
                      variant='outline'
                      onClick={() => {
                        void downloadConversationExport(task).catch((error: Error) => {
                          toast.error(error.message || '导出文件下载失败')
                        })
                      }}
                    >
                      <Download className='mr-1 size-4' />下载
                    </Button>
                  ) : (
                    <span className='text-muted-foreground'>-</span>
                  )}
                </td>
              </tr>
            ))}
            {tasks.length === 0 ? (
              <tr>
                <td colSpan={7} className='px-3 py-8 text-center text-muted-foreground'>
                  暂无导出记录
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
      <div className='text-muted-foreground text-xs'>
        已选字段：{selectedFields.map((field) => fieldMap.get(field)).filter(Boolean).join('、') || '未选择'}
      </div>
      <AlertDialog
        open={showDeleteSnapshotsDialog}
        onOpenChange={setShowDeleteSnapshotsDialog}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除对话快照？</AlertDialogTitle>
            <AlertDialogDescription>
              将永久删除当前开始日期到结束日期内的对话快照，删除后无法用于对话导出，也无法恢复。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setShowDeleteSnapshotsDialog(false)
                deleteSnapshots.mutate()
              }}
            >
              确认删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
