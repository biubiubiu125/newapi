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
import { Plus, Edit, Trash2, Save, Send } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { DateTimePicker } from '@/components/datetime-picker'
import { StatusBadge } from '@/components/status-badge'
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
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { api } from '@/lib/api'
import dayjs from '@/lib/dayjs'

import { SettingsSwitchField } from '../components/settings-form-layout'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { ANNOUNCEMENT_CONTENT_MAX_CHARS } from './utils'

import { localizeConsoleErrorText } from '@/lib/server-error-message'

type Announcement = {
  id: number
  title?: string
  content: string
  publishDate: string
  type: 'default' | 'ongoing' | 'success' | 'warning' | 'error'
  extra?: string
}

type AnnouncementsSectionProps = {
  enabled: boolean
  data: string
}

function createAnnouncementSchema(
  t: (key: string, options?: Record<string, unknown>) => string
) {
  return z.object({
    title: z.string().max(100, t('Title cannot exceed 100 characters')).optional(),
    content: z
      .string()
      .min(1, t('Announcement content cannot be empty'))
      .max(
        ANNOUNCEMENT_CONTENT_MAX_CHARS,
        t('Announcement content cannot exceed {{count}} characters', {
          count: ANNOUNCEMENT_CONTENT_MAX_CHARS,
        })
      ),
    publishDate: z.string().min(1, t('Publish time is required')),
    type: z.enum(['default', 'ongoing', 'success', 'warning', 'error']),
    extra: z
      .string()
      .max(100, t('Additional information cannot exceed 100 characters'))
      .optional(),
  })
}

type AnnouncementFormValues = z.infer<
  ReturnType<typeof createAnnouncementSchema>
>

const typeOptions = [
  {
    value: 'default',
    label: 'Default',
    color: 'bg-gray-500',
    badgeVariant: 'neutral' as const,
  },
  {
    value: 'ongoing',
    label: 'In Progress',
    color: 'bg-blue-500',
    badgeVariant: 'info' as const,
  },
  {
    value: 'success',
    label: 'Success',
    color: 'bg-green-500',
    badgeVariant: 'success' as const,
  },
  {
    value: 'warning',
    label: 'Warning',
    color: 'bg-orange-500',
    badgeVariant: 'warning' as const,
  },
  {
    value: 'error',
    label: 'Error',
    color: 'bg-red-500',
    badgeVariant: 'danger' as const,
  },
]

