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
import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Eye, RefreshCw, Send } from 'lucide-react'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { SettingsSection } from '../components/settings-section'

type TelegramRecord = {
  id: number
  title: string
  content: string
  chat_id: string
  display_name: string
  source: string
  status: string
  attempt_count: number
  failure_reason: string
  created_at: number
  sent_at: number
}

function formatPushStatus(status: string) {
  const statusMap: Record<string, string> = {
    pending: '等待推送',
    running: '推送中',
    succeeded: '已发送',
    failed: '失败',
  }
  return statusMap[status] ?? status
}

function formatPushSource(source: string) {
  if (!source) return '手动推送'
  const sourceMap: Record<string, string> = {
    auto: '自动推送',
    manual: '手动推送',
  }
  return sourceMap[source] ?? (source || '-')
}

export function TelegramPushSection() {
  const queryClient = useQueryClient()
  const [botToken, setBotToken] = useState('')
  const [chatId, setChatId] = useState('')
  const [displayName, setDisplayName] = useState('RKAPI')
  const [testText, setTestText] = useState('Telegram 推送测试成功')
  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')
  const [detailRecord, setDetailRecord] = useState<TelegramRecord | null>(null)

  const { data: settings } = useQuery({
    queryKey: ['telegram-push-settings'],
    queryFn: async () => {
      const res = await api.get('/api/telegram_push/settings')
      return res.data.data as {
        bot_token: string
        chat_id: string
        display_name: string
      }
    },
  })

  useEffect(() => {
    if (settings) {
      setBotToken(settings.bot_token ?? '')
      setChatId(settings.chat_id ?? '')
      setDisplayName(settings.display_name ?? 'RKAPI')
    }
  }, [settings])

  const { data: records = [] } = useQuery({
    queryKey: ['telegram-push-records'],
    queryFn: async () => {
      const res = await api.get('/api/telegram_push/records?p=1&page_size=20')
      return (res.data?.data?.items ?? []) as TelegramRecord[]
    },
    refetchInterval: 5000,
  })

  const saveSettings = useMutation({
    mutationFn: async () => {
      const res = await api.put('/api/telegram_push/settings', {
        bot_token: botToken,
        chat_id: chatId,
        display_name: displayName,
      })
      if (!res.data.success) throw new Error(res.data.message)
    },
    onSuccess: () => toast.success('Telegram 推送配置已保存'),
    onError: (error: Error) => toast.error(error.message),
  })

  const testPush = useMutation({
    mutationFn: async () => {
      const res = await api.post('/api/telegram_push/test', { text: testText })
      if (!res.data.success) throw new Error(res.data.message)
    },
    onSuccess: () => toast.success('测试推送已发送'),
    onError: (error: Error) => toast.error(error.message),
  })

  const pushAnnouncement = useMutation({
    mutationFn: async () => {
      if (!title.trim() && !content.trim()) {
        throw new Error('公告标题和内容不能同时为空')
      }
      const res = await api.post('/api/telegram_push/announcements', {
        title,
        content,
      })
      if (!res.data.success) throw new Error(res.data.message)
    },
    onSuccess: () => {
      toast.success('公告推送任务已创建')
      queryClient.invalidateQueries({ queryKey: ['telegram-push-records'] })
    },
    onError: (error: Error) => toast.error(error.message),
  })

  const retryRecord = useMutation({
    mutationFn: async (id: number) => {
      const res = await api.post(`/api/telegram_push/records/${id}/retry`)
      if (!res.data.success) throw new Error(res.data.message)
    },
    onSuccess: () => {
      toast.success('已重新推送')
      queryClient.invalidateQueries({ queryKey: ['telegram-push-records'] })
    },
    onError: (error: Error) => toast.error(error.message),
  })

  return (
    <SettingsSection
      title='Telegram 推送'
      description='公告推送按原文发送，不做脱敏，消息前缀使用项目显示名称。'
    >
      <div className='grid gap-4 lg:grid-cols-3'>
        <div className='space-y-2'>
          <Label>项目显示名称</Label>
          <Input
            value={displayName}
            maxLength={32}
            placeholder='RKAPI'
            onChange={(e) => setDisplayName(e.target.value)}
          />
          <p className='text-muted-foreground text-xs'>
            Telegram 文本前缀，例如 [{displayName.trim() || 'RKAPI'}]公告标题
          </p>
        </div>
        <div className='space-y-2'>
          <Label>Bot Token</Label>
          <Input
            type='password'
            value={botToken}
            autoComplete='off'
            onChange={(e) => setBotToken(e.target.value)}
          />
          <p className='text-muted-foreground text-xs leading-5'>
            在 Telegram 搜索 @BotFather，发送 /newbot 创建机器人后复制 Bot Token。
            Token 等同机器人密钥，只填写在这里，不要发到群组或公开页面。
          </p>
        </div>
        <div className='space-y-2'>
          <Label>Chat ID</Label>
          <Input value={chatId} onChange={(e) => setChatId(e.target.value)} />
          <p className='text-muted-foreground text-xs leading-5'>
            私聊填管理员 Telegram 用户 ID，频道填频道 ID 或 @频道用户名，群组填群组 ID。
            私聊用户需先主动给机器人发过消息；频道/群组需把机器人加入并授予发消息权限。
          </p>
        </div>
      </div>
      <div className='flex flex-wrap gap-2'>
        <Button
          onClick={() => saveSettings.mutate()}
          disabled={saveSettings.isPending}
        >
          保存配置
        </Button>
        <Input
          className='max-w-md'
          value={testText}
          onChange={(e) => setTestText(e.target.value)}
        />
        <Button
          variant='outline'
          onClick={() => testPush.mutate()}
          disabled={testPush.isPending}
        >
          <Send className='mr-2 size-4' />
          测试推送
        </Button>
      </div>
      <div className='grid gap-3'>
        <Label>手动推送公告</Label>
        <Input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder='公告标题'
        />
        <Textarea
          value={content}
          onChange={(e) => setContent(e.target.value)}
          className='h-40 resize-none'
          placeholder='公告内容原文'
        />
        <Button
          className='w-fit'
          onClick={() => pushAnnouncement.mutate()}
          disabled={pushAnnouncement.isPending}
        >
          推送公告
        </Button>
      </div>
      <div className='flex items-center justify-between'>
        <Label>推送记录</Label>
        <Button
          variant='outline'
          size='sm'
          onClick={() =>
            queryClient.invalidateQueries({
              queryKey: ['telegram-push-records'],
            })
          }
        >
          <RefreshCw className='mr-2 size-4' />
          刷新
        </Button>
      </div>
      <div className='overflow-x-auto rounded-md border'>
        <table className='w-full min-w-[800px] text-sm'>
          <thead className='bg-muted/50'>
            <tr>
              <th className='px-3 py-2 text-left'>记录</th>
              <th className='px-3 py-2 text-left'>项目</th>
              <th className='px-3 py-2 text-left'>方式</th>
              <th className='px-3 py-2 text-left'>标题</th>
              <th className='px-3 py-2 text-left'>状态</th>
              <th className='px-3 py-2 text-left'>次数</th>
              <th className='px-3 py-2 text-left'>失败原因</th>
              <th className='px-3 py-2 text-left'>操作</th>
            </tr>
          </thead>
          <tbody>
            {records.map((record) => (
              <tr key={record.id} className='border-t'>
                <td className='px-3 py-2'>#{record.id}</td>
                <td className='px-3 py-2'>{record.display_name || '-'}</td>
                <td className='px-3 py-2'>{formatPushSource(record.source)}</td>
                <td className='max-w-[240px] truncate px-3 py-2'>
                  {record.title || record.content || '-'}
                </td>
                <td className='px-3 py-2'>{formatPushStatus(record.status)}</td>
                <td className='px-3 py-2'>{record.attempt_count}</td>
                <td className='max-w-[280px] truncate px-3 py-2'>
                  {record.failure_reason || '-'}
                </td>
                <td className='px-3 py-2'>
                  <div className='flex flex-wrap gap-2'>
                    <Button
                      size='sm'
                      variant='outline'
                      onClick={() => setDetailRecord(record)}
                    >
                      <Eye className='mr-1 size-4' />
                      详情
                    </Button>
                    {record.status === 'failed' ? (
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => retryRecord.mutate(record.id)}
                      >
                        重试
                      </Button>
                    ) : null}
                  </div>
                </td>
              </tr>
            ))}
            {records.length === 0 ? (
              <tr>
                <td
                  colSpan={8}
                  className='text-muted-foreground px-3 py-8 text-center'
                >
                  暂无推送记录
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
      <Dialog
        open={!!detailRecord}
        onOpenChange={(open) => !open && setDetailRecord(null)}
      >
        <DialogContent className='max-h-[85vh] max-w-3xl overflow-y-auto'>
          <DialogHeader>
            <DialogTitle>推送记录详情</DialogTitle>
          </DialogHeader>
          {detailRecord ? (
            <div className='space-y-4'>
              <div className='grid gap-3 sm:grid-cols-2'>
                <div className='space-y-1'>
                  <Label>记录</Label>
                  <div className='rounded-md border px-3 py-2 text-sm'>
                    #{detailRecord.id}
                  </div>
                </div>
                <div className='space-y-1'>
                  <Label>状态</Label>
                  <div className='rounded-md border px-3 py-2 text-sm'>
                    {formatPushStatus(detailRecord.status)}
                  </div>
                </div>
                <div className='space-y-1'>
                  <Label>推送方式</Label>
                  <div className='rounded-md border px-3 py-2 text-sm'>
                    {formatPushSource(detailRecord.source)}
                  </div>
                </div>
                <div className='space-y-1'>
                  <Label>推送次数</Label>
                  <div className='rounded-md border px-3 py-2 text-sm'>
                    {detailRecord.attempt_count}
                  </div>
                </div>
                <div className='space-y-1'>
                  <Label>Chat ID</Label>
                  <div className='rounded-md border px-3 py-2 text-sm break-all'>
                    {detailRecord.chat_id || '-'}
                  </div>
                </div>
                <div className='space-y-1'>
                  <Label>项目显示名称</Label>
                  <div className='rounded-md border px-3 py-2 text-sm break-all'>
                    {detailRecord.display_name || '-'}
                  </div>
                </div>
              </div>
              <div className='space-y-1'>
                <Label>标题原文</Label>
                <div className='rounded-md border px-3 py-2 text-sm break-words whitespace-pre-wrap'>
                  {detailRecord.title || '-'}
                </div>
              </div>
              <div className='space-y-1'>
                <Label>内容原文</Label>
                <div className='max-h-80 overflow-y-auto rounded-md border px-3 py-2 text-sm break-words whitespace-pre-wrap'>
                  {detailRecord.content || '-'}
                </div>
              </div>
              <div className='space-y-1'>
                <Label>失败原因</Label>
                <div className='rounded-md border px-3 py-2 text-sm break-words whitespace-pre-wrap'>
                  {detailRecord.failure_reason || '-'}
                </div>
              </div>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </SettingsSection>
  )
}
