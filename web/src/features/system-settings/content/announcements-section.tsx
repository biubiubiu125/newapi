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

const announcementSchema = z.object({
  title: z.string().max(100, '标题不能超过 100 个字符').optional(),
  content: z
    .string()
    .min(1, '公告内容不能为空')
    .max(2000, '公告内容不能超过 2000 个字符'),
  publishDate: z.string().min(1, '发布时间不能为空'),
  type: z.enum(['default', 'ongoing', 'success', 'warning', 'error']),
  extra: z.string().max(100, '附加信息不能超过 100 个字符').optional(),
})

type AnnouncementFormValues = z.infer<typeof announcementSchema>

const typeOptions = [
  {
    value: 'default',
    label: '默认',
    color: 'bg-gray-500',
    badgeVariant: 'neutral' as const,
  },
  {
    value: 'ongoing',
    label: '进行中',
    color: 'bg-blue-500',
    badgeVariant: 'info' as const,
  },
  {
    value: 'success',
    label: '成功',
    color: 'bg-green-500',
    badgeVariant: 'success' as const,
  },
  {
    value: 'warning',
    label: '警告',
    color: 'bg-orange-500',
    badgeVariant: 'warning' as const,
  },
  {
    value: 'error',
    label: '错误',
    color: 'bg-red-500',
    badgeVariant: 'danger' as const,
  },
]

export function AnnouncementsSection({
  enabled,
  data,
}: AnnouncementsSectionProps) {
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
      toast.success('设置已保存')
    } catch {
      toast.error('设置保存失败')
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
      toast.error('请选择要删除的公告')
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
      toast.success('公告已删除，点击“保存设置”后生效')
    } else if (deleteTarget === 'batch') {
      setAnnouncements((prev) =>
        prev.filter((item) => !selectedIds.includes(item.id))
      )
      setSelectedIds([])
      setHasChanges(true)
      toast.success(`已删除 ${selectedIds.length} 条公告，点击“保存设置”后生效`)
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
      toast.success('公告已更新，点击“保存设置”后生效')
    } else {
      const newId = Math.max(...announcements.map((item) => item.id), 0) + 1
      setAnnouncements((prev) => [...prev, { id: newId, ...normalizedValues }])
      toast.success('公告已添加，点击“保存设置”后生效')
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
        '公告已保存；如有新增或变更公告，将自动创建 Telegram 推送任务'
      )
    } catch {
      toast.error('公告保存失败')
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

    if (diffMins <= 0) return '刚刚'
    if (diffMins < 60) return `${diffMins} 分钟前`
    if (diffHours < 24) return `${diffHours} 小时前`
    return `${diffDays} 天前`
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
      toast.success('公告推送任务已创建')
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '公告推送失败')
    } finally {
      setTelegramPushingId(null)
    }
  }

  return (
    <SettingsSection title='公告'>
      <div className='space-y-4'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div className='flex flex-wrap items-center gap-2'>
            <Button onClick={handleAdd} size='sm'>
              <Plus className='mr-2 h-4 w-4' />
              新增公告
            </Button>
            <Button
              onClick={handleBatchDelete}
              size='sm'
              variant='destructive'
              disabled={selectedIds.length === 0}
            >
              <Trash2 className='mr-2 h-4 w-4' />
              删除（{selectedIds.length}）
            </Button>
            <Button
              onClick={handleSaveAll}
              size='sm'
              variant='secondary'
              disabled={!hasChanges || updateOption.isPending}
            >
              <Save className='mr-2 h-4 w-4' />
              {updateOption.isPending ? '保存中...' : '保存设置'}
            </Button>
          </div>
          <SettingsSwitchField
            checked={isEnabled}
            onCheckedChange={handleToggleEnabled}
            label='启用公告'
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
                <TableHead>标题</TableHead>
                <TableHead>内容</TableHead>
                <TableHead>发布时间</TableHead>
                <TableHead>类型</TableHead>
                <TableHead>附加信息</TableHead>
                <TableHead className='w-40'>操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {sortedAnnouncements.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className='h-24 text-center'>
                    暂无公告，点击“新增公告”创建
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
                        label={
                          typeOptions.find(
                            (opt) => opt.value === announcement.type
                          )?.label
                        }
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
                          aria-label='推送到 Telegram'
                          title='推送到 Telegram'
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
              {editingAnnouncement ? '编辑公告' : '新增公告'}
            </DialogTitle>
            <DialogDescription>
              创建或更新控制台显示的系统公告
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
                      <FormLabel>标题</FormLabel>
                      <FormControl>
                        <Input placeholder='公告标题（可选）' {...field} />
                      </FormControl>
                      <FormDescription>
                        没有标题时，列表会使用内容前 40 个字作为标题。
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
                      <FormLabel>内容</FormLabel>
                      <FormControl>
                        <Textarea
                          className='h-72 resize-none overflow-y-auto'
                          placeholder='请输入公告内容（支持 Markdown/HTML）'
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        公告内容最多 2000 字，保存原文，不做脱敏。
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
                      <FormLabel>发布时间</FormLabel>
                      <FormControl>
                        <DateTimePicker
                          value={
                            field.value ? new Date(field.value) : undefined
                          }
                          onChange={(date) =>
                            field.onChange(date ? date.toISOString() : '')
                          }
                          placeholder='选择发布时间'
                        />
                      </FormControl>
                      <FormDescription>
                        公告开始显示的日期和时间。
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
                      <FormLabel>类型</FormLabel>
                      <Select
                        items={typeOptions.map((option) => ({
                          value: option.value,
                          label: (
                            <div className='flex items-center gap-2'>
                              <div
                                className={`h-3 w-3 rounded-full ${option.color}`}
                              />
                              {option.label}
                            </div>
                          ),
                        }))}
                        onValueChange={field.onChange}
                        value={field.value}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue placeholder='选择公告类型' />
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
                                  {option.label}
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
                      <FormLabel>附加信息（可选）</FormLabel>
                      <FormControl>
                        <Input placeholder='附加信息' {...field} />
                      </FormControl>
                      <FormDescription>
                        可选补充信息，最多 100 个字符。
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
                  取消
                </Button>
                <Button type='submit'>
                  {editingAnnouncement ? '更新' : '新增'}
                </Button>
              </DialogFooter>
            </form>
          </Form>
        </DialogContent>
      </Dialog>

      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除？</AlertDialogTitle>
            <AlertDialogDescription>
              {deleteTarget === 'single'
                ? '该公告将从列表中移除。'
                : `${selectedIds.length} 条公告将从列表中移除。`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDelete}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