export function AnnouncementsSection({
  enabled,
  data,
}: AnnouncementsSectionProps) {
  const { t } = useTranslation()
  const announcementSchema = useMemo(() => createAnnouncementSchema(t), [t])
  const updateOption = useUpdateOption()
  const [announcements, setAnnouncements] = useState<Announcement[]>([])
  const [isEnabled, setIsEnabled] = useState(enabled)
  const [hasChanges, setHasChanges] = useState(false)
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [showDialog, setShowDialog] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [editingAnnouncement, setEditingAnnouncement] =
    useState<Announcement | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<'single' | 'batch'>('single')
  const [telegramPushingId, setTelegramPushingId] = useState<number | null>(
    null
  )

  const form = useForm<AnnouncementFormValues>({
    resolver: zodResolver(announcementSchema),
    defaultValues: {
      title: '',
      content: '',
      publishDate: new Date().toISOString(),
      type: 'default',
      extra: '',
    },
  })

  useEffect(() => {
    try {
      const parsed = JSON.parse(data || '[]')
      if (Array.isArray(parsed)) {
        setAnnouncements(
          parsed.map((item, idx) => {
            if (typeof item === 'string') {
              return {
                id: idx + 1,
                title: '',
                content: item,
                publishDate: new Date().toISOString(),
                type: 'default',
                extra: '',
              }
            }
            return {
              ...item,
              title: typeof item.title === 'string' ? item.title : '',
              content: typeof item.content === 'string' ? item.content : '',
              id: item.id || idx + 1,
            }
          })
        )
      }
    } catch {
      setAnnouncements([])
    }
  }, [data])

  useEffect(() => {
    setIsEnabled(enabled)
  }, [enabled])

  const handleToggleEnabled = async (checked: boolean) => {
    try {
      await updateOption.mutateAsync({
        key: 'console_setting.announcements_enabled',
        value: checked,
      })
      setIsEnabled(checked)
      toast.success(t('Settings saved'))
    } catch {
      toast.error(t('Failed to save settings'))
    }
  }

  const handleAdd = () => {
    setEditingAnnouncement(null)
    form.reset({
      title: '',
      content: '',
      publishDate: new Date().toISOString(),
      type: 'default',
      extra: '',
    })
    setShowDialog(true)
  }

  const handleEdit = (announcement: Announcement) => {
    setEditingAnnouncement(announcement)
    form.reset({
      title: announcement.title || '',
      content: announcement.content,
      publishDate: announcement.publishDate,
      type: announcement.type,
      extra: announcement.extra || '',
    })
    setShowDialog(true)
  }

  const handleDelete = (announcement: Announcement) => {
    setEditingAnnouncement(announcement)
    setDeleteTarget('single')
    setShowDeleteDialog(true)
  }

  const handleBatchDelete = () => {
    if (selectedIds.length === 0) {
      toast.error(t('Please select announcements to delete'))
      return
    }
    setDeleteTarget('batch')
    setShowDeleteDialog(true)
  }

  const confirmDelete = () => {
    if (deleteTarget === 'single' && editingAnnouncement) {
      setAnnouncements((prev) =>
        prev.filter((item) => item.id !== editingAnnouncement.id)
      )
      setHasChanges(true)
      toast.success(t('Announcement deleted. Click "Save Settings" to apply.'))
    } else if (deleteTarget === 'batch') {
      setAnnouncements((prev) =>
        prev.filter((item) => !selectedIds.includes(item.id))
      )
      setSelectedIds([])
      setHasChanges(true)
      toast.success(
        t('Deleted {{count}} announcements. Click "Save Settings" to apply.', {
          count: selectedIds.length,
        })
      )
    }
    setShowDeleteDialog(false)
    setEditingAnnouncement(null)
  }

  const handleSubmitForm = (values: AnnouncementFormValues) => {
    const normalizedValues = {
      ...values,
      title: values.title?.trim() || undefined,
      content: values.content.trim(),
      extra: values.extra?.trim() || undefined,
    }
    if (editingAnnouncement) {
      setAnnouncements((prev) =>
        prev.map((item) =>
          item.id === editingAnnouncement.id
            ? { ...item, ...normalizedValues }
            : item
        )
      )
      toast.success(t('Announcement updated. Click "Save Settings" to apply.'))
    } else {
      const newId = Math.max(...announcements.map((item) => item.id), 0) + 1
      setAnnouncements((prev) => [...prev, { id: newId, ...normalizedValues }])
      toast.success(t('Announcement added. Click "Save Settings" to apply.'))
    }
    setHasChanges(true)
    setShowDialog(false)
  }

  const handleSaveAll = async () => {
    try {
      await updateOption.mutateAsync({
        key: 'console_setting.announcements',
        value: JSON.stringify(announcements),
        skipToast: true,
      })
      setHasChanges(false)
      toast.success(
        t(
          'Announcements saved. New or changed announcements will automatically create Telegram push jobs.'
        )
      )
    } catch {
      toast.error(t('Failed to save announcements'))
    }
  }

  const toggleSelectAll = (checked: boolean) => {
    setSelectedIds(checked ? announcements.map((item) => item.id) : [])
  }

  const toggleSelectOne = (id: number, checked: boolean) => {
    setSelectedIds((prev) =>
      checked ? [...prev, id] : prev.filter((item) => item !== id)
    )
  }

  const sortedAnnouncements = useMemo(() => {
    return [...announcements].sort((a, b) => {
      return (
        new Date(b.publishDate).getTime() - new Date(a.publishDate).getTime()
      )
    })
  }, [announcements])

  const getRelativeTime = (date: string) => {
    const now = new Date()
    const past = new Date(date)
    const diffMs = now.getTime() - past.getTime()
    const diffMins = Math.floor(diffMs / 60000)
    const diffHours = Math.floor(diffMins / 60)
    const diffDays = Math.floor(diffHours / 24)

    if (diffMins <= 0) return t('Just now')
    if (diffMins < 60) return t('{{count}} minutes ago', { count: diffMins })
    if (diffHours < 24) return t('{{count}} hours ago', { count: diffHours })
    return t('{{count}} days ago', { count: diffDays })
  }

  const getAnnouncementTitle = (announcement: Announcement) => {
    const title = announcement.title?.trim()
    if (title) return title
    const content = announcement.content.trim()
    return content.length > 40 ? `${content.slice(0, 40)}...` : content
  }

  const handleTelegramPush = async (announcement: Announcement) => {
    setTelegramPushingId(announcement.id)
    try {
      const res = await api.post('/api/telegram_push/announcements', {
        announcement_id: String(announcement.id),
        title: announcement.title?.trim() ?? '',
        content: announcement.content,
      })
      if (!res.data.success) throw new Error(res.data.message)
      toast.success(t('Announcement push task created'))
    } catch (error) {
      toast.error(
        localizeConsoleErrorText(error instanceof Error ? error.message : '', 'Failed to push announcement')
      )
    } finally {
      setTelegramPushingId(null)
    }
  }

  return (
    <SettingsSection title={t('Announcements')}>
      <div className='space-y-4'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div className='flex flex-wrap items-center gap-2'>
            <Button onClick={handleAdd} size='sm'>
              <Plus className='mr-2 h-4 w-4' />
              {t('Add Announcement')}
            </Button>
            <Button
              onClick={handleBatchDelete}
              size='sm'
              variant='destructive'
              disabled={selectedIds.length === 0}
            >
              <Trash2 className='mr-2 h-4 w-4' />
              {t('Delete ({{count}})', { count: selectedIds.length })}
            </Button>
            <Button
              onClick={handleSaveAll}
              size='sm'
              variant='secondary'
              disabled={!hasChanges || updateOption.isPending}
            >
              <Save className='mr-2 h-4 w-4' />
              {updateOption.isPending ? t('Saving...') : t('Save Settings')}
            </Button>
          </div>
          <SettingsSwitchField
            checked={isEnabled}
            onCheckedChange={handleToggleEnabled}
            label={t('Enable announcements')}
            className='border-b-0 py-0'
          />
        </div>

        <div className='rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className='w-12'>
                  <Checkbox
                    checked={
                      selectedIds.length === announcements.length &&
                      announcements.length > 0
                    }
                    onCheckedChange={toggleSelectAll}
                  />
                </TableHead>
                <TableHead>{t('Title')}</TableHead>
                <TableHead>{t('Content')}</TableHead>
                <TableHead>{t('Publish Date')}</TableHead>
                <TableHead>{t('Type')}</TableHead>
                <TableHead>{t('Additional information')}</TableHead>
                <TableHead className='w-40'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {sortedAnnouncements.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className='h-24 text-center'>
                    {t('No announcements yet. Click "Add Announcement" to create one.')}
                  </TableCell>
                </TableRow>
              ) : (
                sortedAnnouncements.map((announcement) => (
                  <TableRow key={announcement.id}>
                    <TableCell>
                      <Checkbox
                        checked={selectedIds.includes(announcement.id)}
                        onCheckedChange={(checked) =>
                          toggleSelectOne(announcement.id, checked as boolean)
                        }
                      />
                    </TableCell>
                    <TableCell className='max-w-xs font-medium'>
                      <div
                        className='truncate'
                        title={getAnnouncementTitle(announcement)}
                      >
                        {getAnnouncementTitle(announcement)}
                      </div>
                    </TableCell>
                    <TableCell className='max-w-md'>
                      <div
                        className='line-clamp-2'
                        title={announcement.content}
                      >
                        {announcement.content}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className='flex flex-col gap-1'>
                        <span className='text-sm font-medium'>
                          {getRelativeTime(announcement.publishDate)}
                        </span>
                        <span className='text-muted-foreground text-xs'>
                          {dayjs(announcement.publishDate).format(
                            'YYYY-MM-DD HH:mm:ss'
                          )}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <StatusBadge
                        label={t(
                          typeOptions.find(
                            (opt) => opt.value === announcement.type
                          )?.label || 'Default'
                        )}
                        variant={
                          typeOptions.find(
                            (opt) => opt.value === announcement.type
                          )?.badgeVariant ?? 'neutral'
                        }
                        copyable={false}
                      />
                    </TableCell>
                    <TableCell
                      className='text-muted-foreground max-w-xs truncate'
                      title={announcement.extra}
                    >
                      {announcement.extra || '-'}
                    </TableCell>
                    <TableCell>
                      <div className='flex gap-2'>
                        <Button
                          onClick={() => handleTelegramPush(announcement)}
                          size='sm'
                          variant='ghost'
                          disabled={telegramPushingId === announcement.id}
                          aria-label={t('Push to Telegram')}
                          title={t('Push to Telegram')}
                        >
                          <Send className='h-4 w-4' />
                        </Button>
                        <Button
                          onClick={() => handleEdit(announcement)}
                          size='sm'
                          variant='ghost'
                        >
                          <Edit className='h-4 w-4' />
                        </Button>
                        <Button
                          onClick={() => handleDelete(announcement)}
                          size='sm'
                          variant='ghost'
                        >
                          <Trash2 className='h-4 w-4' />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      </div>

      <Dialog open={showDialog} onOpenChange={setShowDialog}>
        <DialogContent className='max-h-[85vh] max-w-4xl overflow-hidden'>
          <DialogHeader>
            <DialogTitle>
              {editingAnnouncement
                ? t('Edit Announcement')
                : t('Add Announcement')}
            </DialogTitle>
            <DialogDescription>
              {t('Create or update system announcements for the dashboard')}
            </DialogDescription>
          </DialogHeader>
          <Form {...form}>
            <form
              onSubmit={form.handleSubmit(handleSubmitForm)}
              className='flex max-h-[calc(85vh-7rem)] flex-col'
            >
              <div className='flex-1 space-y-4 overflow-y-auto pr-1'>
                <FormField
                  control={form.control}
                  name='title'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Title')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t('Announcement title (optional)')}
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'If no title is provided, the list uses the first 40 characters of the content.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='content'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Content')}</FormLabel>
                      <FormControl>
                        <Textarea
                          className='h-72 resize-none overflow-y-auto'
                          placeholder={t(
                            'Enter announcement content (supports Markdown/HTML)'
                          )}
                          {...field}
                          maxLength={ANNOUNCEMENT_CONTENT_MAX_CHARS}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          '{{used}}/{{max}}, saved as original text without redaction.',
                          {
                            used: field.value?.length ?? 0,
                            max: ANNOUNCEMENT_CONTENT_MAX_CHARS,
                          }
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='publishDate'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Publish Date')}</FormLabel>
                      <FormControl>
                        <DateTimePicker
                          value={
                            field.value ? new Date(field.value) : undefined
                          }
                          onChange={(date) =>
                            field.onChange(date ? date.toISOString() : '')
                          }
                          placeholder={t('Select publish date')}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Date and time when this announcement should be displayed')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='type'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Type')}</FormLabel>
                      <Select
                        items={typeOptions.map((option) => ({
                          value: option.value,
                          label: (
                            <div className='flex items-center gap-2'>
                              <div
                                className={`h-3 w-3 rounded-full ${option.color}`}
                              />
                              {t(option.label)}
                            </div>
                          ),
                        }))}
                        onValueChange={field.onChange}
                        value={field.value}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue placeholder={t('Select announcement type')} />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            {typeOptions.map((option) => (
                              <SelectItem
                                key={option.value}
                                value={option.value}
                              >
                                <div className='flex items-center gap-2'>
                                  <div
                                    className={`h-3 w-3 rounded-full ${option.color}`}
                                  />
                                  {t(option.label)}
                                </div>
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='extra'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Additional information (optional)')}
                      </FormLabel>
                      <FormControl>
                        <Input placeholder={t('Additional information')} {...field} />
                      </FormControl>
                      <FormDescription>
                        {t('Optional additional information, up to 100 characters.')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
              <DialogFooter className='bg-background sticky bottom-0 mt-4 border-t pt-4'>
                <Button
                  type='button'
                  variant='outline'
                  onClick={() => setShowDialog(false)}
                >
                  {t('Cancel')}
                </Button>
                <Button type='submit'>
                  {editingAnnouncement ? t('Update') : t('Add')}
                </Button>
              </DialogFooter>
            </form>
          </Form>
        </DialogContent>
      </Dialog>

      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Confirm delete')}</AlertDialogTitle>
            <AlertDialogDescription>
              {deleteTarget === 'single'
                ? t('This announcement will be removed from the list.')
                : t('{{count}} announcements will be removed from the list.', {
                    count: selectedIds.length,
                  })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDelete}>
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
