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
import { Bell, Megaphone } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNotificationStore } from '@/stores/notification-store'
import { getAnnouncementColorClass } from '@/lib/colors'
import { getAnnouncementKey } from '@/lib/notifications'
import { formatDateTimeObject } from '@/lib/time'
import { cn } from '@/lib/utils'
import { useNotifications } from '@/hooks/use-notifications'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Markdown } from '@/components/ui/markdown'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

type AnnouncementItem = {
  id?: string | number
  type?: string
  content?: string
  extra?: string
  publishDate?: string | Date
  title?: string
  link?: string
}

function toAnnouncementItem(
  item: Record<string, unknown> | string
): AnnouncementItem {
  if (typeof item === 'string') {
    return { content: item }
  }

  return {
    id:
      typeof item.id === 'string' || typeof item.id === 'number'
        ? item.id
        : undefined,
    type: typeof item.type === 'string' ? item.type : undefined,
    content: typeof item.content === 'string' ? item.content : undefined,
    extra: typeof item.extra === 'string' ? item.extra : undefined,
    publishDate:
      typeof item.publishDate === 'string' || item.publishDate instanceof Date
        ? item.publishDate
        : undefined,
    title: typeof item.title === 'string' ? item.title : undefined,
    link: typeof item.link === 'string' ? item.link : undefined,
  }
}

function AnnouncementDot(props: { type?: string }) {
  return (
    <span
      className={cn(
        'mt-1.5 inline-block size-2 shrink-0 rounded-full',
        getAnnouncementColorClass(props.type)
      )}
    />
  )
}

function NoticePanel(props: { notice: string; loading: boolean }) {
  const { t } = useTranslation()

  if (props.loading) {
    return (
      <div className='text-muted-foreground flex min-h-48 items-center justify-center text-sm'>
        {t('Loading...')}
      </div>
    )
  }

  if (!props.notice) {
    return (
      <div className='text-muted-foreground flex min-h-48 items-center justify-center text-sm'>
        {t('No announcements at this time')}
      </div>
    )
  }

  return (
    <ScrollArea className='h-[min(55vh,28rem)] pr-3'>
      <Markdown>{props.notice}</Markdown>
    </ScrollArea>
  )
}

function AnnouncementsPanel(props: {
  announcements: AnnouncementItem[]
  loading: boolean
}) {
  const { t } = useTranslation()

  if (props.loading) {
    return (
      <div className='text-muted-foreground flex min-h-48 items-center justify-center text-sm'>
        {t('Loading...')}
      </div>
    )
  }

  if (props.announcements.length === 0) {
    return (
      <div className='text-muted-foreground flex min-h-48 items-center justify-center text-sm'>
        {t('No system announcements')}
      </div>
    )
  }

  return (
    <ScrollArea className='h-[min(55vh,28rem)] pr-3'>
      <div className='flex flex-col'>
        {props.announcements.map((item, idx) => {
          const publishDate = item.publishDate
            ? new Date(item.publishDate)
            : null
          const absoluteTime =
            publishDate && !Number.isNaN(publishDate.getTime())
              ? formatDateTimeObject(publishDate)
              : ''

          return (
            <div key={`${absoluteTime}-${idx}`}>
              <div className='py-3'>
                <div className='flex items-start gap-3'>
                  <AnnouncementDot type={item.type} />
                  <div className='flex min-w-0 flex-1 flex-col gap-2'>
                    {item.title ? (
                      <div className='text-sm font-medium'>{item.title}</div>
                    ) : null}
                    <div className='text-sm'>
                      <Markdown>{item.content || ''}</Markdown>
                    </div>
                    {item.extra ? (
                      <div className='text-muted-foreground text-xs'>
                        <Markdown>{item.extra}</Markdown>
                      </div>
                    ) : null}
                    {absoluteTime ? (
                      <div className='text-muted-foreground text-xs'>
                        {absoluteTime}
                      </div>
                    ) : null}
                  </div>
                </div>
              </div>
              {idx < props.announcements.length - 1 ? <Separator /> : null}
            </div>
          )
        })}
      </div>
    </ScrollArea>
  )
}

export function HomeNoticeDialog() {
  const { t } = useTranslation()
  const notifications = useNotifications()
  const {
    isNoticeClosed,
    setClosedUntilDate,
    markNoticeRead,
    markAnnouncementsRead,
  } = useNotificationStore()
  const [open, setOpen] = useState(false)
  const [autoOpened, setAutoOpened] = useState(false)
  const [activeTab, setActiveTab] = useState<'notice' | 'announcements'>(
    'notice'
  )
  const announcements = notifications.announcements.map(toAnnouncementItem)

  useEffect(() => {
    if (autoOpened) return
    if (!notifications.notice || isNoticeClosed()) return

    setActiveTab('notice')
    setOpen(true)
    setAutoOpened(true)
  }, [autoOpened, isNoticeClosed, notifications.notice])

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen && notifications.notice) {
      markNoticeRead(notifications.notice)
    }
    setOpen(nextOpen)
  }

  const handleCloseToday = () => {
    setClosedUntilDate(new Date().toDateString())
    handleOpenChange(false)
  }

  const handleTabChange = (value: string) => {
    const nextTab = value === 'announcements' ? 'announcements' : 'notice'
    setActiveTab(nextTab)
    if (nextTab !== 'announcements') return
    const keys = announcements.map((item) => getAnnouncementKey(item))
    markAnnouncementsRead(keys)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className='max-w-[calc(100%-2rem)] sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{t('System Notice')}</DialogTitle>
          <DialogDescription>
            {t('Latest platform updates and notices')}
          </DialogDescription>
        </DialogHeader>

        <Tabs value={activeTab} onValueChange={handleTabChange}>
          <TabsList className='grid w-full grid-cols-2'>
            <TabsTrigger value='notice' className='gap-1.5'>
              <Bell className='size-3.5' />
              {t('System Notice')}
            </TabsTrigger>
            <TabsTrigger value='announcements' className='gap-1.5'>
              <Megaphone className='size-3.5' />
              {t('Announcements')}
            </TabsTrigger>
          </TabsList>

          <TabsContent value='notice' className='mt-2'>
            <NoticePanel
              notice={notifications.notice}
              loading={!notifications.notice && notifications.loading}
            />
          </TabsContent>

          <TabsContent value='announcements' className='mt-2'>
            <AnnouncementsPanel
              announcements={announcements}
              loading={notifications.loading}
            />
          </TabsContent>
        </Tabs>

        <DialogFooter>
          <Button type='button' variant='outline' onClick={handleCloseToday}>
            {t('Close Today')}
          </Button>
          <Button type='button' onClick={() => handleOpenChange(false)}>
            {t('Close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
