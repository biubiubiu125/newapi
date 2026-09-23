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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Eye, RefreshCw, Send } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { toastUnhandledConsoleError } from '@/lib/handle-server-error'

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
import { api } from '@/lib/api'

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

function formatPushStatus(
  status: string,
  t: (key: string) => string
) {
  const statusMap: Record<string, string> = {
    pending: 'Waiting to push',
    running: 'Pushing',
    succeeded: 'Sent',
    failed: 'Failed',
  }
  return t(statusMap[status] ?? status)
}

function formatPushSource(
  source: string,
  t: (key: string) => string
) {
  if (!source) return t('Manual push')
  const sourceMap: Record<string, string> = {
    auto: 'Automatic push',
    manual: 'Manual push',
  }
  return t(sourceMap[source] ?? source)
}

export function TelegramPushSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [botToken, setBotToken] = useState('')
  const [chatId, setChatId] = useState('')
  const [displayName, setDisplayName] = useState('RKAPI')
  const [testText, setTestText] = useState(() =>
    t('Telegram push test succeeded')
  )
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
    onSuccess: () => toast.success(t('Telegram push settings saved')),
    onError: toastUnhandledConsoleError,
  })

  const testPush = useMutation({
    mutationFn: async () => {
      const res = await api.post('/api/telegram_push/test', { text: testText })
      if (!res.data.success) throw new Error(res.data.message)
    },
    onSuccess: () => toast.success(t('Test push sent')),
    onError: toastUnhandledConsoleError,
  })

  const pushAnnouncement = useMutation({
    mutationFn: async () => {
      if (!title.trim() && !content.trim()) {
        throw new Error(
          t('Announcement title and content cannot both be empty')
        )
      }
      const res = await api.post('/api/telegram_push/announcements', {
        title,
        content,
      })
      if (!res.data.success) throw new Error(res.data.message)
    },
    onSuccess: () => {
      toast.success(t('Announcement push task created'))
      queryClient.invalidateQueries({ queryKey: ['telegram-push-records'] })
    },
    onError: toastUnhandledConsoleError,
  })

  const retryRecord = useMutation({
    mutationFn: async (id: number) => {
      const res = await api.post(`/api/telegram_push/records/${id}/retry`)
      if (!res.data.success) throw new Error(res.data.message)
    },
    onSuccess: () => {
      toast.success(t('Pushed again'))
      queryClient.invalidateQueries({ queryKey: ['telegram-push-records'] })
    },
    onError: toastUnhandledConsoleError,
  })

  return (
    <SettingsSection
      title={t('Telegram Push')}
      description={t(
        'Announcements are sent as original text without redaction. The message prefix uses the project display name.'
      )}
    >
      <div className='grid gap-4 lg:grid-cols-3'>
        <div className='space-y-2'>
          <Label>{t('Project display name')}</Label>
          <Input
            value={displayName}
            maxLength={32}
            placeholder='RKAPI'
            onChange={(e) => setDisplayName(e.target.value)}
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Telegram text prefix, for example [{{name}}] announcement title',
              { name: displayName.trim() || 'RKAPI' }
            )}
          </p>
        </div>
        <div className='space-y-2'>
          <Label>{t('Bot Token')}</Label>
          <Input
            type='password'
            value={botToken}
            autoComplete='off'
            onChange={(e) => setBotToken(e.target.value)}
          />
          <p className='text-muted-foreground text-xs leading-5'>
            {t(
              'Search @BotFather in Telegram, send /newbot to create a bot, then copy the Bot Token. The token is the bot secret: fill it in here only, and never post it in a group or public page.'
            )}
          </p>
        </div>
        <div className='space-y-2'>
          <Label>{t('Chat ID')}</Label>
          <Input value={chatId} onChange={(e) => setChatId(e.target.value)} />
          <p className='text-muted-foreground text-xs leading-5'>
            {t(
              'For private chat, enter the admin Telegram user ID. For a channel, enter the channel ID or @username. For a group, enter the group ID. Private-chat users must message the bot first; the bot must be added to the channel or group with permission to send messages.'
            )}
          </p>
        </div>
      </div>
      <div className='flex flex-wrap gap-2'>
        <Button
          onClick={() => saveSettings.mutate()}
          disabled={saveSettings.isPending}
        >
          {t('Save configuration')}
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
          {t('Test push')}
        </Button>
      </div>
      <div className='grid gap-3'>
        <Label>{t('Manually push announcement')}</Label>
        <Input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder={t('Announcement title')}
        />
        <Textarea
          value={content}
          onChange={(e) => setContent(e.target.value)}
          className='h-40 resize-none'
          placeholder={t('Original announcement content')}
        />
        <Button
          className='w-fit'
          onClick={() => pushAnnouncement.mutate()}
          disabled={pushAnnouncement.isPending}
        >
          {t('Push announcement')}
        </Button>
      </div>
      <div className='flex items-center justify-between'>
        <Label>{t('Push records')}</Label>
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
          {t('Refresh')}
        </Button>
      </div>
      <div className='overflow-x-auto rounded-md border'>
        <table className='w-full min-w-[800px] text-sm'>
          <thead className='bg-muted/50'>
            <tr>
              <th className='px-3 py-2 text-left'>{t('Record')}</th>
              <th className='px-3 py-2 text-left'>{t('Project')}</th>
              <th className='px-3 py-2 text-left'>{t('Method')}</th>
              <th className='px-3 py-2 text-left'>{t('Title')}</th>
              <th className='px-3 py-2 text-left'>{t('Status')}</th>
              <th className='px-3 py-2 text-left'>{t('Count')}</th>
              <th className='px-3 py-2 text-left'>{t('Failure reason')}</th>
              <th className='px-3 py-2 text-left'>{t('Actions')}</th>
            </tr>
          </thead>
          <tbody>
            {records.map((record) => (
              <tr key={record.id} className='border-t'>
                <td className='px-3 py-2'>#{record.id}</td>
                <td className='px-3 py-2'>{record.display_name || '-'}</td>
                <td className='px-3 py-2'>
                  {formatPushSource(record.source, t)}
                </td>
                <td className='max-w-[240px] truncate px-3 py-2'>
                  {record.title || record.content || '-'}
                </td>
                <td className='px-3 py-2'>
                  {formatPushStatus(record.status, t)}
                </td>
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
                      {t('Details')}
                    </Button>
                    {record.status === 'failed' ? (
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => retryRecord.mutate(record.id)}
                      >
                        {t('Retry')}
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
                  {t('No push records')}
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
            <DialogTitle>{t('Push record details')}</DialogTitle>
          </DialogHeader>
          {detailRecord ? (
            <div className='space-y-4'>
              <div className='grid gap-3 sm:grid-cols-2'>
                <div className='space-y-1'>
                  <Label>{t('Record')}</Label>
                  <div className='rounded-md border px-3 py-2 text-sm'>
                    #{detailRecord.id}
                  </div>
                </div>
                <div className='space-y-1'>
                  <Label>{t('Status')}</Label>
                  <div className='rounded-md border px-3 py-2 text-sm'>
                    {formatPushStatus(detailRecord.status, t)}
                  </div>
                </div>
                <div className='space-y-1'>
                  <Label>{t('Push method')}</Label>
                  <div className='rounded-md border px-3 py-2 text-sm'>
                    {formatPushSource(detailRecord.source, t)}
                  </div>
                </div>
                <div className='space-y-1'>
                  <Label>{t('Push count')}</Label>
                  <div className='rounded-md border px-3 py-2 text-sm'>
                    {detailRecord.attempt_count}
                  </div>
                </div>
                <div className='space-y-1'>
                  <Label>{t('Chat ID')}</Label>
                  <div className='rounded-md border px-3 py-2 text-sm break-all'>
                    {detailRecord.chat_id || '-'}
                  </div>
                </div>
                <div className='space-y-1'>
                  <Label>{t('Project display name')}</Label>
                  <div className='rounded-md border px-3 py-2 text-sm break-all'>
                    {detailRecord.display_name || '-'}
                  </div>
                </div>
              </div>
              <div className='space-y-1'>
                <Label>{t('Original title')}</Label>
                <div className='rounded-md border px-3 py-2 text-sm break-words whitespace-pre-wrap'>
                  {detailRecord.title || '-'}
                </div>
              </div>
              <div className='space-y-1'>
                <Label>{t('Original content')}</Label>
                <div className='max-h-80 overflow-y-auto rounded-md border px-3 py-2 text-sm break-words whitespace-pre-wrap'>
                  {detailRecord.content || '-'}
                </div>
              </div>
              <div className='space-y-1'>
                <Label>{t('Failure reason')}</Label>
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
